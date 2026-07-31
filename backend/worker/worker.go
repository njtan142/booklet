// Package worker runs queued tool jobs.
//
// It is the same binary as the API server, selected with ROLE=worker, so both
// share the database, storage and tool registry without a second image.
//
// The loop is deliberately poll-based rather than LISTEN/NOTIFY: a notification
// is lost if no worker is connected when it fires, which would strand a job
// until the next unrelated wakeup. Polling costs one indexed query every two
// seconds and recovers on its own after any outage.
package worker

import (
	"context"
	"fmt"
	"log"
	"os"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	"booklet/jobs"
	"booklet/logger"
	"booklet/tools"

	"github.com/google/uuid"
)

const (
	defaultConcurrency      = 2
	defaultPollInterval     = 2 * time.Second
	defaultHeartbeatEvery   = 30 * time.Second
	defaultStaleTimeout     = 5 * time.Minute
	defaultReaperInterval   = time.Minute
	shutdownDrainTimeout    = 30 * time.Second
	claimBackoffOnDBFailure = 5 * time.Second
)

// Config is resolved from the environment at startup.
type Config struct {
	// WorkerID identifies this process in jobs.locked_by. It must be unique per
	// process: two workers sharing an id would heartbeat each other's jobs and
	// defeat the reaper.
	WorkerID          string
	Concurrency       int
	PollInterval      time.Duration
	HeartbeatInterval time.Duration
	StaleTimeout      time.Duration
	ReaperInterval    time.Duration
}

// LoadConfig reads worker settings from the environment, falling back to
// defaults that are safe on a single small host.
func LoadConfig() Config {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "worker"
	}

	return Config{
		// The uuid suffix keeps ids unique when several replicas share a
		// hostname, as they do under a single compose service scaled up.
		WorkerID:          fmt.Sprintf("%s-%s", hostname, uuid.New().String()[:8]),
		Concurrency:       envInt("WORKER_CONCURRENCY", defaultConcurrency),
		PollInterval:      envDuration("JOB_POLL_INTERVAL", defaultPollInterval),
		HeartbeatInterval: envDuration("JOB_HEARTBEAT_INTERVAL", defaultHeartbeatEvery),
		StaleTimeout:      envDuration("JOB_STALE_TIMEOUT", defaultStaleTimeout),
		ReaperInterval:    envDuration("JOB_REAPER_INTERVAL", defaultReaperInterval),
	}
}

// Run starts the claim loop, the heartbeat and the reaper, and blocks until ctx
// is cancelled. On cancellation it stops claiming and waits for in-flight jobs
// to finish, so a deploy does not strand jobs in 'running' for the reaper to
// clean up.
func Run(ctx context.Context) error {
	cfg := LoadConfig()
	if cfg.Concurrency < 1 {
		cfg.Concurrency = 1
	}

	log.Printf("Worker %s starting: concurrency=%d poll=%s heartbeat=%s stale=%s",
		cfg.WorkerID, cfg.Concurrency, cfg.PollInterval, cfg.HeartbeatInterval, cfg.StaleTimeout)
	log.Printf("Worker %s registered tools: %d", cfg.WorkerID, len(tools.List()))

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		runHeartbeat(ctx, cfg)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		runReaper(ctx, cfg)
	}()

	// Bounded by a semaphore rather than an unbounded goroutine spawn: tool
	// engines are memory-hungry, and an unbounded worker would OOM the container
	// the moment a user queues fifty conversions.
	sem := make(chan struct{}, cfg.Concurrency)
	var jobsWG sync.WaitGroup

	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

claimLoop:
	for {
		select {
		case <-ctx.Done():
			break claimLoop
		default:
		}

		// Acquire a slot before claiming. Claiming first would mark a job
		// 'running' while it sits waiting for a slot, inflating its runtime and
		// risking a reap before it ever starts.
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break claimLoop
		}

		job, err := jobs.Claim(ctx, cfg.WorkerID)
		if err != nil {
			<-sem
			if ctx.Err() != nil {
				break claimLoop
			}
			log.Printf("Worker %s: failed to claim a job: %v", cfg.WorkerID, err)
			if !sleepCtx(ctx, claimBackoffOnDBFailure) {
				break claimLoop
			}
			continue
		}

		if job == nil {
			<-sem
			select {
			case <-ticker.C:
			case <-ctx.Done():
				break claimLoop
			}
			continue
		}

		jobsWG.Add(1)
		go func(j *jobs.Job) {
			defer jobsWG.Done()
			defer func() { <-sem }()
			execute(cfg, j)
		}(job)
	}

	log.Printf("Worker %s: shutting down, waiting for in-flight jobs...", cfg.WorkerID)

	done := make(chan struct{})
	go func() {
		jobsWG.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Printf("Worker %s: all in-flight jobs finished", cfg.WorkerID)
	case <-time.After(shutdownDrainTimeout):
		// The reaper on another worker requeues whatever is abandoned here.
		log.Printf("Worker %s: drain timed out after %s; abandoning in-flight jobs to the reaper",
			cfg.WorkerID, shutdownDrainTimeout)
	}

	wg.Wait()
	return nil
}

// execute runs one job to a terminal state.
//
// The job context is deliberately context.Background rather than the loop's
// ctx: on shutdown the in-flight job should be allowed to finish and report its
// own result during the drain window, not be cancelled mid-write and left for
// the reaper.
func execute(cfg Config, job *jobs.Job) {
	ctx := context.Background()
	rl := logger.NewRequestLogger()
	ctx = logger.WithLogger(ctx, rl)

	start := time.Now()
	success := false
	defer func() {
		rl.PrintTask(fmt.Sprintf("job %s (%s)", job.ID, job.ToolSlug), time.Since(start), success)
	}()

	rl.Logf("Worker %s claimed job %s: tool=%s attempt=%d/%d inputs=%d",
		cfg.WorkerID, job.ID, job.ToolSlug, job.Attempt, job.MaxAttempts, len(job.InputDocumentIDs))

	tool, ok := tools.Get(job.ToolSlug)
	if !ok || tool.Run == nil {
		// Retrying cannot help: the slug will still be unknown next time.
		failJob(ctx, rl, job, fmt.Errorf("unknown or unimplemented tool %q", job.ToolSlug), true)
		return
	}

	reporter := jobs.NewReporter(ctx, job.ID, len(job.InputDocumentIDs))

	err := runGuarded(ctx, tool, job, reporter)
	if err != nil {
		failJob(ctx, rl, job, err, jobs.IsPermanent(err))
		return
	}

	if err := jobs.Complete(ctx, job.ID); err != nil {
		rl.Logf("Error: job %s finished but could not be marked completed: %v", job.ID, err)
		return
	}

	success = true
	rl.Logf("Job %s completed in %s", job.ID, time.Since(start))
}

// runGuarded converts a panic in tool code into an error, so one malformed PDF
// cannot take the worker process down and strand every other running job.
func runGuarded(ctx context.Context, tool *tools.Tool, job *jobs.Job, reporter *jobs.Reporter) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("Job %s (%s) panicked: %v\n%s", job.ID, job.ToolSlug, rec, debug.Stack())
			err = fmt.Errorf("tool %s panicked: %v", job.ToolSlug, rec)
		}
	}()

	return tool.Run(ctx, job, reporter)
}

func failJob(ctx context.Context, rl *logger.RequestLogger, job *jobs.Job, cause error, permanent bool) {
	status, err := jobs.Fail(ctx, job.ID, cause.Error(), permanent)
	if err != nil {
		rl.Logf("Error: job %s failed (%v) and the failure could not be recorded: %v", job.ID, cause, err)
		return
	}

	if status == jobs.StatusQueued {
		rl.Logf("Job %s failed on attempt %d/%d (%v); requeued with backoff",
			job.ID, job.Attempt, job.MaxAttempts, cause)
		return
	}

	reason := "no attempts remain"
	if permanent {
		reason = "permanent failure, not retried"
	}
	rl.Logf("Job %s failed permanently after attempt %d/%d (%v): %s",
		job.ID, job.Attempt, job.MaxAttempts, cause, reason)
}

// runHeartbeat keeps this worker's running jobs alive. One statement covers all
// of them, so the cost does not grow with concurrency.
func runHeartbeat(ctx context.Context, cfg Config) {
	ticker := time.NewTicker(cfg.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := jobs.Heartbeat(ctx, cfg.WorkerID); err != nil && ctx.Err() == nil {
				// Not fatal on its own; it only matters if it keeps failing for
				// longer than StaleTimeout, at which point the reaper correctly
				// concludes this worker is gone.
				log.Printf("Worker %s: heartbeat failed: %v", cfg.WorkerID, err)
			}
		}
	}
}

// runReaper returns jobs abandoned by dead workers to the queue.
//
// Every worker runs one. They are safe to overlap: the update is conditional on
// the stale heartbeat, so a second reaper finds nothing left to do.
func runReaper(ctx context.Context, cfg Config) {
	ticker := time.NewTicker(cfg.ReaperInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			requeued, failed, err := jobs.ReapStale(ctx, cfg.StaleTimeout)
			if err != nil {
				if ctx.Err() == nil {
					log.Printf("Worker %s: reaper failed: %v", cfg.WorkerID, err)
				}
				continue
			}
			if requeued > 0 || failed > 0 {
				log.Printf("Worker %s: reaper requeued %d and failed %d stale job(s)",
					cfg.WorkerID, requeued, failed)
			}
		}
	}
}

// sleepCtx waits for d, returning false if ctx is cancelled first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
		log.Printf("Warning: %s=%q is not a positive integer; using %d", key, os.Getenv(key), fallback)
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
		log.Printf("Warning: %s=%q is not a valid duration; using %s", key, os.Getenv(key), fallback)
	}
	return fallback
}

// Package jobs is the data layer for the Postgres-backed tool queue.
//
// It exists because the document pipeline's in-process goroutines cannot
// survive a restart, which conflicts with the rule that all state lives in
// Postgres and MinIO. Tool execution is therefore queued: a job row is the
// durable record, and any worker can pick it up after a crash.
//
// Claiming uses FOR UPDATE SKIP LOCKED so several workers can poll the same
// table without blocking each other or handing the same job to two workers.
package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"booklet/db"

	"github.com/google/uuid"
)

// Job status values. A job is queued until a worker claims it, running while
// held, and then terminal in completed or failed.
const (
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
)

// maxBackoffSeconds caps the retry delay. Without a cap, attempt 10 of a job
// with a raised max_attempts would sleep for hours.
const maxBackoffSeconds = 300

// ErrNotFound is returned when no job row matches the id.
var ErrNotFound = errors.New("job not found")

// Job is one queued tool invocation.
type Job struct {
	ID              string          `json:"id"`
	UserID          string          `json:"user_id"`
	ToolSlug        string          `json:"tool_slug"`
	Status          string          `json:"status"`
	Params          json.RawMessage `json:"params"`
	ProgressCurrent int             `json:"progress_current"`
	ProgressTotal   int             `json:"progress_total"`
	ProgressStep    string          `json:"progress_step,omitempty"`
	Error           string          `json:"error,omitempty"`
	Attempt         int             `json:"attempt"`
	MaxAttempts     int             `json:"max_attempts"`
	CreatedAt       time.Time       `json:"created_at"`
	StartedAt       *time.Time      `json:"started_at,omitempty"`
	CompletedAt     *time.Time      `json:"completed_at,omitempty"`

	// InputDocumentIDs and OutputDocumentIDs are ordered by position, which is
	// what makes Merge input order and Split output order meaningful.
	InputDocumentIDs  []string `json:"input_document_ids"`
	OutputDocumentIDs []string `json:"output_document_ids"`
}

// PermanentError marks a failure that must not be retried. Validation and
// permission failures are permanent: re-running them burns attempts and delays
// the terminal state the caller is polling for, without ever succeeding.
type PermanentError struct{ Err error }

func (e *PermanentError) Error() string { return e.Err.Error() }
func (e *PermanentError) Unwrap() error { return e.Err }

// Permanent wraps err so the worker fails the job immediately.
func Permanent(err error) error { return &PermanentError{Err: err} }

// IsPermanent reports whether err should skip the retry path.
func IsPermanent(err error) bool {
	var p *PermanentError
	return errors.As(err, &p)
}

// Enqueue writes a job and its ordered inputs in one transaction, so a job can
// never be claimed with a partially written input list.
func Enqueue(ctx context.Context, userID, toolSlug string, params json.RawMessage, inputDocumentIDs []string) (string, error) {
	if len(params) == 0 {
		params = json.RawMessage(`{}`)
	}

	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	jobID := uuid.New().String()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO jobs (id, user_id, tool_slug, status, params)
		VALUES ($1, $2, $3, $4, $5)`,
		jobID, userID, toolSlug, StatusQueued, []byte(params)); err != nil {
		return "", fmt.Errorf("failed to insert job: %w", err)
	}

	for i, docID := range inputDocumentIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO job_inputs (job_id, document_id, position)
			VALUES ($1, $2, $3)`, jobID, docID, i); err != nil {
			return "", fmt.Errorf("failed to insert job input %d: %w", i, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}
	return jobID, nil
}

// Claim atomically takes the oldest runnable job for this worker.
//
// SKIP LOCKED is what makes several workers safe on one table: a row already
// locked by another worker's claim is passed over instead of blocking. The
// attempt counter is incremented here, at claim time, so a worker that dies
// without reporting anything still consumes an attempt and cannot loop forever.
//
// Returns nil, nil when the queue is empty.
func Claim(ctx context.Context, workerID string) (*Job, error) {
	var j Job
	var params []byte
	var step sql.NullString
	var startedAt sql.NullTime

	err := db.DB.QueryRowContext(ctx, `
		UPDATE jobs
		SET status = $2, started_at = NOW(), heartbeat_at = NOW(),
		    attempt = attempt + 1, locked_by = $1
		WHERE id = (
			SELECT id FROM jobs
			WHERE status = $3 AND run_after <= NOW()
			ORDER BY created_at
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id::text, user_id, tool_slug, status, params, attempt, max_attempts,
		          progress_current, progress_total, progress_step, created_at, started_at`,
		workerID, StatusRunning, StatusQueued).
		Scan(&j.ID, &j.UserID, &j.ToolSlug, &j.Status, &params, &j.Attempt, &j.MaxAttempts,
			&j.ProgressCurrent, &j.ProgressTotal, &step, &j.CreatedAt, &startedAt)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	j.Params = json.RawMessage(params)
	j.ProgressStep = step.String
	if startedAt.Valid {
		t := startedAt.Time
		j.StartedAt = &t
	}

	if j.InputDocumentIDs, err = documentIDs(ctx, `
		SELECT document_id::text FROM job_inputs WHERE job_id = $1 ORDER BY position`, j.ID); err != nil {
		return nil, fmt.Errorf("failed to load inputs for job %s: %w", j.ID, err)
	}

	return &j, nil
}

// Get returns a job with its inputs and outputs. Callers must check UserID
// themselves: jobs are not documents and carry no mode bits.
func Get(ctx context.Context, jobID string) (*Job, error) {
	var j Job
	var params []byte
	var step, jobErr sql.NullString
	var startedAt, completedAt sql.NullTime

	err := db.DB.QueryRowContext(ctx, `
		SELECT id::text, user_id, tool_slug, status, params, progress_current, progress_total,
		       progress_step, error, attempt, max_attempts, created_at, started_at, completed_at
		FROM jobs WHERE id = $1`, jobID).
		Scan(&j.ID, &j.UserID, &j.ToolSlug, &j.Status, &params, &j.ProgressCurrent, &j.ProgressTotal,
			&step, &jobErr, &j.Attempt, &j.MaxAttempts, &j.CreatedAt, &startedAt, &completedAt)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	j.Params = json.RawMessage(params)
	j.ProgressStep = step.String
	j.Error = jobErr.String
	if startedAt.Valid {
		t := startedAt.Time
		j.StartedAt = &t
	}
	if completedAt.Valid {
		t := completedAt.Time
		j.CompletedAt = &t
	}

	if j.InputDocumentIDs, err = documentIDs(ctx,
		`SELECT document_id::text FROM job_inputs WHERE job_id = $1 ORDER BY position`, jobID); err != nil {
		return nil, err
	}
	if j.OutputDocumentIDs, err = documentIDs(ctx,
		`SELECT document_id::text FROM job_outputs WHERE job_id = $1 ORDER BY position`, jobID); err != nil {
		return nil, err
	}

	return &j, nil
}

// ListByUser returns the caller's jobs, newest first.
func ListByUser(ctx context.Context, userID string, limit int) ([]Job, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	rows, err := db.DB.QueryContext(ctx, `
		SELECT id::text, user_id, tool_slug, status, params, progress_current, progress_total,
		       progress_step, error, attempt, max_attempts, created_at, started_at, completed_at
		FROM jobs WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	jobs := []Job{}
	for rows.Next() {
		var j Job
		var params []byte
		var step, jobErr sql.NullString
		var startedAt, completedAt sql.NullTime

		if err := rows.Scan(&j.ID, &j.UserID, &j.ToolSlug, &j.Status, &params, &j.ProgressCurrent,
			&j.ProgressTotal, &step, &jobErr, &j.Attempt, &j.MaxAttempts, &j.CreatedAt,
			&startedAt, &completedAt); err != nil {
			return nil, err
		}

		j.Params = json.RawMessage(params)
		j.ProgressStep = step.String
		j.Error = jobErr.String
		if startedAt.Valid {
			t := startedAt.Time
			j.StartedAt = &t
		}
		if completedAt.Valid {
			t := completedAt.Time
			j.CompletedAt = &t
		}
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Outputs are fetched per job rather than in one join, because the list is
	// capped at 200 rows and a join would duplicate every job row per output.
	for i := range jobs {
		ids, err := documentIDs(ctx,
			`SELECT document_id::text FROM job_outputs WHERE job_id = $1 ORDER BY position`, jobs[i].ID)
		if err != nil {
			return nil, err
		}
		jobs[i].OutputDocumentIDs = ids
	}

	return jobs, nil
}

// UpdateProgress records tool progress. The heartbeat is refreshed at the same
// time, so a tool reporting progress can never be reaped as stale.
func UpdateProgress(ctx context.Context, jobID string, current, total int, step string) error {
	_, err := db.DB.ExecContext(ctx, `
		UPDATE jobs
		SET progress_current = $2, progress_total = $3, progress_step = $4, heartbeat_at = NOW()
		WHERE id = $1`, jobID, current, total, step)
	return err
}

// Heartbeat refreshes every running job held by this worker in one statement.
func Heartbeat(ctx context.Context, workerID string) error {
	_, err := db.DB.ExecContext(ctx, `
		UPDATE jobs SET heartbeat_at = NOW()
		WHERE locked_by = $1 AND status = $2`, workerID, StatusRunning)
	return err
}

// AddOutput links a produced document to the job at the given position.
//
// The position matters: job_outputs has UNIQUE (job_id, position), so an
// ordered multi-output tool that forgets to assign positions fails here on the
// second row instead of silently losing page order.
func AddOutput(ctx context.Context, jobID, documentID string, position int) error {
	_, err := db.DB.ExecContext(ctx, `
		INSERT INTO job_outputs (job_id, document_id, position)
		VALUES ($1, $2, $3)`, jobID, documentID, position)
	return err
}

// Complete marks a job finished and clears the worker lock.
func Complete(ctx context.Context, jobID string) error {
	_, err := db.DB.ExecContext(ctx, `
		UPDATE jobs
		SET status = $2, completed_at = NOW(), locked_by = NULL, heartbeat_at = NULL, error = NULL
		WHERE id = $1`, jobID, StatusCompleted)
	return err
}

// Fail records an error and decides between a retry and a terminal failure.
//
// The decision is made in SQL against the current attempt count rather than in
// Go, so two workers racing on the same job cannot both conclude there is an
// attempt left. Returns the resulting status.
func Fail(ctx context.Context, jobID, message string, permanent bool) (string, error) {
	var status string
	err := db.DB.QueryRowContext(ctx, `
		UPDATE jobs
		SET status = CASE WHEN $3 OR attempt >= max_attempts THEN $4 ELSE $5 END,
		    error = $2,
		    completed_at = CASE WHEN $3 OR attempt >= max_attempts THEN NOW() ELSE NULL END,
		    started_at = CASE WHEN $3 OR attempt >= max_attempts THEN started_at ELSE NULL END,
		    locked_by = NULL,
		    heartbeat_at = NULL,
		    run_after = CASE WHEN $3 OR attempt >= max_attempts THEN run_after
		                     ELSE NOW() + make_interval(secs => LEAST($6, 10 * POWER(2, attempt))) END
		WHERE id = $1
		RETURNING status`,
		jobID, message, permanent, StatusFailed, StatusQueued, maxBackoffSeconds).Scan(&status)

	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return status, err
}

// ReapStale requeues or fails jobs whose worker stopped heartbeating.
//
// This is what makes a worker crash recoverable: the job row stays 'running'
// with a frozen heartbeat until the reaper returns it to the queue. Jobs that
// have exhausted their attempts are failed instead, so a job that reliably
// kills its worker cannot cycle forever.
func ReapStale(ctx context.Context, staleAfter time.Duration) (requeued, failed int, err error) {
	seconds := staleAfter.Seconds()

	res, err := db.DB.ExecContext(ctx, `
		UPDATE jobs
		SET status = $1, locked_by = NULL, heartbeat_at = NULL, started_at = NULL,
		    error = 'worker stopped responding; requeued',
		    run_after = NOW() + make_interval(secs => LEAST($3, 10 * POWER(2, attempt)))
		WHERE status = $2
		  AND heartbeat_at < NOW() - make_interval(secs => $4)
		  AND attempt < max_attempts`,
		StatusQueued, StatusRunning, maxBackoffSeconds, seconds)
	if err != nil {
		return 0, 0, err
	}
	n, _ := res.RowsAffected()
	requeued = int(n)

	res, err = db.DB.ExecContext(ctx, `
		UPDATE jobs
		SET status = $1, completed_at = NOW(), locked_by = NULL, heartbeat_at = NULL,
		    error = 'worker stopped responding and no attempts remain'
		WHERE status = $2
		  AND heartbeat_at < NOW() - make_interval(secs => $3)
		  AND attempt >= max_attempts`,
		StatusFailed, StatusRunning, seconds)
	if err != nil {
		return requeued, 0, err
	}
	n, _ = res.RowsAffected()
	failed = int(n)

	return requeued, failed, nil
}

// QueueDepth returns the number of jobs waiting to be claimed.
func QueueDepth(ctx context.Context) (int, error) {
	var n int
	err := db.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM jobs WHERE status = $1`, StatusQueued).Scan(&n)
	return n, err
}

// Reporter lets a tool publish progress without holding a database handle or
// knowing its own job id.
type Reporter struct {
	ctx   context.Context
	jobID string
	total int
}

// NewReporter binds a reporter to a job. total is the expected unit count,
// typically the page or input count.
func NewReporter(ctx context.Context, jobID string, total int) *Reporter {
	return &Reporter{ctx: ctx, jobID: jobID, total: total}
}

// SetTotal adjusts the denominator once a tool knows the real unit count, e.g.
// after opening a PDF and reading its page count.
func (r *Reporter) SetTotal(total int) { r.total = total }

// Progress publishes a step. Failures are non-fatal: losing a progress update
// must never abort work that is otherwise succeeding.
func (r *Reporter) Progress(current int, step string) {
	if r == nil {
		return
	}
	_ = UpdateProgress(r.ctx, r.jobID, current, r.total, step)
}

func documentIDs(ctx context.Context, query, jobID string) ([]string, error) {
	rows, err := db.DB.QueryContext(ctx, query, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

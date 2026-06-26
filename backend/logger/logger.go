package logger

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

type LogEntry struct {
	Timestamp time.Time
	Message   string
}

type RequestLogger struct {
	mu      sync.Mutex
	entries []LogEntry
}

type ctxKey struct{}

// NewRequestLogger creates a new logger instance for a request or task
func NewRequestLogger() *RequestLogger {
	return &RequestLogger{
		entries: make([]LogEntry, 0, 16),
	}
}

// FromContext retrieves the RequestLogger from context
func FromContext(ctx context.Context) *RequestLogger {
	if ctx == nil {
		return nil
	}
	rl, _ := ctx.Value(ctxKey{}).(*RequestLogger)
	return rl
}

// WithLogger injects a RequestLogger into context
func WithLogger(ctx context.Context, rl *RequestLogger) context.Context {
	return context.WithValue(ctx, ctxKey{}, rl)
}

// Logf records a log message. It logs to the RequestLogger in context if present,
// otherwise it falls back to standard log.Printf immediately.
func Logf(ctx context.Context, format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	rl := FromContext(ctx)
	if rl == nil {
		log.Println(msg)
		return
	}
	rl.mu.Lock()
	rl.entries = append(rl.entries, LogEntry{
		Timestamp: time.Now(),
		Message:   msg,
	})
	rl.mu.Unlock()
	log.Println(msg)
}

// Logf directly records a log message on the RequestLogger instance
func (rl *RequestLogger) Logf(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	if rl == nil {
		log.Println(msg)
		return
	}
	rl.mu.Lock()
	rl.entries = append(rl.entries, LogEntry{
		Timestamp: time.Now(),
		Message:   msg,
	})
	rl.mu.Unlock()
	log.Println(msg)
}

// Print logs all accumulated entries in one go
func (rl *RequestLogger) Print(method, path, remoteAddr string, statusCode int, duration time.Duration) {
	if rl == nil {
		return
	}
	rl.mu.Lock()
	entries := rl.entries
	rl.mu.Unlock()

	var out strings.Builder
	out.WriteString(fmt.Sprintf("\n=== REQUEST DIAGNOSTICS: %s %s from %s | Status: %d | Duration: %v ===\n", method, path, remoteAddr, statusCode, duration))
	for _, entry := range entries {
		out.WriteString(fmt.Sprintf("[%s] %s\n", entry.Timestamp.Format("2006-01-02 15:04:05.000"), entry.Message))
	}
	out.WriteString("=========================================================================")
	log.Println(out.String())
}

// PrintTask logs all accumulated entries for a background task/worker
func (rl *RequestLogger) PrintTask(taskName string, duration time.Duration, success bool) {
	if rl == nil {
		return
	}
	rl.mu.Lock()
	entries := rl.entries
	rl.mu.Unlock()

	status := "SUCCESS"
	if !success {
		status = "FAILED"
	}

	var out strings.Builder
	out.WriteString(fmt.Sprintf("\n=== BACKGROUND TASK DIAGNOSTICS: %s | Status: %s | Duration: %v ===\n", taskName, status, duration))
	for _, entry := range entries {
		out.WriteString(fmt.Sprintf("[%s] %s\n", entry.Timestamp.Format("2006-01-02 15:04:05.000"), entry.Message))
	}
	out.WriteString("=========================================================================")
	log.Println(out.String())
}

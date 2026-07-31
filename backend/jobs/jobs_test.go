package jobs

import (
	"errors"
	"testing"
)

func TestIsPermanent(t *testing.T) {
	base := errors.New("input document is not a PDF")

	if IsPermanent(base) {
		t.Error("a plain error must be retryable: a transient sidecar outage looks like this")
	}
	if !IsPermanent(Permanent(base)) {
		t.Error("a wrapped error must be permanent")
	}
	if IsPermanent(nil) {
		t.Error("nil is not a permanent failure")
	}
}

func TestPermanentUnwrapsToCause(t *testing.T) {
	base := errors.New("user lacks execute permission")
	wrapped := Permanent(base)

	// The worker writes err.Error() into jobs.error, so the cause must survive
	// wrapping or the caller polling the job sees a useless message.
	if wrapped.Error() != base.Error() {
		t.Errorf("Error() = %q, want %q", wrapped.Error(), base.Error())
	}
	if !errors.Is(wrapped, base) {
		t.Error("errors.Is must see through Permanent to the cause")
	}
}

// A permanent failure nested inside a wrapped error chain must still be
// recognised, since tools wrap errors with context as they return them.
func TestIsPermanentThroughWrappedChain(t *testing.T) {
	inner := Permanent(errors.New("unsupported page range"))
	outer := errors.New("rotate failed")

	if IsPermanent(outer) {
		t.Error("an unrelated error must not be permanent")
	}

	joined := errors.Join(outer, inner)
	if !IsPermanent(joined) {
		t.Error("a permanent error inside a chain must still be detected")
	}
}

func TestReporterNilIsSafe(t *testing.T) {
	// Tools receive a reporter from the worker, but unit tests and future
	// synchronous callers may pass nil. Progress must not panic there, since a
	// lost progress update is never worth failing real work over.
	var r *Reporter
	r.Progress(1, "step")
}

func TestNewReporterSetTotal(t *testing.T) {
	r := NewReporter(nil, "job-1", 0)
	if r.total != 0 {
		t.Errorf("total = %d, want 0", r.total)
	}

	// A tool typically learns the real page count only after opening the file.
	r.SetTotal(42)
	if r.total != 42 {
		t.Errorf("total after SetTotal = %d, want 42", r.total)
	}
}

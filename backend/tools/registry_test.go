package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"booklet/jobs"
)

func noopRun(context.Context, *jobs.Job, *jobs.Reporter) error { return nil }

func TestRegisterAppliesDefaults(t *testing.T) {
	t.Cleanup(restore)
	reset()

	// MinInputs 0 and an empty InputKinds are the zero values a registration is
	// most likely to omit; both must land on the safe default rather than
	// meaning "no inputs required" and "accepts everything".
	Register(&Tool{Slug: "rotate", Label: "Rotate", Run: noopRun})

	got, ok := Get("rotate")
	if !ok {
		t.Fatal("expected rotate to be registered")
	}
	if got.MinInputs != 1 {
		t.Errorf("MinInputs = %d, want 1", got.MinInputs)
	}
	if len(got.InputKinds) != 1 || got.InputKinds[0] != "pdf" {
		t.Errorf("InputKinds = %v, want [pdf]", got.InputKinds)
	}
}

func TestRegisterPanicsOnDuplicateSlug(t *testing.T) {
	t.Cleanup(restore)
	reset()

	Register(&Tool{Slug: "merge", Run: noopRun})

	defer func() {
		if recover() == nil {
			t.Error("registering a duplicate slug must panic so the clash surfaces at startup")
		}
	}()
	Register(&Tool{Slug: "merge", Run: noopRun})
}

func TestRegisterPanicsOnEmptySlug(t *testing.T) {
	t.Cleanup(restore)
	reset()

	defer func() {
		if recover() == nil {
			t.Error("an empty slug must panic: it can never be routed or enqueued")
		}
	}()
	Register(&Tool{Label: "No slug", Run: noopRun})
}

func TestGetUnknownSlug(t *testing.T) {
	t.Cleanup(restore)
	reset()

	if _, ok := Get("does-not-exist"); ok {
		t.Error("Get must report false for an unregistered slug")
	}
}

func TestListIsSortedBySlug(t *testing.T) {
	t.Cleanup(restore)
	reset()

	Register(&Tool{Slug: "rotate", Run: noopRun})
	Register(&Tool{Slug: "merge", Run: noopRun})
	Register(&Tool{Slug: "split", Run: noopRun})

	// Map iteration order is random, so without the sort the catalog response and
	// therefore the frontend menu would reshuffle between requests.
	want := []string{"merge", "rotate", "split"}
	got := List()
	if len(got) != len(want) {
		t.Fatalf("List() returned %d tools, want %d", len(got), len(want))
	}
	for i, slug := range want {
		if got[i].Slug != slug {
			t.Errorf("List()[%d] = %q, want %q", i, got[i].Slug, slug)
		}
	}
}

func TestAvailableSkipsUnimplementedAndUnreachable(t *testing.T) {
	t.Cleanup(restore)
	reset()

	Register(&Tool{Slug: "implemented", Run: noopRun})
	// Registered but not yet implemented: advertising it would let a caller
	// enqueue work nothing can perform.
	Register(&Tool{Slug: "unimplemented"})
	// Behind a sidecar that is down: hidden rather than failing at run time.
	Register(&Tool{Slug: "sidecar-down", Run: noopRun, Available: func(context.Context) bool { return false }})
	Register(&Tool{Slug: "sidecar-up", Run: noopRun, Available: func(context.Context) bool { return true }})

	got := Available(context.Background())
	want := map[string]bool{"implemented": true, "sidecar-up": true}

	if len(got) != len(want) {
		t.Fatalf("Available() returned %d tools, want %d: %v", len(got), len(want), slugsOf(got))
	}
	for _, tool := range got {
		if !want[tool.Slug] {
			t.Errorf("Available() unexpectedly included %q", tool.Slug)
		}
	}
}

func TestCheckArity(t *testing.T) {
	cases := []struct {
		name    string
		tool    Tool
		inputs  int
		wantErr bool
	}{
		{"single input ok", Tool{Slug: "rotate", MinInputs: 1, MaxInputs: 1}, 1, false},
		{"single input rejects two", Tool{Slug: "rotate", MinInputs: 1, MaxInputs: 1}, 2, true},
		{"single input rejects zero", Tool{Slug: "rotate", MinInputs: 1, MaxInputs: 1}, 0, true},
		{"merge needs two", Tool{Slug: "merge", MinInputs: 2}, 1, true},
		{"merge accepts two", Tool{Slug: "merge", MinInputs: 2}, 2, false},
		// MaxInputs 0 means unbounded, which is what Merge relies on.
		{"merge is unbounded", Tool{Slug: "merge", MinInputs: 2}, 500, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.tool.CheckArity(tc.inputs)
			if (err != nil) != tc.wantErr {
				t.Errorf("CheckArity(%d) error = %v, wantErr %t", tc.inputs, err, tc.wantErr)
			}
		})
	}
}

func TestAcceptsKind(t *testing.T) {
	pdfOnly := Tool{Slug: "rotate", InputKinds: []string{"pdf"}}
	if !pdfOnly.AcceptsKind("pdf") {
		t.Error("rotate must accept pdf")
	}
	// An 'export' row has no page structure, so a paginating tool must refuse it.
	if pdfOnly.AcceptsKind("export") {
		t.Error("rotate must not accept export")
	}

	converter := Tool{Slug: "word-to-pdf", InputKinds: []string{"source"}}
	if !converter.AcceptsKind("source") {
		t.Error("word-to-pdf must accept source")
	}
	if converter.AcceptsKind("pdf") {
		t.Error("word-to-pdf must not accept pdf")
	}
}

func TestValidateHookRejectsBadParams(t *testing.T) {
	t.Cleanup(restore)
	reset()

	Register(&Tool{
		Slug: "rotate",
		Run:  noopRun,
		Validate: func(params json.RawMessage) error {
			var p struct{ Angle int }
			if err := json.Unmarshal(params, &p); err != nil {
				return err
			}
			if p.Angle != 90 && p.Angle != 180 && p.Angle != 270 {
				return errBadAngle
			}
			return nil
		},
	})

	tool, _ := Get("rotate")
	if err := tool.Validate(json.RawMessage(`{"angle":90}`)); err != nil {
		t.Errorf("angle 90 should validate, got %v", err)
	}
	if err := tool.Validate(json.RawMessage(`{"angle":45}`)); err == nil {
		t.Error("angle 45 should be rejected before a job is created")
	}
}

var errBadAngle = errors.New("angle must be 90, 180 or 270")

func slugsOf(list []*Tool) []string {
	out := make([]string, 0, len(list))
	for _, t := range list {
		out = append(out, t.Slug)
	}
	return out
}

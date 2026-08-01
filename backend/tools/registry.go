// Package tools holds the registry of PDF tools that run on the job queue.
//
// A tool is registered once here and becomes simultaneously executable by the
// worker, enqueueable through POST /api/tools/jobs, and visible in the frontend
// menu via GET /api/tools. Splitting those three concerns across separate lists
// is how a tool ends up runnable but invisible, or advertised but unroutable.
//
// Stage 2 adds the implementations. This file defines only the contract and the
// lookup, so the worker and the API can land first.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"booklet/jobs"
)

// ParamType describes a parameter well enough for the frontend to render an
// input for it without hardcoding per-tool knowledge.
type ParamType string

const (
	ParamString ParamType = "string"
	ParamInt    ParamType = "int"
	ParamBool   ParamType = "bool"
	ParamEnum   ParamType = "enum"
	// ParamPageRange is a page selection such as "1-4,7,9-". Kept distinct from
	// ParamString so the UI can offer thumbnail-based selection.
	ParamPageRange ParamType = "page_range"
	// ParamPassword must never be echoed back in a job's params.
	ParamPassword ParamType = "password"
)

// Param is one tool parameter in the catalog.
type Param struct {
	Name     string    `json:"name"`
	Label    string    `json:"label"`
	Type     ParamType `json:"type"`
	Required bool      `json:"required"`
	Default  any       `json:"default,omitempty"`
	Options  []string  `json:"options,omitempty"`
	Min      *int      `json:"min,omitempty"`
	Max      *int      `json:"max,omitempty"`
	Help     string    `json:"help,omitempty"`
}

// Tool is the registration record for one tool.
type Tool struct {
	Slug        string  `json:"slug"`
	Label       string  `json:"label"`
	Description string  `json:"description"`
	Icon        string  `json:"icon"`
	Params      []Param `json:"params"`

	// MinInputs and MaxInputs bound the selection. MaxInputs of 0 means
	// unbounded, which is what Merge needs.
	MinInputs int `json:"min_inputs"`
	MaxInputs int `json:"max_inputs"`

	// InputKinds lists the document kinds this tool accepts ("pdf", "source",
	// "export"). The frontend uses it to grey out tools for the current
	// selection; the API enforces it.
	InputKinds []string `json:"input_kinds"`

	// PreservesText decides how embeddings reach the derived document: true
	// copies the parent's page embeddings through a page map, false re-runs the
	// extract and embed pipeline. Rotating 500 pages must not issue 500 Ollama
	// calls, which is the whole point of the distinction.
	PreservesText bool `json:"preserves_text"`

	// Run executes the tool. Nil means registered-but-unimplemented, and the
	// API refuses to enqueue it rather than queueing work nothing can perform.
	Run RunFunc `json:"-"`

	// Available reports whether the tool's backing engine can be reached. Nil
	// means always available. Tools behind a sidecar report its health here so
	// the catalog can hide them instead of failing at run time.
	Available func(context.Context) bool `json:"-"`

	// Validate rejects bad params before a job is created, so the caller gets a
	// synchronous 400 instead of polling a job that was always going to fail.
	Validate func(params json.RawMessage) error `json:"-"`
}

// RunFunc executes a tool. The reporter publishes progress; returning
// jobs.Permanent(err) marks the failure non-retryable.
type RunFunc func(ctx context.Context, job *jobs.Job, reporter *jobs.Reporter) error

var (
	mu       sync.RWMutex
	registry = map[string]*Tool{}
)

// Register adds a tool. It panics on a duplicate or malformed slug, because
// both are programming errors that must surface at startup rather than as a
// silently shadowed tool in production.
func Register(t *Tool) {
	mu.Lock()
	defer mu.Unlock()

	if t.Slug == "" {
		panic("tools: cannot register a tool with an empty slug")
	}
	if _, exists := registry[t.Slug]; exists {
		panic(fmt.Sprintf("tools: duplicate tool slug %q", t.Slug))
	}
	if t.MinInputs < 1 {
		t.MinInputs = 1
	}
	if len(t.InputKinds) == 0 {
		t.InputKinds = []string{"pdf"}
	}
	registry[t.Slug] = t
}

// Get returns the tool for slug.
func Get(slug string) (*Tool, bool) {
	mu.RLock()
	defer mu.RUnlock()
	t, ok := registry[slug]
	return t, ok
}

// List returns every registered tool ordered by slug, so the catalog response
// and therefore the frontend menu is stable across requests.
func List() []*Tool {
	mu.RLock()
	defer mu.RUnlock()

	out := make([]*Tool, 0, len(registry))
	for _, t := range registry {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out
}

// Available returns the tools that are implemented and whose engine is
// reachable. Unavailable tools are omitted from the catalog rather than shown
// and failing on use.
func Available(ctx context.Context) []*Tool {
	all := List()
	out := make([]*Tool, 0, len(all))
	for _, t := range all {
		if t.Run == nil {
			continue
		}
		if t.Available != nil && !t.Available(ctx) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// AcceptsKind reports whether the tool accepts a document of this kind.
func (t *Tool) AcceptsKind(kind string) bool {
	for _, k := range t.InputKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// CheckArity validates the input count against the tool's bounds.
func (t *Tool) CheckArity(n int) error {
	if n < t.MinInputs {
		return fmt.Errorf("%s requires at least %d input document(s), got %d", t.Slug, t.MinInputs, n)
	}
	if t.MaxInputs > 0 && n > t.MaxInputs {
		return fmt.Errorf("%s accepts at most %d input document(s), got %d", t.Slug, t.MaxInputs, n)
	}
	return nil
}

// reset clears the registry. Test-only.
//
// The registry is process-wide and populated by each tool's init(), so a test
// that clears it would strand every later test in the package with an empty
// catalog. The first clear snapshots the init-time contents so restore() can
// put them back.
func reset() {
	mu.Lock()
	defer mu.Unlock()

	if baseline == nil {
		baseline = make(map[string]*Tool, len(registry))
		for slug, tool := range registry {
			baseline[slug] = tool
		}
	}
	registry = map[string]*Tool{}
}

// restore returns the registry to its init-time contents. Test-only; pair it
// with t.Cleanup in any test that calls reset().
func restore() {
	mu.Lock()
	defer mu.Unlock()

	if baseline == nil {
		return
	}
	registry = make(map[string]*Tool, len(baseline))
	for slug, tool := range baseline {
		registry[slug] = tool
	}
}

var baseline map[string]*Tool

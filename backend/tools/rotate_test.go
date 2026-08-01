package tools

import (
	"encoding/json"
	"testing"
)

func TestRotateIsRegistered(t *testing.T) {
	// init() registers rotate at package load. registry_test.go calls reset(),
	// so this asserts against a fresh lookup rather than assuming ordering.
	tool, ok := Get("rotate")
	if !ok {
		t.Fatal("rotate must be registered by init()")
	}
	if tool.Run == nil {
		t.Error("rotate must have a Run function or the API refuses to enqueue it")
	}
	if !tool.PreservesText {
		t.Error("rotate must preserve text: rotation moves glyphs but changes none of them")
	}
	if tool.MinInputs != 1 || tool.MaxInputs != 1 {
		t.Errorf("rotate arity = [%d,%d], want exactly one input", tool.MinInputs, tool.MaxInputs)
	}
	if !tool.AcceptsKind("pdf") || tool.AcceptsKind("export") {
		t.Errorf("rotate input kinds = %v, want pdf only", tool.InputKinds)
	}
	if tool.Validate == nil {
		t.Error("rotate must validate its angle synchronously, before a job is created")
	}
}

func TestParseRotateParams(t *testing.T) {
	cases := []struct {
		name      string
		raw       string
		wantAngle int
		wantPages string
		wantErr   bool
	}{
		{"numeric angle", `{"angle":90}`, 90, "", false},
		// The catalog declares angle as an enum, and a shadcn Select yields a
		// string, so both encodings must parse.
		{"string angle", `{"angle":"180"}`, 180, "", false},
		{"angle with pages", `{"angle":270,"pages":"1-4,7"}`, 270, "1-4,7", false},
		{"missing angle", `{}`, 0, "", true},
		{"empty params", ``, 0, "", true},
		{"null angle", `{"angle":null}`, 0, "", true},
		{"non-numeric string", `{"angle":"ninety"}`, 0, "", true},
		{"malformed json", `{"angle":`, 0, "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseRotateParams(json.RawMessage(tc.raw))
			if (err != nil) != tc.wantErr {
				t.Fatalf("parseRotateParams(%s) error = %v, wantErr %t", tc.raw, err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if got.Angle != tc.wantAngle {
				t.Errorf("angle = %d, want %d", got.Angle, tc.wantAngle)
			}
			if got.Pages != tc.wantPages {
				t.Errorf("pages = %q, want %q", got.Pages, tc.wantPages)
			}
		})
	}
}

func TestValidateRotateParamsRejectsNonRightAngles(t *testing.T) {
	// pdfcpu accepts 45 and produces something no printer handles sensibly, so
	// the tool restricts rotation to the three angles the UI offers.
	for _, raw := range []string{`{"angle":45}`, `{"angle":0}`, `{"angle":-90}`, `{"angle":360}`} {
		if err := validateRotateParams(json.RawMessage(raw)); err == nil {
			t.Errorf("%s must be rejected: only 90, 180 and 270 are supported", raw)
		}
	}

	for _, raw := range []string{`{"angle":90}`, `{"angle":180}`, `{"angle":"270"}`} {
		if err := validateRotateParams(json.RawMessage(raw)); err != nil {
			t.Errorf("%s must validate, got %v", raw, err)
		}
	}
}

func TestValidateRotateParamsRejectsBadPageSelection(t *testing.T) {
	if err := validateRotateParams(json.RawMessage(`{"angle":90,"pages":"1-"}`)); err != nil {
		t.Errorf("an open-ended range must be accepted, got %v", err)
	}
	// Rejecting this at enqueue time is the difference between a 400 and a job
	// the caller polls until it fails.
	if err := validateRotateParams(json.RawMessage(`{"angle":90,"pages":"not-a-range"}`)); err == nil {
		t.Error("a malformed page selection must be rejected before the job is created")
	}
}

func TestIdentityPageSources(t *testing.T) {
	got := identityPageSources("parent", 3)
	if len(got) != 3 {
		t.Fatalf("expected 3 sources, got %d", len(got))
	}
	for i, src := range got {
		if src.DocumentID != "parent" || src.Page != i+1 {
			t.Errorf("source[%d] = %+v, want parent page %d", i, src, i+1)
		}
	}
	if len(identityPageSources("parent", 0)) != 0 {
		t.Error("a zero page count must produce no sources")
	}
}

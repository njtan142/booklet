package tools

import (
	"testing"
)

func TestDerivedDocumentIDIsDeterministic(t *testing.T) {
	jobID := "8b1f0c9a-4e3d-4a1b-9f2c-7d6e5a4b3c21"

	// Determinism is what makes a retry idempotent: attempt two must overwrite
	// the row attempt one left behind instead of adding a second derived
	// document to the user's library.
	first := DerivedDocumentID(jobID, 0)
	second := DerivedDocumentID(jobID, 0)
	if first != second {
		t.Errorf("DerivedDocumentID is not stable across calls: %s vs %s", first, second)
	}

	// Distinct positions must not collide, or Split would overwrite page 1 with
	// page 2 instead of producing N documents.
	if DerivedDocumentID(jobID, 1) == first {
		t.Error("positions 0 and 1 produced the same document id")
	}
	if DerivedDocumentID("2c9f1a3b-6d4e-4f8a-8b1c-3e5d7a9c0f42", 0) == first {
		t.Error("different jobs produced the same document id")
	}
}

func TestDerivedName(t *testing.T) {
	cases := []struct {
		parent string
		suffix string
		want   string
	}{
		{"Report.pdf", "rotated 90", "Report (rotated 90).pdf"},
		// The extension is stripped first so chained tools do not accumulate
		// ".pdf" in the middle of the name.
		{"Report (rotated 90).pdf", "compressed", "Report (rotated 90) (compressed).pdf"},
		{"scan.PDF", "rotated 180", "scan (rotated 180).PDF"},
		// A name with no extension still gets a usable one.
		{"notes", "merged", "notes (merged).pdf"},
		// A dotfile-style name has no base to keep.
		{".pdf", "rotated 90", "document (rotated 90).pdf"},
		{"a.b.pdf", "rotated 270", "a.b (rotated 270).pdf"},
	}

	for _, tc := range cases {
		if got := DerivedName(tc.parent, tc.suffix); got != tc.want {
			t.Errorf("DerivedName(%q, %q) = %q, want %q", tc.parent, tc.suffix, got, tc.want)
		}
	}
}

func TestBuildSourcePagesIdentityMap(t *testing.T) {
	pages := []splitPage{
		{Number: 1, StoragePath: "d/1.pdf", Width: 842, Height: 595},
		{Number: 2, StoragePath: "d/2.pdf", Width: 842, Height: 595},
	}
	sources := []PageSource{{DocumentID: "parent", Page: 1}, {DocumentID: "parent", Page: 2}}

	got, err := buildSourcePages(pages, sources)
	if err != nil {
		t.Fatalf("buildSourcePages returned %v", err)
	}
	if len(got) != 1 || got[0].DocumentID != "parent" {
		t.Fatalf("expected one parent group for 'parent', got %+v", got)
	}
	if len(got[0].Mapping) != 2 {
		t.Fatalf("expected 2 mapped pages, got %d", len(got[0].Mapping))
	}

	// The derived page's own storage path and its measured dimensions must be
	// carried, not the parent's: reusing the parent's path would make two
	// documents share one page object.
	first := got[0].Mapping[0]
	if first.SourcePage != 1 || first.DerivedPage != 1 {
		t.Errorf("first mapping = %+v, want source 1 -> derived 1", first)
	}
	if first.StoragePath != "d/1.pdf" {
		t.Errorf("mapping storage path = %q, want the derived page's own object", first.StoragePath)
	}
	if first.Width != 842 || first.Height != 595 {
		t.Errorf("mapping dimensions = %vx%v, want the derived page's measured 842x595", first.Width, first.Height)
	}
}

func TestBuildSourcePagesPreservesParentOrder(t *testing.T) {
	// Merge's case: two parents, each contributing a contiguous run. The group
	// order must follow the user's input order, not map iteration order.
	pages := []splitPage{
		{Number: 1, StoragePath: "m/1.pdf"},
		{Number: 2, StoragePath: "m/2.pdf"},
		{Number: 3, StoragePath: "m/3.pdf"},
	}
	sources := []PageSource{
		{DocumentID: "second", Page: 1},
		{DocumentID: "first", Page: 1},
		{DocumentID: "second", Page: 2},
	}

	got, err := buildSourcePages(pages, sources)
	if err != nil {
		t.Fatalf("buildSourcePages returned %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 parent groups, got %d", len(got))
	}
	if got[0].DocumentID != "second" || got[1].DocumentID != "first" {
		t.Errorf("parent order = [%s %s], want [second first] (first appearance)",
			got[0].DocumentID, got[1].DocumentID)
	}

	// The absolute derived page numbers must survive the grouping, or a merged
	// document would renumber its second half.
	if len(got[0].Mapping) != 2 ||
		got[0].Mapping[0].DerivedPage != 1 || got[0].Mapping[1].DerivedPage != 3 {
		t.Errorf("second's mapping = %+v, want derived pages 1 and 3", got[0].Mapping)
	}
	if len(got[1].Mapping) != 1 || got[1].Mapping[0].DerivedPage != 2 {
		t.Errorf("first's mapping = %+v, want derived page 2", got[1].Mapping)
	}
}

func TestBuildSourcePagesRejectsLengthMismatch(t *testing.T) {
	// This is the guard that catches a wrong page map. Without it a tool that
	// drops or adds a page would write a document whose pages silently belong to
	// the wrong parent pages.
	pages := []splitPage{{Number: 1, StoragePath: "d/1.pdf"}, {Number: 2, StoragePath: "d/2.pdf"}}
	sources := []PageSource{{DocumentID: "parent", Page: 1}}

	if _, err := buildSourcePages(pages, sources); err == nil {
		t.Error("a page map shorter than the derived page count must be rejected")
	}
}

func TestBuildSourcePagesRejectsEmptyInput(t *testing.T) {
	if _, err := buildSourcePages(nil, nil); err == nil {
		t.Error("an empty derived document must be rejected rather than written with no pages")
	}
}

func TestBuildSourcePagesRejectsMissingParent(t *testing.T) {
	pages := []splitPage{{Number: 1, StoragePath: "d/1.pdf"}}
	sources := []PageSource{{Page: 1}}

	if _, err := buildSourcePages(pages, sources); err == nil {
		t.Error("a page map entry with no parent document id must be rejected")
	}
}

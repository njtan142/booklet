package pdf

import (
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// TestRotateDoesNotSwapReportedDimensions pins down how a rotated page reports
// its size, because the tools package decides whether to copy or recompute page
// geometry based on the answer.
//
// pdfcpu implements a 90 degree rotation by setting the page's /Rotate entry,
// not by rewriting its MediaBox. processSinglePage reads the MediaBox and
// ignores /Rotate, so a rotated page reports the *same* width and height as its
// parent. A viewer still displays it rotated.
//
// This means CopyPageEmbeddings must not be relied on to "swap" anything: the
// derived dimensions equal the parent's, and any tool that genuinely changes
// page geometry has to say so explicitly.
func TestRotateDoesNotSwapReportedDimensions(t *testing.T) {
	tempDir := t.TempDir()
	inputPath := filepath.Join(tempDir, "input.pdf")
	rotatedPath := filepath.Join(tempDir, "rotated.pdf")

	if err := writeMinimalTestPDF(inputPath); err != nil {
		t.Fatalf("failed to write test PDF: %v", err)
	}

	_, beforeW, beforeH, err := processSinglePage(inputPath)
	if err != nil {
		t.Fatalf("processSinglePage on the original returned error: %v", err)
	}

	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	conf.WriteObjectStream = false
	conf.WriteXRefStream = false

	if err := api.RotateFile(inputPath, rotatedPath, 90, nil, conf); err != nil {
		t.Fatalf("RotateFile returned error: %v", err)
	}

	_, afterW, afterH, err := processSinglePage(rotatedPath)
	if err != nil {
		t.Fatalf("processSinglePage on the rotated file returned error: %v", err)
	}

	if afterW != beforeW || afterH != beforeH {
		t.Errorf("a 90 degree rotation changed the reported dimensions from %.2fx%.2f to %.2fx%.2f; "+
			"the tools package assumes /Rotate leaves the MediaBox alone",
			beforeW, beforeH, afterW, afterH)
	}
}

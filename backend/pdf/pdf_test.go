package pdf

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCalculateBookletLayout_S4_N4(t *testing.T) {
	// 4 pages, signature size 4 (1 sheet total, 2 sides)
	sheets := CalculateBookletLayout(4, 4)

	if len(sheets) != 2 {
		t.Fatalf("Expected 2 sheet sides (1 sheet), got %d", len(sheets))
	}

	// Sheet 1 Front (Side 0): Left = 4, Right = 1
	if sheets[0].LeftPage != 4 || sheets[0].RightPage != 1 {
		t.Errorf("Sheet 1 Front: expected Left=4 Right=1, got Left=%d Right=%d", sheets[0].LeftPage, sheets[0].RightPage)
	}

	// Sheet 1 Back (Side 1): Left = 2, Right = 3
	if sheets[1].LeftPage != 2 || sheets[1].RightPage != 3 {
		t.Errorf("Sheet 1 Back: expected Left=2 Right=3, got Left=%d Right=%d", sheets[1].LeftPage, sheets[1].RightPage)
	}
}

func TestCalculateBookletLayout_S8_N6(t *testing.T) {
	// 6 pages, signature size 8 (padded to 8, 2 sheets total, 4 sides)
	sheets := CalculateBookletLayout(6, 8)

	if len(sheets) != 4 {
		t.Fatalf("Expected 4 sheet sides (2 sheets), got %d", len(sheets))
	}

	// Sheet 1 Front (Side 0): Left = getP(8) = 0 (blank), Right = getP(1) = 1
	if sheets[0].LeftPage != 0 || sheets[0].RightPage != 1 {
		t.Errorf("Sheet 1 Front: expected Left=0 Right=1, got Left=%d Right=%d", sheets[0].LeftPage, sheets[0].RightPage)
	}

	// Sheet 1 Back (Side 1): Left = getP(2) = 2, Right = getP(7) = 0 (blank)
	if sheets[1].LeftPage != 2 || sheets[1].RightPage != 0 {
		t.Errorf("Sheet 1 Back: expected Left=2 Right=0, got Left=%d Right=%d", sheets[1].LeftPage, sheets[1].RightPage)
	}

	// Sheet 2 Front (Side 2): Left = getP(6) = 6, Right = getP(3) = 3
	if sheets[2].LeftPage != 6 || sheets[2].RightPage != 3 {
		t.Errorf("Sheet 2 Front: expected Left=6 Right=3, got Left=%d Right=%d", sheets[2].LeftPage, sheets[2].RightPage)
	}

	// Sheet 2 Back (Side 3): Left = getP(4) = 4, Right = getP(5) = 5
	if sheets[3].LeftPage != 4 || sheets[3].RightPage != 5 {
		t.Errorf("Sheet 2 Back: expected Left=4 Right=5, got Left=%d Right=%d", sheets[3].LeftPage, sheets[3].RightPage)
	}
}

func TestCalculateBookletLayout_InvalidSignature(t *testing.T) {
	// Signature size 3 is invalid (not multiple of 4), should fallback to 4
	sheets := CalculateBookletLayout(4, 3)

	if len(sheets) != 2 {
		t.Fatalf("Expected fallback to signature size 4 which yields 2 sheet sides, got %d", len(sheets))
	}

	if sheets[0].LeftPage != 4 || sheets[0].RightPage != 1 {
		t.Errorf("Expected Left=4 Right=1, got Left=%d Right=%d", sheets[0].LeftPage, sheets[0].RightPage)
	}
}

func TestMapPagesToSheets(t *testing.T) {
	// Case 1: Booklet page 13 is ruined -> should map to Sheet 7
	startSheet, endSheet := MapPagesToSheets(13, 13)
	if startSheet != 7 || endSheet != 7 {
		t.Errorf("Expected booklet page 13 to map to Sheet 7-7, got Sheet %d-%d", startSheet, endSheet)
	}

	// Case 2: Booklet pages 13-16 are ruined -> should map to Sheets 7-8
	startSheet, endSheet = MapPagesToSheets(13, 16)
	if startSheet != 7 || endSheet != 8 {
		t.Errorf("Expected booklet pages 13-16 to map to Sheets 7-8, got Sheets %d-%d", startSheet, endSheet)
	}
}

func TestProcessSinglePage_ProcessesGeneratedPDF(t *testing.T) {
	tempDir := t.TempDir()
	inputPath := filepath.Join(tempDir, "input.pdf")

	if err := writeMinimalTestPDF(inputPath); err != nil {
		t.Fatalf("failed to write test PDF: %v", err)
	}

	text, width, height, err := processSinglePage(inputPath)
	if err != nil {
		t.Fatalf("processSinglePage returned error: %v", err)
	}

	if strings.ReplaceAll(text, " ", "") != "Hello" {
		t.Fatalf("expected extracted text to normalize to %q, got %q", "Hello", text)
	}

	if width <= 0 || height <= 0 {
		t.Fatalf("expected positive page dimensions, got %.2f x %.2f", width, height)
	}

	if _, err := os.Stat(inputPath); err != nil {
		t.Fatalf("expected test PDF to exist, stat err=%v", err)
	}
}

func writeMinimalTestPDF(path string) error {
	var builder strings.Builder
	objectOffsets := make([]int, 0, 6)

	writeObject := func(content string) {
		objectOffsets = append(objectOffsets, builder.Len())
		builder.WriteString(content)
	}

	builder.WriteString("%PDF-1.4\n")
	writeObject("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	writeObject("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")
	writeObject("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 300] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>\nendobj\n")
	writeObject("4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n")
	stream := "BT /F1 12 Tf 72 150 Td (Hello) Tj ET\n"
	writeObject(fmt.Sprintf("5 0 obj\n<< /Length %d >>\nstream\n%sendstream\nendobj\n", len(stream), stream))

	xrefStart := builder.Len()
	builder.WriteString("xref\n0 6\n0000000000 65535 f \n")
	for i := 0; i < 5; i++ {
		builder.WriteString(fmt.Sprintf("%010d 00000 n \n", objectOffsets[i]))
	}
	builder.WriteString(fmt.Sprintf("trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefStart))

	return os.WriteFile(path, []byte(builder.String()), 0644)
}


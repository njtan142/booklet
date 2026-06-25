package pdf

import (
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

func TestMapPagesToSheets_S8_N8(t *testing.T) {
	// 8 booklet pages, signature size 8
	// Sheet 1 has pages: [8, 1, 2, 7]
	// Sheet 2 has pages: [6, 3, 4, 5]

	// Case 1: Page 7 is ruined -> should map to Sheet 1
	startSheet, endSheet := MapPagesToSheets(8, 8, 7, 7)
	if startSheet != 1 || endSheet != 1 {
		t.Errorf("Expected page 7 to map to Sheet 1-1, got Sheet %d-%d", startSheet, endSheet)
	}

	// Case 2: Pages 5-6 are ruined -> should map to Sheet 2
	startSheet, endSheet = MapPagesToSheets(8, 8, 5, 6)
	if startSheet != 2 || endSheet != 2 {
		t.Errorf("Expected pages 5-6 to map to Sheet 2-2, got Sheet %d-%d", startSheet, endSheet)
	}

	// Case 3: Pages 6-7 are ruined -> should map to Sheets 1-2
	startSheet, endSheet = MapPagesToSheets(8, 8, 6, 7)
	if startSheet != 1 || endSheet != 2 {
		t.Errorf("Expected pages 6-7 to map to Sheets 1-2, got Sheets %d-%d", startSheet, endSheet)
	}
}


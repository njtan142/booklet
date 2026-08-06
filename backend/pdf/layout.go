package pdf

import (
	"math"
)

// CalculateBookletLayout calculates the sequence of pages for a custom duplex booklet
func CalculateBookletLayout(numPages int, signatureSize int) []SheetSide {
	S := signatureSize
	if S <= 0 || S%4 != 0 {
		S = 4 // Fallback to 4
	}

	N := numPages
	// M must be smallest multiple of S greater than or equal to N
	M := int(math.Ceil(float64(N)/float64(S))) * S

	var sheets []SheetSide
	numSignatures := M / S
	for sig := 0; sig < numSignatures; sig++ {
		basePage := sig * S // 0-based offset
		numSigSheets := S / 4

		for k := 0; k < numSigSheets; k++ {
			// Calculate the 1-based page index within this signature
			p1 := basePage + (2*k + 1)
			p2 := basePage + (2*k + 2)
			p3 := basePage + (S - 2*k - 1)
			p4 := basePage + (S - 2*k)

			// Apply blank page filtering (if index > N, it's a padded blank page)
			getP := func(idx int) int {
				if idx > N {
					return 0
				}
				return idx
			}

			// Front side of sheet: Left = p4, Right = p1
			sheets = append(sheets, SheetSide{
				LeftPage:  getP(p4),
				RightPage: getP(p1),
			})

			// Back side of sheet: Left = p2, Right = p3
			sheets = append(sheets, SheetSide{
				LeftPage:  getP(p2),
				RightPage: getP(p3),
			})
		}
	}
	return sheets
}

// MapPagesToSheets maps a range of 1-based booklet PDF pages to the physical sheet range that contains them
func MapPagesToSheets(startPage int, endPage int) (int, int) {
	startSheet := (startPage + 1) / 2
	endSheet := (endPage + 1) / 2
	return startSheet, endSheet
}

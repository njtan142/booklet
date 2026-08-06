package pdf

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"booklet/logger"
	"booklet/storage"

	"github.com/google/uuid"
	"github.com/signintech/gopdf"
)

// CompileBooklet programmatically positions single-page PDFs onto a landscape canvas using gopdf
func CompileBooklet(ctx context.Context, dbPages []DBPageInfo, config BookletConfig) (string, error) {
	// Create a temp directory for downloaded single pages
	tempDir, err := os.MkdirTemp("", "booklet-compile-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	logger.Logf(ctx, "Compiling booklet using gopdf for %d pages (Signature size: %d, Margin: %.2f, Gutter: %.2f)...", len(dbPages), config.SignatureSize, config.Margin, config.Gutter)

	// Sort database pages sequentially by page number
	sort.Slice(dbPages, func(i, j int) bool {
		return dbPages[i].PageNumber < dbPages[j].PageNumber
	})

	// Download all required single-page PDF files locally in order
	var localPagePaths []string
	pagesMap := make(map[int]DBPageInfo)
	for _, dbPage := range dbPages {
		localPath := filepath.Join(tempDir, fmt.Sprintf("page_%d.pdf", dbPage.PageNumber))
		err := storage.DownloadFile(ctx, dbPage.StoragePath, localPath)
		if err != nil {
			return "", fmt.Errorf("failed to download page %d: %w", dbPage.PageNumber, err)
		}
		OptimizePagePDF(ctx, localPath)
		localPagePaths = append(localPagePaths, localPath)
		pagesMap[dbPage.PageNumber] = dbPage
	}

	if len(localPagePaths) == 0 {
		return "", fmt.Errorf("no pages to compile")
	}

	// Calculate booklet imposition layout (sheets of front/back sides)
	sheets := CalculateBookletLayout(len(dbPages), config.SignatureSize)

	// Create new PDF document
	pdfDoc := gopdf.GoPdf{}

	// Configure paper size
	var sheetWidth, sheetHeight float64
	if strings.ToLower(config.PaperSize) == "letter" {
		// Letter Landscape: 8.5 x 11 in
		sheetWidth = 792.00
		sheetHeight = 612.00
	} else if strings.ToLower(config.PaperSize) == "folio" {
		// Folio Landscape: 8.5 x 13 in
		sheetWidth = 936.00
		sheetHeight = 612.00
	} else {
		// Default A4 Landscape: 841.89 x 595.28
		sheetWidth = 841.89
		sheetHeight = 595.28
	}

	pdfDoc.Start(gopdf.Config{PageSize: gopdf.Rect{W: sheetWidth, H: sheetHeight}})

	// Calculate layout metrics
	margin := config.Margin
	gutter := config.Gutter

	availWidth := sheetWidth - (2 * margin) - gutter
	slotWidth := availWidth / 2
	availHeight := sheetHeight - (2 * margin)

	// Draw sheets
	for _, sheet := range sheets {
		pdfDoc.AddPage()
		logger.Logf(ctx, "[CompileBooklet] Processing sheet: LeftPage=%d, RightPage=%d", sheet.LeftPage, sheet.RightPage)

		// Helper function to draw page inside a slot (left or right)
		drawPageInSlot := func(pageNum int, isRightSlot bool) error {
			if pageNum == 0 {
				// Blank/padded page, don't draw anything
				return nil
			}

			// Since pageNum is 1-based original page index
			dbPage, exists := pagesMap[pageNum]
			if !exists {
				logger.Logf(ctx, "[CompileBooklet] Warning: page %d not found in pagesMap", pageNum)
				return nil // Page out of scope
			}
			localPath := filepath.Join(tempDir, fmt.Sprintf("page_%d.pdf", pageNum))
			logger.Logf(ctx, "[CompileBooklet] drawPageInSlot: pageNum=%d, isRightSlot=%t, localPath=%s", pageNum, isRightSlot, localPath)

			// Calculate slot bounds
			var slotX float64
			if isRightSlot {
				slotX = margin + slotWidth + gutter
			} else {
				slotX = margin
			}
			slotY := margin

			// Calculate scale factors to fit page within slot (keep aspect ratio)
			scaleW := slotWidth / dbPage.Width
			scaleH := availHeight / dbPage.Height
			scale := math.Min(scaleW, scaleH)

			drawW := dbPage.Width * scale
			drawH := dbPage.Height * scale

			// Center page inside the slot
			offsetX := slotX + (slotWidth-drawW)/2
			offsetY := slotY + (availHeight-drawH)/2

			// Import and place template safely with image rendering fallback if gofpdi panics/fails
			return DrawPageInSlotSafe(ctx, &pdfDoc, localPath, tempDir, pageNum, offsetX, offsetY, drawW, drawH)
		}

		// Draw Left Page
		if err := drawPageInSlot(sheet.LeftPage, false); err != nil {
			return "", err
		}

		// Draw Right Page
		if err := drawPageInSlot(sheet.RightPage, true); err != nil {
			return "", err
		}

		// Draw folding guidelines if enabled
		if config.Guides {
			pdfDoc.SetLineWidth(0.5)
			pdfDoc.SetStrokeColor(180, 180, 180)
			pdfDoc.SetLineType("dashed")
			pdfDoc.Line(sheetWidth/2, 0, sheetWidth/2, sheetHeight)
			pdfDoc.SetLineType("solid")
		}
	}

	// Write compiled PDF to local temp file
	bookletID := uuid.New().String()
	localOutPath := filepath.Join(tempDir, fmt.Sprintf("booklet_%s.pdf", bookletID))
	
	err = pdfDoc.WritePdf(localOutPath)
	if err != nil {
		return "", fmt.Errorf("failed to write compiled PDF: %w", err)
	}

	// Upload compiled booklet to MinIO
	storageKey := fmt.Sprintf("booklets/%s.pdf", bookletID)
	err = storage.UploadFile(ctx, storageKey, localOutPath, "application/pdf")
	if err != nil {
		return "", fmt.Errorf("failed to upload booklet to MinIO: %w", err)
	}

	return storageKey, nil
}

// CompileBookletSlice compiles only specific physical sheets and/or sides (fronts/backs) of a booklet directly from single pages to a local file
func CompileBookletSlice(ctx context.Context, dbPages []DBPageInfo, config BookletConfig, filterType string, sheetRange string, localOutPath string) error {
	logger.Logf(ctx, "[CompileBookletSlice] Compiling slice for signatureSize=%d, filterType=%s, sheetRange=%s to %s", config.SignatureSize, filterType, sheetRange, localOutPath)
	
	// Create a temp directory for downloaded single pages
	tempDir, err := os.MkdirTemp("", "booklet-compile-slice-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Sort database pages sequentially by page number
	sort.Slice(dbPages, func(i, j int) bool {
		return dbPages[i].PageNumber < dbPages[j].PageNumber
	})

	// Calculate all booklet sheets first
	allSides := CalculateBookletLayout(len(dbPages), config.SignatureSize)
	totalBookletPages := len(allSides)
	totalSheets := totalBookletPages / 2

	// Determine start and end sheets (1-based indices)
	startSheet := 1
	endSheet := totalSheets

	if sheetRange != "" {
		parts := strings.Split(sheetRange, "-")
		if len(parts) == 1 {
			if s, err := strconv.Atoi(parts[0]); err == nil {
				startSheet = s
				endSheet = s
			}
		} else if len(parts) == 2 {
			if s, err := strconv.Atoi(parts[0]); err == nil {
				startSheet = s
			}
			if e, err := strconv.Atoi(parts[1]); err == nil {
				endSheet = e
			}
		}
	}

	// Validate sheet ranges
	if startSheet < 1 {
		startSheet = 1
	}
	if endSheet > totalSheets {
		endSheet = totalSheets
	}
	if startSheet > endSheet {
		return fmt.Errorf("invalid sheet range: %s", sheetRange)
	}

	// Select the sides we want to render
	var selectedSides []SheetSide
	for sheetNum := startSheet; sheetNum <= endSheet; sheetNum++ {
		frontIdx := 2 * (sheetNum - 1)
		backIdx := 2*(sheetNum - 1) + 1

		switch strings.ToLower(filterType) {
		case "fronts":
			if frontIdx < len(allSides) {
				selectedSides = append(selectedSides, allSides[frontIdx])
			}
		case "backs":
			if backIdx < len(allSides) {
				selectedSides = append(selectedSides, allSides[backIdx])
			}
		default:
			if frontIdx < len(allSides) {
				selectedSides = append(selectedSides, allSides[frontIdx])
			}
			if backIdx < len(allSides) {
				selectedSides = append(selectedSides, allSides[backIdx])
			}
		}
	}

	if len(selectedSides) == 0 {
		return fmt.Errorf("no sheets selected by filter")
	}

	// Find the exact pages we need to download
	pagesMap := make(map[int]DBPageInfo)
	for _, dbPage := range dbPages {
		pagesMap[dbPage.PageNumber] = dbPage
	}

	neededPages := make(map[int]bool)
	for _, side := range selectedSides {
		if side.LeftPage > 0 {
			neededPages[side.LeftPage] = true
		}
		if side.RightPage > 0 {
			neededPages[side.RightPage] = true
		}
	}

	// Download only the needed single pages
	for pageNum := range neededPages {
		dbPage, exists := pagesMap[pageNum]
		if !exists {
			continue
		}
		localPath := filepath.Join(tempDir, fmt.Sprintf("page_%d.pdf", pageNum))
		err := storage.DownloadFile(ctx, dbPage.StoragePath, localPath)
		if err != nil {
			return fmt.Errorf("failed to download page %d: %w", pageNum, err)
		}
		OptimizePagePDF(ctx, localPath)
	}

	// Create new PDF document
	pdfDoc := gopdf.GoPdf{}

	// Configure paper size
	var sheetWidth, sheetHeight float64
	if strings.ToLower(config.PaperSize) == "letter" {
		sheetWidth = 792.00
		sheetHeight = 612.00
	} else if strings.ToLower(config.PaperSize) == "folio" {
		// Folio Landscape: 8.5 x 13 in
		sheetWidth = 936.00
		sheetHeight = 612.00
	} else {
		sheetWidth = 841.89
		sheetHeight = 595.28
	}

	pdfDoc.Start(gopdf.Config{PageSize: gopdf.Rect{W: sheetWidth, H: sheetHeight}})

	// Calculate layout metrics
	margin := config.Margin
	gutter := config.Gutter

	availWidth := sheetWidth - (2 * margin) - gutter
	slotWidth := availWidth / 2
	availHeight := sheetHeight - (2 * margin)

	// Draw selected sheet sides
	for _, sheet := range selectedSides {
		pdfDoc.AddPage()

		// Helper function to draw page inside a slot (left or right)
		drawPageInSlot := func(pageNum int, isRightSlot bool) error {
			if pageNum == 0 {
				return nil
			}

			dbPage, exists := pagesMap[pageNum]
			if !exists {
				return nil
			}
			localPath := filepath.Join(tempDir, fmt.Sprintf("page_%d.pdf", pageNum))

			// Calculate slot bounds
			var slotX float64
			if isRightSlot {
				slotX = margin + slotWidth + gutter
			} else {
				slotX = margin
			}
			slotY := margin

			// Calculate scale factors
			scaleW := slotWidth / dbPage.Width
			scaleH := availHeight / dbPage.Height
			scale := math.Min(scaleW, scaleH)

			drawW := dbPage.Width * scale
			drawH := dbPage.Height * scale

			// Center page inside the slot
			offsetX := slotX + (slotWidth-drawW)/2
			offsetY := slotY + (availHeight-drawH)/2

			// Import and place template safely with image rendering fallback if gofpdi panics/fails
			return DrawPageInSlotSafe(ctx, &pdfDoc, localPath, tempDir, pageNum, offsetX, offsetY, drawW, drawH)
		}

		// Draw Left Page
		if err := drawPageInSlot(sheet.LeftPage, false); err != nil {
			return err
		}

		// Draw Right Page
		if err := drawPageInSlot(sheet.RightPage, true); err != nil {
			return err
		}

		// Draw folding guidelines if enabled
		if config.Guides {
			pdfDoc.SetLineWidth(0.5)
			pdfDoc.SetStrokeColor(180, 180, 180)
			pdfDoc.SetLineType("dashed")
			pdfDoc.Line(sheetWidth/2, 0, sheetWidth/2, sheetHeight)
			pdfDoc.SetLineType("solid")
		}
	}

	// Write compiled PDF slice directly to destination
	err = pdfDoc.WritePdf(localOutPath)
	if err != nil {
		return fmt.Errorf("failed to write compiled PDF slice: %w", err)
	}

	return nil
}

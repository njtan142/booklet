package handlers

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"booklet/db"
	"booklet/logger"
	"booklet/pdf"
	"booklet/permissions"
	"booklet/storage"

	"github.com/signintech/gopdf"
)

func HandleGetBookletPreviewPDF(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "HandleGetBookletPreviewPDF", http.MethodGet) {
		return
	}

	docID, ok := parseUUIDParam(w, r, "HandleGetBookletPreviewPDF", "id")
	if !ok {
		return
	}

	startTime := time.Now()
	logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Received preview request for docID=%s", docID)

	if !permissions.EnforceDocument(w, r, docID, permissions.PermRead) {
		return
	}

	// Parse query parameters
	q := r.URL.Query()
	margin, _ := strconv.ParseFloat(q.Get("margin"), 64)
	gutter, _ := strconv.ParseFloat(q.Get("gutter"), 64)
	paperSize := q.Get("paper_size")
	if paperSize == "" {
		paperSize = "a4"
	}
	sigSize, _ := strconv.Atoi(q.Get("signature_size"))
	if sigSize <= 0 {
		sigSize = 4
	}
	guides := q.Get("guides") == "true"
	side := q.Get("side") // "front" or "back"
	if side != "back" {
		side = "front"
	}

	logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Parsed params: margin=%.2f, gutter=%.2f, paperSize=%s, sigSize=%d, guides=%t, side=%s",
		margin, gutter, paperSize, sigSize, guides, side)

	// Create temp directory for execution
	tempDir, err := os.MkdirTemp("", "booklet-preview-*")
	if handleServerError(w, r, "[HandleGetBookletPreviewPDF]", "failed to create temp dir", err) {
		return
	}
	defer os.RemoveAll(tempDir)
	logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Created tempDir: %s", tempDir)

	// Fetch page records for first signature (page_number <= sigSize)
	ctx := r.Context()
	logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Querying document pages from DB (page_number <= %d)", sigSize)
	rows, err := db.DB.Query(`
		SELECT page_number, storage_path, width, height 
		FROM document_pages 
		WHERE document_id = $1 AND page_number <= $2
		ORDER BY page_number ASC`, docID, sigSize)

	if handleServerError(w, r, "[HandleGetBookletPreviewPDF]", "database error", err) {
		return
	}
	defer rows.Close()

	var dbPages []pdf.DBPageInfo
	for rows.Next() {
		var p pdf.DBPageInfo
		if err := rows.Scan(&p.PageNumber, &p.StoragePath, &p.Width, &p.Height); err != nil {
			if handleServerError(w, r, "[HandleGetBookletPreviewPDF]", "database error", err) {
				return
			}
		}
		dbPages = append(dbPages, p)
	}

	logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Found %d pages in DB for signature", len(dbPages))

	if len(dbPages) == 0 && handleNotFound(w, r, "[HandleGetBookletPreviewPDF]", fmt.Sprintf("no pages found for document %s", docID), "no pages found for document") {
		return
	}

	// Download files
	downloadStart := time.Now()
	var localPagePaths []string
	for _, dbPage := range dbPages {
		localPath := filepath.Join(tempDir, fmt.Sprintf("page_%d.pdf", dbPage.PageNumber))
		logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Downloading storagePath=%s -> localPath=%s", dbPage.StoragePath, localPath)
		err := storage.DownloadFile(ctx, dbPage.StoragePath, localPath)
		if handleServerError(w, r, "[HandleGetBookletPreviewPDF]", "failed to download pages", err) {
			return
		}

		pdf.OptimizePagePDF(ctx, localPath)

		info, err := os.Stat(localPath)
		if err == nil {
			logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Downloaded and optimized page %d successfully. Size: %d bytes", dbPage.PageNumber, info.Size())
		}
		localPagePaths = append(localPagePaths, localPath)
	}
	logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Finished downloading all pages in %s", time.Since(downloadStart))

	// Merge files safely
	mergeStart := time.Now()
	logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Merging %d files safely...", len(localPagePaths))
	tempMergedPath, err := pdf.MergeFilesSafe(localPagePaths, tempDir)
	if handleServerError(w, r, "[HandleGetBookletPreviewPDF]", "failed to merge pages", err) {
		return
	}

	mergedInfo, err := os.Stat(tempMergedPath)
	if err == nil {
		logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Merged PDF created at %s, size: %d bytes (took %s)", tempMergedPath, mergedInfo.Size(), time.Since(mergeStart))
	} else {
		logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Merged PDF created at %s (took %s)", tempMergedPath, time.Since(mergeStart))
	}

	// Calculate layout sheets
	sheets := pdf.CalculateBookletLayout(len(dbPages), sigSize)
	if len(sheets) == 0 && handleServerError(w, r, "[HandleGetBookletPreviewPDF]", "invalid booklet layout", nil) {
		return
	}

	var targetSheet pdf.SheetSide
	if side == "back" {
		if len(sheets) > 1 {
			targetSheet = sheets[1]
		} else {
			targetSheet = sheets[0]
		}
	} else {
		targetSheet = sheets[0]
	}

	logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Target sheet pages: LeftPage=%d, RightPage=%d", targetSheet.LeftPage, targetSheet.RightPage)

	// Create new PDF document using gopdf
	pdfDoc := gopdf.GoPdf{}

	// Configure paper size
	var sheetWidth, sheetHeight float64
	if strings.ToLower(paperSize) == "letter" {
		// Letter Landscape: 8.5 x 11 in
		sheetWidth = 792.00
		sheetHeight = 612.00
	} else if strings.ToLower(paperSize) == "folio" {
		// Folio Landscape: 8.5 x 13 in
		sheetWidth = 936.00
		sheetHeight = 612.00
	} else {
		// Default A4 Landscape
		sheetWidth = 841.89
		sheetHeight = 595.28
	}

	pdfDoc.Start(gopdf.Config{PageSize: gopdf.Rect{W: sheetWidth, H: sheetHeight}})
	pdfDoc.AddPage()

	availWidth := sheetWidth - (2 * margin) - gutter
	slotWidth := availWidth / 2
	availHeight := sheetHeight - (2 * margin)

	// Map pages for easy lookup by 1-based page number
	pagesMap := make(map[int]pdf.DBPageInfo)
	for _, p := range dbPages {
		pagesMap[p.PageNumber] = p
	}

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

		var slotX float64
		if isRightSlot {
			slotX = margin + slotWidth + gutter
		} else {
			slotX = margin
		}
		slotY := margin

		scaleW := slotWidth / dbPage.Width
		scaleH := availHeight / dbPage.Height
		scale := math.Min(scaleW, scaleH)

		drawW := dbPage.Width * scale
		drawH := dbPage.Height * scale

		offsetX := slotX + (slotWidth-drawW)/2
		offsetY := slotY + (availHeight-drawH)/2

		// Import and place template safely with image rendering fallback if gofpdi panics/fails
		return pdf.DrawPageInSlotSafe(ctx, &pdfDoc, localPath, tempDir, pageNum, offsetX, offsetY, drawW, drawH)
	}

	if err := drawPageInSlot(targetSheet.LeftPage, false); err != nil {
		if handleServerError(w, r, "[HandleGetBookletPreviewPDF]", "failed to compile preview sheet", err) {
			return
		}
	}

	if err := drawPageInSlot(targetSheet.RightPage, true); err != nil {
		if handleServerError(w, r, "[HandleGetBookletPreviewPDF]", "failed to compile preview sheet", err) {
			return
		}
	}

	// Draw folding guidelines if enabled
	if guides {
		pdfDoc.SetLineWidth(0.5)
		pdfDoc.SetStrokeColor(180, 180, 180)
		pdfDoc.SetLineType("dashed")
		pdfDoc.Line(sheetWidth/2, 0, sheetWidth/2, sheetHeight)
		pdfDoc.SetLineType("solid")
	}

	localFilteredPath := filepath.Join(tempDir, "preview_sheet.pdf")
	err = pdfDoc.WritePdf(localFilteredPath)
	if handleServerError(w, r, "[HandleGetBookletPreviewPDF]", "failed to write preview sheet", err) {
		return
	}

	filteredInfo, err := os.Stat(localFilteredPath)
	if err == nil {
		logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Slice extraction complete: %s, size: %d bytes (took %s)", localFilteredPath, filteredInfo.Size(), time.Since(startTime))
	} else {
		logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Slice extraction complete: %s (took %s)", localFilteredPath, time.Since(startTime))
	}

	// Stream back
	f, err := os.Open(localFilteredPath)
	if handleServerError(w, r, "[HandleGetBookletPreviewPDF]", "failed to read preview sheet", err) {
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", "inline")
	if _, err := io.Copy(w, f); err != nil {
		logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Error: failed to stream preview PDF bytes: %v", err)
	}
	logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Preview PDF streamed successfully. Total elapsed handler time: %s", time.Since(startTime))
}

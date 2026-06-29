package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"booklet/db"
	"booklet/embeddings"
	"booklet/logger"
	"booklet/metrics"
	"booklet/pdf"
	"booklet/storage"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/signintech/gopdf"
)

type statusWriter struct {
	http.ResponseWriter
	statusCode int
}

// InstrumentHandler wraps http.HandlerFunc to export Prometheus metrics
func InstrumentHandler(path string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		
		sw := &statusWriter{ResponseWriter: w, statusCode: http.StatusOK}
		
		handler(sw, r)
		
		duration := time.Since(start).Seconds()
		statusStr := strconv.Itoa(sw.statusCode)
		
		metrics.HttpRequestsTotal.With(prometheus.Labels{
			"method": r.Method,
			"status": statusStr,
			"path":   path,
		}).Inc()
		
		metrics.HttpRequestDuration.With(prometheus.Labels{
			"method": r.Method,
			"path":   path,
		}).Observe(duration)
	}
}

func (sw *statusWriter) WriteHeader(statusCode int) {
	sw.statusCode = statusCode
	sw.ResponseWriter.WriteHeader(statusCode)
}

// 1. Document Handlers

type DocumentResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	TotalPages  int       `json:"total_pages"`
	SplitPages  int       `json:"split_pages"`
	ParsedPages int       `json:"parsed_pages"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func HandleListDocuments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		logger.Logf(r.Context(), "HandleListDocuments: method %s not allowed", r.Method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	logger.Logf(r.Context(), "HandleListDocuments: querying database for active documents")
	rows, err := db.DB.Query(`SELECT id, name, total_pages, split_pages, parsed_pages, status, created_at, updated_at FROM documents WHERE is_dismissed = FALSE ORDER BY created_at DESC`)
	if err != nil {
		logger.Logf(r.Context(), "Error: failed to query documents list: %v", err)
		http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	docs := []DocumentResponse{}
	for rows.Next() {
		var d DocumentResponse
		var id string
		if err := rows.Scan(&id, &d.Name, &d.TotalPages, &d.SplitPages, &d.ParsedPages, &d.Status, &d.CreatedAt, &d.UpdatedAt); err != nil {
			logger.Logf(r.Context(), "Error: failed to scan document row: %v", err)
			http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		d.ID = id
		docs = append(docs, d)
	}

	logger.Logf(r.Context(), "HandleListDocuments: successfully retrieved %d active documents", len(docs))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(docs)
}

type DocumentPageDetail struct {
	PageNumber int     `json:"page_number"`
	Text       string  `json:"text_preview"`
	Width      float64 `json:"width"`
	Height     float64 `json:"height"`
}

type DocumentDetailResponse struct {
	DocumentResponse
	Pages []DocumentPageDetail `json:"pages"`
}

func HandleDismissDocument(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		logger.Logf(r.Context(), "HandleDismissDocument: method %s not allowed", r.Method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	docID := r.PathValue("id")
	logger.Logf(r.Context(), "HandleDismissDocument: request to dismiss docID=%s", docID)
	if _, err := uuid.Parse(docID); err != nil {
		logger.Logf(r.Context(), "HandleDismissDocument: invalid UUID format: %s", docID)
		http.Error(w, "invalid UUID format", http.StatusBadRequest)
		return
	}

	_, err := db.DB.Exec(`UPDATE documents SET is_dismissed = TRUE, updated_at = CURRENT_TIMESTAMP WHERE id = $1`, docID)
	if err != nil {
		logger.Logf(r.Context(), "Error: failed to dismiss document %s: %v", docID, err)
		http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	logger.Logf(r.Context(), "Document %s dismissed successfully", docID)
	w.WriteHeader(http.StatusNoContent)
}

func HandleGetDocument(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		logger.Logf(r.Context(), "HandleGetDocument: method %s not allowed", r.Method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	docID := r.PathValue("id")
	logger.Logf(r.Context(), "HandleGetDocument: fetching document docID=%s", docID)
	if _, err := uuid.Parse(docID); err != nil {
		logger.Logf(r.Context(), "HandleGetDocument: invalid UUID format: %s", docID)
		http.Error(w, "invalid UUID format", http.StatusBadRequest)
		return
	}

	var d DocumentDetailResponse
	var id string
	err := db.DB.QueryRow(`
		SELECT id, name, total_pages, split_pages, parsed_pages, status, created_at, updated_at 
		FROM documents WHERE id = $1`, docID).Scan(&id, &d.Name, &d.TotalPages, &d.SplitPages, &d.ParsedPages, &d.Status, &d.CreatedAt, &d.UpdatedAt)
	
	if err == sql.ErrNoRows {
		logger.Logf(r.Context(), "GetDocument: document %s not found", docID)
		http.Error(w, "document not found", http.StatusNotFound)
		return
	} else if err != nil {
		logger.Logf(r.Context(), "Error: failed to query document %s: %v", docID, err)
		http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	d.ID = id

	logger.Logf(r.Context(), "HandleGetDocument: query metadata success, fetching pages for document %s", docID)
	// Fetch pages details
	rows, err := db.DB.Query(`
		SELECT page_number, text_content, width, height 
		FROM document_pages 
		WHERE document_id = $1 
		ORDER BY page_number ASC`, docID)
	
	if err != nil {
		logger.Logf(r.Context(), "Error: failed to query pages for document %s: %v", docID, err)
		http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var pages []DocumentPageDetail
	for rows.Next() {
		var p DocumentPageDetail
		if err := rows.Scan(&p.PageNumber, &p.Text, &p.Width, &p.Height); err != nil {
			logger.Logf(r.Context(), "Error: failed to scan page row for document %s: %v", docID, err)
			http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		// Truncate preview text
		if len(p.Text) > 200 {
			p.Text = p.Text[:200] + "..."
		}
		pages = append(pages, p)
	}
	d.Pages = pages

	logger.Logf(r.Context(), "HandleGetDocument: returning document details with %d pages", len(pages))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(d)
}

func HandleGetPagePDF(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		logger.Logf(r.Context(), "HandleGetPagePDF: method %s not allowed", r.Method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	docID := r.PathValue("id")
	pageNumStr := r.PathValue("page_number")
	logger.Logf(r.Context(), "HandleGetPagePDF: request page docID=%s pageNum=%s", docID, pageNumStr)

	if _, err := uuid.Parse(docID); err != nil {
		logger.Logf(r.Context(), "HandleGetPagePDF: invalid UUID format: %s", docID)
		http.Error(w, "invalid UUID format", http.StatusBadRequest)
		return
	}

	pageNum, err := strconv.Atoi(pageNumStr)
	if err != nil || pageNum < 1 {
		logger.Logf(r.Context(), "HandleGetPagePDF: invalid page number: %s", pageNumStr)
		http.Error(w, "invalid page number", http.StatusBadRequest)
		return
	}

	// Verify page exists and get storage path
	var storagePath string
	err = db.DB.QueryRow(`
		SELECT storage_path 
		FROM document_pages 
		WHERE document_id = $1 AND page_number = $2`, docID, pageNum).Scan(&storagePath)
	
	if err == sql.ErrNoRows {
		logger.Logf(r.Context(), "HandleGetPagePDF: page %d of document %s not found in DB", pageNum, docID)
		http.Error(w, "page not found", http.StatusNotFound)
		return
	} else if err != nil {
		logger.Logf(r.Context(), "Error: failed to query page PDF %s/%d: %v", docID, pageNum, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	logger.Logf(r.Context(), "HandleGetPagePDF: fetching storagePath=%s from MinIO", storagePath)
	// Get file from MinIO and stream it
	ctx := r.Context()
	object, err := storage.MinioClient.GetObject(ctx, storage.BucketName, storagePath, minio.GetObjectOptions{})
	if err != nil {
		logger.Logf(r.Context(), "Error: failed to get page PDF from MinIO: %v", err)
		http.Error(w, "failed to read page from storage", http.StatusInternalServerError)
		return
	}
	defer object.Close()

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", "inline")
	n, err := io.Copy(w, object)
	if err != nil {
		logger.Logf(r.Context(), "Error: failed to stream page PDF: %v", err)
	} else {
		logger.Logf(r.Context(), "HandleGetPagePDF: successfully streamed %d bytes of page PDF", n)
	}
}

func HandleGetBookletPreviewPDF(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		logger.Logf(r.Context(), "HandleGetBookletPreviewPDF: method %s not allowed", r.Method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	docID := r.PathValue("id")
	startTime := time.Now()
	logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Received preview request for docID=%s", docID)

	if _, err := uuid.Parse(docID); err != nil {
		logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Invalid UUID format: %s", docID)
		http.Error(w, "invalid UUID format", http.StatusBadRequest)
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
	if err != nil {
		logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Error: failed to create temp dir: %v", err)
		http.Error(w, "failed to create temp dir", http.StatusInternalServerError)
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
	
	if err != nil {
		logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Error: failed to query pages for preview: %v", err)
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var dbPages []pdf.DBPageInfo
	for rows.Next() {
		var p pdf.DBPageInfo
		if err := rows.Scan(&p.PageNumber, &p.StoragePath, &p.Width, &p.Height); err != nil {
			logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Error: failed to scan page info: %v", err)
			http.Error(w, "database error", http.StatusInternalServerError)
			return
		}
		dbPages = append(dbPages, p)
	}

	logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Found %d pages in DB for signature", len(dbPages))

	if len(dbPages) == 0 {
		logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Error: no pages found for document %s", docID)
		http.Error(w, "no pages found for document", http.StatusNotFound)
		return
	}

	// Download files
	downloadStart := time.Now()
	var localPagePaths []string
	for _, dbPage := range dbPages {
		localPath := filepath.Join(tempDir, fmt.Sprintf("page_%d.pdf", dbPage.PageNumber))
		logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Downloading storagePath=%s -> localPath=%s", dbPage.StoragePath, localPath)
		err := storage.DownloadFile(ctx, dbPage.StoragePath, localPath)
		if err != nil {
			logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Error: failed to download page %d: %v", dbPage.PageNumber, err)
			http.Error(w, "failed to download pages", http.StatusInternalServerError)
			return
		}
		
		info, err := os.Stat(localPath)
		if err == nil {
			logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Downloaded page %d successfully. Size: %d bytes", dbPage.PageNumber, info.Size())
		}
		localPagePaths = append(localPagePaths, localPath)
	}
	logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Finished downloading all pages in %s", time.Since(downloadStart))

	// Merge files safely
	mergeStart := time.Now()
	logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Merging %d files safely...", len(localPagePaths))
	tempMergedPath, err := pdf.MergeFilesSafe(localPagePaths, tempDir)
	if err != nil {
		logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Error: failed to merge pages: %v", err)
		http.Error(w, "failed to merge pages", http.StatusInternalServerError)
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
	if len(sheets) == 0 {
		logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Error: calculated layout has 0 sheets")
		http.Error(w, "invalid booklet layout", http.StatusInternalServerError)
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

		tplID := pdfDoc.ImportPage(localPath, 1, "/MediaBox")
		pdfDoc.UseImportedTemplate(tplID, offsetX, offsetY, drawW, drawH)

		return nil
	}

	if err := drawPageInSlot(targetSheet.LeftPage, false); err != nil {
		logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Error: failed to draw left page: %v", err)
		http.Error(w, "failed to compile preview sheet", http.StatusInternalServerError)
		return
	}

	if err := drawPageInSlot(targetSheet.RightPage, true); err != nil {
		logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Error: failed to draw right page: %v", err)
		http.Error(w, "failed to compile preview sheet", http.StatusInternalServerError)
		return
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
	if err != nil {
		logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Error: failed to write preview PDF: %v", err)
		http.Error(w, "failed to write preview sheet", http.StatusInternalServerError)
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
	if err != nil {
		logger.Logf(r.Context(), "[HandleGetBookletPreviewPDF] Error: failed to open filtered file: %v", err)
		http.Error(w, "failed to read preview sheet", http.StatusInternalServerError)
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

var processingSemaphore chan struct{}

func init() {
	maxParallel := 5
	if envVal := os.Getenv("MAX_PARALLEL_DOCUMENTS"); envVal != "" {
		if val, err := strconv.Atoi(envVal); err == nil && val > 0 {
			maxParallel = val
		}
	}
	processingSemaphore = make(chan struct{}, maxParallel)
}

func HandleUploadDocument(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		logger.Logf(r.Context(), "HandleUploadDocument: method %s not allowed", r.Method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 32 MB max memory for parsing form
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		logger.Logf(r.Context(), "Error: failed to parse multipart form for upload: %v", err)
		http.Error(w, "failed to parse multipart form", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		logger.Logf(r.Context(), "Error: missing file in upload request: %v", err)
		http.Error(w, "missing file in form-data", http.StatusBadRequest)
		return
	}
	defer file.Close()

	docID := uuid.New()
	logger.Logf(r.Context(), "HandleUploadDocument: starting upload for file=%s (docID=%s)", header.Filename, docID)
	
	// Create local temp file to inspect PDF page count and perform split
	tempDir, err := os.MkdirTemp("", "booklet-upload-*")
	if err != nil {
		logger.Logf(r.Context(), "Error: failed to create temp dir for upload: %v", err)
		http.Error(w, "failed to create temp dir", http.StatusInternalServerError)
		return
	}
	// We clean up the temp directory after processing in background worker, not here.

	localPath := filepath.Join(tempDir, header.Filename)
	outField, err := os.Create(localPath)
	if err != nil {
		os.RemoveAll(tempDir)
		logger.Logf(r.Context(), "Error: failed to create temp file %s: %v", localPath, err)
		http.Error(w, "failed to create temp file", http.StatusInternalServerError)
		return
	}
	
	if _, err := io.Copy(outField, file); err != nil {
		outField.Close()
		os.RemoveAll(tempDir)
		logger.Logf(r.Context(), "Error: failed to save uploaded file to %s: %v", localPath, err)
		http.Error(w, "failed to save uploaded file", http.StatusInternalServerError)
		return
	}
	outField.Close()

	// Insert document metadata with processing status
	_, err = db.DB.Exec(`
		INSERT INTO documents (id, name, total_pages, split_pages, parsed_pages, status, created_at, updated_at) 
		VALUES ($1, $2, $3, $4, $5, $6, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, 
		docID, header.Filename, 0, 0, 0, "queued")
	
	if err != nil {
		os.RemoveAll(tempDir)
		logger.Logf(r.Context(), "Error: failed to insert document %s metadata into database: %v", docID, err)
		http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	metrics.DocumentUploadsTotal.With(prometheus.Labels{"status": "queued"}).Inc()

	logger.Logf(r.Context(), "HandleUploadDocument: metadata inserted, starting background processing worker")
	// Spawn background worker to split pages, extract text, upload to MinIO and generate embeddings
	go runBackgroundDocumentProcessing(docID, localPath, tempDir)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"message":     "Document uploaded and processing started.",
		"document_id": docID.String(),
	})
}

func runBackgroundDocumentProcessing(docID uuid.UUID, localPath string, tempDir string) {
	start := time.Now()
	rl := logger.NewRequestLogger()
	ctx := logger.WithLogger(context.Background(), rl)
	success := false

	var processedPages int32 = 0
	var totalPagesVal int32 = 0
	var currentStep atomic.Value
	currentStep.Store("queued")

	stopTicker := make(chan struct{})

	// 1. Ticker and memory cleanup defer block (runs last)
	defer func() {
		close(stopTicker)
		duration := time.Since(start)
		rl.PrintTask(fmt.Sprintf("Document Processing (docID=%s)", docID), duration, success)
		if err := os.RemoveAll(tempDir); err != nil {
			log.Printf("Warning: failed to clean up temp dir %s: %v", tempDir, err)
		}
		if recovered := recover(); recovered != nil {
			rl.Logf("panic: background processing crashed for document %s: %v", docID, recovered)
			rl.PrintTask(fmt.Sprintf("Document Processing (docID=%s)", docID), time.Since(start), false)
			updateDocStatus(docID, "failed")
			metrics.DocumentUploadsTotal.With(prometheus.Labels{"status": "failed"}).Inc()
		}
		runtime.GC()
		debug.FreeOSMemory()
	}()

	// 2. Ticker goroutine
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				step := currentStep.Load().(string)
				total := atomic.LoadInt32(&totalPagesVal)
				processed := atomic.LoadInt32(&processedPages)
				if total > 0 {
					log.Printf("[Document Processing Progress (docID=%s)] Step: %s | Page %d/%d (%.1f%%)", docID, step, processed, total, float64(processed)/float64(total)*100)
				} else {
					log.Printf("[Document Processing Progress (docID=%s)] Step: %s (preparing document)", docID, step)
				}
			case <-stopTicker:
				return
			}
		}
	}()

	rl.Logf("Background processing queued for document: %s (%s)", localPath, docID)

	// 3. Acquire semaphore (runs first in cleanup order)
	processingSemaphore <- struct{}{}
	defer func() {
		<-processingSemaphore
	}()

	rl.Logf("Background processing started for document: %s (%s)", localPath, docID)

	// Get page count first
	totalPages, err := pdf.GetPageCount(localPath)
	if err != nil {
		rl.Logf("Error: failed to get page count for %s: %v", docID, err)
		updateDocStatus(docID, "failed")
		metrics.DocumentUploadsTotal.With(prometheus.Labels{"status": "failed"}).Inc()
		return
	}

	// Update total page count and status to processing in database immediately
	_, err = db.DB.Exec(`UPDATE documents SET total_pages = $1, status = 'processing', updated_at = CURRENT_TIMESTAMP WHERE id = $2`, totalPages, docID)
	if err != nil {
		rl.Logf("Error: failed to update page count and status for %s: %v", docID, err)
		updateDocStatus(docID, "failed")
		return
	}

	atomic.StoreInt32(&totalPagesVal, int32(totalPages))
	currentStep.Store("splitting PDF")

	var processedCount int32
	err = pdf.SplitDocument(ctx, docID.String(), localPath, func(current, total int, step string) {
		currentStep.Store(step)
		if step == "splitting PDF" {
			atomic.StoreInt32(&processedPages, int32(current))
			atomic.StoreInt32(&totalPagesVal, int32(total))
			// Dynamically update split_pages count in database during splitting
			_, _ = db.DB.Exec(`UPDATE documents SET split_pages = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`, current, docID)
		}
	}, func(page pdf.PageInfo) error {
		// Upload single page to MinIO
		objectName := fmt.Sprintf("documents/%s/pages/page_%d.pdf", docID, page.PageNumber)
		err = storage.UploadFile(ctx, objectName, page.LocalPath, "application/pdf")
		if err != nil {
			return fmt.Errorf("failed to upload page %d to MinIO: %w", page.PageNumber, err)
		}

		// Generate embedding
		embeddingVec, err := embeddings.ActiveEmbedder.Embed(ctx, page.Text)
		if err != nil {
			rl.Logf("Warning: failed to generate embedding for page %d of %s: %v", page.PageNumber, docID, err)
			embeddingVec = make([]float32, embeddings.ActiveEmbedder.Dimension())
		}

		// Convert vector array to PostgreSQL vector format string
		embeddingStr := db.Float32ArrayToString(embeddingVec)

		pageID := uuid.New()
		_, err = db.DB.Exec(`
			INSERT INTO document_pages (id, document_id, page_number, text_content, embedding, storage_path, width, height, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, CURRENT_TIMESTAMP)`,
			pageID, docID, page.PageNumber, page.Text, embeddingStr, objectName, page.Width, page.Height)
		if err != nil {
			return fmt.Errorf("failed to save page %d metadata: %w", page.PageNumber, err)
		}

		currentProcessed := atomic.AddInt32(&processedCount, 1)
		atomic.StoreInt32(&processedPages, currentProcessed)

		// Update parsed_pages and updated_at in documents table
		_, err = db.DB.Exec(`UPDATE documents SET parsed_pages = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`, currentProcessed, docID)
		if err != nil {
			rl.Logf("Warning: failed to update processed pages count: %v", err)
		}

		return nil
	})

	if err != nil {
		rl.Logf("Error: failed to split/process document %s: %v", docID, err)
		updateDocStatus(docID, "failed")
		metrics.DocumentUploadsTotal.With(prometheus.Labels{"status": "failed"}).Inc()
		return
	}

	updateDocStatus(docID, "ready")
	metrics.DocumentUploadsTotal.With(prometheus.Labels{"status": "success"}).Inc()
	rl.Logf("Background processing completed successfully for document: %s", docID)
	success = true
}

func updateDocStatus(id uuid.UUID, status string) {
	if status == "ready" {
		_, err := db.DB.Exec(`UPDATE documents SET status = $1, split_pages = total_pages, parsed_pages = total_pages, updated_at = CURRENT_TIMESTAMP WHERE id = $2`, status, id)
		if err != nil {
			log.Printf("Error: failed to update status for %s to %s: %v", id, status, err)
		}
	} else {
		_, err := db.DB.Exec(`UPDATE documents SET status = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`, status, id)
		if err != nil {
			log.Printf("Error: failed to update status for %s to %s: %v", id, status, err)
		}
	}
}

// 2. Booklet Handlers

type BookletCompileRequest struct {
	Margin        float64 `json:"margin"`
	Gutter        float64 `json:"gutter"`
	PaperSize     string  `json:"paper_size"`
	SignatureSize int     `json:"signature_size"`
	Guides        bool    `json:"guides"`
}

type BookletResponse struct {
	ID        string    `json:"id"`
	DocID     string    `json:"document_id"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type BookletListResponse struct {
	ID            string    `json:"id"`
	DocID         string    `json:"document_id"`
	DocName       string    `json:"document_name"`
	TotalPages    int       `json:"total_pages"`
	Status        string    `json:"status"`
	Margin        float64   `json:"config_margin"`
	Gutter        float64   `json:"config_gutter"`
	PaperSize     string    `json:"config_paper_size"`
	SignatureSize int       `json:"config_signature_size"`
	Guides        bool      `json:"config_guides"`
	CreatedAt     time.Time `json:"created_at"`
}

func HandleListBooklets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		logger.Logf(r.Context(), "HandleListBooklets: method %s not allowed", r.Method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rows, err := db.DB.Query(`
		SELECT 
			cb.id, 
			cb.document_id, 
			d.name, 
			d.total_pages,
			cb.status, 
			cb.config_margin, 
			cb.config_gutter, 
			cb.config_paper_size, 
			cb.config_signature_size, 
			cb.config_guides, 
			cb.created_at
		FROM compiled_booklets cb
		JOIN documents d ON cb.document_id = d.id
		ORDER BY cb.created_at DESC`)
	if err != nil {
		logger.Logf(r.Context(), "Error: failed to query booklets: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var list []BookletListResponse
	for rows.Next() {
		var item BookletListResponse
		err := rows.Scan(
			&item.ID,
			&item.DocID,
			&item.DocName,
			&item.TotalPages,
			&item.Status,
			&item.Margin,
			&item.Gutter,
			&item.PaperSize,
			&item.SignatureSize,
			&item.Guides,
			&item.CreatedAt,
		)
		if err != nil {
			logger.Logf(r.Context(), "Error: failed to scan booklet: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		list = append(list, item)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

func cleanOldBookletSessions(ctx context.Context, docID string, req BookletCompileRequest, currentBookletID uuid.UUID) {
	rows, err := db.DB.Query(`
		SELECT id, storage_path 
		FROM compiled_booklets
		WHERE document_id = $1
		  AND config_margin = $2
		  AND config_gutter = $3
		  AND config_paper_size = $4
		  AND config_signature_size = $5
		  AND config_guides = $6
		  AND id != $7`,
		docID, req.Margin, req.Gutter, req.PaperSize, req.SignatureSize, req.Guides, currentBookletID)
	if err != nil {
		logger.Logf(ctx, "Warning: failed to query old booklet sessions for cleanup: %v", err)
		return
	}
	defer rows.Close()

	var idsToDelete []string
	var pathsToDelete []string

	for rows.Next() {
		var id string
		var storagePath sql.NullString
		if err := rows.Scan(&id, &storagePath); err == nil {
			idsToDelete = append(idsToDelete, id)
			if storagePath.Valid && storagePath.String != "" {
				pathsToDelete = append(pathsToDelete, storagePath.String)
			}
		}
	}

	for _, path := range pathsToDelete {
		if err := storage.DeleteFile(ctx, path); err != nil {
			logger.Logf(ctx, "Warning: failed to delete old booklet file %s: %v", path, err)
		}
	}

	for _, id := range idsToDelete {
		_, err := db.DB.Exec(`DELETE FROM compiled_booklets WHERE id = $1`, id)
		if err != nil {
			logger.Logf(ctx, "Warning: failed to delete old booklet row %s: %v", id, err)
		} else {
			logger.Logf(ctx, "Cleaned up old booklet session %s", id)
		}
	}
}

func HandleCompileBooklet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		logger.Logf(r.Context(), "HandleCompileBooklet: method %s not allowed", r.Method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	docID := r.PathValue("id")
	logger.Logf(r.Context(), "HandleCompileBooklet: request compilation for docID=%s", docID)
	if _, err := uuid.Parse(docID); err != nil {
		logger.Logf(r.Context(), "HandleCompileBooklet: invalid UUID format: %s", docID)
		http.Error(w, "invalid UUID format", http.StatusBadRequest)
		return
	}

	var req BookletCompileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Logf(r.Context(), "Error: failed to decode booklet compile request JSON: %v", err)
		http.Error(w, "invalid JSON payload", http.StatusBadRequest)
		return
	}

	// Validate parameters
	if req.Margin < 0 {
		req.Margin = 12.0
	}
	if req.Gutter < 0 {
		req.Gutter = 24.0
	}
	if req.PaperSize == "" {
		req.PaperSize = "a4"
	}
	if req.SignatureSize <= 0 || req.SignatureSize%4 != 0 {
		req.SignatureSize = 4
	}

	logger.Logf(r.Context(), "HandleCompileBooklet: params - margin=%.2f gutter=%.2f paperSize=%s signatureSize=%d guides=%t", 
		req.Margin, req.Gutter, req.PaperSize, req.SignatureSize, req.Guides)

	// Verify document exists and is ready
	var docStatus string
	err := db.DB.QueryRow(`SELECT status FROM documents WHERE id = $1`, docID).Scan(&docStatus)
	if err == sql.ErrNoRows {
		logger.Logf(r.Context(), "CompileBooklet: document %s not found", docID)
		http.Error(w, "document not found", http.StatusNotFound)
		return
	} else if err != nil {
		logger.Logf(r.Context(), "Error: failed to check status for document %s during compile: %v", docID, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if docStatus != "ready" {
		logger.Logf(r.Context(), "CompileBooklet: document %s is in status '%s', not ready", docID, docStatus)
		http.Error(w, "document is not ready for booklet compilation", http.StatusConflict)
		return
	}

	// Check for a cached/in-progress booklet compilation
	var cachedID string
	var cachedStatus string
	err = db.DB.QueryRow(`
		SELECT id, status FROM compiled_booklets
		WHERE document_id = $1 
		  AND (status = 'ready' OR status = 'compiling')
		  AND config_margin = $2 
		  AND config_gutter = $3 
		  AND config_paper_size = $4 
		  AND config_signature_size = $5
		  AND config_guides = $6
		ORDER BY created_at DESC LIMIT 1`,
		docID, req.Margin, req.Gutter, req.PaperSize, req.SignatureSize, req.Guides).Scan(&cachedID, &cachedStatus)

	if err == nil {
		logger.Logf(r.Context(), "Found cached booklet compilation %s (status: %s) for document %s", cachedID, cachedStatus, docID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]string{
			"message":    "Booklet retrieved from cache.",
			"booklet_id": cachedID,
		})
		return
	} else if err != sql.ErrNoRows {
		logger.Logf(r.Context(), "Warning: failed to query cached booklets: %v", err)
	}

	bookletID := uuid.New()
	logger.Logf(r.Context(), "HandleCompileBooklet: inserting new compiled booklet row %s with status 'compiling'", bookletID)
	_, err = db.DB.Exec(`
		INSERT INTO compiled_booklets (id, document_id, status, config_margin, config_gutter, config_paper_size, config_signature_size, config_guides, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, CURRENT_TIMESTAMP)`,
		bookletID, docID, "compiling", req.Margin, req.Gutter, req.PaperSize, req.SignatureSize, req.Guides)
	
	if err != nil {
		logger.Logf(r.Context(), "Error: failed to insert compiled booklet %s for document %s: %v", bookletID, docID, err)
		http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Clean up any old sessions (e.g. failed ones) for the same document and config
	cleanOldBookletSessions(r.Context(), docID, req, bookletID)

	// Spawn background booklet compiler
	logger.Logf(r.Context(), "HandleCompileBooklet: starting background compiler task for bookletID=%s", bookletID)
	go runBackgroundBookletCompilation(bookletID, docID, req)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"message":    "Booklet compilation started.",
		"booklet_id": bookletID.String(),
	})
}

func runBackgroundBookletCompilation(bookletID uuid.UUID, docID string, req BookletCompileRequest) {
	start := time.Now()
	rl := logger.NewRequestLogger()
	ctx := logger.WithLogger(context.Background(), rl)
	success := false

	defer func() {
		duration := time.Since(start)
		rl.PrintTask(fmt.Sprintf("Booklet Compilation (bookletID=%s)", bookletID), duration, success)
		runtime.GC()
		debug.FreeOSMemory()
	}()

	rl.Logf("Background booklet compilation started for: %s", bookletID)

	// Fetch all document pages
	rows, err := db.DB.Query(`
		SELECT page_number, storage_path, width, height 
		FROM document_pages 
		WHERE document_id = $1 
		ORDER BY page_number ASC`, docID)
	
	if err != nil {
		rl.Logf("Error: failed to fetch pages for booklet %s: %v", bookletID, err)
		updateBookletStatus(ctx, bookletID, "failed", "")
		return
	}
	defer rows.Close()

	var dbPages []pdf.DBPageInfo
	for rows.Next() {
		var p pdf.DBPageInfo
		if err := rows.Scan(&p.PageNumber, &p.StoragePath, &p.Width, &p.Height); err != nil {
			rl.Logf("Error: failed to scan page info for booklet %s: %v", bookletID, err)
			updateBookletStatus(ctx, bookletID, "failed", "")
			return
		}
		dbPages = append(dbPages, p)
	}

	rl.Logf("Fetched %d pages, running CompileBooklet in pdf package", len(dbPages))
	// Run booklet compilation using GoPDF canvas layout
	storagePath, err := pdf.CompileBooklet(ctx, dbPages, pdf.BookletConfig{
		Margin:        req.Margin,
		Gutter:        req.Gutter,
		PaperSize:     req.PaperSize,
		SignatureSize: req.SignatureSize,
		Guides:        req.Guides,
	})

	if err != nil {
		rl.Logf("Error: booklet compilation failed for %s: %v", bookletID, err)
		updateBookletStatus(ctx, bookletID, "failed", "")
		return
	}

	rl.Logf("CompileBooklet complete, updating status to ready with path: %s", storagePath)
	updateBookletStatus(ctx, bookletID, "ready", storagePath)
	metrics.BookletCompilationDuration.Observe(time.Since(start).Seconds())
	rl.Logf("Background booklet compilation completed successfully for: %s", bookletID)
	success = true
}

func updateBookletStatus(ctx context.Context, id uuid.UUID, status string, storagePath string) {
	var err error
	if storagePath != "" {
		_, err = db.DB.Exec(`UPDATE compiled_booklets SET status = $1, storage_path = $2 WHERE id = $3`, status, storagePath, id)
	} else {
		_, err = db.DB.Exec(`UPDATE compiled_booklets SET status = $1 WHERE id = $2`, status, id)
	}
	if err != nil {
		logger.Logf(ctx, "Error: failed to update booklet status for %s to %s: %v", id, status, err)
	}
}

func HandleGetBooklet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		logger.Logf(r.Context(), "HandleGetBooklet: method %s not allowed", r.Method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	bookletID := r.PathValue("id")
	logger.Logf(r.Context(), "HandleGetBooklet: request status for bookletID=%s", bookletID)
	if _, err := uuid.Parse(bookletID); err != nil {
		logger.Logf(r.Context(), "HandleGetBooklet: invalid UUID format: %s", bookletID)
		http.Error(w, "invalid UUID format", http.StatusBadRequest)
		return
	}

	var b BookletResponse
	err := db.DB.QueryRow(`
		SELECT id, document_id, status, created_at 
		FROM compiled_booklets WHERE id = $1`, bookletID).Scan(&b.ID, &b.DocID, &b.Status, &b.CreatedAt)
	
	if err == sql.ErrNoRows {
		logger.Logf(r.Context(), "GetBooklet: booklet %s not found", bookletID)
		http.Error(w, "booklet not found", http.StatusNotFound)
		return
	} else if err != nil {
		logger.Logf(r.Context(), "Error: failed to query booklet %s: %v", bookletID, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	logger.Logf(r.Context(), "HandleGetBooklet: returned bookletID=%s status=%s", bookletID, b.Status)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(b)
}

func HandleDownloadBooklet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		logger.Logf(r.Context(), "HandleDownloadBooklet: method %s not allowed", r.Method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	bookletID := r.PathValue("id")
	logger.Logf(r.Context(), "HandleDownloadBooklet: request download for bookletID=%s", bookletID)
	if _, err := uuid.Parse(bookletID); err != nil {
		logger.Logf(r.Context(), "HandleDownloadBooklet: invalid UUID format: %s", bookletID)
		http.Error(w, "invalid UUID format", http.StatusBadRequest)
		return
	}

	var status, storagePath, paperSize, docID string
	var sigSize, totalOriginalPages int
	var margin, gutter float64
	var guides bool
	err := db.DB.QueryRow(`
		SELECT cb.status, cb.storage_path, cb.config_signature_size, d.total_pages, cb.config_paper_size, cb.document_id, cb.config_margin, cb.config_gutter, cb.config_guides
		FROM compiled_booklets cb
		JOIN documents d ON cb.document_id = d.id
		WHERE cb.id = $1`, bookletID).Scan(&status, &storagePath, &sigSize, &totalOriginalPages, &paperSize, &docID, &margin, &gutter, &guides)
	if err == sql.ErrNoRows {
		logger.Logf(r.Context(), "DownloadBooklet: booklet %s not found", bookletID)
		http.Error(w, "booklet not found", http.StatusNotFound)
		return
	} else if err != nil {
		logger.Logf(r.Context(), "Error: failed to query booklet %s: %v", bookletID, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if status != "ready" {
		logger.Logf(r.Context(), "DownloadBooklet: booklet %s is in status '%s', not ready for download", bookletID, status)
		http.Error(w, "booklet is not ready for download", http.StatusConflict)
		return
	}

	filter := r.URL.Query().Get("filter") // fronts, backs
	sheets := r.URL.Query().Get("sheets") // e.g. 1-10 or 12
	pagesParam := r.URL.Query().Get("pages") // booklet pages that were ruined, e.g. 13-16 or 14

	logger.Logf(r.Context(), "HandleDownloadBooklet: query params - filter=%q sheets=%q pagesParam=%q", filter, sheets, pagesParam)

	if pagesParam != "" {
		startPage := 1
		endPage := totalOriginalPages

		parts := strings.Split(pagesParam, "-")
		if len(parts) == 1 {
			if p, err := strconv.Atoi(parts[0]); err == nil {
				startPage = p
				endPage = p
			}
		} else if len(parts) == 2 {
			if p, err := strconv.Atoi(parts[0]); err == nil {
				startPage = p
			}
			if e, err := strconv.Atoi(parts[1]); err == nil {
				endPage = e
			}
		}

		// Map booklet pages to physical sheet range
		startSheet, endSheet := pdf.MapPagesToSheets(startPage, endPage)
		sheets = fmt.Sprintf("%d-%d", startSheet, endSheet)
		logger.Logf(r.Context(), "HandleDownloadBooklet: mapped pagesParam %s to sheet range %s", pagesParam, sheets)
	}

	ctx := r.Context()
	targetPath := storagePath

	// Apply filtering/slicing on-the-fly if requested
	if filter != "" || sheets != "" {
		logger.Logf(r.Context(), "HandleDownloadBooklet: slice requested. Slicing booklet targetPath=%s on-the-fly", targetPath)
		// Fetch original pages from DB to compile slice
		rows, err := db.DB.Query(`
			SELECT page_number, storage_path, width, height 
			FROM document_pages 
			WHERE document_id = $1
			ORDER BY page_number ASC`, docID)
		if err != nil {
			logger.Logf(r.Context(), "Error: failed to query pages for booklet slice %s: %v", bookletID, err)
			http.Error(w, "database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var dbPages []pdf.DBPageInfo
		for rows.Next() {
			var p pdf.DBPageInfo
			if err := rows.Scan(&p.PageNumber, &p.StoragePath, &p.Width, &p.Height); err != nil {
				logger.Logf(r.Context(), "Error: failed to scan page info for booklet slice %s: %v", bookletID, err)
				http.Error(w, "database error", http.StatusInternalServerError)
				return
			}
			dbPages = append(dbPages, p)
		}

		filteredKey, err := pdf.CompileBookletSlice(ctx, dbPages, pdf.BookletConfig{
			Margin:        margin,
			Gutter:        gutter,
			PaperSize:     paperSize,
			SignatureSize: sigSize,
			Guides:        guides,
		}, filter, sheets)

		if err != nil {
			logger.Logf(r.Context(), "Error: failed to slice booklet pages for %s: %v", bookletID, err)
			http.Error(w, "failed to slice booklet pages: "+err.Error(), http.StatusInternalServerError)
			return
		}
		targetPath = filteredKey
		logger.Logf(r.Context(), "HandleDownloadBooklet: compiled slice successfully, temporary slice storage path is %s", filteredKey)
		// Schedule clean up of temporary sliced files in MinIO after streaming
		defer func() {
			go func() {
				// Wait a brief moment to ensure connection closes, then delete the temp file
				time.Sleep(30 * time.Second)
				_ = storage.DeleteFile(context.Background(), filteredKey)
			}()
		}()
	}

	// Instead of redirecting directly, we download and stream the PDF to client to prevent CORS blocks
	// or we can redirect to the presigned URL. Since MinIO might be internal in docker-compose,
	// streaming the PDF directly from the backend is 100% reliable and SRE-friendly!
	logger.Logf(r.Context(), "Streaming PDF booklet %s to client...", targetPath)
	
	// Create a temporary file to download to
	tempDir, err := os.MkdirTemp("", "booklet-stream-*")
	if err != nil {
		logger.Logf(r.Context(), "Error: failed to create temp dir for streaming %s: %v", bookletID, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tempDir)

	tempFile := filepath.Join(tempDir, "temp.pdf")
	err = storage.DownloadFile(ctx, targetPath, tempFile)
	if err != nil {
		logger.Logf(r.Context(), "Error: failed to download booklet %s from storage: %v", bookletID, err)
		http.Error(w, "failed to stream from object store", http.StatusInternalServerError)
		return
	}

	f, err := os.Open(tempFile)
	if err != nil {
		logger.Logf(r.Context(), "Error: failed to open temp file %s for streaming: %v", tempFile, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", "attachment; filename=\"booklet.pdf\"")
	
	fi, err := f.Stat()
	if err == nil {
		w.Header().Set("Content-Length", strconv.FormatInt(fi.Size(), 10))
	}

	n, err := io.Copy(w, f)
	if err != nil {
		logger.Logf(r.Context(), "Error: failed to stream booklet PDF bytes: %v", err)
	} else {
		logger.Logf(r.Context(), "HandleDownloadBooklet: successfully streamed %d bytes of booklet PDF", n)
	}
}

// 3. Semantic Search Handler

type SearchResult struct {
	DocumentID string  `json:"document_id"`
	DocName    string  `json:"document_name"`
	PageNumber int     `json:"page_number"`
	Text       string  `json:"text_snippet"`
	Similarity float64 `json:"similarity"`
}

func HandleSemanticSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		logger.Logf(r.Context(), "HandleSemanticSearch: method %s not allowed", r.Method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query().Get("q")
	logger.Logf(r.Context(), "HandleSemanticSearch: query=%q", query)
	if query == "" {
		logger.Logf(r.Context(), "HandleSemanticSearch: missing query parameter 'q'")
		http.Error(w, "missing query parameter 'q'", http.StatusBadRequest)
		return
	}

	docFilter := r.URL.Query().Get("document_id")

	start := time.Now()
	ctx := r.Context()

	// Compute embedding for search query
	queryVec, err := embeddings.ActiveEmbedder.Embed(ctx, query)
	if err != nil {
		logger.Logf(r.Context(), "Error: failed to embed semantic search query: %v", err)
		http.Error(w, "failed to embed search query: "+err.Error(), http.StatusInternalServerError)
		return
	}

	queryVecStr := db.Float32ArrayToString(queryVec)

	// Perform cosine distance search
	// Cosine distance = 1 - Cosine Similarity.
	// pgvector <=> is cosine distance. So 1 - (embedding <=> queryVec) is the cosine similarity score.
	sqlQuery := `
		SELECT p.document_id, d.name, p.page_number, p.text_content, 
		       1 - (p.embedding <=> $1) as similarity
		FROM document_pages p
		JOIN documents d ON p.document_id = d.id
	`
	
	var args []interface{}
	args = append(args, queryVecStr)

	if docFilter != "" {
		if _, err := uuid.Parse(docFilter); err == nil {
			sqlQuery += " WHERE p.document_id = $2"
			args = append(args, docFilter)
			logger.Logf(r.Context(), "HandleSemanticSearch: filtering by document_id=%s", docFilter)
		}
	}

	sqlQuery += " ORDER BY p.embedding <=> $1 LIMIT 10"

	rows, err := db.DB.Query(sqlQuery, args...)
	if err != nil {
		logger.Logf(r.Context(), "Error: semantic search database query failed: %v", err)
		http.Error(w, "database query failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	results := []SearchResult{}
	for rows.Next() {
		var r SearchResult
		var docID string
		if err := rows.Scan(&docID, &r.DocName, &r.PageNumber, &r.Text, &r.Similarity); err != nil {
			logger.Logf(ctx, "Error: failed to scan semantic search row: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		r.DocumentID = docID
		
		// Create a smart snippet around matches or just truncate
		if len(r.Text) > 300 {
			// Find index of query word in text for better snippet context if possible
			lowerText := strings.ToLower(r.Text)
			lowerQuery := strings.ToLower(query)
			idx := strings.Index(lowerText, lowerQuery)
			if idx > 100 {
				r.Text = "..." + r.Text[idx-100:idx+200] + "..."
			} else {
				r.Text = r.Text[:300] + "..."
			}
		}

		results = append(results, r)
	}

	logger.Logf(ctx, "HandleSemanticSearch: returned %d results", len(results))
	metrics.VectorSearchDuration.Observe(time.Since(start).Seconds())

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

func HandleDocumentSearchPreviewPDF(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		logger.Logf(r.Context(), "HandleDocumentSearchPreviewPDF: method %s not allowed", r.Method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	docID := r.PathValue("id")
	q := r.URL.Query().Get("q")
	logger.Logf(r.Context(), "HandleDocumentSearchPreviewPDF: docID=%s q=%q", docID, q)

	if _, err := uuid.Parse(docID); err != nil {
		http.Error(w, "invalid UUID format", http.StatusBadRequest)
		return
	}

	if q == "" {
		http.Error(w, "missing query parameter 'q'", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// 1. Compute embedding for the search query
	queryVec, err := embeddings.ActiveEmbedder.Embed(ctx, q)
	if err != nil {
		logger.Logf(ctx, "Error: failed to embed search query: %v", err)
		http.Error(w, "failed to embed search query: "+err.Error(), http.StatusInternalServerError)
		return
	}
	queryVecStr := db.Float32ArrayToString(queryVec)

	// 2. Query top 10 matching page numbers and their storage paths for this document
	rows, err := db.DB.Query(`
		SELECT page_number, storage_path
		FROM document_pages
		WHERE document_id = $1
		ORDER BY embedding <=> $2
		LIMIT 10
	`, docID, queryVecStr)
	if err != nil {
		logger.Logf(ctx, "Error: failed to query matching pages: %v", err)
		http.Error(w, "database query failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type pageMatch struct {
		pageNum     int
		storagePath string
	}
	var matches []pageMatch
	for rows.Next() {
		var m pageMatch
		if err := rows.Scan(&m.pageNum, &m.storagePath); err != nil {
			logger.Logf(ctx, "Error: failed to scan page match: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		matches = append(matches, m)
	}

	if len(matches) == 0 {
		http.Error(w, "no matching pages found", http.StatusNotFound)
		return
	}

	// 3. Sort matches by page number ascending so the compiled PDF has logical page ordering
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].pageNum < matches[j].pageNum
	})

	// 4. Create a temporary directory to download the single-page PDFs
	tempDir, err := os.MkdirTemp("", "search-preview-*")
	if err != nil {
		logger.Logf(ctx, "Error: failed to create temp dir: %v", err)
		http.Error(w, "failed to create temporary workspace", http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tempDir)

	var localPaths []string
	for _, m := range matches {
		destPath := filepath.Join(tempDir, fmt.Sprintf("page_%d.pdf", m.pageNum))
		err := storage.DownloadFile(ctx, m.storagePath, destPath)
		if err != nil {
			logger.Logf(ctx, "Error: failed to download page %d: %v", m.pageNum, err)
			http.Error(w, "failed to download page from storage", http.StatusInternalServerError)
			return
		}
		localPaths = append(localPaths, destPath)
	}

	// 5. Merge the PDFs using MergeFilesSafe
	mergedPath, err := pdf.MergeFilesSafe(localPaths, tempDir)
	if err != nil {
		logger.Logf(ctx, "Error: failed to merge pages: %v", err)
		http.Error(w, "failed to generate preview PDF: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 6. Stream the merged PDF to the client
	f, err := os.Open(mergedPath)
	if err != nil {
		logger.Logf(ctx, "Error: failed to open merged PDF: %v", err)
		http.Error(w, "failed to read preview PDF", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", "inline")
	if _, err := io.Copy(w, f); err != nil {
		logger.Logf(ctx, "Error: failed to stream search preview PDF: %v", err)
	}
}

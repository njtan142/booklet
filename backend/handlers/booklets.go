package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"booklet/db"
	"booklet/logger"
	"booklet/metrics"
	"booklet/pdf"
	"booklet/permissions"
	"booklet/smtp"
	"booklet/storage"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/signintech/gopdf"
)

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

	// Dynamic check to fail/cleanup stale compiled booklets that haven't updated in 30 minutes
	_, err := db.DB.Exec(`
		UPDATE compiled_booklets 
		SET status = 'failed' 
		WHERE status = 'compiling' 
		  AND created_at < CURRENT_TIMESTAMP - INTERVAL '30 minutes'
	`)
	if err != nil {
		logger.Logf(r.Context(), "Warning: failed to dynamically clean up stale compiled booklets: %v", err)
	}

	// This join exposes document names, so it must be filtered by document
	// visibility too. Without the clause, user B can enumerate user A's document
	// titles through the booklet list without ever calling /api/documents.
	bookletQuery := `
		SELECT 
			cb.id, 
			cb.document_id, 
			d.name, 
			COALESCE(d.total_pages, 0),
			cb.status, 
			cb.config_margin, 
			cb.config_gutter, 
			cb.config_paper_size, 
			cb.config_signature_size, 
			cb.config_guides, 
			cb.created_at
		FROM compiled_booklets cb
		JOIN documents d ON cb.document_id = d.id`
	var bookletArgs []any
	if !permissions.IsAdmin(r) {
		userID := permissions.CurrentUserID(r)
		if userID == "" {
			logger.Logf(r.Context(), "HandleListBooklets: no authenticated user on request")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		clause, clauseArgs := permissions.VisibilityClause(userID, len(bookletArgs)+1, "d.")
		bookletQuery += " WHERE " + clause
		bookletArgs = append(bookletArgs, clauseArgs...)
	}
	bookletQuery += " ORDER BY cb.created_at DESC"

	rows, err := db.DB.Query(bookletQuery, bookletArgs...)
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

// enforceBookletAccess checks perm on the document a booklet was compiled from,
// writing the HTTP error response itself when access is refused.
//
// Booklets have no independent ownership: they inherit it from their parent
// document. Every booklet-keyed route must go through here, because a booklet id
// is otherwise an unguarded handle onto another user's document.
//
// A booklet whose parent is unreadable returns 404, identical to a booklet that
// does not exist.
func enforceBookletAccess(w http.ResponseWriter, r *http.Request, bookletID string, perm permissions.Perm) bool {
	if permissions.IsAdmin(r) {
		return true
	}

	var docID string
	err := db.DB.QueryRowContext(r.Context(),
		`SELECT document_id::text FROM compiled_booklets WHERE id = $1`, bookletID).Scan(&docID)
	if errors.Is(err, sql.ErrNoRows) {
		logger.Logf(r.Context(), "enforceBookletAccess: booklet %s not found", bookletID)
		http.Error(w, "booklet not found", http.StatusNotFound)
		return false
	}
	if err != nil {
		logger.Logf(r.Context(), "Error: failed to resolve parent document for booklet %s: %v", bookletID, err)
		http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
		return false
	}

	if !permissions.EnforceDocument(w, r, docID, perm) {
		return false
	}
	return true
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

type BookletCleanupRequest struct {
	Margin           float64 `json:"margin"`
	Gutter           float64 `json:"gutter"`
	PaperSize        string  `json:"paper_size"`
	SignatureSize    int     `json:"signature_size"`
	Guides           bool    `json:"guides"`
	CurrentBookletID string  `json:"current_booklet_id"`
}

func HandleCleanupBooklets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		logger.Logf(r.Context(), "HandleCleanupBooklets: method %s not allowed", r.Method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	docID := r.PathValue("id")
	if _, err := uuid.Parse(docID); err != nil {
		logger.Logf(r.Context(), "HandleCleanupBooklets: invalid UUID format: %s", docID)
		http.Error(w, "invalid UUID format", http.StatusBadRequest)
		return
	}

	// Cleanup deletes compiled artifacts belonging to this document.
	if !permissions.EnforceDocument(w, r, docID, permissions.PermWrite) {
		return
	}

	var req BookletCleanupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Logf(r.Context(), "Error: failed to decode booklet cleanup request JSON: %v", err)
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	currentUUID, err := uuid.Parse(req.CurrentBookletID)
	if err != nil {
		logger.Logf(r.Context(), "Error: invalid CurrentBookletID: %v", err)
		http.Error(w, "invalid CurrentBookletID", http.StatusBadRequest)
		return
	}

	compileReq := BookletCompileRequest{
		Margin:        req.Margin,
		Gutter:        req.Gutter,
		PaperSize:     req.PaperSize,
		SignatureSize: req.SignatureSize,
		Guides:        req.Guides,
	}

	cleanOldBookletSessions(r.Context(), docID, compileReq, currentUUID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "Old booklet sessions cleaned up successfully"})
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

	// Compiling writes a derived artifact from the document, so it needs write.
	if !permissions.EnforceDocument(w, r, docID, permissions.PermWrite) {
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

	if os.Getenv("SYNC_PROCESSING") == "true" {
		logger.Logf(r.Context(), "HandleCompileBooklet: executing compilation synchronously (serverless mode)")
		runBackgroundBookletCompilation(bookletID, docID, req)
	} else {
		// Spawn background booklet compiler
		logger.Logf(r.Context(), "HandleCompileBooklet: starting background compiler task for bookletID=%s", bookletID)
		go runBackgroundBookletCompilation(bookletID, docID, req)
	}

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

	if !enforceBookletAccess(w, r, bookletID, permissions.PermRead) {
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

	if !enforceBookletAccess(w, r, bookletID, permissions.PermRead) {
		return
	}

	var status, storagePath, paperSize, docID string
	var sigSize, totalOriginalPages int
	var margin, gutter float64
	var guides bool
	err := db.DB.QueryRow(`
		SELECT cb.status, cb.storage_path, cb.config_signature_size, COALESCE(d.total_pages, 0), cb.config_paper_size, cb.document_id, cb.config_margin, cb.config_gutter, cb.config_guides
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

	var localSliceFile string
	var tempSliceDir string

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

		tempSliceDir, err = os.MkdirTemp("", "booklet-slice-*")
		if err != nil {
			logger.Logf(r.Context(), "Error: failed to create temp dir for slice: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		defer os.RemoveAll(tempSliceDir)

		localSliceFile = filepath.Join(tempSliceDir, "slice.pdf")
		err = pdf.CompileBookletSlice(ctx, dbPages, pdf.BookletConfig{
			Margin:        margin,
			Gutter:        gutter,
			PaperSize:     paperSize,
			SignatureSize: sigSize,
			Guides:        guides,
		}, filter, sheets, localSliceFile)

		if err != nil {
			logger.Logf(r.Context(), "Error: failed to slice booklet pages for %s: %v", bookletID, err)
			http.Error(w, "failed to slice booklet pages: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	var streamFile string
	var tempStreamDir string

	if localSliceFile != "" {
		streamFile = localSliceFile
	} else {
		// Download the main compiled booklet from storage
		var err error
		tempStreamDir, err = os.MkdirTemp("", "booklet-stream-*")
		if err != nil {
			logger.Logf(r.Context(), "Error: failed to create temp dir for streaming %s: %v", bookletID, err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		defer os.RemoveAll(tempStreamDir)

		streamFile = filepath.Join(tempStreamDir, "temp.pdf")
		logger.Logf(r.Context(), "Streaming PDF booklet %s to client...", targetPath)
		err = storage.DownloadFile(ctx, targetPath, streamFile)
		if err != nil {
			logger.Logf(r.Context(), "Error: failed to download booklet %s from storage: %v", bookletID, err)
			http.Error(w, "failed to stream from object store", http.StatusInternalServerError)
			return
		}
	}

	f, err := os.Open(streamFile)
	if err != nil {
		logger.Logf(r.Context(), "Error: failed to open file %s for streaming: %v", streamFile, err)
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

// HandleEmailBooklet downloads a booklet PDF and sends it as an email attachment.
// Requires standard OIDC/mock user auth.
func HandleEmailBooklet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	bookletID := r.PathValue("id")
	if _, err := uuid.Parse(bookletID); err != nil {
		logger.Logf(r.Context(), "HandleEmailBooklet: invalid UUID format: %s", bookletID)
		http.Error(w, "invalid UUID format", http.StatusBadRequest)
		return
	}

	if !enforceBookletAccess(w, r, bookletID, permissions.PermRead) {
		return
	}

	type EmailRequest struct {
		Email string `json:"email"`
	}

	var req EmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Logf(r.Context(), "Error: failed to decode email request: %v", err)
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Email == "" {
		http.Error(w, "recipient email is required", http.StatusBadRequest)
		return
	}

	// Check if SMTP is configured
	smtpCfg, err := smtp.GetSMTPConfig(r.Context())
	if err != nil || !smtpCfg.IsConfigured() {
		logger.Logf(r.Context(), "HandleEmailBooklet: SMTP not configured or error: %v", err)
		http.Error(w, "SMTP server is not configured by the administrator", http.StatusServiceUnavailable)
		return
	}

	// Fetch booklet and original document name
	var status, storagePath, docName string
	err = db.DB.QueryRowContext(r.Context(), `
		SELECT cb.status, cb.storage_path, d.name
		FROM compiled_booklets cb
		JOIN documents d ON cb.document_id = d.id
		WHERE cb.id = $1
	`, bookletID).Scan(&status, &storagePath, &docName)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "booklet not found", http.StatusNotFound)
			return
		}
		logger.Logf(r.Context(), "Error: failed to fetch booklet metadata: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if status != "ready" || storagePath == "" {
		http.Error(w, "booklet is not compiled or compilation failed", http.StatusBadRequest)
		return
	}

	// Fetch PDF from MinIO
	object, err := storage.MinioClient.GetObject(r.Context(), storage.BucketName, storagePath, minio.GetObjectOptions{})
	if err != nil {
		logger.Logf(r.Context(), "Error: failed to get booklet PDF from MinIO: %v", err)
		http.Error(w, fmt.Sprintf("failed to retrieve PDF from storage: %v", err), http.StatusInternalServerError)
		return
	}
	defer object.Close()

	pdfBytes, err := io.ReadAll(object)
	if err != nil {
		logger.Logf(r.Context(), "Error: failed to read PDF data: %v", err)
		http.Error(w, fmt.Sprintf("failed to read booklet data: %v", err), http.StatusInternalServerError)
		return
	}

	// Compose Email
	attachmentName := fmt.Sprintf("%s_booklet.pdf", strings.ReplaceAll(docName, " ", "_"))
	subject := fmt.Sprintf("Your Booklet PDF: %s", docName)
	htmlBody := fmt.Sprintf(`
		<h3>Your Booklet is Ready!</h3>
		<p>Hi there,</p>
		<p>Please find attached the compiled PDF booklet for <strong>%s</strong> from Booklet Studio.</p>
		<p>Best regards,<br/>Booklet Studio Team</p>
	`, docName)

	err = smtp.SendEmail(r.Context(), smtpCfg, req.Email, subject, htmlBody, attachmentName, pdfBytes)
	if err != nil {
		logger.Logf(r.Context(), "Error: booklet email dispatch failed: %v", err)
		http.Error(w, fmt.Sprintf("failed to send booklet email: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": "Booklet PDF successfully emailed to " + req.Email,
	})
}

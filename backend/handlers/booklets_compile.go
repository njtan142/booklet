package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"runtime/debug"
	"time"

	"booklet/db"
	"booklet/logger"
	"booklet/metrics"
	"booklet/pdf"
	"booklet/permissions"
	"booklet/storage"

	"github.com/google/uuid"
)

// BookletCompileRequest holds the layout parameters for a booklet compilation job.
type BookletCompileRequest struct {
	Margin        float64 `json:"margin"`
	Gutter        float64 `json:"gutter"`
	PaperSize     string  `json:"paper_size"`
	SignatureSize int     `json:"signature_size"`
	Guides        bool    `json:"guides"`
}

// BookletCleanupRequest extends BookletCompileRequest with the ID of the booklet
// that should be preserved when cleaning up old sessions with matching config.
type BookletCleanupRequest struct {
	Margin           float64 `json:"margin"`
	Gutter           float64 `json:"gutter"`
	PaperSize        string  `json:"paper_size"`
	SignatureSize    int     `json:"signature_size"`
	Guides           bool    `json:"guides"`
	CurrentBookletID string  `json:"current_booklet_id"`
}

func HandleCompileBooklet(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "HandleCompileBooklet", http.MethodPost) {
		return
	}

	docID, ok := parseUUIDParam(w, r, "HandleCompileBooklet", "id")
	if !ok {
		return
	}
	logger.Logf(r.Context(), "HandleCompileBooklet: request compilation for docID=%s", docID)

	// Compiling writes a derived artifact from the document, so it needs write.
	if !permissions.EnforceDocument(w, r, docID, permissions.PermWrite) {
		return
	}

	var req BookletCompileRequest
	if !decodeJSON(w, r, "HandleCompileBooklet", &req) {
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
	if handleDBError(w, r, "CompileBooklet", "document not found", err) {
		return
	}

	if docStatus != "ready" && handleConflict(w, r, "CompileBooklet", fmt.Sprintf("document %s is in status '%s', not ready", docID, docStatus), "document is not ready for booklet compilation") {
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
		respondJSON(w, http.StatusAccepted, map[string]string{
			"message":    "Booklet retrieved from cache.",
			"booklet_id": cachedID,
		})
		return
	}

	bookletID := uuid.New()
	logger.Logf(r.Context(), "HandleCompileBooklet: inserting new compiled booklet row %s with status 'compiling'", bookletID)
	_, err = db.DB.Exec(`
		INSERT INTO compiled_booklets (id, document_id, status, config_margin, config_gutter, config_paper_size, config_signature_size, config_guides, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, CURRENT_TIMESTAMP)`,
		bookletID, docID, "compiling", req.Margin, req.Gutter, req.PaperSize, req.SignatureSize, req.Guides)

	if handleServerError(w, r, "HandleCompileBooklet", "database error", err) {
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

	respondJSON(w, http.StatusAccepted, map[string]string{
		"message":    "Booklet compilation started.",
		"booklet_id": bookletID.String(),
	})
}

func HandleCleanupBooklets(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "HandleCleanupBooklets", http.MethodPost) {
		return
	}

	docID, ok := parseUUIDParam(w, r, "HandleCleanupBooklets", "id")
	if !ok {
		return
	}

	// Cleanup deletes compiled artifacts belonging to this document.
	if !permissions.EnforceDocument(w, r, docID, permissions.PermWrite) {
		return
	}

	var req BookletCleanupRequest
	if !decodeJSON(w, r, "HandleCleanupBooklets", &req) {
		return
	}

	currentUUID, err := uuid.Parse(req.CurrentBookletID)
	if err != nil && handleBadRequest(w, r, "HandleCleanupBooklets", fmt.Sprintf("invalid CurrentBookletID: %v", err), "invalid CurrentBookletID") {
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

	respondJSON(w, http.StatusOK, map[string]string{"message": "Old booklet sessions cleaned up successfully"})
}

func runBackgroundBookletCompilation(bookletID uuid.UUID, docID string, req BookletCompileRequest) {
	start := time.Now()
	rl := logger.NewRequestLogger()
	ctx := logger.WithLogger(context.Background(), rl)
	success := false

	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			rl.Logf("PANIC in runBackgroundBookletCompilation: %v\nStack trace:\n%s", r, string(stack))
			updateBookletStatus(ctx, bookletID, "failed", "")
		}
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

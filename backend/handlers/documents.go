package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"booklet/db"
	"booklet/embeddings"
	"booklet/logger"
	"booklet/metrics"
	"booklet/pdf"
	"booklet/permissions"
	"booklet/storage"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/prometheus/client_golang/prometheus"
)

// 1. Document Handlers

type DocumentResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	TotalPages  int    `json:"total_pages"`
	SplitPages  int    `json:"split_pages"`
	ParsedPages int    `json:"parsed_pages"`
	Status      string `json:"status"`
	// Kind and MimeType drive the frontend tool menu: a tool declares the kinds
	// it accepts, so the client cannot offer Rotate on a DOCX source without
	// them. The API enforces the same rule, this only avoids offering the tool.
	Kind      string    `json:"kind"`
	MimeType  string    `json:"mime_type"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func HandleListDocuments(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "HandleListDocuments", http.MethodGet) {
		return
	}

	// Dynamic check to fail/cleanup stale documents: fail processing documents after 15 minutes of inactivity,
	// and fail queued documents if they have been waiting in the queue for more than 30 minutes.
	_, err := db.DB.Exec(`
		UPDATE documents 
		SET status = 'failed', updated_at = CURRENT_TIMESTAMP 
		WHERE (status = 'processing' AND updated_at < CURRENT_TIMESTAMP - INTERVAL '15 minutes')
		   OR (status = 'queued' AND updated_at < CURRENT_TIMESTAMP - INTERVAL '30 minutes')
	`)
	if err != nil {
		logger.Logf(r.Context(), "Warning: failed to dynamically clean up stale documents: %v", err)
	}

	logger.Logf(r.Context(), "HandleListDocuments: querying database for active documents")

	// Restrict the listing to documents the caller may read. Without this every
	// authenticated user sees every document in the system.
	// total_pages is nullable for non-paginated kinds ('source'/'export'), so
	// coalesce it rather than scanning NULL into an int.
	query := `SELECT id, name, COALESCE(total_pages, 0), split_pages, parsed_pages, status, kind, mime_type, created_at, updated_at
		FROM documents WHERE is_dismissed = FALSE`
	var args []any
	if !permissions.IsAdmin(r) {
		userID, ok := requireUser(w, r, "HandleListDocuments")
		if !ok {
			return
		}
		clause, clauseArgs := permissions.VisibilityClause(userID, len(args)+1, "")
		query += " AND " + clause
		args = append(args, clauseArgs...)
	}
	query += " ORDER BY created_at DESC"

	rows, err := db.DB.Query(query, args...)
	if handleServerError(w, r, "HandleListDocuments", "database error", err) {
		return
	}
	defer rows.Close()

	docs := []DocumentResponse{}
	for rows.Next() {
		var d DocumentResponse
		var id string
		if err := rows.Scan(&id, &d.Name, &d.TotalPages, &d.SplitPages, &d.ParsedPages, &d.Status, &d.Kind, &d.MimeType, &d.CreatedAt, &d.UpdatedAt); err != nil {
			if handleServerError(w, r, "HandleListDocuments", "database error", err) {
				return
			}
		}
		d.ID = id
		docs = append(docs, d)
	}

	logger.Logf(r.Context(), "HandleListDocuments: successfully retrieved %d active documents", len(docs))
	respondJSON(w, http.StatusOK, docs)
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

func HandleRenameDocument(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch && r.Method != http.MethodPost {
		handleMethodNotAllowed(w, r, "HandleRenameDocument")
		return
	}

	docID, ok := parseUUIDParam(w, r, "HandleRenameDocument", "id")
	if !ok {
		return
	}
	logger.Logf(r.Context(), "HandleRenameDocument: request to rename docID=%s", docID)

	if !permissions.EnforceDocument(w, r, docID, permissions.PermWrite) {
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, "HandleRenameDocument", &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" && handleBadRequest(w, r, "HandleRenameDocument", "empty name rejected", "name must not be empty") {
		return
	}

	_, err := db.DB.Exec(`UPDATE documents SET name = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`, req.Name, docID)
	if handleServerError(w, r, "HandleRenameDocument", "database error", err) {
		return
	}

	logger.Logf(r.Context(), "Document %s renamed to %q", docID, req.Name)
	respondJSON(w, http.StatusOK, map[string]string{"id": docID, "name": req.Name})
}

func deleteDocumentInternal(ctx context.Context, docID string) error {
	var originalStoragePath string
	err := db.DB.QueryRowContext(ctx, `SELECT original_storage_path FROM documents WHERE id = $1`, docID).Scan(&originalStoragePath)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("failed to query document: %w", err)
	}

	pageRows, err := db.DB.QueryContext(ctx, `SELECT storage_path FROM document_pages WHERE document_id = $1`, docID)
	var pagePaths []string
	if err == nil {
		for pageRows.Next() {
			var p string
			if scanErr := pageRows.Scan(&p); scanErr == nil && p != "" {
				pagePaths = append(pagePaths, p)
			}
		}
		pageRows.Close()
	}

	if _, err := db.DB.ExecContext(ctx, `DELETE FROM document_pages WHERE document_id = $1`, docID); err != nil {
		return fmt.Errorf("failed to delete page records: %w", err)
	}
	if _, err := db.DB.ExecContext(ctx, `DELETE FROM compiled_booklets WHERE document_id = $1`, docID); err != nil {
		return fmt.Errorf("failed to delete compiled booklets: %w", err)
	}
	if _, err := db.DB.ExecContext(ctx, `DELETE FROM documents WHERE id = $1`, docID); err != nil {
		return fmt.Errorf("failed to delete document row: %w", err)
	}

	if originalStoragePath != "" {
		if err := storage.DeleteFile(ctx, originalStoragePath); err != nil {
			logger.Logf(ctx, "Warning: failed to delete original file %s for document %s: %v", originalStoragePath, docID, err)
		}
	}
	for _, p := range pagePaths {
		if p != "" {
			if err := storage.DeleteFile(ctx, p); err != nil {
				logger.Logf(ctx, "Warning: failed to delete page file %s for document %s: %v", p, docID, err)
			}
		}
	}
	return nil
}

func HandleDeleteDocument(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		handleMethodNotAllowed(w, r, "HandleDeleteDocument")
		return
	}

	docID, ok := parseUUIDParam(w, r, "HandleDeleteDocument", "id")
	if !ok {
		return
	}
	logger.Logf(r.Context(), "HandleDeleteDocument: request to delete docID=%s", docID)

	if !permissions.EnforceDocument(w, r, docID, permissions.PermWrite) {
		return
	}

	if err := deleteDocumentInternal(r.Context(), docID); handleServerError(w, r, "HandleDeleteDocument", "failed to delete document", err) {
		return
	}

	logger.Logf(r.Context(), "Document %s deleted successfully", docID)
	w.WriteHeader(http.StatusNoContent)
}

type BulkDeleteRequest struct {
	IDs []string `json:"ids"`
}

type BulkDeleteResponse struct {
	DeletedCount int      `json:"deleted_count"`
	DeletedIDs   []string `json:"deleted_ids"`
}

func HandleBulkDeleteDocuments(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "HandleBulkDeleteDocuments", http.MethodPost) {
		return
	}

	var req BulkDeleteRequest
	if !decodeJSON(w, r, "HandleBulkDeleteDocuments", &req) {
		return
	}

	if len(req.IDs) == 0 {
		respondJSON(w, http.StatusOK, BulkDeleteResponse{DeletedCount: 0, DeletedIDs: []string{}})
		return
	}

	var validIDs []string
	for _, id := range req.IDs {
		trimmed := strings.TrimSpace(id)
		if trimmed != "" {
			if _, err := uuid.Parse(trimmed); err == nil {
				validIDs = append(validIDs, trimmed)
			}
		}
	}

	if len(validIDs) == 0 {
		respondJSON(w, http.StatusOK, BulkDeleteResponse{DeletedCount: 0, DeletedIDs: []string{}})
		return
	}

	ctx := r.Context()
	var targetIDs []string

	if permissions.IsAdmin(r) {
		targetIDs = validIDs
	} else {
		userID, ok := requireUser(w, r, "HandleBulkDeleteDocuments")
		if !ok {
			return
		}
		_, denied, err := permissions.CheckMany(ctx, validIDs, userID, permissions.PermWrite)
		if handleServerError(w, r, "HandleBulkDeleteDocuments", "permission check failed", err) {
			return
		}
		deniedMap := make(map[string]bool)
		for _, d := range denied {
			deniedMap[d] = true
		}
		for _, id := range validIDs {
			if !deniedMap[id] {
				targetIDs = append(targetIDs, id)
			}
		}
	}

	if len(targetIDs) == 0 {
		respondJSON(w, http.StatusOK, BulkDeleteResponse{DeletedCount: 0, DeletedIDs: []string{}})
		return
	}

	var deletedIDs []string
	for _, docID := range targetIDs {
		if err := deleteDocumentInternal(ctx, docID); err != nil {
			logger.Logf(ctx, "HandleBulkDeleteDocuments: failed to delete doc %s: %v", docID, err)
			continue
		}
		deletedIDs = append(deletedIDs, docID)
	}

	logger.Logf(ctx, "HandleBulkDeleteDocuments: deleted %d documents", len(deletedIDs))
	respondJSON(w, http.StatusOK, BulkDeleteResponse{
		DeletedCount: len(deletedIDs),
		DeletedIDs:   deletedIDs,
	})
}


func HandleDismissDocument(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "HandleDismissDocument", http.MethodPost) {
		return
	}

	docID, ok := parseUUIDParam(w, r, "HandleDismissDocument", "id")
	if !ok {
		return
	}
	logger.Logf(r.Context(), "HandleDismissDocument: request to dismiss docID=%s", docID)

	if !permissions.EnforceDocument(w, r, docID, permissions.PermWrite) {
		return
	}

	_, err := db.DB.Exec(`UPDATE documents SET is_dismissed = TRUE, updated_at = CURRENT_TIMESTAMP WHERE id = $1`, docID)
	if handleServerError(w, r, "HandleDismissDocument", "database error", err) {
		return
	}

	_, err = db.DB.Exec(`DELETE FROM compiled_booklets WHERE document_id = $1`, docID)
	if err != nil {
		logger.Logf(r.Context(), "Warning: failed to delete booklets for dismissed document %s: %v", docID, err)
	}

	logger.Logf(r.Context(), "Document %s dismissed successfully", docID)
	w.WriteHeader(http.StatusNoContent)
}

func HandleGetDocument(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "HandleGetDocument", http.MethodGet) {
		return
	}

	docID, ok := parseUUIDParam(w, r, "HandleGetDocument", "id")
	if !ok {
		return
	}
	logger.Logf(r.Context(), "HandleGetDocument: fetching document docID=%s", docID)

	if !permissions.EnforceDocument(w, r, docID, permissions.PermRead) {
		return
	}

	var d DocumentDetailResponse
	var id string
	err := db.DB.QueryRow(`
		SELECT id, name, COALESCE(total_pages, 0), split_pages, parsed_pages, status, kind, mime_type, created_at, updated_at 
		FROM documents WHERE id = $1`, docID).Scan(&id, &d.Name, &d.TotalPages, &d.SplitPages, &d.ParsedPages, &d.Status, &d.Kind, &d.MimeType, &d.CreatedAt, &d.UpdatedAt)

	if handleDBError(w, r, "GetDocument", "document not found", err) {
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

	if handleServerError(w, r, "HandleGetDocument", "database error", err) {
		return
	}
	defer rows.Close()

	var pages []DocumentPageDetail
	for rows.Next() {
		var p DocumentPageDetail
		if err := rows.Scan(&p.PageNumber, &p.Text, &p.Width, &p.Height); err != nil {
			if handleServerError(w, r, "HandleGetDocument", "database error", err) {
				return
			}
		}
		// Truncate preview text
		if len(p.Text) > 200 {
			p.Text = p.Text[:200] + "..."
		}
		pages = append(pages, p)
	}
	d.Pages = pages

	logger.Logf(r.Context(), "HandleGetDocument: returning document details with %d pages", len(pages))
	respondJSON(w, http.StatusOK, d)
}

func HandleGetPagePDF(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "HandleGetPagePDF", http.MethodGet) {
		return
	}

	docID, ok := parseUUIDParam(w, r, "HandleGetPagePDF", "id")
	if !ok {
		return
	}
	pageNumStr := r.PathValue("page_number")
	logger.Logf(r.Context(), "HandleGetPagePDF: request page docID=%s pageNum=%s", docID, pageNumStr)

	pageNum, err := strconv.Atoi(pageNumStr)
	if (err != nil || pageNum < 1) && handleBadRequest(w, r, "HandleGetPagePDF", "invalid page number", "invalid page number") {
		return
	}

	if !permissions.EnforceDocument(w, r, docID, permissions.PermRead) {
		return
	}

	// Verify page exists and get storage path
	var storagePath string
	err = db.DB.QueryRow(`
		SELECT storage_path 
		FROM document_pages 
		WHERE document_id = $1 AND page_number = $2`, docID, pageNum).Scan(&storagePath)

	if handleDBError(w, r, "HandleGetPagePDF", "page not found", err) {
		return
	}

	logger.Logf(r.Context(), "HandleGetPagePDF: fetching storagePath=%s from MinIO", storagePath)
	// Get file from MinIO and stream it
	ctx := r.Context()
	object, err := storage.MinioClient.GetObject(ctx, storage.BucketName, storagePath, minio.GetObjectOptions{})
	if handleServerError(w, r, "HandleGetPagePDF", "failed to read page from storage", err) {
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
	if !requireMethod(w, r, "HandleUploadDocument", http.MethodPost) {
		return
	}

	// 32 MB max memory for parsing form
	if err := r.ParseMultipartForm(32 << 20); handleServerError(w, r, "HandleUploadDocument", "failed to parse multipart form", err) {
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil && handleBadRequest(w, r, "HandleUploadDocument", "missing file in upload request", "missing file in form-data") {
		return
	}
	defer file.Close()

	// Resolve the owner before touching storage: an upload with no owner would be
	// an unreachable row under the permission model.
	ownerID, ok := requireUser(w, r, "HandleUploadDocument")
	if !ok {
		return
	}
	groupID, err := db.PrimaryGroupID(ownerID)
	if handleServerError(w, r, "HandleUploadDocument", "failed to resolve user group", err) {
		return
	}

	docID := uuid.New()
	logger.Logf(r.Context(), "HandleUploadDocument: starting upload for file=%s (docID=%s owner=%s)", header.Filename, docID, ownerID)

	// Create local temp file to inspect PDF page count and perform split
	tempDir, err := os.MkdirTemp("", "booklet-upload-*")
	if handleServerError(w, r, "HandleUploadDocument", "failed to create temp dir", err) {
		return
	}
	// We clean up the temp directory after processing in background worker, not here.

	localPath := filepath.Join(tempDir, header.Filename)
	outField, err := os.Create(localPath)
	if err != nil {
		os.RemoveAll(tempDir)
		if handleServerError(w, r, "HandleUploadDocument", "failed to create temp file", err) {
			return
		}
	}

	if _, err := io.Copy(outField, file); err != nil {
		outField.Close()
		os.RemoveAll(tempDir)
		if handleServerError(w, r, "HandleUploadDocument", "failed to save uploaded file", err) {
			return
		}
	}
	outField.Close()

	// Upload original PDF to MinIO
	originalKey := fmt.Sprintf("documents/%s/original.pdf", docID)
	err = storage.UploadFile(r.Context(), originalKey, localPath, "application/pdf")
	if err != nil {
		os.RemoveAll(tempDir)
		if handleServerError(w, r, "HandleUploadDocument", "failed to upload original PDF to storage", err) {
			return
		}
	}

	// Insert document metadata with processing status
	_, err = db.DB.Exec(`
		INSERT INTO documents (id, name, total_pages, split_pages, parsed_pages, status, original_storage_path,
		                       owner_id, group_id, mode, kind, mime_type, original_filename, created_at, updated_at) 
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'pdf', 'application/pdf', $11, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		docID, header.Filename, 0, 0, 0, "queued", originalKey, ownerID, groupID, db.ModeDefault, header.Filename)

	if err != nil {
		os.RemoveAll(tempDir)
		if handleServerError(w, r, "HandleUploadDocument", "database error", err) {
			return
		}
	}

	metrics.DocumentUploadsTotal.With(prometheus.Labels{"status": "queued"}).Inc()

	if os.Getenv("SYNC_PROCESSING") == "true" {
		logger.Logf(r.Context(), "HandleUploadDocument: executing document processing synchronously (serverless mode)")
		runBackgroundDocumentProcessing(docID, localPath, tempDir)
	} else {
		logger.Logf(r.Context(), "HandleUploadDocument: metadata inserted, starting background processing worker")
		// Spawn background worker to split pages, extract text, upload to MinIO and generate embeddings
		go runBackgroundDocumentProcessing(docID, localPath, tempDir)
	}

	respondJSON(w, http.StatusAccepted, map[string]string{
		"message":     "Document uploaded and processing started.",
		"document_id": docID.String(),
	})
}

func runBackgroundDocumentProcessing(docID uuid.UUID, localPath string, tempDir string) {
	start := time.Now()
	rl := logger.NewRequestLogger()
	ctx := logger.WithLogger(context.Background(), rl)
	success := false

	var existingPages int32 = 0
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM document_pages WHERE document_id = $1`, docID).Scan(&existingPages)

	processedPages := existingPages
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

	processedCount := existingPages
	err = pdf.SplitDocument(ctx, docID.String(), localPath, func(current, total int, step string) {
		currentStep.Store(step)
		if step == "splitting PDF" {
			// When splitting, the progress is calculated relative to the split count,
			// but we keep the database split_pages up to date.
			atomic.StoreInt32(&processedPages, int32(current))
			atomic.StoreInt32(&totalPagesVal, int32(total))
			// Dynamically update split_pages count in database during splitting
			_, _ = db.DB.Exec(`UPDATE documents SET split_pages = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`, current, docID)
		}
	}, func(page pdf.PageInfo) error {
		// Check if page already exists in document_pages ledger to support resumption
		var exists bool
		err = db.DB.QueryRow(`
			SELECT EXISTS(
				SELECT 1 FROM document_pages 
				WHERE document_id = $1 AND page_number = $2
			)`, docID, page.PageNumber).Scan(&exists)
		if err != nil {
			return fmt.Errorf("failed to check page existence in ledger: %w", err)
		}
		if exists {
			logger.Logf(ctx, "Page %d of document %s already exists in ledger. Skipping.", page.PageNumber, docID)
			return nil
		}

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

		// Update parsed_pages and updated_at in documents table periodically to minimize DB query overhead
		if currentProcessed%10 == 0 || int(currentProcessed) == totalPages {
			_, err = db.DB.Exec(`
				UPDATE documents 
				SET parsed_pages = $1, 
				    updated_at = CURRENT_TIMESTAMP 
				WHERE id = $2`, currentProcessed, docID)
			if err != nil {
				rl.Logf("Warning: failed to update processed pages count: %v", err)
			}
		}

		// Periodically release memory back to the OS during large document processing
		if currentProcessed%100 == 0 {
			runtime.GC()
			debug.FreeOSMemory()
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
	query := `UPDATE documents SET status = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`
	if status == "ready" {
		query = `UPDATE documents SET status = $1, split_pages = total_pages, parsed_pages = total_pages, updated_at = CURRENT_TIMESTAMP WHERE id = $2`
	}
	if _, err := db.DB.Exec(query, status, id); err != nil {
		logger.Logf(context.Background(), "Error: failed to update status for %s to %s: %v", id, status, err)
	}
}

func HandleResumeDocument(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "HandleResumeDocument", http.MethodPost) {
		return
	}

	docIDStr, ok := parseUUIDParam(w, r, "HandleResumeDocument", "id")
	if !ok {
		return
	}
	logger.Logf(r.Context(), "HandleResumeDocument: request to resume docID=%s", docIDStr)
	docID := uuid.MustParse(docIDStr)

	if !permissions.EnforceDocument(w, r, docIDStr, permissions.PermWrite) {
		return
	}

	var status, originalStoragePath, name string
	err := db.DB.QueryRow(`
		SELECT status, original_storage_path, name 
		FROM documents WHERE id = $1`, docID).Scan(&status, &originalStoragePath, &name)

	if handleDBError(w, r, "HandleResumeDocument", "document not found", err) {
		return
	}

	if status == "ready" {
		logger.Logf(r.Context(), "HandleResumeDocument: document %s is already ready", docID)
		respondJSON(w, http.StatusOK, map[string]string{"message": "Document is already fully processed", "document_id": docID.String()})
		return
	}

	if originalStoragePath == "" && handleConflict(w, r, "HandleResumeDocument", "missing original storage path", "original document file is missing, cannot resume") {
		return
	}

	// Create local temp file/dir to download original PDF
	tempDir, err := os.MkdirTemp("", "booklet-resume-*")
	if handleServerError(w, r, "HandleResumeDocument", "failed to create temp dir", err) {
		return
	}

	localPath := filepath.Join(tempDir, name)
	err = storage.DownloadFile(r.Context(), originalStoragePath, localPath)
	if err != nil {
		os.RemoveAll(tempDir)
		if handleServerError(w, r, "HandleResumeDocument", "failed to retrieve original file", err) {
			return
		}
	}

	// Update status back to queued/processing so client knows it is running
	_, _ = db.DB.Exec(`UPDATE documents SET status = 'queued', updated_at = CURRENT_TIMESTAMP WHERE id = $1`, docID)

	if os.Getenv("SYNC_PROCESSING") == "true" {
		logger.Logf(r.Context(), "HandleResumeDocument: executing document processing synchronously (serverless mode)")
		runBackgroundDocumentProcessing(docID, localPath, tempDir)
	} else {
		logger.Logf(r.Context(), "HandleResumeDocument: started background processing worker for resumption")
		go runBackgroundDocumentProcessing(docID, localPath, tempDir)
	}

	respondJSON(w, http.StatusAccepted, map[string]string{
		"message":     "Document processing resumed successfully.",
		"document_id": docID.String(),
	})
}

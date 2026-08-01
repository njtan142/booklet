package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
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
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	TotalPages  int       `json:"total_pages"`
	SplitPages  int       `json:"split_pages"`
	ParsedPages int       `json:"parsed_pages"`
	Status      string    `json:"status"`
	// Kind and MimeType drive the frontend tool menu: a tool declares the kinds
	// it accepts, so the client cannot offer Rotate on a DOCX source without
	// them. The API enforces the same rule, this only avoids offering the tool.
	Kind      string    `json:"kind"`
	MimeType  string    `json:"mime_type"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func HandleListDocuments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		logger.Logf(r.Context(), "HandleListDocuments: method %s not allowed", r.Method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
		userID := permissions.CurrentUserID(r)
		if userID == "" {
			logger.Logf(r.Context(), "HandleListDocuments: no authenticated user on request")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		clause, clauseArgs := permissions.VisibilityClause(userID, len(args)+1, "")
		query += " AND " + clause
		args = append(args, clauseArgs...)
	}
	query += " ORDER BY created_at DESC"

	rows, err := db.DB.Query(query, args...)
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
		if err := rows.Scan(&id, &d.Name, &d.TotalPages, &d.SplitPages, &d.ParsedPages, &d.Status, &d.Kind, &d.MimeType, &d.CreatedAt, &d.UpdatedAt); err != nil {
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

	if !permissions.EnforceDocument(w, r, docID, permissions.PermWrite) {
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

	if !permissions.EnforceDocument(w, r, docID, permissions.PermRead) {
		return
	}

	var d DocumentDetailResponse
	var id string
	err := db.DB.QueryRow(`
		SELECT id, name, COALESCE(total_pages, 0), split_pages, parsed_pages, status, kind, mime_type, created_at, updated_at 
		FROM documents WHERE id = $1`, docID).Scan(&id, &d.Name, &d.TotalPages, &d.SplitPages, &d.ParsedPages, &d.Status, &d.Kind, &d.MimeType, &d.CreatedAt, &d.UpdatedAt)
	
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

	if !permissions.EnforceDocument(w, r, docID, permissions.PermRead) {
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

	// Resolve the owner before touching storage: an upload with no owner would be
	// an unreachable row under the permission model.
	ownerID := permissions.CurrentUserID(r)
	if ownerID == "" {
		logger.Logf(r.Context(), "HandleUploadDocument: no authenticated user on upload request")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	groupID, err := db.PrimaryGroupID(ownerID)
	if err != nil {
		logger.Logf(r.Context(), "Error: failed to resolve primary group for %s: %v", ownerID, err)
		http.Error(w, "failed to resolve user group", http.StatusInternalServerError)
		return
	}

	docID := uuid.New()
	logger.Logf(r.Context(), "HandleUploadDocument: starting upload for file=%s (docID=%s owner=%s)", header.Filename, docID, ownerID)
	
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

	// Upload original PDF to MinIO
	originalKey := fmt.Sprintf("documents/%s/original.pdf", docID)
	err = storage.UploadFile(r.Context(), originalKey, localPath, "application/pdf")
	if err != nil {
		os.RemoveAll(tempDir)
		logger.Logf(r.Context(), "Error: failed to upload original PDF %s to MinIO: %v", docID, err)
		http.Error(w, "storage error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Insert document metadata with processing status
	_, err = db.DB.Exec(`
		INSERT INTO documents (id, name, total_pages, split_pages, parsed_pages, status, original_storage_path,
		                       owner_id, group_id, mode, kind, mime_type, original_filename, created_at, updated_at) 
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'pdf', 'application/pdf', $11, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, 
		docID, header.Filename, 0, 0, 0, "queued", originalKey, ownerID, groupID, db.ModeDefault, header.Filename)
	
	if err != nil {
		os.RemoveAll(tempDir)
		logger.Logf(r.Context(), "Error: failed to insert document %s metadata into database: %v", docID, err)
		http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
		return
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

		// Update parsed_pages and updated_at in documents table using an exact count of the processed pages to prevent race conditions
		_, err = db.DB.Exec(`
			UPDATE documents 
			SET parsed_pages = (SELECT COUNT(*) FROM document_pages WHERE document_id = $1), 
			    updated_at = CURRENT_TIMESTAMP 
			WHERE id = $1`, docID)
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

func HandleResumeDocument(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		logger.Logf(r.Context(), "HandleResumeDocument: method %s not allowed", r.Method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	docIDStr := r.PathValue("id")
	logger.Logf(r.Context(), "HandleResumeDocument: request to resume docID=%s", docIDStr)
	docID, err := uuid.Parse(docIDStr)
	if err != nil {
		logger.Logf(r.Context(), "HandleResumeDocument: invalid UUID format: %s", docIDStr)
		http.Error(w, "invalid UUID format", http.StatusBadRequest)
		return
	}

	if !permissions.EnforceDocument(w, r, docIDStr, permissions.PermWrite) {
		return
	}

	var status, originalStoragePath, name string
	err = db.DB.QueryRow(`
		SELECT status, original_storage_path, name 
		FROM documents WHERE id = $1`, docID).Scan(&status, &originalStoragePath, &name)
	
	if err == sql.ErrNoRows {
		logger.Logf(r.Context(), "HandleResumeDocument: document %s not found", docID)
		http.Error(w, "document not found", http.StatusNotFound)
		return
	} else if err != nil {
		logger.Logf(r.Context(), "Error: failed to query document details: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if status == "ready" {
		logger.Logf(r.Context(), "HandleResumeDocument: document %s is already ready", docID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"message": "Document is already fully processed", "document_id": docID.String()})
		return
	}

	if originalStoragePath == "" {
		logger.Logf(r.Context(), "Error: original storage path is empty for document %s", docID)
		http.Error(w, "original document file is missing, cannot resume", http.StatusConflict)
		return
	}

	// Create local temp file/dir to download original PDF
	tempDir, err := os.MkdirTemp("", "booklet-resume-*")
	if err != nil {
		logger.Logf(r.Context(), "Error: failed to create temp dir for resume: %v", err)
		http.Error(w, "failed to create temp dir", http.StatusInternalServerError)
		return
	}

	localPath := filepath.Join(tempDir, name)
	err = storage.DownloadFile(r.Context(), originalStoragePath, localPath)
	if err != nil {
		os.RemoveAll(tempDir)
		logger.Logf(r.Context(), "Error: failed to download original PDF from storage for resume: %v", err)
		http.Error(w, "failed to retrieve original file: "+err.Error(), http.StatusInternalServerError)
		return
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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"message":     "Document processing resumed successfully.",
		"document_id": docID.String(),
	})
}

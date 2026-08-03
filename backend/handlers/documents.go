package handlers

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"booklet/db"
	"booklet/logger"
	"booklet/permissions"
	"booklet/storage"

	"github.com/minio/minio-go/v7"
)

// DocumentResponse representation for listing and details
type DocumentResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	TotalPages  int       `json:"total_pages"`
	SplitPages  int       `json:"split_pages"`
	ParsedPages int       `json:"parsed_pages"`
	Status      string    `json:"status"`
	Kind        string    `json:"kind"`
	MimeType    string    `json:"mime_type"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
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

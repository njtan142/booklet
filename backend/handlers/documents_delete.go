package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"booklet/db"
	"booklet/logger"
	"booklet/permissions"
	"booklet/storage"

	"github.com/google/uuid"
)

type BulkDeleteRequest struct {
	IDs []string `json:"ids"`
}

type BulkDeleteResponse struct {
	DeletedCount int      `json:"deleted_count"`
	DeletedIDs   []string `json:"deleted_ids"`
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

package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"booklet/db"
	"booklet/logger"
	"booklet/permissions"
)

type BookletProgressRequest struct {
	BatchSize       int             `json:"batch_size"`
	CompletedSheets json.RawMessage `json:"completed_sheets"`
}

type BookletProgressResponse struct {
	BookletID        string          `json:"booklet_id"`
	BatchSize        int             `json:"batch_size"`
	CompletedSheets  json.RawMessage `json:"completed_sheets"`
	CompletedBatches json.RawMessage `json:"completed_batches,omitempty"`
}

// HandleGetBookletProgress gets the printing progress of a booklet.
func HandleGetBookletProgress(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "HandleGetBookletProgress", http.MethodGet) {
		return
	}

	bookletID, ok := parseUUIDParam(w, r, "HandleGetBookletProgress", "id")
	if !ok {
		return
	}
	logger.Logf(r.Context(), "HandleGetBookletProgress: request progress for bookletID=%s", bookletID)

	// Resolving the parent document also verifies the booklet exists.
	if !enforceBookletAccess(w, r, bookletID, permissions.PermRead) {
		return
	}

	var batchSize int
	var completedSheetsStr string
	var completedBatchesStr string
	err := db.DB.QueryRow(`
		SELECT batch_size, completed_sheets, completed_batches
		FROM booklet_print_progress
		WHERE booklet_id = $1`, bookletID).Scan(&batchSize, &completedSheetsStr, &completedBatchesStr)

	if errors.Is(err, sql.ErrNoRows) {
		// Return default progress
		respondJSON(w, http.StatusOK, BookletProgressResponse{
			BookletID:        bookletID,
			BatchSize:        10,
			CompletedSheets:  json.RawMessage(`{}`),
			CompletedBatches: json.RawMessage(`{}`),
		})
		return
	} else if handleServerError(w, r, "HandleGetBookletProgress", "database error", err) {
		return
	}

	respondJSON(w, http.StatusOK, BookletProgressResponse{
		BookletID:        bookletID,
		BatchSize:        batchSize,
		CompletedSheets:  json.RawMessage(completedSheetsStr),
		CompletedBatches: json.RawMessage(completedBatchesStr),
	})
}

// HandleUpdateBookletProgress saves the printing progress of a booklet.
func HandleUpdateBookletProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		handleMethodNotAllowed(w, r, "HandleUpdateBookletProgress")
		return
	}

	bookletID, ok := parseUUIDParam(w, r, "HandleUpdateBookletProgress", "id")
	if !ok {
		return
	}
	logger.Logf(r.Context(), "HandleUpdateBookletProgress: request progress update for bookletID=%s", bookletID)

	// Progress is print state for the booklet, so it requires write on the parent.
	// Resolving the parent document also verifies the booklet exists.
	if !enforceBookletAccess(w, r, bookletID, permissions.PermWrite) {
		return
	}

	var req BookletProgressRequest
	if !decodeJSON(w, r, "HandleUpdateBookletProgress", &req) {
		return
	}

	completedSheetsStr := "{}"
	if len(req.CompletedSheets) > 0 {
		completedSheetsStr = string(req.CompletedSheets)
	}

	_, err := db.DB.Exec(`
		INSERT INTO booklet_print_progress (booklet_id, batch_size, completed_sheets, updated_at)
		VALUES ($1, $2, $3, CURRENT_TIMESTAMP)
		ON CONFLICT (booklet_id)
		DO UPDATE SET batch_size = EXCLUDED.batch_size, completed_sheets = EXCLUDED.completed_sheets, updated_at = CURRENT_TIMESTAMP`,
		bookletID, req.BatchSize, completedSheetsStr)

	if handleServerError(w, r, "HandleUpdateBookletProgress", "database error", err) {
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "Booklet progress updated successfully"})
}

// HandleBookletProgress dispatches GET, POST and PUT methods to progress handlers.
func HandleBookletProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		HandleGetBookletProgress(w, r)
	} else if r.Method == http.MethodPost || r.Method == http.MethodPut {
		HandleUpdateBookletProgress(w, r)
	} else {
		handleMethodNotAllowed(w, r, "HandleBookletProgress")
	}
}

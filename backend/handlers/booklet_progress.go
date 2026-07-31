package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"booklet/db"
	"booklet/logger"
	"booklet/permissions"

	"github.com/google/uuid"
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
	if r.Method != http.MethodGet {
		logger.Logf(r.Context(), "HandleGetBookletProgress: method %s not allowed", r.Method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	bookletID := r.PathValue("id")
	logger.Logf(r.Context(), "HandleGetBookletProgress: request progress for bookletID=%s", bookletID)
	if _, err := uuid.Parse(bookletID); err != nil {
		logger.Logf(r.Context(), "HandleGetBookletProgress: invalid UUID format: %s", bookletID)
		http.Error(w, "invalid UUID format", http.StatusBadRequest)
		return
	}

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

	if err == sql.ErrNoRows {
		// Return default progress
		resp := BookletProgressResponse{
			BookletID:        bookletID,
			BatchSize:        10,
			CompletedSheets:  json.RawMessage(`{}`),
			CompletedBatches: json.RawMessage(`{}`),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	} else if err != nil {
		logger.Logf(r.Context(), "Error: failed to query booklet progress %s: %v", bookletID, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := BookletProgressResponse{
		BookletID:        bookletID,
		BatchSize:        batchSize,
		CompletedSheets:  json.RawMessage(completedSheetsStr),
		CompletedBatches: json.RawMessage(completedBatchesStr),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleUpdateBookletProgress saves the printing progress of a booklet.
func HandleUpdateBookletProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		logger.Logf(r.Context(), "HandleUpdateBookletProgress: method %s not allowed", r.Method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	bookletID := r.PathValue("id")
	logger.Logf(r.Context(), "HandleUpdateBookletProgress: request progress update for bookletID=%s", bookletID)
	if _, err := uuid.Parse(bookletID); err != nil {
		logger.Logf(r.Context(), "HandleUpdateBookletProgress: invalid UUID format: %s", bookletID)
		http.Error(w, "invalid UUID format", http.StatusBadRequest)
		return
	}

	// Progress is print state for the booklet, so it requires write on the parent.
	// Resolving the parent document also verifies the booklet exists.
	if !enforceBookletAccess(w, r, bookletID, permissions.PermWrite) {
		return
	}

	var req BookletProgressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Logf(r.Context(), "Error: failed to decode booklet progress request JSON: %v", err)
		http.Error(w, "invalid JSON", http.StatusBadRequest)
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

	if err != nil {
		logger.Logf(r.Context(), "Error: failed to upsert booklet progress for %s: %v", bookletID, err)
		http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "Booklet progress updated successfully"})
}

// HandleBookletProgress dispatches GET, POST and PUT methods to progress handlers.
func HandleBookletProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		HandleGetBookletProgress(w, r)
	} else if r.Method == http.MethodPost || r.Method == http.MethodPut {
		HandleUpdateBookletProgress(w, r)
	} else {
		logger.Logf(r.Context(), "HandleBookletProgress: method %s not allowed", r.Method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

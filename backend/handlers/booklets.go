package handlers

import (
	"net/http"
	"time"

	"booklet/db"
	"booklet/logger"
	"booklet/permissions"
)

// BookletResponse is the response payload for a single compiled booklet status query.
type BookletResponse struct {
	ID        string    `json:"id"`
	DocID     string    `json:"document_id"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// BookletListResponse is the response payload for a booklet list entry,
// joining compiled_booklets with the parent document for display metadata.
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
	if !requireMethod(w, r, "HandleListBooklets", http.MethodGet) {
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
		JOIN documents d ON cb.document_id = d.id
		WHERE d.is_dismissed = FALSE`
	var bookletArgs []any
	if !permissions.IsAdmin(r) {
		userID, ok := requireUser(w, r, "HandleListBooklets")
		if !ok {
			return
		}
		clause, clauseArgs := permissions.VisibilityClause(userID, len(bookletArgs)+1, "d.")
		bookletQuery += " AND " + clause
		bookletArgs = append(bookletArgs, clauseArgs...)
	}
	bookletQuery += " ORDER BY cb.created_at DESC"

	rows, err := db.DB.Query(bookletQuery, bookletArgs...)
	if handleServerError(w, r, "HandleListBooklets", "database error", err) {
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
		if handleServerError(w, r, "HandleListBooklets", "database error", err) {
			return
		}
		list = append(list, item)
	}

	respondJSON(w, http.StatusOK, list)
}

func HandleGetBooklet(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "HandleGetBooklet", http.MethodGet) {
		return
	}

	bookletID, ok := parseUUIDParam(w, r, "HandleGetBooklet", "id")
	if !ok {
		return
	}
	logger.Logf(r.Context(), "HandleGetBooklet: request status for bookletID=%s", bookletID)

	if !enforceBookletAccess(w, r, bookletID, permissions.PermRead) {
		return
	}

	var b BookletResponse
	err := db.DB.QueryRow(`
		SELECT id, document_id, status, created_at 
		FROM compiled_booklets WHERE id = $1`, bookletID).Scan(&b.ID, &b.DocID, &b.Status, &b.CreatedAt)

	if handleDBError(w, r, "HandleGetBooklet", "booklet not found", err) {
		return
	}

	logger.Logf(r.Context(), "HandleGetBooklet: returned bookletID=%s status=%s", bookletID, b.Status)
	respondJSON(w, http.StatusOK, b)
}

// enforceBookletAccess checks perm on the document a booklet was compiled from,
// writing the HTTP error response itself when access is refused.
func enforceBookletAccess(w http.ResponseWriter, r *http.Request, bookletID string, perm permissions.Perm) bool {
	if permissions.IsAdmin(r) {
		return true
	}

	var docID string
	err := db.DB.QueryRowContext(r.Context(),
		`SELECT document_id::text FROM compiled_booklets WHERE id = $1`, bookletID).Scan(&docID)
	if handleDBError(w, r, "enforceBookletAccess", "booklet not found", err) {
		return false
	}

	return permissions.EnforceDocument(w, r, docID, perm)
}

package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"booklet/db"
	"booklet/embeddings"
	"booklet/logger"
	"booklet/metrics"
	"booklet/pdf"
	"booklet/permissions"
	"booklet/storage"

	"github.com/google/uuid"
)

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

	var conditions []string

	if docFilter != "" {
		if _, err := uuid.Parse(docFilter); err == nil {
			args = append(args, docFilter)
			conditions = append(conditions, fmt.Sprintf("p.document_id = $%d", len(args)))
			logger.Logf(r.Context(), "HandleSemanticSearch: filtering by document_id=%s", docFilter)
		}
	}

	// Restrict results to readable documents. The placeholder offset depends on
	// whether the optional document filter consumed $2, which is why
	// VisibilityClause takes startIdx rather than hardcoding $1.
	if !permissions.IsAdmin(r) {
		userID := permissions.CurrentUserID(r)
		if userID == "" {
			logger.Logf(r.Context(), "HandleSemanticSearch: no authenticated user on request")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		clause, clauseArgs := permissions.VisibilityClause(userID, len(args)+1, "d.")
		conditions = append(conditions, clause)
		args = append(args, clauseArgs...)
	}

	if len(conditions) > 0 {
		sqlQuery += " WHERE " + strings.Join(conditions, " AND ")
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

	if !permissions.EnforceDocument(w, r, docID, permissions.PermRead) {
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

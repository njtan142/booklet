package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"booklet/logger"
	"booklet/permissions"

	"github.com/google/uuid"
)

// requireMethod checks if the request method matches expectedMethod.
// If it does not match, it logs the error, sends http.StatusMethodNotAllowed, and returns false.
func requireMethod(w http.ResponseWriter, r *http.Request, handlerName, expectedMethod string) bool {
	if r.Method != expectedMethod {
		logger.Logf(r.Context(), "%s: method %s not allowed", handlerName, r.Method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}

// handleMethodNotAllowed logs method not allowed and writes http.StatusMethodNotAllowed (405).
// Always returns true.
func handleMethodNotAllowed(w http.ResponseWriter, r *http.Request, handlerName string) bool {
	logger.Logf(r.Context(), "%s: method %s not allowed", handlerName, r.Method)
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	return true
}

// requireUser checks if the request is associated with an authenticated user ID.
// If unauthenticated, it logs the attempt, sends http.StatusUnauthorized (401), and returns ("", false).
func requireUser(w http.ResponseWriter, r *http.Request, handlerName string) (string, bool) {
	userID := permissions.CurrentUserID(r)
	if userID == "" {
		logger.Logf(r.Context(), "%s: no authenticated user on request", handlerName)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	return userID, true
}

// handleUnauthorized logs unauthorized access attempt and writes http.StatusUnauthorized (401).
// Always returns true.
func handleUnauthorized(w http.ResponseWriter, r *http.Request, handlerName string) bool {
	logger.Logf(r.Context(), "%s: unauthorized access attempt", handlerName)
	http.Error(w, "unauthorized", http.StatusUnauthorized)
	return true
}

// handleForbidden logs forbidden access attempt and writes http.StatusForbidden (403).
// Always returns true.
func handleForbidden(w http.ResponseWriter, r *http.Request, handlerName, logMsg, userMsg string) bool {
	logger.Logf(r.Context(), "%s Error: %s", handlerName, logMsg)
	http.Error(w, userMsg, http.StatusForbidden)
	return true
}

// requireAdmin checks if the requester has admin permissions.
// If not, it logs an unauthorized access attempt, sends http.StatusUnauthorized, and returns false.
func requireAdmin(w http.ResponseWriter, r *http.Request, handlerName string) bool {
	if !permissions.IsAdmin(r) {
		logger.Logf(r.Context(), "%s: unauthorized access attempt", handlerName)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

// requireAPIKey checks if the request presents the expected admin API key (via X-API-Key or Bearer token).
// If unauthorized, it logs the error, sends http.StatusUnauthorized, and returns false.
func requireAPIKey(w http.ResponseWriter, r *http.Request, handlerName string) bool {
	adminKey := os.Getenv("ADMIN_API_KEY")
	if adminKey == "" {
		adminKey = "dev-admin-key"
	}

	reqKey := r.Header.Get("X-API-Key")
	if reqKey == "" {
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			reqKey = strings.TrimPrefix(authHeader, "Bearer ")
		}
	}

	if reqKey != adminKey {
		logger.Logf(r.Context(), "%s: unauthorized access attempt", handlerName)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

// parseUUIDParam extracts the path parameter paramName from r and parses it as a UUID.
// If missing or invalid, it logs the error, sends http.StatusBadRequest, and returns false.
func parseUUIDParam(w http.ResponseWriter, r *http.Request, handlerName, paramName string) (string, bool) {
	val := r.PathValue(paramName)
	if _, err := uuid.Parse(val); err != nil {
		logger.Logf(r.Context(), "%s: invalid UUID format for %s: %s", handlerName, paramName, val)
		http.Error(w, "invalid UUID format", http.StatusBadRequest)
		return "", false
	}
	return val, true
}

// decodeJSON decodes JSON from r.Body into v.
// If decoding fails, it logs the error, sends http.StatusBadRequest, and returns false.
func decodeJSON(w http.ResponseWriter, r *http.Request, handlerName string, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		logger.Logf(r.Context(), "%s: failed to decode JSON payload: %v", handlerName, err)
		http.Error(w, "invalid JSON payload", http.StatusBadRequest)
		return false
	}
	return true
}

// handleDBError checks for database query errors.
// If err is sql.ErrNoRows, it responds with http.StatusNotFound (or custom msg).
// If err is another error, it responds with http.StatusInternalServerError.
// Returns true if there was an error (and response was handled), false if err == nil.
func handleDBError(w http.ResponseWriter, r *http.Request, handlerName, notFoundMsg string, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, sql.ErrNoRows) {
		logger.Logf(r.Context(), "%s: %s", handlerName, notFoundMsg)
		http.Error(w, notFoundMsg, http.StatusNotFound)
		return true
	}
	logger.Logf(r.Context(), "%s database error: %v", handlerName, err)
	http.Error(w, err.Error(), http.StatusInternalServerError)
	return true
}

// handleError checks if err != nil. If so, logs formatted error message and writes http.Error with statusCode.
// Returns true if there was an error handled, false otherwise.
func handleError(w http.ResponseWriter, r *http.Request, handlerName, msg string, err error, statusCode int) bool {
	if err == nil {
		return false
	}
	if msg != "" {
		logger.Logf(r.Context(), "%s Error: %s: %v", handlerName, msg, err)
		http.Error(w, msg, statusCode)
	} else {
		logger.Logf(r.Context(), "%s Error: %v", handlerName, err)
		http.Error(w, err.Error(), statusCode)
	}
	return true
}

// handleServerError checks if err != nil. If so, logs error and writes http.StatusInternalServerError.
// Returns true if there was an error handled, false otherwise.
func handleServerError(w http.ResponseWriter, r *http.Request, handlerName, userMsg string, err error) bool {
	return handleError(w, r, handlerName, userMsg, err, http.StatusInternalServerError)
}

// handleBadRequest logs logMsg and writes http.StatusBadRequest (400) with userMsg.
// Always returns true.
func handleBadRequest(w http.ResponseWriter, r *http.Request, handlerName, logMsg, userMsg string) bool {
	logger.Logf(r.Context(), "%s Error: %s", handlerName, logMsg)
	http.Error(w, userMsg, http.StatusBadRequest)
	return true
}

// handleNotFound logs the not-found logMsg and writes http.StatusNotFound (404) with userMsg.
// Always returns true.
func handleNotFound(w http.ResponseWriter, r *http.Request, handlerName, logMsg, userMsg string) bool {
	logger.Logf(r.Context(), "%s Error: %s", handlerName, logMsg)
	http.Error(w, userMsg, http.StatusNotFound)
	return true
}

// handleConflict logs logMsg and writes http.StatusConflict (409) with userMsg.
// Always returns true.
func handleConflict(w http.ResponseWriter, r *http.Request, handlerName, logMsg, userMsg string) bool {
	logger.Logf(r.Context(), "%s Error: %s", handlerName, logMsg)
	http.Error(w, userMsg, http.StatusConflict)
	return true
}

// handleServiceUnavailable logs logMsg and writes http.StatusServiceUnavailable (503) with userMsg.
// Always returns true.
func handleServiceUnavailable(w http.ResponseWriter, r *http.Request, handlerName, logMsg, userMsg string) bool {
	logger.Logf(r.Context(), "%s Error: %s", handlerName, logMsg)
	http.Error(w, userMsg, http.StatusServiceUnavailable)
	return true
}

// respondJSON sets Content-Type header and encodes data as JSON response.
func respondJSON(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

// streamPDFFile streams a local PDF file to the HTTP response writer.
func streamPDFFile(w http.ResponseWriter, r *http.Request, handlerName, filePath, downloadFilename string) {
	f, err := os.Open(filePath)
	if handleServerError(w, r, handlerName, "internal server error", err) {
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "application/pdf")
	if downloadFilename != "" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", downloadFilename))
	} else {
		w.Header().Set("Content-Disposition", "inline")
	}

	if fi, err := f.Stat(); err == nil {
		w.Header().Set("Content-Length", strconv.FormatInt(fi.Size(), 10))
	}

	n, err := io.Copy(w, f)
	if err != nil {
		logger.Logf(r.Context(), "%s Error: failed to stream PDF bytes: %v", handlerName, err)
	} else {
		logger.Logf(r.Context(), "%s: successfully streamed %d bytes of PDF", handlerName, n)
	}
}

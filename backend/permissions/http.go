package permissions

import (
	"errors"
	"net/http"

	"booklet/auth"
	"booklet/logger"
)

// EnforceDocument checks perm on docID for the caller and writes the HTTP error
// response itself when access is refused. It returns true only when the handler
// should continue.
//
// Refusals return 404, not 403: a document that exists but is unreadable must be
// indistinguishable from one that does not exist, or the endpoint becomes an
// existence oracle.
//
// A request bearing the admin API key bypasses the check, which is why handlers
// using this helper must tolerate auth.GetUser returning ok == false.
func EnforceDocument(w http.ResponseWriter, r *http.Request, docID string, perm Perm) bool {
	if IsAdmin(r) {
		return true
	}

	user, ok := auth.GetUser(r.Context())
	if !ok || user.ID == "" {
		logger.Logf(r.Context(), "permissions: no authenticated user on request for document %s", docID)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}

	allowed, err := Check(r.Context(), docID, user.ID, perm)
	if errors.Is(err, ErrNotFound) {
		logger.Logf(r.Context(), "permissions: document %s not found", docID)
		http.Error(w, "document not found", http.StatusNotFound)
		return false
	}
	if err != nil {
		logger.Logf(r.Context(), "permissions: failed to check %s on document %s: %v", permName(perm), docID, err)
		http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
		return false
	}
	if !allowed {
		logger.Logf(r.Context(), "permissions: user %s denied %s on document %s", user.ID, permName(perm), docID)
		http.Error(w, "document not found", http.StatusNotFound)
		return false
	}

	return true
}

// CurrentUserID returns the caller's user id, or "" for an admin-key request
// that carries no session.
func CurrentUserID(r *http.Request) string {
	if user, ok := auth.GetUser(r.Context()); ok {
		return user.ID
	}
	return ""
}

func permName(p Perm) string {
	switch p {
	case PermRead:
		return "read"
	case PermWrite:
		return "write"
	case PermExecute:
		return "execute"
	default:
		return "unknown"
	}
}

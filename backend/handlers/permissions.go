package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"booklet/db"
	"booklet/logger"
	"booklet/permissions"

	"github.com/google/uuid"
)

// HandleDocumentPermissions serves GET (read the ownership triple) and PUT
// (change it) on /api/documents/{id}/permissions.
func HandleDocumentPermissions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleGetDocumentPermissions(w, r)
	case http.MethodPut:
		handleUpdateDocumentPermissions(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleGetDocumentPermissions(w http.ResponseWriter, r *http.Request) {
	docID := r.PathValue("id")
	if _, err := uuid.Parse(docID); err != nil {
		http.Error(w, "invalid UUID format", http.StatusBadRequest)
		return
	}

	// Reading the mode requires read access to the document itself, or the share
	// dialog would become a way to inspect other users' ownership.
	if !permissions.EnforceDocument(w, r, docID, permissions.PermRead) {
		return
	}

	perms, err := permissions.GetDocumentPermissions(r.Context(), docID)
	if errors.Is(err, permissions.ErrNotFound) {
		http.Error(w, "document not found", http.StatusNotFound)
		return
	}
	if err != nil {
		logger.Logf(r.Context(), "handleGetDocumentPermissions: failed to read %s: %v", docID, err)
		http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, perms)
}

type updatePermissionsRequest struct {
	OwnerID *string `json:"owner_id"`
	GroupID *string `json:"group_id"`
	Mode    *int16  `json:"mode"`
}

// handleUpdateDocumentPermissions applies chown, chgrp and chmod.
//
// The authorisation split lives in permissions.UpdateDocumentPermissions: write
// permission is enough to change the mode, but only the owner or the admin key
// may change the owner or the group.
func handleUpdateDocumentPermissions(w http.ResponseWriter, r *http.Request) {
	docID := r.PathValue("id")
	if _, err := uuid.Parse(docID); err != nil {
		http.Error(w, "invalid UUID format", http.StatusBadRequest)
		return
	}

	isAdmin := permissions.IsAdmin(r)
	actorID := permissions.CurrentUserID(r)
	if !isAdmin && actorID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req updatePermissionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Logf(r.Context(), "handleUpdateDocumentPermissions: failed to decode body: %v", err)
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.OwnerID == nil && req.GroupID == nil && req.Mode == nil {
		http.Error(w, "specify at least one of owner_id, group_id or mode", http.StatusBadRequest)
		return
	}

	// Validate referenced rows up front so a bad id is a 400/404 rather than a
	// foreign-key violation surfacing as a 500.
	if req.OwnerID != nil {
		if *req.OwnerID == "" {
			http.Error(w, "owner_id must not be empty", http.StatusBadRequest)
			return
		}
		exists, err := db.UserExists(*req.OwnerID)
		if err != nil {
			http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if !exists {
			http.Error(w, "owner_id does not match a known user", http.StatusBadRequest)
			return
		}
	}
	if req.GroupID != nil && *req.GroupID != "" {
		if _, err := uuid.Parse(*req.GroupID); err != nil {
			http.Error(w, "invalid group_id", http.StatusBadRequest)
			return
		}
		if _, err := db.GetGroup(*req.GroupID); errors.Is(err, db.ErrGroupNotFound) {
			http.Error(w, "group not found", http.StatusBadRequest)
			return
		} else if err != nil {
			http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	updated, err := permissions.UpdateDocumentPermissions(r.Context(), docID, actorID,
		permissions.ModeUpdate{OwnerID: req.OwnerID, GroupID: req.GroupID, Mode: req.Mode}, isAdmin)

	switch {
	case errors.Is(err, permissions.ErrNotFound):
		// Covers both "absent" and "not readable", so the endpoint cannot be
		// used to probe for document ids.
		http.Error(w, "document not found", http.StatusNotFound)
		return
	case errors.Is(err, permissions.ErrInvalidMode):
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	case errors.Is(err, permissions.ErrNotOwner):
		logger.Logf(r.Context(), "handleUpdateDocumentPermissions: %s attempted chown/chgrp on %s without ownership", actorID, docID)
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	case err != nil:
		logger.Logf(r.Context(), "handleUpdateDocumentPermissions: failed on %s: %v", docID, err)
		// A group-membership refusal is the caller's fault, not the server's.
		if strings.Contains(err.Error(), "do not belong to") {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	logger.Logf(r.Context(), "handleUpdateDocumentPermissions: %s updated permissions on %s (mode=%d)",
		actorID, docID, updated.Mode)
	writeJSON(w, http.StatusOK, updated)
}

// HandleListGroups returns the groups the caller belongs to, which is the set of
// valid chgrp targets for the share dialog.
func HandleListGroups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := permissions.CurrentUserID(r)
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	groups, err := db.ListGroupsForUser(userID)
	if err != nil {
		logger.Logf(r.Context(), "HandleListGroups: failed to list groups for %s: %v", userID, err)
		http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	logger.Logf(r.Context(), "HandleListGroups: returning %d group(s) for user %s", len(groups), userID)
	writeJSON(w, http.StatusOK, groups)
}

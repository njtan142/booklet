package handlers

import (
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
		handleMethodNotAllowed(w, r, "HandleDocumentPermissions")
	}
}

func handleGetDocumentPermissions(w http.ResponseWriter, r *http.Request) {
	docID, ok := parseUUIDParam(w, r, "handleGetDocumentPermissions", "id")
	if !ok {
		return
	}

	// Reading the mode requires read access to the document itself, or the share
	// dialog would become a way to inspect other users' ownership.
	if !permissions.EnforceDocument(w, r, docID, permissions.PermRead) {
		return
	}

	perms, err := permissions.GetDocumentPermissions(r.Context(), docID)
	if errors.Is(err, permissions.ErrNotFound) && handleNotFound(w, r, "handleGetDocumentPermissions", "document not found", "document not found") {
		return
	}
	if handleServerError(w, r, "handleGetDocumentPermissions", "database error", err) {
		return
	}

	respondJSON(w, http.StatusOK, perms)
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
	docID, ok := parseUUIDParam(w, r, "handleUpdateDocumentPermissions", "id")
	if !ok {
		return
	}

	isAdmin := permissions.IsAdmin(r)
	actorID := permissions.CurrentUserID(r)
	if !isAdmin && actorID == "" && handleUnauthorized(w, r, "handleUpdateDocumentPermissions") {
		return
	}

	var req updatePermissionsRequest
	if !decodeJSON(w, r, "handleUpdateDocumentPermissions", &req) {
		return
	}
	if req.OwnerID == nil && req.GroupID == nil && req.Mode == nil && handleBadRequest(w, r, "handleUpdateDocumentPermissions", "no updates specified", "specify at least one of owner_id, group_id or mode") {
		return
	}

	// Validate referenced rows up front so a bad id is a 400/404 rather than a
	// foreign-key violation surfacing as a 500.
	if req.OwnerID != nil {
		if *req.OwnerID == "" && handleBadRequest(w, r, "handleUpdateDocumentPermissions", "empty owner_id", "owner_id must not be empty") {
			return
		}
		exists, err := db.UserExists(*req.OwnerID)
		if handleServerError(w, r, "handleUpdateDocumentPermissions", "database error", err) {
			return
		}
		if !exists && handleBadRequest(w, r, "handleUpdateDocumentPermissions", "unknown owner_id", "owner_id does not match a known user") {
			return
		}
	}
	if req.GroupID != nil && *req.GroupID != "" {
		if _, err := uuid.Parse(*req.GroupID); err != nil && handleBadRequest(w, r, "handleUpdateDocumentPermissions", "invalid group_id", "invalid group_id") {
			return
		}
		if _, err := db.GetGroup(*req.GroupID); errors.Is(err, db.ErrGroupNotFound) && handleBadRequest(w, r, "handleUpdateDocumentPermissions", "group not found", "group not found") {
			return
		} else if handleServerError(w, r, "handleUpdateDocumentPermissions", "database error", err) {
			return
		}
	}

	updated, err := permissions.UpdateDocumentPermissions(r.Context(), docID, actorID,
		permissions.ModeUpdate{OwnerID: req.OwnerID, GroupID: req.GroupID, Mode: req.Mode}, isAdmin)

	switch {
	case errors.Is(err, permissions.ErrNotFound):
		handleNotFound(w, r, "handleUpdateDocumentPermissions", "document not found", "document not found")
		return
	case errors.Is(err, permissions.ErrInvalidMode):
		handleBadRequest(w, r, "handleUpdateDocumentPermissions", err.Error(), err.Error())
		return
	case errors.Is(err, permissions.ErrNotOwner):
		handleForbidden(w, r, "handleUpdateDocumentPermissions", "attempted chown/chgrp without ownership", err.Error())
		return
	case err != nil:
		if strings.Contains(err.Error(), "do not belong to") {
			handleForbidden(w, r, "handleUpdateDocumentPermissions", err.Error(), err.Error())
			return
		}
		handleServerError(w, r, "handleUpdateDocumentPermissions", "database error", err)
		return
	}

	logger.Logf(r.Context(), "handleUpdateDocumentPermissions: %s updated permissions on %s (mode=%d)",
		actorID, docID, updated.Mode)
	respondJSON(w, http.StatusOK, updated)
}

// HandleListGroups returns the groups the caller belongs to, which is the set of
// valid chgrp targets for the share dialog.
func HandleListGroups(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "HandleListGroups", http.MethodGet) {
		return
	}

	userID, ok := requireUser(w, r, "HandleListGroups")
	if !ok {
		return
	}

	groups, err := db.ListGroupsForUser(userID)
	if handleServerError(w, r, "HandleListGroups", "database error", err) {
		return
	}

	respondJSON(w, http.StatusOK, groups)
}

package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"booklet/db"
	"booklet/logger"
	"booklet/permissions"

	"github.com/google/uuid"
)

// The admin group routes authenticate with X-API-Key checked inside the handler,
// never with auth.RequireAuth. An operator holding only an API key has no
// session cookie, so wrapping these in RequireAuth would return 401 before the
// key check ever ran. This mirrors the existing admin routes in admin.go.

// HandleAdminGroups serves GET (list all groups) and POST (create a group) on
// /api/admin/groups.
func HandleAdminGroups(w http.ResponseWriter, r *http.Request) {
	if !permissions.IsAdmin(r) {
		logger.Logf(r.Context(), "HandleAdminGroups: unauthorized access attempt")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodGet:
		groups, err := db.ListGroups()
		if err != nil {
			logger.Logf(r.Context(), "HandleAdminGroups: failed to list groups: %v", err)
			http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, groups)

	case http.MethodPost:
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.Name == "" {
			http.Error(w, "group name is required", http.StatusBadRequest)
			return
		}

		// Always non-personal: a personal group is created by the login path and
		// bound to users.primary_group_id, and RemoveGroupMember refuses to empty
		// one. Letting an admin mint a second "personal" group would produce a
		// group with those restrictions and no owner.
		group, err := db.CreateGroup(req.Name, false)
		if errors.Is(err, db.ErrGroupNameTaken) {
			http.Error(w, "a group with that name already exists", http.StatusConflict)
			return
		}
		if err != nil {
			logger.Logf(r.Context(), "HandleAdminGroups: failed to create group %q: %v", req.Name, err)
			http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		logger.Logf(r.Context(), "HandleAdminGroups: created group %s (%s)", group.Name, group.ID)
		writeJSON(w, http.StatusCreated, group)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleAdminGroupMembers serves GET (list members) and POST (add a member) on
// /api/admin/groups/{id}/members.
func HandleAdminGroupMembers(w http.ResponseWriter, r *http.Request) {
	if !permissions.IsAdmin(r) {
		logger.Logf(r.Context(), "HandleAdminGroupMembers: unauthorized access attempt")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	groupID := r.PathValue("id")
	if _, err := uuid.Parse(groupID); err != nil {
		http.Error(w, "invalid UUID format", http.StatusBadRequest)
		return
	}
	if _, err := db.GetGroup(groupID); errors.Is(err, db.ErrGroupNotFound) {
		http.Error(w, "group not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	switch r.Method {
	case http.MethodGet:
		members, err := db.ListGroupMembers(groupID)
		if err != nil {
			logger.Logf(r.Context(), "HandleAdminGroupMembers: failed to list members of %s: %v", groupID, err)
			http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, members)

	case http.MethodPost:
		var req struct {
			UserID string `json:"user_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.UserID == "" {
			http.Error(w, "user_id is required", http.StatusBadRequest)
			return
		}

		exists, err := db.UserExists(req.UserID)
		if err != nil {
			http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if !exists {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}

		if err := db.AddGroupMember(groupID, req.UserID); err != nil {
			logger.Logf(r.Context(), "HandleAdminGroupMembers: failed to add %s to %s: %v", req.UserID, groupID, err)
			http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		logger.Logf(r.Context(), "HandleAdminGroupMembers: added %s to group %s", req.UserID, groupID)
		writeJSON(w, http.StatusOK, map[string]string{
			"status":   "success",
			"group_id": groupID,
			"user_id":  req.UserID,
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleAdminGroupMember serves DELETE on
// /api/admin/groups/{id}/members/{user_id}.
func HandleAdminGroupMember(w http.ResponseWriter, r *http.Request) {
	if !permissions.IsAdmin(r) {
		logger.Logf(r.Context(), "HandleAdminGroupMember: unauthorized access attempt")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	groupID := r.PathValue("id")
	userID := r.PathValue("user_id")
	if _, err := uuid.Parse(groupID); err != nil {
		http.Error(w, "invalid UUID format", http.StatusBadRequest)
		return
	}
	if userID == "" {
		http.Error(w, "user_id is required", http.StatusBadRequest)
		return
	}

	err := db.RemoveGroupMember(groupID, userID)
	if errors.Is(err, db.ErrGroupNotFound) {
		http.Error(w, "group not found", http.StatusNotFound)
		return
	}
	if err != nil {
		logger.Logf(r.Context(), "HandleAdminGroupMember: failed to remove %s from %s: %v", userID, groupID, err)
		// Removing the sole member of a personal group is refused by the data
		// layer: primary_group_id would still point at it and every document
		// that user creates would land in a group they cannot reach.
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	logger.Logf(r.Context(), "HandleAdminGroupMember: removed %s from group %s", userID, groupID)
	writeJSON(w, http.StatusOK, map[string]string{
		"status":   "success",
		"group_id": groupID,
		"user_id":  userID,
	})
}

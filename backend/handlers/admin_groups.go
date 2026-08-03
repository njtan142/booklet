package handlers

import (
	"errors"
	"net/http"

	"booklet/db"
	"booklet/logger"
)

// The admin group routes authenticate with X-API-Key checked inside the handler,
// never with auth.RequireAuth. An operator holding only an API key has no
// session cookie, so wrapping these in RequireAuth would return 401 before the
// key check ever ran. This mirrors the existing admin routes in admin.go.

// HandleAdminGroups serves GET (list all groups) and POST (create a group) on
// /api/admin/groups.
func HandleAdminGroups(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r, "HandleAdminGroups") {
		return
	}

	switch r.Method {
	case http.MethodGet:
		groups, err := db.ListGroups()
		if handleServerError(w, r, "HandleAdminGroups", "database error", err) {
			return
		}
		respondJSON(w, http.StatusOK, groups)

	case http.MethodPost:
		var req struct {
			Name string `json:"name"`
		}
		if !decodeJSON(w, r, "HandleAdminGroups", &req) {
			return
		}
		if req.Name == "" && handleBadRequest(w, r, "HandleAdminGroups", "missing group name", "group name is required") {
			return
		}

		// Always non-personal: a personal group is created by the login path and
		// bound to users.primary_group_id, and RemoveGroupMember refuses to empty
		// one. Letting an admin mint a second "personal" group would produce a
		// group with those restrictions and no owner.
		group, err := db.CreateGroup(req.Name, false)
		if errors.Is(err, db.ErrGroupNameTaken) && handleConflict(w, r, "HandleAdminGroups", "group name taken", "a group with that name already exists") {
			return
		}
		if handleServerError(w, r, "HandleAdminGroups", "database error", err) {
			return
		}

		logger.Logf(r.Context(), "HandleAdminGroups: created group %s (%s)", group.Name, group.ID)
		respondJSON(w, http.StatusCreated, group)

	default:
		handleMethodNotAllowed(w, r, "HandleAdminGroups")
	}
}

// HandleAdminGroupMembers serves GET (list members) and POST (add a member) on
// /api/admin/groups/{id}/members.
func HandleAdminGroupMembers(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r, "HandleAdminGroupMembers") {
		return
	}

	groupID, ok := parseUUIDParam(w, r, "HandleAdminGroupMembers", "id")
	if !ok {
		return
	}
	_, err := db.GetGroup(groupID)
	if handleDBError(w, r, "HandleAdminGroupMembers", "group not found", err) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		members, err := db.ListGroupMembers(groupID)
		if handleServerError(w, r, "HandleAdminGroupMembers", "database error", err) {
			return
		}
		respondJSON(w, http.StatusOK, members)

	case http.MethodPost:
		var req struct {
			UserID string `json:"user_id"`
		}
		if !decodeJSON(w, r, "HandleAdminGroupMembers", &req) {
			return
		}
		if req.UserID == "" && handleBadRequest(w, r, "HandleAdminGroupMembers", "missing user_id", "user_id is required") {
			return
		}

		exists, err := db.UserExists(req.UserID)
		if handleServerError(w, r, "HandleAdminGroupMembers", "database error", err) {
			return
		}
		if !exists && handleNotFound(w, r, "HandleAdminGroupMembers", "user not found", "user not found") {
			return
		}

		if err := db.AddGroupMember(groupID, req.UserID); handleServerError(w, r, "HandleAdminGroupMembers", "failed to add member to group", err) {
			return
		}

		logger.Logf(r.Context(), "HandleAdminGroupMembers: added %s to group %s", req.UserID, groupID)
		respondJSON(w, http.StatusOK, map[string]string{
			"status":   "success",
			"group_id": groupID,
			"user_id":  req.UserID,
		})

	default:
		handleMethodNotAllowed(w, r, "HandleAdminGroupMembers")
	}
}

// HandleAdminGroupMember serves DELETE on
// /api/admin/groups/{id}/members/{user_id}.
func HandleAdminGroupMember(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r, "HandleAdminGroupMember") || !requireMethod(w, r, "HandleAdminGroupMember", http.MethodDelete) {
		return
	}

	groupID, ok := parseUUIDParam(w, r, "HandleAdminGroupMember", "id")
	if !ok {
		return
	}
	userID := r.PathValue("user_id")
	if userID == "" && handleBadRequest(w, r, "HandleAdminGroupMember", "missing user_id", "user_id is required") {
		return
	}

	err := db.RemoveGroupMember(groupID, userID)
	if errors.Is(err, db.ErrGroupNotFound) && handleNotFound(w, r, "HandleAdminGroupMember", "group not found", "group not found") {
		return
	}
	if err != nil && handleBadRequest(w, r, "HandleAdminGroupMember", err.Error(), err.Error()) {
		return
	}

	logger.Logf(r.Context(), "HandleAdminGroupMember: removed %s from group %s", userID, groupID)
	respondJSON(w, http.StatusOK, map[string]string{
		"status":   "success",
		"group_id": groupID,
		"user_id":  userID,
	})
}

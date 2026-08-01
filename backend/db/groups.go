package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrGroupNotFound is returned when no group row matches the id or name.
var ErrGroupNotFound = errors.New("group not found")

// ErrGroupNameTaken is returned when a create would collide with an existing
// group name. groups.name is UNIQUE, so this is a 409, not a 500.
var ErrGroupNameTaken = errors.New("group name already exists")

// Group is one app-managed group. Membership is resolved from group_members on
// every permission check, because the session JWT carries no group claim.
type Group struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	IsPersonal bool      `json:"is_personal"`
	CreatedAt  time.Time `json:"created_at"`

	// MemberCount is only populated by ListGroups, which the admin panel uses.
	MemberCount int `json:"member_count,omitempty"`
}

// GroupMember is one row of a group's membership list.
type GroupMember struct {
	UserID   string    `json:"user_id"`
	Email    string    `json:"email"`
	Name     string    `json:"name"`
	JoinedAt time.Time `json:"joined_at"`
}

// CreateGroup inserts a named group.
//
// The name collision is reported as ErrGroupNameTaken rather than surfacing the
// raw unique-violation, so the handler can answer 409 without matching on
// driver error strings.
func CreateGroup(name string, isPersonal bool) (*Group, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("group name must not be empty")
	}

	g := &Group{ID: uuid.New().String(), Name: name, IsPersonal: isPersonal}
	err := DB.QueryRow(`
		INSERT INTO groups (id, name, is_personal)
		VALUES ($1, $2, $3)
		ON CONFLICT (name) DO NOTHING
		RETURNING id, name, is_personal, created_at;
	`, g.ID, name, isPersonal).Scan(&g.ID, &g.Name, &g.IsPersonal, &g.CreatedAt)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrGroupNameTaken
	}
	if err != nil {
		return nil, err
	}
	return g, nil
}

// GetGroup returns one group by id.
func GetGroup(groupID string) (*Group, error) {
	var g Group
	err := DB.QueryRow(`
		SELECT id::text, name, is_personal, created_at FROM groups WHERE id = $1
	`, groupID).Scan(&g.ID, &g.Name, &g.IsPersonal, &g.CreatedAt)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrGroupNotFound
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// ListGroups returns every group with its member count, for the admin panel.
func ListGroups() ([]Group, error) {
	rows, err := DB.Query(`
		SELECT g.id::text, g.name, g.is_personal, g.created_at,
		       (SELECT COUNT(*) FROM group_members m WHERE m.group_id = g.id)
		FROM groups g
		ORDER BY g.is_personal, g.name;
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := []Group{}
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.IsPersonal, &g.CreatedAt, &g.MemberCount); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

// ListGroupsForUser returns the groups userID belongs to.
//
// This is what the share dialog offers as chgrp targets: a user may only hand a
// document to a group they are themselves a member of, or they could push a
// document somewhere they can no longer reach.
func ListGroupsForUser(userID string) ([]Group, error) {
	rows, err := DB.Query(`
		SELECT g.id::text, g.name, g.is_personal, g.created_at
		FROM groups g
		JOIN group_members m ON m.group_id = g.id
		WHERE m.user_id = $1
		ORDER BY g.is_personal, g.name;
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := []Group{}
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.IsPersonal, &g.CreatedAt); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

// ListGroupMembers returns the members of one group.
func ListGroupMembers(groupID string) ([]GroupMember, error) {
	rows, err := DB.Query(`
		SELECT u.id, u.email, COALESCE(u.name, ''), m.joined_at
		FROM group_members m
		JOIN users u ON u.id = m.user_id
		WHERE m.group_id = $1
		ORDER BY u.email;
	`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	members := []GroupMember{}
	for rows.Next() {
		var m GroupMember
		if err := rows.Scan(&m.UserID, &m.Email, &m.Name, &m.JoinedAt); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

// AddGroupMember adds a user to a group. Repeating the call is harmless, which
// matters because the admin UI cannot know the current membership without a
// round-trip it would otherwise have to serialise against.
func AddGroupMember(groupID, userID string) error {
	_, err := DB.Exec(`
		INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING;
	`, groupID, userID)
	return err
}

// RemoveGroupMember removes a user from a group.
//
// It refuses to empty a personal group: primary_group_id would still point at
// it, and every subsequent document that user creates would land in a group
// they are not a member of, becoming unreachable through the group triple.
func RemoveGroupMember(groupID, userID string) error {
	var isPersonal bool
	err := DB.QueryRow(`SELECT is_personal FROM groups WHERE id = $1`, groupID).Scan(&isPersonal)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrGroupNotFound
	}
	if err != nil {
		return err
	}
	if isPersonal {
		return fmt.Errorf("cannot remove a member from a personal group")
	}

	_, err = DB.Exec(`DELETE FROM group_members WHERE group_id = $1 AND user_id = $2`, groupID, userID)
	return err
}

// IsGroupMember reports whether userID belongs to groupID.
func IsGroupMember(groupID, userID string) (bool, error) {
	var exists bool
	err := DB.QueryRow(`
		SELECT EXISTS (SELECT 1 FROM group_members WHERE group_id = $1 AND user_id = $2)
	`, groupID, userID).Scan(&exists)
	return exists, err
}

// UserExists reports whether a user row exists. Used before adding a member, so
// the caller gets a 404 instead of a foreign-key violation surfacing as a 500.
func UserExists(userID string) (bool, error) {
	var exists bool
	err := DB.QueryRow(`SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)`, userID).Scan(&exists)
	return exists, err
}

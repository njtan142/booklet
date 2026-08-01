package permissions

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"booklet/db"
)

// ErrNotOwner is returned when a caller who is not the owner attempts to chown
// or chgrp. Write permission alone is not enough: on a real filesystem only the
// owner (or root) may hand a file to somebody else.
var ErrNotOwner = errors.New("only the owner may change ownership")

// ErrInvalidMode is returned for a mode outside the nine rwx bits.
var ErrInvalidMode = errors.New("mode must be between 0 and 0o777")

// DocumentPermissions is the ownership triple of one document.
type DocumentPermissions struct {
	DocumentID string `json:"document_id"`
	OwnerID    string `json:"owner_id"`
	OwnerEmail string `json:"owner_email,omitempty"`
	GroupID    string `json:"group_id,omitempty"`
	GroupName  string `json:"group_name,omitempty"`
	Mode       int16  `json:"mode"`
}

// GetDocumentPermissions returns the ownership triple for docID, resolving the
// owner's email and the group's name so the share dialog does not need a second
// round-trip per document.
func GetDocumentPermissions(ctx context.Context, docID string) (*DocumentPermissions, error) {
	var p DocumentPermissions
	var owner, ownerEmail, groupID, groupName sql.NullString

	err := db.DB.QueryRowContext(ctx, `
		SELECT d.id::text, d.owner_id, u.email, d.group_id::text, g.name, d.mode
		FROM documents d
		LEFT JOIN users u  ON u.id = d.owner_id
		LEFT JOIN groups g ON g.id = d.group_id
		WHERE d.id = $1`, docID).
		Scan(&p.DocumentID, &owner, &ownerEmail, &groupID, &groupName, &p.Mode)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	p.OwnerID = owner.String
	p.OwnerEmail = ownerEmail.String
	p.GroupID = groupID.String
	p.GroupName = groupName.String
	return &p, nil
}

// ModeUpdate is a partial change to a document's ownership triple. A nil field
// means "leave unchanged", which is what lets the share dialog send only the
// mode without having to echo back an owner it never displayed.
type ModeUpdate struct {
	OwnerID *string
	GroupID *string
	Mode    *int16
}

// UpdateDocumentPermissions applies an ownership change on behalf of actorID.
//
// isAdmin bypasses the owner requirement, mirroring root. A non-admin actor
// must hold write permission to change the mode, and must additionally *be* the
// owner to change the owner or the group — otherwise any user with group write
// access could seize a document by chowning it to themselves.
func UpdateDocumentPermissions(ctx context.Context, docID, actorID string, upd ModeUpdate, isAdmin bool) (*DocumentPermissions, error) {
	if upd.Mode != nil && (*upd.Mode < 0 || *upd.Mode > 0o777) {
		return nil, ErrInvalidMode
	}

	current, err := GetDocumentPermissions(ctx, docID)
	if err != nil {
		return nil, err
	}

	if !isAdmin {
		isOwner := current.OwnerID != "" && current.OwnerID == actorID
		if (upd.OwnerID != nil || upd.GroupID != nil) && !isOwner {
			return nil, ErrNotOwner
		}
		if upd.Mode != nil && !isOwner {
			allowed, err := Check(ctx, docID, actorID, PermWrite)
			if err != nil {
				return nil, err
			}
			if !allowed {
				return nil, ErrNotFound
			}
		}
	}

	// A user may only hand a document to a group they belong to. Without this a
	// document could be pushed into a group its owner cannot reach, and the
	// group triple would then grant access to strangers instead.
	if upd.GroupID != nil && *upd.GroupID != "" && !isAdmin {
		member, err := db.IsGroupMember(*upd.GroupID, actorID)
		if err != nil {
			return nil, err
		}
		if !member {
			return nil, fmt.Errorf("cannot assign a document to a group you do not belong to")
		}
	}

	// COALESCE keeps the untouched columns as they are, so a partial update
	// never blanks a field the caller did not mention.
	var newOwner, newGroup any
	if upd.OwnerID != nil {
		newOwner = *upd.OwnerID
	}
	if upd.GroupID != nil {
		newGroup = *upd.GroupID
	}
	var newMode any
	if upd.Mode != nil {
		newMode = *upd.Mode
	}

	if _, err := db.DB.ExecContext(ctx, `
		UPDATE documents
		SET owner_id = COALESCE($2, owner_id),
		    group_id = COALESCE($3::uuid, group_id),
		    mode     = COALESCE($4, mode),
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1`, docID, newOwner, newGroup, newMode); err != nil {
		return nil, err
	}

	return GetDocumentPermissions(ctx, docID)
}

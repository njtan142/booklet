// Package permissions implements Unix-style owner/group/other access control
// for documents.
//
// Every document carries an owner_id, a group_id and a SMALLINT mode holding
// three rwx triples. Access is decided by *first matching class*, exactly as a
// real filesystem does: if the caller is the owner, only the owner triple is
// consulted — even when it denies and a later triple would allow.
//
// Two code paths must agree on that rule: Check, used for single-document
// access, and VisibilityClause, injected into list and search queries. Both
// derive from the same first-matching-class order, and permissions_test.go
// asserts they agree across the full mode matrix. An OR of the three triples
// would *not* implement these semantics — for mode 0o044 it grants the owner
// read access that Check denies — so the SQL is a CASE, never an OR chain.
package permissions

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"booklet/db"
)

// Perm is a single rwx bit within a mode triple.
type Perm int

const (
	// PermExecute (1) allows running a tool that derives from this document.
	PermExecute Perm = 1
	// PermWrite (2) allows rename, dismiss, delete and mode changes.
	PermWrite Perm = 2
	// PermRead (4) allows view, download, search and use as a tool input.
	PermRead Perm = 4
)

// ErrNotFound is returned when the document row does not exist. Callers should
// respond 404 for both ErrNotFound and a plain denial so that the existence of
// another user's document is never leaked.
var ErrNotFound = errors.New("document not found")

// Decide applies first-matching-class-wins to a mode.
//
// isOwner and inGroup describe the caller's relationship to the document. A
// document with no owner is treated as not-owned, which sends the decision to
// the group and then the other triple — mirroring the SQL, where comparing
// against a NULL owner_id yields NULL and falls through.
func Decide(isOwner, inGroup bool, mode int16, perm Perm) bool {
	var triple int16
	switch {
	case isOwner:
		triple = mode >> 6
	case inGroup:
		triple = mode >> 3
	default:
		triple = mode
	}
	return triple&int16(perm) == int16(perm)
}

// Check reports whether userID holds perm on docID.
//
// It resolves group membership from group_members on every call, because the
// session JWT carries no group claim.
func Check(ctx context.Context, docID, userID string, perm Perm) (bool, error) {
	var owner sql.NullString
	var mode int16
	var inGroup bool

	err := db.DB.QueryRowContext(ctx, `
		SELECT d.owner_id,
		       d.mode,
		       COALESCE(d.group_id IN (SELECT group_id FROM group_members WHERE user_id = $2), FALSE)
		FROM documents d
		WHERE d.id = $1`, docID, userID).Scan(&owner, &mode, &inGroup)

	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}

	isOwner := owner.Valid && owner.String == userID
	return Decide(isOwner, inGroup, mode, perm), nil
}

// CheckMany reports whether userID holds perm on every document in docIDs.
// It runs a single query rather than one round-trip per document, for tools
// like Merge that take many inputs. A missing document counts as a denial and
// is reported in the returned missing slice.
func CheckMany(ctx context.Context, docIDs []string, userID string, perm Perm) (allowed bool, denied []string, err error) {
	if len(docIDs) == 0 {
		return true, nil, nil
	}

	placeholders := make([]string, len(docIDs))
	args := make([]any, 0, len(docIDs)+1)
	for i, id := range docIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args = append(args, id)
	}
	userIdx := len(docIDs) + 1
	args = append(args, userID)

	query := fmt.Sprintf(`
		SELECT d.id::text,
		       d.owner_id,
		       d.mode,
		       COALESCE(d.group_id IN (SELECT group_id FROM group_members WHERE user_id = $%d), FALSE)
		FROM documents d
		WHERE d.id IN (%s)`, userIdx, strings.Join(placeholders, ", "))

	rows, err := db.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return false, nil, err
	}
	defer rows.Close()

	seen := make(map[string]bool, len(docIDs))
	for rows.Next() {
		var id string
		var owner sql.NullString
		var mode int16
		var inGroup bool
		if err := rows.Scan(&id, &owner, &mode, &inGroup); err != nil {
			return false, nil, err
		}
		seen[id] = true
		isOwner := owner.Valid && owner.String == userID
		if !Decide(isOwner, inGroup, mode, perm) {
			denied = append(denied, id)
		}
	}
	if err := rows.Err(); err != nil {
		return false, nil, err
	}

	// Documents that did not come back do not exist; treat them as denied so
	// callers cannot distinguish "absent" from "forbidden".
	for _, id := range docIDs {
		if !seen[id] {
			denied = append(denied, id)
		}
	}

	return len(denied) == 0, denied, nil
}

// VisibilityClause returns a SQL predicate selecting the documents userID may
// read, together with the args to bind.
//
// startIdx is the next free placeholder number at the call site: HandleSemanticSearch
// already binds $1 to the query vector and optionally $2 to a document filter,
// so a clause hardcoded to $1 could not be dropped in. alias is the documents
// table alias, "" for a bare FROM documents or "d." where it is joined as d.
//
// The returned predicate is already parenthesised, so it can be AND-ed directly
// into an existing WHERE. Exactly one arg is returned: userID, bound once and
// referenced twice.
func VisibilityClause(userID string, startIdx int, alias string) (string, []any) {
	return visibilityClause(userID, startIdx, alias, PermRead)
}

func visibilityClause(userID string, startIdx int, alias string, perm Perm) (string, []any) {
	if alias != "" && !strings.HasSuffix(alias, ".") {
		alias += "."
	}
	p := fmt.Sprintf("$%d", startIdx)

	// Postgres binds & tighter than =, so (mode >> 6) & 4 = 4 parses as
	// ((mode >> 6) & 4) = 4. The CASE — not an OR chain — is what makes this
	// agree with Decide when an earlier triple denies what a later one allows.
	clause := fmt.Sprintf(`(CASE
		WHEN %[1]sowner_id = %[2]s THEN (%[1]smode >> 6) & %[3]d = %[3]d
		WHEN %[1]sgroup_id IN (SELECT group_id FROM group_members WHERE user_id = %[2]s) THEN (%[1]smode >> 3) & %[3]d = %[3]d
		ELSE %[1]smode & %[3]d = %[3]d
	END)`, alias, p, int(perm))

	return clause, []any{userID}
}

// IsAdmin reports whether the request carries the admin API key, which bypasses
// all document checks. Mirrors the key extraction used by the existing admin
// handlers: X-API-Key, or a Bearer token in Authorization.
func IsAdmin(r *http.Request) bool {
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

	return reqKey != "" && reqKey == adminKey
}

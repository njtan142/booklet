package permissions

import (
	"context"
	"database/sql"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"booklet/db"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

// modeCase is one row of the ownership/mode matrix. Each row is evaluated three
// ways — Decide, Check and VisibilityClause — and all three must agree.
type modeCase struct {
	name     string
	mode     int16
	isOwner  bool
	inGroup  bool
	perm     Perm
	expected bool
}

// The read matrix from the plan. The 0o044 and 0o004 rows are the important
// ones: an OR of the three triples would grant the owner read access there,
// diverging from first-matching-class-wins.
var readMatrix = []modeCase{
	{"0o600 owner", 0o600, true, true, PermRead, true},
	{"0o644 owner", 0o644, true, true, PermRead, true},
	{"0o044 owner denied by owner triple", 0o044, true, true, PermRead, false},
	{"0o004 owner denied by owner triple", 0o004, true, true, PermRead, false},
	{"0o604 other reads", 0o604, false, false, PermRead, true},
	{"0o640 group reads", 0o640, false, true, PermRead, true},
	{"0o620 group has write not read", 0o620, false, true, PermRead, false},
	{"0o600 stranger denied", 0o600, false, false, PermRead, false},
}

// Write and execute triples must be masked the same way as read. These rows
// exercise Decide's bit math on arbitrary modes; they are not statements about
// what the defaults are. 0o644 denying the owner execute is precisely why
// db.ModeDefault is 0o744 — see TestDefaultModesGrantToolExecution.
var writeExecMatrix = []modeCase{
	{"0o644 owner writes", 0o644, true, true, PermWrite, true},
	{"0o444 owner cannot write", 0o444, true, true, PermWrite, false},
	{"0o664 group writes", 0o664, false, true, PermWrite, true},
	{"0o644 group cannot write", 0o644, false, true, PermWrite, false},
	{"0o666 other writes", 0o666, false, false, PermWrite, true},
	{"0o664 other cannot write", 0o664, false, false, PermWrite, false},
	{"0o744 owner executes", 0o744, true, true, PermExecute, true},
	{"0o644 owner cannot execute", 0o644, true, true, PermExecute, false},
	{"0o654 group executes", 0o654, false, true, PermExecute, true},
	{"0o645 other executes", 0o645, false, false, PermExecute, true},
}

func allCases() []modeCase {
	return append(append([]modeCase{}, readMatrix...), writeExecMatrix...)
}

// The default modes must satisfy the exact composite permission that
// POST /api/tools/jobs demands of every input. Regression test for the case
// where ModeDefault was 0o644: the owner of a freshly uploaded document held
// rw- but no x, so the handler denied them and no tool job could ever be
// created by a non-admin. The bit math was right; the default was not.
func TestDefaultModesGrantToolExecution(t *testing.T) {
	const toolInputPerm = PermRead | PermExecute

	cases := []struct {
		name    string
		mode    int16
		isOwner bool
		inGroup bool
	}{
		{"owner of an uploaded document", db.ModeDefault, true, true},
		{"owner of a derived document", db.ModeDefault, true, true},
		{"legacy group member", db.ModeLegacy, false, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !Decide(tc.isOwner, tc.inGroup, tc.mode, toolInputPerm) {
				t.Errorf("mode 0o%o denies PermRead|PermExecute (isOwner=%t, inGroup=%t); "+
					"tool jobs would 404 for this caller", tc.mode, tc.isOwner, tc.inGroup)
			}
		})
	}
}

// Widening the defaults must not have widened them past the owner: group and
// other still get r-- under ModeDefault, so an upload stays private.
func TestDefaultModeDoesNotLeakBeyondOwner(t *testing.T) {
	if Decide(false, true, db.ModeDefault, PermWrite) {
		t.Error("ModeDefault must not grant write to the group")
	}
	if Decide(false, false, db.ModeDefault, PermWrite) {
		t.Error("ModeDefault must not grant write to other")
	}
	if Decide(false, true, db.ModeDefault, PermExecute) {
		t.Error("ModeDefault must not grant execute to the group")
	}
	if Decide(false, false, db.ModeDefault, PermExecute) {
		t.Error("ModeDefault must not grant execute to other")
	}
}

func TestDecideMatrix(t *testing.T) {
	for _, tc := range allCases() {
		t.Run(tc.name, func(t *testing.T) {
			if got := Decide(tc.isOwner, tc.inGroup, tc.mode, tc.perm); got != tc.expected {
				t.Errorf("Decide(isOwner=%t, inGroup=%t, mode=0o%o, perm=%d) = %t, want %t",
					tc.isOwner, tc.inGroup, tc.mode, tc.perm, got, tc.expected)
			}
		})
	}
}

// An ownerless document must fall through to the group and other triples rather
// than being treated as owned by the caller.
func TestDecideOwnerlessFallsThroughToOther(t *testing.T) {
	if Decide(false, false, 0o004, PermRead) != true {
		t.Error("mode 0o004 should be readable via the other triple")
	}
	if Decide(false, false, 0o040, PermRead) != false {
		t.Error("mode 0o040 grants read to the group only, so a stranger must be denied")
	}
}

func TestVisibilityClauseUsesCaseNotOrChain(t *testing.T) {
	clause, args := VisibilityClause("alice", 3, "d.")

	if len(args) != 1 || args[0] != "alice" {
		t.Fatalf("expected exactly one arg (the user id), got %v", args)
	}
	if !strings.Contains(clause, "CASE") {
		t.Error("clause must be a CASE so it matches Decide's first-matching-class-wins order")
	}
	if strings.Contains(clause, " OR ") {
		t.Error("clause must not be an OR chain: it would grant the owner access that Check denies")
	}
	if !strings.Contains(clause, "$3") {
		t.Errorf("clause must honour startIdx=3, got %q", clause)
	}
	if strings.Contains(clause, "$1") || strings.Contains(clause, "$2") {
		t.Errorf("clause must not emit placeholders below startIdx, got %q", clause)
	}
	// The owner branch must precede the group branch, which must precede other.
	ownerIdx := strings.Index(clause, "owner_id")
	groupIdx := strings.Index(clause, "group_id")
	elseIdx := strings.Index(clause, "ELSE")
	if !(ownerIdx < groupIdx && groupIdx < elseIdx) {
		t.Errorf("branches must be ordered owner, group, other; got %q", clause)
	}
	if !strings.HasPrefix(clause, "(") || !strings.HasSuffix(clause, ")") {
		t.Error("clause must be parenthesised so it can be AND-ed into an existing WHERE")
	}
}

func TestVisibilityClauseAliasNormalisation(t *testing.T) {
	withDot, _ := VisibilityClause("alice", 1, "d.")
	withoutDot, _ := VisibilityClause("alice", 1, "d")
	if withDot != withoutDot {
		t.Errorf("alias %q and %q must produce the same clause", "d.", "d")
	}

	bare, _ := VisibilityClause("alice", 1, "")
	if strings.Contains(bare, "d.") {
		t.Errorf("empty alias must not qualify columns, got %q", bare)
	}
	if !strings.Contains(bare, "owner_id = $1") {
		t.Errorf("empty alias should reference bare columns, got %q", bare)
	}
}

func TestIsAdmin(t *testing.T) {
	t.Setenv("ADMIN_API_KEY", "super-secret-key")

	cases := []struct {
		name     string
		header   string
		value    string
		expected bool
	}{
		{"no header", "", "", false},
		{"correct api key", "X-API-Key", "super-secret-key", true},
		{"wrong api key", "X-API-Key", "nope", false},
		{"empty api key", "X-API-Key", "", false},
		{"bearer token", "Authorization", "Bearer super-secret-key", true},
		{"wrong bearer token", "Authorization", "Bearer nope", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/api/documents", nil)
			if tc.header != "" {
				r.Header.Set(tc.header, tc.value)
			}
			if got := IsAdmin(r); got != tc.expected {
				t.Errorf("IsAdmin() = %t, want %t", got, tc.expected)
			}
		})
	}
}

// TestCheckAndVisibilityClauseAgree is the regression test that matters most:
// Check and VisibilityClause are separate implementations of the same rule, and
// a divergence means a document is visible in a list but 404s on fetch, or is
// hidden from its own owner in one path and not the other.
//
// It needs a real Postgres, because the point is to compare Go logic against
// SQL evaluated by Postgres itself. Set TEST_DATABASE_URL to run it.
func TestCheckAndVisibilityClauseAgree(t *testing.T) {
	connStr := os.Getenv("TEST_DATABASE_URL")
	if connStr == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping Check/VisibilityClause agreement test")
	}

	conn, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	defer conn.Close()
	if err := conn.Ping(); err != nil {
		t.Fatalf("failed to reach test database: %v", err)
	}

	prev := db.DB
	db.DB = conn
	defer func() { db.DB = prev }()

	ctx := context.Background()
	suffix := strings.ReplaceAll(uuid.New().String(), "-", "")[:8]

	alice := "test_alice_" + suffix
	bob := "test_bob_" + suffix
	insideGroup := uuid.New().String()
	outsideGroup := uuid.New().String()

	// Fixtures. Cleanup cascades from users and groups to memberships.
	for _, u := range []string{alice, bob} {
		if _, err := conn.ExecContext(ctx,
			`INSERT INTO users (id, email, name) VALUES ($1, $2, 'Test User')`,
			u, u+"@test.local"); err != nil {
			t.Fatalf("failed to insert user %s: %v", u, err)
		}
	}
	for _, g := range []struct{ id, name string }{
		{insideGroup, "test_inside_" + suffix},
		{outsideGroup, "test_outside_" + suffix},
	} {
		if _, err := conn.ExecContext(ctx,
			`INSERT INTO groups (id, name) VALUES ($1, $2)`, g.id, g.name); err != nil {
			t.Fatalf("failed to insert group %s: %v", g.name, err)
		}
	}
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)`, insideGroup, alice); err != nil {
		t.Fatalf("failed to add alice to group: %v", err)
	}

	t.Cleanup(func() {
		_, _ = conn.Exec(`DELETE FROM documents WHERE owner_id IN ($1, $2)`, alice, bob)
		_, _ = conn.Exec(`DELETE FROM groups WHERE id IN ($1, $2)`, insideGroup, outsideGroup)
		_, _ = conn.Exec(`DELETE FROM users WHERE id IN ($1, $2)`, alice, bob)
	})

	for i, tc := range allCases() {
		tc := tc
		t.Run(fmt.Sprintf("%02d_%s", i, strings.ReplaceAll(tc.name, " ", "_")), func(t *testing.T) {
			owner := bob
			if tc.isOwner {
				owner = alice
			}
			group := outsideGroup
			if tc.inGroup {
				group = insideGroup
			}

			docID := uuid.New().String()
			if _, err := conn.ExecContext(ctx, `
				INSERT INTO documents (id, name, total_pages, status, owner_id, group_id, mode)
				VALUES ($1, $2, 1, 'ready', $3, $4, $5)`,
				docID, "perm-test-"+tc.name, owner, group, tc.mode); err != nil {
				t.Fatalf("failed to insert document: %v", err)
			}

			// 1. Go-side check.
			got, err := Check(ctx, docID, alice, tc.perm)
			if err != nil {
				t.Fatalf("Check failed: %v", err)
			}
			if got != tc.expected {
				t.Errorf("Check() = %t, want %t (mode 0o%o, owner=%t, group=%t)",
					got, tc.expected, tc.mode, tc.isOwner, tc.inGroup)
			}

			// 2. SQL-side check, evaluated by Postgres. startIdx 2 leaves $1 for
			// the document id, proving the offset parameter actually works.
			clause, args := visibilityClause(alice, 2, "d.", tc.perm)
			query := `SELECT ` + clause + ` FROM documents d WHERE d.id = $1`
			queryArgs := append([]any{docID}, args...)

			var visible sql.NullBool
			if err := conn.QueryRowContext(ctx, query, queryArgs...).Scan(&visible); err != nil {
				t.Fatalf("VisibilityClause query failed: %v\nquery: %s", err, query)
			}
			sqlResult := visible.Valid && visible.Bool
			if sqlResult != tc.expected {
				t.Errorf("VisibilityClause = %t, want %t (mode 0o%o, owner=%t, group=%t)",
					sqlResult, tc.expected, tc.mode, tc.isOwner, tc.inGroup)
			}

			// 3. The two must never disagree, whatever the expectation was.
			if got != sqlResult {
				t.Errorf("Check() = %t but VisibilityClause = %t for mode 0o%o (owner=%t, group=%t): "+
					"the document would be visible in one code path and not the other",
					got, sqlResult, tc.mode, tc.isOwner, tc.inGroup)
			}
		})
	}
}

func TestCheckMissingDocumentReturnsNotFound(t *testing.T) {
	connStr := os.Getenv("TEST_DATABASE_URL")
	if connStr == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping database-backed test")
	}

	conn, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	defer conn.Close()

	prev := db.DB
	db.DB = conn
	defer func() { db.DB = prev }()

	_, err = Check(context.Background(), uuid.New().String(), "nobody", PermRead)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound for an absent document, got %v", err)
	}
}

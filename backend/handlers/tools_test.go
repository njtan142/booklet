package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"booklet/auth"
	"booklet/jobs"
	"booklet/tools"
)

// authed returns a request carrying a session user, as auth.RequireAuth would
// have left it.
func authed(r *http.Request, userID string) *http.Request {
	return r.WithContext(auth.WithUser(r.Context(), &auth.User{ID: userID, Email: userID + "@x.local"}))
}

func postJob(t *testing.T, body any) *http.Request {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("failed to encode request: %v", err)
	}
	return httptest.NewRequest(http.MethodPost, "/api/tools/jobs", bytes.NewReader(encoded))
}

func noopRun(context.Context, *jobs.Job, *jobs.Reporter) error { return nil }

// registerTool adds a tool for one test. Slugs must be unique across the whole
// package run: the registry is process-wide and Register panics on a duplicate.
func registerTool(t *testing.T, tool *tools.Tool) *tools.Tool {
	t.Helper()
	if _, exists := tools.Get(tool.Slug); exists {
		t.Fatalf("slug %q is already registered by another test", tool.Slug)
	}
	tools.Register(tool)
	return tool
}

func TestCreateToolJob_RequiresSession(t *testing.T) {
	// No session user on the context: a job row references users(id), so there
	// is nothing to attribute the work to.
	req := postJob(t, createJobRequest{ToolSlug: "anything"})
	w := httptest.NewRecorder()
	HandleToolJobs(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without a session, got %d", w.Code)
	}
}

func TestCreateToolJob_RejectsUnknownTool(t *testing.T) {
	req := authed(postJob(t, createJobRequest{ToolSlug: "does-not-exist"}), "alice")
	w := httptest.NewRecorder()
	HandleToolJobs(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for an unknown slug, got %d", w.Code)
	}
}

func TestCreateToolJob_RejectsUnimplementedTool(t *testing.T) {
	// Registered but with a nil Run: enqueueing it would queue work that no
	// worker can perform, so it must be refused synchronously.
	registerTool(t, &tools.Tool{Slug: "test-unimplemented"})

	req := authed(postJob(t, createJobRequest{
		ToolSlug:         "test-unimplemented",
		InputDocumentIDs: []string{"11111111-1111-1111-1111-111111111111"},
	}), "alice")
	w := httptest.NewRecorder()
	HandleToolJobs(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for an unimplemented tool, got %d", w.Code)
	}
}

func TestCreateToolJob_RejectsUnavailableTool(t *testing.T) {
	registerTool(t, &tools.Tool{
		Slug:      "test-sidecar-down",
		Run:       noopRun,
		Available: func(context.Context) bool { return false },
	})

	req := authed(postJob(t, createJobRequest{
		ToolSlug:         "test-sidecar-down",
		InputDocumentIDs: []string{"11111111-1111-1111-1111-111111111111"},
	}), "alice")
	w := httptest.NewRecorder()
	HandleToolJobs(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when the backing engine is unreachable, got %d", w.Code)
	}
}

func TestCreateToolJob_EnforcesArity(t *testing.T) {
	registerTool(t, &tools.Tool{Slug: "test-arity-merge", Run: noopRun, MinInputs: 2})

	cases := []struct {
		name   string
		inputs []string
		want   int
	}{
		{"zero inputs", nil, http.StatusBadRequest},
		{"one input for a two-input tool", []string{"11111111-1111-1111-1111-111111111111"}, http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := authed(postJob(t, createJobRequest{
				ToolSlug:         "test-arity-merge",
				InputDocumentIDs: tc.inputs,
			}), "alice")
			w := httptest.NewRecorder()
			HandleToolJobs(w, req)

			if w.Code != tc.want {
				t.Errorf("expected %d, got %d", tc.want, w.Code)
			}
		})
	}
}

func TestCreateToolJob_RejectsMalformedDocumentID(t *testing.T) {
	registerTool(t, &tools.Tool{Slug: "test-badid-rotate", Run: noopRun, MaxInputs: 1})

	// A non-UUID must be caught here, not by a failed uuid cast partway through
	// the insert transaction.
	req := authed(postJob(t, createJobRequest{
		ToolSlug:         "test-badid-rotate",
		InputDocumentIDs: []string{"not-a-uuid"},
	}), "alice")
	w := httptest.NewRecorder()
	HandleToolJobs(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for a malformed document id, got %d", w.Code)
	}
}

func TestCreateToolJob_RunsValidateBeforeTouchingTheDatabase(t *testing.T) {
	registerTool(t, &tools.Tool{
		Slug:      "test-validate-rotate",
		Run:       noopRun,
		MaxInputs: 1,
		Validate: func(params json.RawMessage) error {
			var p struct {
				Angle int `json:"angle"`
			}
			if err := json.Unmarshal(params, &p); err != nil {
				return err
			}
			if p.Angle != 90 && p.Angle != 180 && p.Angle != 270 {
				return errTestBadAngle
			}
			return nil
		},
	})

	// db.DB is nil in this test binary, so reaching the permission check would
	// panic. Returning 400 proves validation short-circuits first, which is the
	// behaviour that keeps a doomed job out of the queue.
	req := authed(postJob(t, createJobRequest{
		ToolSlug:         "test-validate-rotate",
		InputDocumentIDs: []string{"11111111-1111-1111-1111-111111111111"},
		Params:           json.RawMessage(`{"angle":45}`),
	}), "alice")
	w := httptest.NewRecorder()
	HandleToolJobs(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for an invalid angle, got %d", w.Code)
	}
}

func TestCreateToolJob_RejectsMalformedBody(t *testing.T) {
	req := authed(httptest.NewRequest(http.MethodPost, "/api/tools/jobs",
		bytes.NewReader([]byte("{not json"))), "alice")
	w := httptest.NewRecorder()
	HandleToolJobs(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for a malformed body, got %d", w.Code)
	}
}

func TestToolJobs_MethodNotAllowed(t *testing.T) {
	req := authed(httptest.NewRequest(http.MethodDelete, "/api/tools/jobs", nil), "alice")
	w := httptest.NewRecorder()
	HandleToolJobs(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestGetToolJob_RejectsMalformedID(t *testing.T) {
	req := authed(httptest.NewRequest(http.MethodGet, "/api/tools/jobs/nope", nil), "alice")
	req.SetPathValue("id", "nope")
	w := httptest.NewRecorder()
	HandleGetToolJob(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for a malformed job id, got %d", w.Code)
	}
}

func TestRedactParams_MasksPasswordFields(t *testing.T) {
	registerTool(t, &tools.Tool{
		Slug: "test-protect",
		Run:  noopRun,
		Params: []tools.Param{
			{Name: "password", Type: tools.ParamPassword},
			{Name: "permissions", Type: tools.ParamString},
		},
	})

	// The frontend polls the job endpoint continuously; echoing the PDF password
	// back would put it in every poll response and any log that captures one.
	got := redactParams("test-protect", json.RawMessage(`{"password":"hunter2","permissions":"print"}`))

	var decoded map[string]any
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("redacted params are not valid JSON: %v", err)
	}
	if decoded["password"] == "hunter2" {
		t.Error("the password survived redaction")
	}
	if decoded["password"] != "********" {
		t.Errorf("password = %v, want ********", decoded["password"])
	}
	if decoded["permissions"] != "print" {
		t.Errorf("non-secret params must survive: permissions = %v", decoded["permissions"])
	}
}

func TestRedactParams_DropsUnparseableParamsForToolsWithSecrets(t *testing.T) {
	registerTool(t, &tools.Tool{
		Slug:   "test-unlock",
		Run:    noopRun,
		Params: []tools.Param{{Name: "password", Type: tools.ParamPassword}},
	})

	// Params that cannot be decoded cannot be selectively redacted, so they must
	// be dropped rather than returned intact on the chance they hold a secret.
	got := string(redactParams("test-unlock", json.RawMessage(`{"password":`)))
	if got != `{}` {
		t.Errorf("unparseable params for a secret-bearing tool = %q, want {}", got)
	}
}

func TestRedactParams_LeavesSecretlessToolsUntouched(t *testing.T) {
	registerTool(t, &tools.Tool{
		Slug:   "test-rotate-plain",
		Run:    noopRun,
		Params: []tools.Param{{Name: "angle", Type: tools.ParamInt}},
	})

	got := string(redactParams("test-rotate-plain", json.RawMessage(`{"angle":90}`)))
	if got != `{"angle":90}` {
		t.Errorf("params = %q, want them returned unchanged", got)
	}
}

func TestListTools_MethodNotAllowed(t *testing.T) {
	req := authed(httptest.NewRequest(http.MethodPost, "/api/tools", nil), "alice")
	w := httptest.NewRecorder()
	HandleListTools(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

// The admin group routes are registered without auth.RequireAuth, so the key
// check inside the handler is the only thing standing in front of them.
func TestAdminGroups_RejectsMissingKey(t *testing.T) {
	os.Setenv("ADMIN_API_KEY", "super-secret-key")
	defer os.Unsetenv("ADMIN_API_KEY")

	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		method  string
		path    string
	}{
		{"list groups", HandleAdminGroups, http.MethodGet, "/api/admin/groups"},
		{"create group", HandleAdminGroups, http.MethodPost, "/api/admin/groups"},
		{"list members", HandleAdminGroupMembers, http.MethodGet, "/api/admin/groups/x/members"},
		{"remove member", HandleAdminGroupMember, http.MethodDelete, "/api/admin/groups/x/members/bob"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()
			tc.handler(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Errorf("expected 401 without an API key, got %d", w.Code)
			}
		})
	}
}

func TestAdminGroups_RejectsWrongKey(t *testing.T) {
	os.Setenv("ADMIN_API_KEY", "super-secret-key")
	defer os.Unsetenv("ADMIN_API_KEY")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/groups", nil)
	req.Header.Set("X-API-Key", "wrong-key")
	w := httptest.NewRecorder()
	HandleAdminGroups(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for a wrong API key, got %d", w.Code)
	}
}

// A session cookie is not admin authority: only the API key is. Without this,
// any logged-in user could manage every group in the system.
func TestAdminGroups_SessionAloneIsNotAdmin(t *testing.T) {
	os.Setenv("ADMIN_API_KEY", "super-secret-key")
	defer os.Unsetenv("ADMIN_API_KEY")

	req := authed(httptest.NewRequest(http.MethodGet, "/api/admin/groups", nil), "alice")
	w := httptest.NewRecorder()
	HandleAdminGroups(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for a session-only caller, got %d", w.Code)
	}
}

func TestAdminGroupMembers_RejectsMalformedGroupID(t *testing.T) {
	os.Setenv("ADMIN_API_KEY", "super-secret-key")
	defer os.Unsetenv("ADMIN_API_KEY")

	// The key check passes, so this exercises the id validation that must run
	// before any database access.
	req := httptest.NewRequest(http.MethodGet, "/api/admin/groups/not-a-uuid/members", nil)
	req.Header.Set("X-API-Key", "super-secret-key")
	req.SetPathValue("id", "not-a-uuid")
	w := httptest.NewRecorder()
	HandleAdminGroupMembers(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for a malformed group id, got %d", w.Code)
	}
}

func TestDocumentPermissions_RejectsMalformedID(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		req := authed(httptest.NewRequest(method, "/api/documents/nope/permissions", nil), "alice")
		req.SetPathValue("id", "nope")
		w := httptest.NewRecorder()
		HandleDocumentPermissions(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400 for a malformed document id, got %d", method, w.Code)
		}
	}
}

func TestDocumentPermissions_RejectsEmptyUpdate(t *testing.T) {
	// An update naming no field would otherwise reach the database and issue a
	// no-op UPDATE; it is a client error, not a silent success.
	body := bytes.NewReader([]byte(`{}`))
	req := authed(httptest.NewRequest(http.MethodPut,
		"/api/documents/11111111-1111-1111-1111-111111111111/permissions", body), "alice")
	req.SetPathValue("id", "11111111-1111-1111-1111-111111111111")
	w := httptest.NewRecorder()
	HandleDocumentPermissions(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when no field is specified, got %d", w.Code)
	}
}

// This route is registered under OptionalAuth, not RequireAuth, so that an admin
// holding only ADMIN_API_KEY can chown without a session cookie. That makes the
// handler solely responsible for turning away a caller with neither credential —
// no middleware will do it. Both methods must self-guard.
func TestDocumentPermissions_RejectsCallerWithNoCredential(t *testing.T) {
	os.Setenv("ADMIN_API_KEY", "super-secret-key")
	defer os.Unsetenv("ADMIN_API_KEY")

	const docID = "11111111-1111-1111-1111-111111111111"

	for _, tc := range []struct {
		method string
		body   string
	}{
		{http.MethodGet, ""},
		{http.MethodPut, `{"mode":420}`},
	} {
		t.Run(tc.method, func(t *testing.T) {
			// No session user and no API key. db.DB is nil in this binary, so a
			// 401 also proves the refusal happens before any database access.
			req := httptest.NewRequest(tc.method, "/api/documents/"+docID+"/permissions",
				bytes.NewReader([]byte(tc.body)))
			req.SetPathValue("id", docID)
			w := httptest.NewRecorder()
			HandleDocumentPermissions(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Errorf("expected 401 for an unauthenticated caller, got %d", w.Code)
			}
		})
	}
}

func TestListGroups_RequiresSession(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/groups", nil)
	w := httptest.NewRecorder()
	HandleListGroups(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without a session, got %d", w.Code)
	}
}

var errTestBadAngle = errors.New("angle must be 90, 180 or 270")

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"booklet/db"
	"booklet/jobs"
	"booklet/logger"
	"booklet/permissions"
	"booklet/tools"

	"github.com/google/uuid"
)

// maxJobInputs caps a single job's input list even for tools that declare
// themselves unbounded (Merge). Without a ceiling one request could enqueue a
// job that pins a worker for hours and starves every other user.
const maxJobInputs = 200

// timeLayout matches the RFC3339 timestamps the rest of the API emits, so the
// frontend parses job times with the same code path as document times.
const timeLayout = time.RFC3339

// JobResponse is the API view of a job. It deliberately mirrors jobs.Job rather
// than returning it directly, because params must be redacted before they reach
// the client and jobs.Job carries them raw.
type JobResponse struct {
	ID                string          `json:"id"`
	ToolSlug          string          `json:"tool_slug"`
	Status            string          `json:"status"`
	Params            json.RawMessage `json:"params"`
	ProgressCurrent   int             `json:"progress_current"`
	ProgressTotal     int             `json:"progress_total"`
	ProgressStep      string          `json:"progress_step,omitempty"`
	Error             string          `json:"error,omitempty"`
	Attempt           int             `json:"attempt"`
	MaxAttempts       int             `json:"max_attempts"`
	CreatedAt         string          `json:"created_at"`
	StartedAt         string          `json:"started_at,omitempty"`
	CompletedAt       string          `json:"completed_at,omitempty"`
	InputDocumentIDs  []string        `json:"input_document_ids"`
	OutputDocumentIDs []string        `json:"output_document_ids"`
}

// HandleListTools serves the tool catalog that drives the frontend menu.
func HandleListTools(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "HandleListTools", http.MethodGet) {
		return
	}

	catalog := tools.Available(r.Context())
	logger.Logf(r.Context(), "HandleListTools: returning %d available tool(s)", len(catalog))

	respondJSON(w, http.StatusOK, catalog)
}

// HandleToolJobs routes POST (enqueue) and GET (list) on /api/tools/jobs.
func HandleToolJobs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		handleCreateToolJob(w, r)
	case http.MethodGet:
		handleListToolJobs(w, r)
	default:
		handleMethodNotAllowed(w, r, "HandleToolJobs")
	}
}

type createJobRequest struct {
	ToolSlug         string          `json:"tool_slug"`
	InputDocumentIDs []string        `json:"input_document_ids"`
	Params           json.RawMessage `json:"params"`
}

// handleCreateToolJob validates a job completely before writing it.
func handleCreateToolJob(w http.ResponseWriter, r *http.Request) {
	// A job row references users(id), so this endpoint needs a real session even
	// though the admin key bypasses the per-document checks below.
	userID, ok := requireUser(w, r, "handleCreateToolJob")
	if !ok {
		return
	}

	var req createJobRequest
	if !decodeJSON(w, r, "handleCreateToolJob", &req) {
		return
	}

	tool, ok := tools.Get(req.ToolSlug)
	if !ok || tool.Run == nil {
		handleBadRequest(w, r, "handleCreateToolJob", fmt.Sprintf("unknown tool %q", req.ToolSlug), fmt.Sprintf("unknown tool %q", req.ToolSlug))
		return
	}
	if tool.Available != nil && !tool.Available(r.Context()) {
		handleServiceUnavailable(w, r, "handleCreateToolJob", fmt.Sprintf("tool %s is unavailable", tool.Slug), fmt.Sprintf("tool %q is currently unavailable", tool.Slug))
		return
	}

	if err := tool.CheckArity(len(req.InputDocumentIDs)); err != nil && handleBadRequest(w, r, "handleCreateToolJob", err.Error(), err.Error()) {
		return
	}
	if len(req.InputDocumentIDs) > maxJobInputs && handleBadRequest(w, r, "handleCreateToolJob", "too many job inputs", fmt.Sprintf("a job accepts at most %d input documents", maxJobInputs)) {
		return
	}

	// Reject malformed ids here rather than letting Postgres fail the uuid cast
	// halfway through the insert.
	for _, id := range req.InputDocumentIDs {
		if _, err := uuid.Parse(id); err != nil && handleBadRequest(w, r, "handleCreateToolJob", "invalid document id: "+id, "invalid document id: "+id) {
			return
		}
	}

	if tool.Validate != nil {
		params := req.Params
		if len(params) == 0 {
			params = json.RawMessage(`{}`)
		}
		if err := tool.Validate(params); err != nil && handleBadRequest(w, r, "handleCreateToolJob", fmt.Sprintf("%s rejected params: %v", tool.Slug, err), err.Error()) {
			return
		}
	}

	if !permissions.IsAdmin(r) {
		// PermRead|PermExecute is checked as one composite bit test: the caller
		// must be able to both read the input and derive from it.
		allowed, denied, err := permissions.CheckMany(r.Context(), req.InputDocumentIDs, userID,
			permissions.PermRead|permissions.PermExecute)
		if handleServerError(w, r, "handleCreateToolJob", "database error", err) {
			return
		}
		if !allowed && handleNotFound(w, r, "handleCreateToolJob", fmt.Sprintf("user %s denied on %d input(s)", userID, len(denied)), "document not found") {
			return
		}
	}

	kinds, err := documentKinds(r.Context(), req.InputDocumentIDs)
	if handleServerError(w, r, "handleCreateToolJob", "database error", err) {
		return
	}
	for _, id := range req.InputDocumentIDs {
		kind, found := kinds[id]
		if !found && handleNotFound(w, r, "handleCreateToolJob", "document not found", "document not found") {
			return
		}
		if !tool.AcceptsKind(kind) && handleBadRequest(w, r, "handleCreateToolJob", fmt.Sprintf("%s rejects kind %q on document %s", tool.Slug, kind, id), fmt.Sprintf("%s does not accept a %q document", tool.Slug, kind)) {
			return
		}
	}

	jobID, err := jobs.Enqueue(r.Context(), userID, tool.Slug, req.Params, req.InputDocumentIDs)
	if handleServerError(w, r, "handleCreateToolJob", "database error", err) {
		return
	}

	logger.Logf(r.Context(), "handleCreateToolJob: queued job %s (%s) for user %s with %d input(s)",
		jobID, tool.Slug, userID, len(req.InputDocumentIDs))

	// 202, not 201: the work has been accepted, not performed. The caller polls
	// GET /api/tools/jobs/{id} for the outcome.
	respondJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID})
}

func handleListToolJobs(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r, "handleListToolJobs")
	if !ok {
		return
	}

	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}

	list, err := jobs.ListByUser(r.Context(), userID, limit)
	if handleServerError(w, r, "handleListToolJobs", "database error", err) {
		return
	}

	out := make([]JobResponse, 0, len(list))
	for i := range list {
		out = append(out, toJobResponse(&list[i]))
	}

	logger.Logf(r.Context(), "handleListToolJobs: returning %d job(s) for user %s", len(out), userID)
	respondJSON(w, http.StatusOK, out)
}

// HandleGetToolJob returns one job's status for the user who created it.
func HandleGetToolJob(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "HandleGetToolJob", http.MethodGet) {
		return
	}

	jobID, ok := parseUUIDParam(w, r, "HandleGetToolJob", "id")
	if !ok {
		return
	}

	job, err := jobs.Get(r.Context(), jobID)
	if errors.Is(err, jobs.ErrNotFound) && handleNotFound(w, r, "HandleGetToolJob", "job not found", "job not found") {
		return
	}
	if handleServerError(w, r, "HandleGetToolJob", "database error", err) {
		return
	}

	// Jobs carry no mode bits, so ownership is the whole access rule: a job is
	// visible to the user who created it, or to the admin key. A mismatch is a
	// 404 for the same reason documents are.
	if !permissions.IsAdmin(r) && job.UserID != permissions.CurrentUserID(r) && handleNotFound(w, r, "HandleGetToolJob", fmt.Sprintf("user %s denied on job %s", permissions.CurrentUserID(r), jobID), "job not found") {
		return
	}

	respondJSON(w, http.StatusOK, toJobResponse(job))
}

// documentKinds maps document id to kind for the given ids. Ids that do not
// exist are simply absent from the result.
func documentKinds(ctx context.Context, ids []string) (map[string]string, error) {
	kinds := make(map[string]string, len(ids))
	if len(ids) == 0 {
		return kinds, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	query := "SELECT id::text, kind FROM documents WHERE id IN (" + strings.Join(placeholders, ", ") + ")"
	rows, err := db.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id, kind string
		if err := rows.Scan(&id, &kind); err != nil {
			return nil, err
		}
		kinds[id] = kind
	}
	return kinds, rows.Err()
}

// toJobResponse converts a job for the wire, redacting password parameters.
func toJobResponse(j *jobs.Job) JobResponse {
	resp := JobResponse{
		ID:                j.ID,
		ToolSlug:          j.ToolSlug,
		Status:            j.Status,
		Params:            redactParams(j.ToolSlug, j.Params),
		ProgressCurrent:   j.ProgressCurrent,
		ProgressTotal:     j.ProgressTotal,
		ProgressStep:      j.ProgressStep,
		Error:             j.Error,
		Attempt:           j.Attempt,
		MaxAttempts:       j.MaxAttempts,
		CreatedAt:         j.CreatedAt.Format(timeLayout),
		InputDocumentIDs:  j.InputDocumentIDs,
		OutputDocumentIDs: j.OutputDocumentIDs,
	}
	if resp.InputDocumentIDs == nil {
		resp.InputDocumentIDs = []string{}
	}
	if resp.OutputDocumentIDs == nil {
		resp.OutputDocumentIDs = []string{}
	}
	if j.StartedAt != nil {
		resp.StartedAt = j.StartedAt.Format(timeLayout)
	}
	if j.CompletedAt != nil {
		resp.CompletedAt = j.CompletedAt.Format(timeLayout)
	}
	return resp
}

// redactParams blanks every password-typed parameter before a job is returned.
func redactParams(toolSlug string, params json.RawMessage) json.RawMessage {
	if len(params) == 0 {
		return json.RawMessage(`{}`)
	}

	tool, ok := tools.Get(toolSlug)
	if !ok {
		return params
	}

	var secrets []string
	for _, p := range tool.Params {
		if p.Type == tools.ParamPassword {
			secrets = append(secrets, p.Name)
		}
	}
	if len(secrets) == 0 {
		return params
	}

	var decoded map[string]any
	if err := json.Unmarshal(params, &decoded); err != nil {
		return json.RawMessage(`{}`)
	}
	for _, name := range secrets {
		if _, present := decoded[name]; present {
			decoded[name] = "********"
		}
	}

	out, err := json.Marshal(decoded)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return out
}

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
//
// Only implemented tools whose engine is reachable are listed: advertising a
// tool behind a downed sidecar would let a user queue work that can only fail.
func HandleListTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	catalog := tools.Available(r.Context())
	logger.Logf(r.Context(), "HandleListTools: returning %d available tool(s)", len(catalog))

	writeJSON(w, http.StatusOK, catalog)
}

// HandleToolJobs routes POST (enqueue) and GET (list) on /api/tools/jobs.
func HandleToolJobs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		handleCreateToolJob(w, r)
	case http.MethodGet:
		handleListToolJobs(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type createJobRequest struct {
	ToolSlug         string          `json:"tool_slug"`
	InputDocumentIDs []string        `json:"input_document_ids"`
	Params           json.RawMessage `json:"params"`
}

// handleCreateToolJob validates a job completely before writing it.
//
// Everything checkable up front — unknown slug, wrong arity, wrong document
// kind, missing permission, malformed params — is rejected synchronously with a
// 4xx. Queueing a job that is already known to be unrunnable would only hand
// the caller a job id to poll until it fails.
func handleCreateToolJob(w http.ResponseWriter, r *http.Request) {
	// A job row references users(id), so this endpoint needs a real session even
	// though the admin key bypasses the per-document checks below.
	userID := permissions.CurrentUserID(r)
	if userID == "" {
		logger.Logf(r.Context(), "handleCreateToolJob: no authenticated user on request")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req createJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Logf(r.Context(), "handleCreateToolJob: failed to decode request: %v", err)
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	tool, ok := tools.Get(req.ToolSlug)
	if !ok || tool.Run == nil {
		logger.Logf(r.Context(), "handleCreateToolJob: unknown or unimplemented tool %q", req.ToolSlug)
		http.Error(w, fmt.Sprintf("unknown tool %q", req.ToolSlug), http.StatusBadRequest)
		return
	}
	if tool.Available != nil && !tool.Available(r.Context()) {
		logger.Logf(r.Context(), "handleCreateToolJob: tool %s is unavailable", tool.Slug)
		http.Error(w, fmt.Sprintf("tool %q is currently unavailable", tool.Slug), http.StatusServiceUnavailable)
		return
	}

	if err := tool.CheckArity(len(req.InputDocumentIDs)); err != nil {
		logger.Logf(r.Context(), "handleCreateToolJob: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.InputDocumentIDs) > maxJobInputs {
		http.Error(w, fmt.Sprintf("a job accepts at most %d input documents", maxJobInputs), http.StatusBadRequest)
		return
	}

	// Reject malformed ids here rather than letting Postgres fail the uuid cast
	// halfway through the insert.
	for _, id := range req.InputDocumentIDs {
		if _, err := uuid.Parse(id); err != nil {
			logger.Logf(r.Context(), "handleCreateToolJob: invalid document id %q", id)
			http.Error(w, "invalid document id: "+id, http.StatusBadRequest)
			return
		}
	}

	if tool.Validate != nil {
		params := req.Params
		if len(params) == 0 {
			params = json.RawMessage(`{}`)
		}
		if err := tool.Validate(params); err != nil {
			logger.Logf(r.Context(), "handleCreateToolJob: %s rejected params: %v", tool.Slug, err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	if !permissions.IsAdmin(r) {
		// PermRead|PermExecute is checked as one composite bit test: the caller
		// must be able to both read the input and derive from it.
		allowed, denied, err := permissions.CheckMany(r.Context(), req.InputDocumentIDs, userID,
			permissions.PermRead|permissions.PermExecute)
		if err != nil {
			logger.Logf(r.Context(), "handleCreateToolJob: permission check failed: %v", err)
			http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if !allowed {
			// 404, not 403: a denial must not confirm that the document exists.
			logger.Logf(r.Context(), "handleCreateToolJob: user %s denied on %d input(s): %v", userID, len(denied), denied)
			http.Error(w, "document not found", http.StatusNotFound)
			return
		}
	}

	kinds, err := documentKinds(r.Context(), req.InputDocumentIDs)
	if err != nil {
		logger.Logf(r.Context(), "handleCreateToolJob: failed to read input kinds: %v", err)
		http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	for _, id := range req.InputDocumentIDs {
		kind, found := kinds[id]
		if !found {
			http.Error(w, "document not found", http.StatusNotFound)
			return
		}
		if !tool.AcceptsKind(kind) {
			logger.Logf(r.Context(), "handleCreateToolJob: %s rejects kind %q on document %s", tool.Slug, kind, id)
			http.Error(w, fmt.Sprintf("%s does not accept a %q document", tool.Slug, kind), http.StatusBadRequest)
			return
		}
	}

	jobID, err := jobs.Enqueue(r.Context(), userID, tool.Slug, req.Params, req.InputDocumentIDs)
	if err != nil {
		logger.Logf(r.Context(), "handleCreateToolJob: failed to enqueue %s: %v", tool.Slug, err)
		http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	logger.Logf(r.Context(), "handleCreateToolJob: queued job %s (%s) for user %s with %d input(s)",
		jobID, tool.Slug, userID, len(req.InputDocumentIDs))

	// 202, not 201: the work has been accepted, not performed. The caller polls
	// GET /api/tools/jobs/{id} for the outcome.
	writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID})
}

func handleListToolJobs(w http.ResponseWriter, r *http.Request) {
	userID := permissions.CurrentUserID(r)
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}

	list, err := jobs.ListByUser(r.Context(), userID, limit)
	if err != nil {
		logger.Logf(r.Context(), "handleListToolJobs: failed to list jobs for %s: %v", userID, err)
		http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	out := make([]JobResponse, 0, len(list))
	for i := range list {
		out = append(out, toJobResponse(&list[i]))
	}

	logger.Logf(r.Context(), "handleListToolJobs: returning %d job(s) for user %s", len(out), userID)
	writeJSON(w, http.StatusOK, out)
}

// HandleGetToolJob returns one job's status for the user who created it.
func HandleGetToolJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	jobID := r.PathValue("id")
	if _, err := uuid.Parse(jobID); err != nil {
		http.Error(w, "invalid UUID format", http.StatusBadRequest)
		return
	}

	job, err := jobs.Get(r.Context(), jobID)
	if errors.Is(err, jobs.ErrNotFound) {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}
	if err != nil {
		logger.Logf(r.Context(), "HandleGetToolJob: failed to load job %s: %v", jobID, err)
		http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Jobs carry no mode bits, so ownership is the whole access rule: a job is
	// visible to the user who created it, or to the admin key. A mismatch is a
	// 404 for the same reason documents are.
	if !permissions.IsAdmin(r) && job.UserID != permissions.CurrentUserID(r) {
		logger.Logf(r.Context(), "HandleGetToolJob: user %s denied on job %s", permissions.CurrentUserID(r), jobID)
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, toJobResponse(job))
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
//
// Protect and Unlock store the PDF password in jobs.params, and the job detail
// endpoint is polled continuously by the frontend; echoing it back would put the
// password in every poll response and in any log that captures one.
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
		// Unparseable params cannot be selectively redacted; drop them wholesale
		// rather than risk returning a secret.
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

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

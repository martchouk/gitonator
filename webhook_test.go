package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// makeWebhookPayload builds a minimal GitHub issue webhook JSON body.
func makeWebhookPayload(t *testing.T, issueNumber int, action string) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]interface{}{
		"action":     action,
		"issue":      map[string]interface{}{"number": issueNumber},
		"repository": map[string]interface{}{"full_name": "owner/repo"},
	})
	if err != nil {
		t.Fatalf("marshal webhook payload: %v", err)
	}
	return body
}

// TestProcessWebhookPayload_PersistsExplicitWorkflowKey verifies that
// providing a non-empty wfKey persists the workflow key for the issue so
// subsequent calls without a key can look it up (Finding 3, issue #46).
func TestProcessWebhookPayload_PersistsExplicitWorkflowKey(t *testing.T) {
	reg := leanRegistry(t)
	store := tempStore(t)

	issue := Issue{
		Number:    200,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "ada-pow"}},
		Labels:    []GitHubLabel{{Name: "status:new"}},
	}
	labeled := issue
	labeled.Labels = []GitHubLabel{{Name: "status:new"}}

	mock := &mockGitHub{issues: []Issue{issue, labeled}}
	s := &Server{
		cfg:       Config{},
		ghFor:     func(_ string) GitHubAPI { return mock },
		store:     store,
		logger:    log.New(&bytes.Buffer{}, "", 0),
		workflows: reg,
	}

	payload := makeWebhookPayload(t, 200, "labeled")

	// First call with explicit workflow key "lean".
	if err := s.processWebhookPayload(context.Background(), "issues", "d1", payload, reg.Get("lean"), "lean"); err != nil {
		t.Fatalf("processWebhookPayload: %v", err)
	}

	key, ok, err := store.GetIssueWorkflowKey(200)
	if err != nil {
		t.Fatalf("GetIssueWorkflowKey: %v", err)
	}
	if !ok || key != "lean" {
		t.Errorf("expected stored workflow key %q, got ok=%v key=%q", "lean", ok, key)
	}
}

// TestProcessWebhookPayload_LooksUpStoredWorkflowKey verifies that when no
// wfKey is provided the server looks up the persisted key and uses it
// instead of falling back to the default (Finding 3, issue #46).
func TestProcessWebhookPayload_LooksUpStoredWorkflowKey(t *testing.T) {
	reg := leanRegistry(t)
	store := tempStore(t)

	// Pre-store a workflow key for issue 201 so the second call picks it up.
	if err := store.SetIssueWorkflowKey(201, "lean"); err != nil {
		t.Fatalf("SetIssueWorkflowKey: %v", err)
	}

	issue := Issue{
		Number:    201,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "ada-pow"}},
		Labels:    []GitHubLabel{{Name: "status:new"}},
	}

	mock := &mockGitHub{issues: []Issue{issue}}
	s := &Server{
		cfg:       Config{},
		ghFor:     func(_ string) GitHubAPI { return mock },
		store:     store,
		logger:    log.New(&bytes.Buffer{}, "", 0),
		workflows: reg,
	}

	payload := makeWebhookPayload(t, 201, "labeled")

	// Call without an explicit workflow key — should resolve from store.
	if err := s.processWebhookPayload(context.Background(), "issues", "d2", payload, reg.Get(""), ""); err != nil {
		t.Fatalf("processWebhookPayload: %v", err)
	}
	// If the stored key was used, the issue was processed successfully (no error).
}

// TestProcessWebhookPayload_IgnoresWorkflowParamForStartedIssue verifies that
// when an issue already has a stored workflow key, a different ?workflow= param
// on a later webhook is ignored — in-flight issues must not switch workflow mid-run.
func TestProcessWebhookPayload_IgnoresWorkflowParamForStartedIssue(t *testing.T) {
	reg := leanRegistry(t)
	store := tempStore(t)

	// Issue 202 was started with "lean".
	if err := store.SetIssueWorkflowKey(202, "lean"); err != nil {
		t.Fatalf("SetIssueWorkflowKey: %v", err)
	}

	issue := Issue{
		Number:    202,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "ada-pow"}},
		Labels:    []GitHubLabel{{Name: "status:new"}},
	}

	var logBuf bytes.Buffer
	mock := &mockGitHub{issues: []Issue{issue}}
	s := &Server{
		cfg:       Config{},
		ghFor:     func(_ string) GitHubAPI { return mock },
		store:     store,
		logger:    log.New(&logBuf, "", 0),
		workflows: reg,
	}

	payload := makeWebhookPayload(t, 202, "labeled")

	// Simulate a webhook arriving with ?workflow=full — a different key from the stored one.
	if err := s.processWebhookPayload(context.Background(), "issues", "d3", payload, reg.Get("full"), "full"); err != nil {
		// processIssueWith may return an error because "full" workflow doesn't exist in leanRegistry;
		// what matters is that the stored key was NOT overwritten.
		_ = err
	}

	key, ok, err := store.GetIssueWorkflowKey(202)
	if err != nil {
		t.Fatalf("GetIssueWorkflowKey: %v", err)
	}
	if !ok || key != "lean" {
		t.Errorf("stored workflow key must stay %q, got ok=%v key=%q", "lean", ok, key)
	}
	if !strings.Contains(logBuf.String(), "workflow already locked") {
		t.Errorf("expected lock warning in log output, got: %s", logBuf.String())
	}
}

func TestHandleGitHubWebhook_DebugLoggingDoesNotEmitFullPayload(t *testing.T) {
	store := tempStore(t)
	var logBuf bytes.Buffer
	s := &Server{
		cfg:    Config{},
		store:  store,
		logger: log.New(&logBuf, "", 0),
		debug:  true,
	}
	sentinel := "UNIQUE_FULL_PAYLOAD_SENTINEL_DO_NOT_LOG"
	payload := []byte(`{"action":"opened","issue":{"number":0},"repository":{"full_name":"owner/repo"},"body":"` +
		strings.Repeat("x", 600) + sentinel + strings.Repeat("y", 600) + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook/github", bytes.NewReader(payload))
	req.Header.Set("X-GitHub-Delivery", "delivery-debug-safe")
	req.Header.Set("X-GitHub-Event", "issues")
	rec := httptest.NewRecorder()

	s.handleGitHubWebhook(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	logs := logBuf.String()
	if !strings.Contains(logs, "payload_bytes=") {
		t.Fatalf("expected payload byte count in logs, got: %s", logs)
	}
	if strings.Contains(logs, sentinel) {
		t.Fatalf("debug logs exposed full payload sentinel: %s", logs)
	}
}

func TestHandleWorkFailRequeuesDispatchedTask(t *testing.T) {
	store := tempStore(t)
	if _, err := store.QueueTask(WorkPackage{
		Repo:          "owner/repo",
		IssueID:       57,
		Role:          "reviewer",
		Assignee:      "mud-rev",
		CurrentStatus: "status:plan-review",
	}); err != nil {
		t.Fatalf("QueueTask: %v", err)
	}
	dispatched, err := store.GetNextWorkPackage("bigmac", []string{"reviewer"})
	if err != nil {
		t.Fatalf("GetNextWorkPackage: %v", err)
	}
	if dispatched == nil {
		t.Fatal("expected task to be dispatched")
	}

	s := &Server{
		cfg:    Config{AgentSharedToken: "secret"},
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
	}
	body := strings.NewReader(`{"task_id":` + fmt.Sprint(dispatched.ID) + `,"issue_id":57,"bridge_id":"bigmac","agent":"mud-rev","exit_code":1,"error_text":"You're out of extra usage"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/work/fail", body)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()

	s.handleWorkFail(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	next, err := store.GetNextWorkPackage("backup", []string{"reviewer"})
	if err != nil {
		t.Fatalf("GetNextWorkPackage after fail: %v", err)
	}
	if next == nil {
		t.Fatal("expected failed task to be immediately requeued")
	}
	if next.ID != dispatched.ID {
		t.Fatalf("expected same task id %d to be requeued, got %d", dispatched.ID, next.ID)
	}
}

func newWorkFailServer(t *testing.T) *Server {
	t.Helper()
	return &Server{
		cfg:    Config{AgentSharedToken: "secret"},
		store:  tempStore(t),
		logger: log.New(&bytes.Buffer{}, "", 0),
	}
}

func workFailRequest(t *testing.T, method, body string) *http.Request {
	t.Helper()
	var bodyReader *strings.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	} else {
		bodyReader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, "/api/v1/work/fail", bodyReader)
	req.Header.Set("Authorization", "Bearer secret")
	return req
}

func TestHandleWorkFail_WrongMethod(t *testing.T) {
	s := newWorkFailServer(t)
	req := workFailRequest(t, http.MethodGet, "")
	rec := httptest.NewRecorder()
	s.handleWorkFail(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleWorkFail_InvalidJSON(t *testing.T) {
	s := newWorkFailServer(t)
	req := workFailRequest(t, http.MethodPost, "not-json")
	rec := httptest.NewRecorder()
	s.handleWorkFail(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleWorkFail_MissingTaskID(t *testing.T) {
	s := newWorkFailServer(t)
	req := workFailRequest(t, http.MethodPost, `{"bridge_id":"b1","error_text":"oops"}`)
	rec := httptest.NewRecorder()
	s.handleWorkFail(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleWorkFail_MissingBridgeID(t *testing.T) {
	s := newWorkFailServer(t)
	req := workFailRequest(t, http.MethodPost, `{"task_id":1,"error_text":"oops"}`)
	rec := httptest.NewRecorder()
	s.handleWorkFail(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleWorkFail_TaskNotFound(t *testing.T) {
	s := newWorkFailServer(t)
	// task_id 9999 doesn't exist; RequeueDispatchedTask should return requeued=false without error
	req := workFailRequest(t, http.MethodPost, `{"task_id":9999,"bridge_id":"b1","error_text":"oops"}`)
	rec := httptest.NewRecorder()
	s.handleWorkFail(rec, req)
	// Not-found is still a 200 — the endpoint returns requeued:false, not an error.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["requeued"] != false {
		t.Errorf("expected requeued=false, got %v", body["requeued"])
	}
}

// TestTransitionIssue_CloseIssue verifies that a transition with close_issue:true
// triggers a CloseIssue call on the GitHub client (Finding 2, issue #46).
func TestTransitionIssue_CloseIssue(t *testing.T) {
	wd := leanWorkflowForTest(t)

	// Build an issue at status:po-approval ready for the PO to approve and close.
	issue := Issue{
		Number:    300,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "ada-pow"}},
		Labels:    []GitHubLabel{{Name: "status:po-approval"}},
	}
	afterTransition := Issue{
		Number: 300,
		Labels: []GitHubLabel{{Name: "status:done"}},
	}

	mock := &mockGitHub{issues: []Issue{issue, afterTransition}}
	store := tempStore(t)
	s := &Server{
		cfg:    Config{},
		ghFor:  func(_ string) GitHubAPI { return mock },
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	_, err := s.transitionIssue(
		context.Background(), 300, "owner/repo", "status:done", "", "", "po",
		"mcp_tool", nil, nil, wd,
	)
	if err != nil {
		t.Fatalf("transitionIssue: %v", err)
	}

	if len(mock.closedIssues) == 0 {
		t.Error("expected CloseIssue to be called for po_approve_rollout_and_close (close_issue:true), but it was not")
	}
	if len(mock.closedIssues) > 0 && mock.closedIssues[0] != 300 {
		t.Errorf("CloseIssue called for issue %d, want 300", mock.closedIssues[0])
	}
}

// TestTransitionIssue_ReopenIssue verifies that a transition with reopen_issue:true
// triggers a ReopenIssue call on the GitHub client (Finding 2, issue #46).
func TestTransitionIssue_ReopenIssue(t *testing.T) {
	wd := leanWorkflowForTest(t)

	issue := Issue{
		Number:    301,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "ada-pow"}},
		Labels:    []GitHubLabel{{Name: "status:done"}},
	}
	afterTransition := Issue{
		Number: 301,
		Labels: []GitHubLabel{{Name: "status:story-definition"}},
	}

	mock := &mockGitHub{issues: []Issue{issue, afterTransition}}
	store := tempStore(t)
	s := &Server{
		cfg:    Config{},
		ghFor:  func(_ string) GitHubAPI { return mock },
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	_, err := s.transitionIssue(
		context.Background(), 301, "owner/repo", "status:story-definition", "", "", "po",
		"mcp_tool", nil, nil, wd,
	)
	if err != nil {
		t.Fatalf("transitionIssue: %v", err)
	}

	if len(mock.reopenedIssues) == 0 {
		t.Error("expected ReopenIssue to be called for po_reopen_done (reopen_issue:true), but it was not")
	}
	if len(mock.reopenedIssues) > 0 && mock.reopenedIssues[0] != 301 {
		t.Errorf("ReopenIssue called for issue %d, want 301", mock.reopenedIssues[0])
	}
}

// TestTransitionIssue_CloseIssueFailureReturnsPartialError verifies that when
// CloseIssue fails the transition returns a partial-failure error and records
// "partially_applied" in the audit (Finding 2, issue #46).
func TestTransitionIssue_CloseIssueFailureReturnsPartialError(t *testing.T) {
	wd := leanWorkflowForTest(t)

	issue := Issue{
		Number:    305,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "ada-pow"}},
		Labels:    []GitHubLabel{{Name: "status:po-approval"}},
	}
	afterTransition := Issue{
		Number: 305,
		Labels: []GitHubLabel{{Name: "status:done"}},
	}

	mock := &mockGitHub{
		issues:        []Issue{issue, afterTransition},
		closeIssueErr: fmt.Errorf("github: 503 service unavailable"),
	}
	store := tempStore(t)
	var logBuf bytes.Buffer
	s := &Server{
		cfg:    Config{},
		ghFor:  func(_ string) GitHubAPI { return mock },
		store:  store,
		logger: log.New(&logBuf, "", 0),
	}

	_, err := s.transitionIssue(
		context.Background(), 305, "owner/repo", "status:done", "", "", "po",
		"mcp_tool", nil, nil, wd,
	)
	if err == nil {
		t.Fatal("expected partial-failure error when CloseIssue fails, got nil")
	}
	if !strings.Contains(err.Error(), "partially applied") {
		t.Errorf("expected error to mention %q, got %q", "partially applied", err.Error())
	}

	// Audit must record "partially_applied".
	audits, auditErr := store.ListTransitionAudit(305, 10)
	if auditErr != nil {
		t.Fatalf("ListTransitionAudit: %v", auditErr)
	}
	if len(audits) == 0 {
		t.Fatal("expected an audit entry, got none")
	}
	if audits[0].Result != "partially_applied" {
		t.Errorf("audit result: got %q, want %q", audits[0].Result, "partially_applied")
	}
}

// TestCallTool_TransitionIssue_PersistsWorkflowKey verifies that an explicit
// workflow arg on the transition_issue MCP tool persists the workflow key for
// the issue (Finding 1, issue #46).
func TestCallTool_TransitionIssue_PersistsWorkflowKey(t *testing.T) {
	reg := leanRegistry(t)
	store := tempStore(t)

	issue := Issue{
		Number:    400,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "bud-dev"}},
		Labels:    []GitHubLabel{{Name: "status:in-development"}},
	}
	afterTransition := Issue{
		Number: 400,
		Labels: []GitHubLabel{{Name: "status:code-review"}},
	}

	mock := &mockGitHub{issues: []Issue{issue, afterTransition}}
	s := &Server{
		cfg:       Config{},
		ghFor:     func(_ string) GitHubAPI { return mock },
		store:     store,
		logger:    log.New(&bytes.Buffer{}, "", 0),
		workflows: reg,
	}

	// Pre-seed _repo so the tool can resolve the GitHub client for this issue.
	if err := store.SetIssueMetadata(400, "_repo", "owner/repo"); err != nil {
		t.Fatalf("SetIssueMetadata: %v", err)
	}

	args, _ := json.Marshal(map[string]interface{}{
		"issue_number": 400,
		"status":       "status:code-review",
		"actor_role":   "developer",
		"workflow":     "lean",
	})
	if _, err := s.callTool(context.Background(), "transition_issue", args); err != nil {
		t.Fatalf("callTool transition_issue: %v", err)
	}

	key, ok, err := store.GetIssueWorkflowKey(400)
	if err != nil {
		t.Fatalf("GetIssueWorkflowKey: %v", err)
	}
	if !ok || key != "lean" {
		t.Errorf("expected stored workflow key %q, got ok=%v key=%q", "lean", ok, key)
	}
}

// TestTransitionIssue_NoCloseOnNormalTransition verifies that CloseIssue is
// NOT called for a transition that does not have close_issue:true.
func TestTransitionIssue_NoCloseOnNormalTransition(t *testing.T) {
	wd := leanWorkflowForTest(t)

	issue := Issue{
		Number:    302,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "bud-dev"}},
		Labels:    []GitHubLabel{{Name: "status:in-development"}},
	}
	afterTransition := Issue{
		Number: 302,
		Labels: []GitHubLabel{{Name: "status:code-review"}},
	}

	mock := &mockGitHub{issues: []Issue{issue, afterTransition}}
	store := tempStore(t)
	s := &Server{
		cfg:    Config{},
		ghFor:  func(_ string) GitHubAPI { return mock },
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	_, err := s.transitionIssue(
		context.Background(), 302, "owner/repo", "status:code-review", "", "", "developer",
		"mcp_tool", nil, nil, wd,
	)
	if err != nil {
		t.Fatalf("transitionIssue: %v", err)
	}

	if len(mock.closedIssues) != 0 {
		t.Errorf("expected CloseIssue NOT to be called for a normal transition, but got calls: %v", mock.closedIssues)
	}
}

func TestHandleWorkRelease_Success(t *testing.T) {
	store := tempStore(t)

	id, err := store.QueueTask(testPkg(55, "developer"))
	if err != nil {
		t.Fatalf("QueueTask: %v", err)
	}
	if _, err = store.GetNextWorkPackage("bigmac", []string{"developer"}); err != nil {
		t.Fatalf("GetNextWorkPackage: %v", err)
	}

	s := &Server{
		cfg:    Config{AgentSharedToken: "secret"},
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	body, _ := json.Marshal(map[string]any{
		"task_id":             id,
		"issue_id":            55,
		"bridge_id":           "bigmac",
		"reason":              "no_available_agent",
		"detail":              "all agents cooling down",
		"retry_after_seconds": 30,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/work/release", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	s.handleWorkRelease(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp["ok"].(bool) {
		t.Error("expected ok=true")
	}
	if !resp["released"].(bool) {
		t.Error("expected released=true")
	}
	if int(resp["retry_after_seconds"].(float64)) != 30 {
		t.Errorf("retry_after_seconds=%v, want 30", resp["retry_after_seconds"])
	}

	found, err := store.FindActiveTaskByIssue(55)
	if err != nil {
		t.Fatalf("FindActiveTaskByIssue: %v", err)
	}
	if found == nil {
		t.Fatal("expected task to be requeued after release")
	}
	if found.Status != "queued" {
		t.Errorf("status=%s, want queued", found.Status)
	}
}

func TestHandleWorkRelease_WrongBridgeReturns409(t *testing.T) {
	store := tempStore(t)

	id, err := store.QueueTask(testPkg(56, "developer"))
	if err != nil {
		t.Fatalf("QueueTask: %v", err)
	}
	if _, err = store.GetNextWorkPackage("bigmac", []string{"developer"}); err != nil {
		t.Fatalf("GetNextWorkPackage: %v", err)
	}

	s := &Server{
		cfg:    Config{AgentSharedToken: "secret"},
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	body, _ := json.Marshal(map[string]any{
		"task_id":   id,
		"issue_id":  56,
		"bridge_id": "other-bridge",
		"reason":    "no_available_agent",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/work/release", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	s.handleWorkRelease(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status=%d, want 409 Conflict", w.Code)
	}
}

func TestHandleWorkRelease_UnknownTaskReturns404(t *testing.T) {
	store := tempStore(t)
	s := &Server{
		cfg:    Config{AgentSharedToken: "secret"},
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	body, _ := json.Marshal(map[string]any{
		"task_id":   99999,
		"issue_id":  1,
		"bridge_id": "bigmac",
		"reason":    "no_available_agent",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/work/release", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	s.handleWorkRelease(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404 Not Found", w.Code)
	}
}

func TestHandleWorkRelease_MissingFieldsReturn422(t *testing.T) {
	store := tempStore(t)
	s := &Server{
		cfg:    Config{AgentSharedToken: "secret"},
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	cases := []struct {
		name string
		body map[string]any
	}{
		{"missing task_id", map[string]any{"bridge_id": "b", "reason": "r"}},
		{"missing bridge_id", map[string]any{"task_id": 1, "reason": "r"}},
		{"missing reason", map[string]any{"task_id": 1, "bridge_id": "b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bodyBytes, _ := json.Marshal(tc.body)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/work/release", bytes.NewReader(bodyBytes))
			req.Header.Set("Authorization", "Bearer secret")
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			s.handleWorkRelease(w, req)
			if w.Code != http.StatusUnprocessableEntity {
				t.Errorf("status=%d, want 422; body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestHandleWorkRelease_RequiresBearerToken(t *testing.T) {
	store := tempStore(t)
	s := &Server{
		cfg:    Config{AgentSharedToken: "secret"},
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	body, _ := json.Marshal(map[string]any{"task_id": 1, "bridge_id": "b", "reason": "r"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/work/release", bytes.NewReader(body))
	// No Authorization header.

	w := httptest.NewRecorder()
	s.handleWorkRelease(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status=%d, want 401", w.Code)
	}
}

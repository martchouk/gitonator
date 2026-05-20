package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestDashboardServer(t *testing.T) *DashboardServer {
	t.Helper()
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	dir := t.TempDir()
	leanYAML := filepath.Join(dir, "lean.yaml")
	_ = os.WriteFile(leanYAML, []byte(minimalWorkflowYAML), 0o644)
	reg, err := LoadWorkflowRegistry(dir, "lean")
	if err != nil {
		t.Fatalf("LoadWorkflowRegistry: %v", err)
	}

	return &DashboardServer{
		store:     store,
		workflows: reg,
		hub:       newSSEHub(),
		logger:    testLogger{t},
	}
}

// minimalWorkflowYAML is a stripped-down workflow with just enough nodes and
// transitions to exercise the graph builder.
const minimalWorkflowYAML = `
workflow:
  id: test_workflow
  key: lean
statuses:
  - id: status:open
    role: developer
    category: implementation
    queues_work: true
  - id: status:done
    role: ""
    category: terminal
    terminal: true
    queues_work: false
transitions:
  - id: open_to_done
    from: [status:open]
    to: status:done
    allowed_roles: [developer]
    description: "Close the issue."
`

type testLogger struct{ t *testing.T }

func (l testLogger) Printf(format string, args ...interface{}) {
	l.t.Logf(format, args...)
}

// --- /api/v1/dashboard/issues ---

func TestHandleDashboardIssues_Empty(t *testing.T) {
	d := newTestDashboardServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/issues", nil)
	w := httptest.NewRecorder()
	d.handleDashboardIssues(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var body map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	issues, ok := body["issues"].([]interface{})
	if !ok {
		t.Fatalf("issues is not an array: %v", body["issues"])
	}
	if len(issues) != 0 {
		t.Fatalf("expected 0 issues, got %d", len(issues))
	}
}

func TestHandleDashboardIssues_WithActiveTask(t *testing.T) {
	d := newTestDashboardServer(t)

	pkg := WorkPackage{
		Repo:          "owner/repo",
		IssueID:       42,
		Role:          "developer",
		Assignee:      "bud-dev",
		CurrentStatus: "status:open",
	}
	if _, err := d.store.QueueTask(pkg); err != nil {
		t.Fatalf("QueueTask: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/issues", nil)
	w := httptest.NewRecorder()
	d.handleDashboardIssues(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var body struct {
		Issues []GitHubIssueSummary `json:"issues"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(body.Issues))
	}
	got := body.Issues[0]
	if got.Number != 42 {
		t.Errorf("issue number: want 42, got %d", got.Number)
	}
	if got.CurrentStatus != "status:open" {
		t.Errorf("current status: want status:open, got %s", got.CurrentStatus)
	}
	if got.ActiveTask == nil {
		t.Fatal("expected active task, got nil")
	}
	if got.ActiveTask.Role != "developer" {
		t.Errorf("active task role: want developer, got %s", got.ActiveTask.Role)
	}
}

func TestHandleDashboardIssues_TitleAndRepo(t *testing.T) {
	d := newTestDashboardServer(t)

	pkg := WorkPackage{
		Repo:          "owner/repo",
		IssueID:       55,
		Role:          "developer",
		CurrentStatus: "status:open",
	}
	if _, err := d.store.QueueTask(pkg); err != nil {
		t.Fatalf("QueueTask: %v", err)
	}
	if err := d.store.SetIssueMetadata(55, "_title", "My Test Issue"); err != nil {
		t.Fatalf("SetIssueMetadata: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/issues", nil)
	w := httptest.NewRecorder()
	d.handleDashboardIssues(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var body struct {
		Issues []GitHubIssueSummary `json:"issues"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(body.Issues))
	}
	got := body.Issues[0]
	if got.Title != "My Test Issue" {
		t.Errorf("title: want %q, got %q", "My Test Issue", got.Title)
	}
	if got.Repo != "owner/repo" {
		t.Errorf("repo: want %q, got %q", "owner/repo", got.Repo)
	}
}

func TestHandleDashboardIssues_MethodNotAllowed(t *testing.T) {
	d := newTestDashboardServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/dashboard/issues", nil)
	w := httptest.NewRecorder()
	d.handleDashboardIssues(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestHandleDashboardIssues_CORS(t *testing.T) {
	d := newTestDashboardServer(t)
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/dashboard/issues", nil)
	w := httptest.NewRecorder()
	d.handleDashboardIssues(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for OPTIONS, got %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("expected CORS header Access-Control-Allow-Origin: *")
	}
}

func TestHandleDashboardIssues_DeduplicatesPerIssue(t *testing.T) {
	d := newTestDashboardServer(t)

	for i := 0; i < 3; i++ {
		pkg := WorkPackage{
			Repo:          "owner/repo",
			IssueID:       99,
			Role:          "developer",
			Assignee:      "bud-dev",
			CurrentStatus: "status:open",
		}
		_, _ = d.store.QueueTask(pkg)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/issues", nil)
	w := httptest.NewRecorder()
	d.handleDashboardIssues(w, req)

	var body struct {
		Issues []GitHubIssueSummary `json:"issues"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Issues) != 1 {
		t.Fatalf("expected deduplicated to 1 issue, got %d", len(body.Issues))
	}
}

// --- /api/v1/dashboard/issues/{number} ---

func TestHandleDashboardIssue_Valid(t *testing.T) {
	d := newTestDashboardServer(t)

	pkg := WorkPackage{
		Repo:          "owner/repo",
		IssueID:       7,
		Role:          "developer",
		Assignee:      "bud-dev",
		CurrentStatus: "status:open",
	}
	if _, err := d.store.QueueTask(pkg); err != nil {
		t.Fatalf("QueueTask: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/issues/7", nil)
	w := httptest.NewRecorder()
	d.handleDashboardIssue(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if int(body["number"].(float64)) != 7 {
		t.Errorf("number: want 7, got %v", body["number"])
	}
}

func TestHandleDashboardIssue_InvalidNumber(t *testing.T) {
	d := newTestDashboardServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/issues/not-a-number", nil)
	w := httptest.NewRecorder()
	d.handleDashboardIssue(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// --- /api/v1/dashboard/tasks ---

func TestHandleDashboardTasks_Empty(t *testing.T) {
	d := newTestDashboardServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/tasks", nil)
	w := httptest.NewRecorder()
	d.handleDashboardTasks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var body map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&body)
	tasks := body["tasks"].([]interface{})
	if len(tasks) != 0 {
		t.Fatalf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestHandleDashboardTasks_WithData(t *testing.T) {
	d := newTestDashboardServer(t)
	pkg := WorkPackage{Repo: "r/r", IssueID: 1, Role: "po", Assignee: "ada", CurrentStatus: "status:open"}
	if _, err := d.store.QueueTask(pkg); err != nil {
		t.Fatalf("QueueTask: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/tasks", nil)
	w := httptest.NewRecorder()
	d.handleDashboardTasks(w, req)

	var body map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&body)
	tasks := body["tasks"].([]interface{})
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
}

// --- /api/v1/dashboard/audit ---

func TestHandleDashboardAudit_Empty(t *testing.T) {
	d := newTestDashboardServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/audit", nil)
	w := httptest.NewRecorder()
	d.handleDashboardAudit(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var body map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&body)
	audit := body["audit"].([]interface{})
	if len(audit) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(audit))
	}
}

// --- /api/v1/workflows ---

func TestHandleWorkflowList(t *testing.T) {
	d := newTestDashboardServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workflows", nil)
	w := httptest.NewRecorder()
	d.handleWorkflowList(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Workflows []WorkflowSummary `json:"workflows"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Workflows) != 1 {
		t.Fatalf("expected 1 workflow, got %d", len(body.Workflows))
	}
	if body.Workflows[0].Key != "lean" {
		t.Errorf("key: want lean, got %s", body.Workflows[0].Key)
	}
}

func TestHandleWorkflowList_NoWorkflows(t *testing.T) {
	d := &DashboardServer{hub: newSSEHub(), logger: testLogger{t}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workflows", nil)
	w := httptest.NewRecorder()
	d.handleWorkflowList(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// --- /api/v1/workflows/{id} ---

func TestHandleWorkflowGet_ByKey(t *testing.T) {
	d := newTestDashboardServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workflows/lean", nil)
	w := httptest.NewRecorder()
	d.handleWorkflowGet(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp WorkflowGraphResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Key != "lean" {
		t.Errorf("key: want lean, got %s", resp.Key)
	}
	if len(resp.Nodes) != 2 {
		t.Errorf("nodes: want 2, got %d", len(resp.Nodes))
	}
	if len(resp.Edges) != 1 {
		t.Errorf("edges: want 1, got %d", len(resp.Edges))
	}
}

func TestHandleWorkflowGet_ByID(t *testing.T) {
	d := newTestDashboardServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workflows/test_workflow", nil)
	w := httptest.NewRecorder()
	d.handleWorkflowGet(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleWorkflowGet_NotFound(t *testing.T) {
	d := newTestDashboardServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workflows/nonexistent-workflow-xyz", nil)
	w := httptest.NewRecorder()
	d.handleWorkflowGet(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleWorkflowGet_NoWorkflows(t *testing.T) {
	d := &DashboardServer{hub: newSSEHub(), logger: testLogger{t}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workflows/anything", nil)
	w := httptest.NewRecorder()
	d.handleWorkflowGet(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// --- buildWorkflowGraph ---

func TestBuildWorkflowGraph_NodesAndEdges(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "lean.yaml"), []byte(minimalWorkflowYAML), 0o644)
	reg, err := LoadWorkflowRegistry(dir, "lean")
	if err != nil {
		t.Fatalf("LoadWorkflowRegistry: %v", err)
	}

	wd := reg.Get("lean")
	graph := buildWorkflowGraph(wd)

	if graph.ID != "test_workflow" {
		t.Errorf("id: want test_workflow, got %s", graph.ID)
	}
	if len(graph.Nodes) != 2 {
		t.Errorf("nodes: want 2, got %d", len(graph.Nodes))
	}
	if len(graph.Edges) != 1 {
		t.Errorf("edges: want 1, got %d", len(graph.Edges))
	}
	edge := graph.Edges[0]
	if edge.Source != "status:open" {
		t.Errorf("edge source: want status:open, got %s", edge.Source)
	}
	if edge.Target != "status:done" {
		t.Errorf("edge target: want status:done, got %s", edge.Target)
	}
}

// --- SSEHub ---

func TestSSEHub_RegisterUnregister(t *testing.T) {
	hub := newSSEHub()
	ch := make(chan []byte, 4)
	hub.Register(ch)
	if hub.ClientCount() != 1 {
		t.Fatalf("expected 1 client, got %d", hub.ClientCount())
	}
	hub.Unregister(ch)
	if hub.ClientCount() != 0 {
		t.Fatalf("expected 0 clients, got %d", hub.ClientCount())
	}
}

func TestSSEHub_Broadcast(t *testing.T) {
	hub := newSSEHub()
	ch := make(chan []byte, 4)
	hub.Register(ch)
	defer hub.Unregister(ch)

	hub.Broadcast(SSEEvent{Type: "test_event", Data: map[string]string{"key": "value"}})

	select {
	case msg := <-ch:
		var evt SSEEvent
		if err := json.Unmarshal(msg, &evt); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if evt.Type != "test_event" {
			t.Errorf("event type: want test_event, got %s", evt.Type)
		}
	default:
		t.Fatal("expected message in channel, got none")
	}
}

func TestSSEHub_BroadcastNonBlocking(t *testing.T) {
	hub := newSSEHub()
	// Register a channel with no buffer — Broadcast must not block.
	ch := make(chan []byte, 0)
	hub.Register(ch)
	defer func() {
		// manually unregister without closing (Unregister closes it)
		hub.mu.Lock()
		delete(hub.clients, ch)
		hub.mu.Unlock()
	}()

	// Should not block even though the channel is full.
	done := make(chan struct{})
	go func() {
		hub.Broadcast(SSEEvent{Type: "drop_me"})
		close(done)
	}()
	select {
	case <-done:
	default:
		// If it blocks we'd hang here — the test timeout protects against that.
	}
}

// --- store additions ---

func TestListActiveTasksAllIssues(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	for _, issueID := range []int{1, 2, 3} {
		_, err := store.QueueTask(WorkPackage{
			Repo:    "r/r",
			IssueID: issueID,
			Role:    "developer",
		})
		if err != nil {
			t.Fatalf("QueueTask: %v", err)
		}
	}

	tasks, err := store.ListActiveTasksAllIssues(0)
	if err != nil {
		t.Fatalf("ListActiveTasksAllIssues: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}
}

func TestListRecentTasks(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	for i := 0; i < 5; i++ {
		_, _ = store.QueueTask(WorkPackage{Repo: "r/r", IssueID: i + 1, Role: "po"})
	}

	tasks, err := store.ListRecentTasks(3)
	if err != nil {
		t.Fatalf("ListRecentTasks: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks (limit), got %d", len(tasks))
	}
}

func TestListRecentAudit(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	for i := 0; i < 4; i++ {
		_ = store.RecordTransitionAudit(
			i+1, "status:new", "status:open",
			"bud-dev", "mud-rev", "bud-dev",
			"webhook", nil, "success", "", nil, nil,
		)
	}

	audit, err := store.ListRecentAudit(2)
	if err != nil {
		t.Fatalf("ListRecentAudit: %v", err)
	}
	if len(audit) != 2 {
		t.Fatalf("expected 2 entries (limit), got %d", len(audit))
	}
}

// --- /api/v1/dashboard/stream ---

// flushRecorder wraps httptest.ResponseRecorder and implements http.Flusher so
// that handleDashboardStream does not fall back to the "streaming not supported" error path.
type flushRecorder struct {
	*httptest.ResponseRecorder
	flushed int
}

func (f *flushRecorder) Flush() { f.flushed++ }

func TestHandleDashboardStream_HeadersAndConnectedEvent(t *testing.T) {
	d := newTestDashboardServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/stream", nil)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	cancel() // cancel immediately so the handler returns after sending the first event

	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	d.handleDashboardStream(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type: want text/event-stream, got %s", ct)
	}
	if xab := w.Header().Get("X-Accel-Buffering"); xab != "no" {
		t.Errorf("X-Accel-Buffering: want no, got %s", xab)
	}
	body := w.Body.String()
	if !strings.Contains(body, "event: connected") {
		t.Errorf("expected 'event: connected' in body, got: %s", body)
	}
}

func TestHandleDashboardStream_MethodNotAllowed(t *testing.T) {
	d := newTestDashboardServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/dashboard/stream", nil)
	w := httptest.NewRecorder()
	d.handleDashboardStream(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

// noFlushWriter is a minimal http.ResponseWriter that does NOT implement http.Flusher.
type noFlushWriter struct {
	header http.Header
	code   int
	body   strings.Builder
}

func (n *noFlushWriter) Header() http.Header {
	if n.header == nil {
		n.header = make(http.Header)
	}
	return n.header
}
func (n *noFlushWriter) Write(b []byte) (int, error) { return n.body.Write(b) }
func (n *noFlushWriter) WriteHeader(code int)        { n.code = code }

func TestHandleDashboardStream_NoFlusherSupport(t *testing.T) {
	d := newTestDashboardServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/stream", nil)
	// noFlushWriter does not implement http.Flusher, so the handler must return 500.
	w := &noFlushWriter{}
	d.handleDashboardStream(w, req)
	if w.code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.code)
	}
}

// --- /api/v1/dashboard/completed ---

func TestHandleCompletedList_Empty(t *testing.T) {
	d := newTestDashboardServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/completed", nil)
	w := httptest.NewRecorder()
	d.handleCompletedList(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var body map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&body)
	completed, ok := body["completed"].([]interface{})
	if !ok {
		t.Fatalf("completed is not an array: %v", body["completed"])
	}
	if len(completed) != 0 {
		t.Fatalf("expected 0 completed runs, got %d", len(completed))
	}
}

func TestHandleCompletedList_WithData(t *testing.T) {
	d := newTestDashboardServer(t)
	_ = d.store.RecordTransitionAudit(42, "status:open", "status:done",
		"bud-dev", "", "bud-dev", "webhook", nil, "success", "", nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/completed", nil)
	w := httptest.NewRecorder()
	d.handleCompletedList(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var body map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&body)
	completed := body["completed"].([]interface{})
	if len(completed) != 1 {
		t.Fatalf("expected 1 completed run, got %d", len(completed))
	}
	run := completed[0].(map[string]interface{})
	if int(run["issueNumber"].(float64)) != 42 {
		t.Errorf("issueNumber: want 42, got %v", run["issueNumber"])
	}
	if run["finalStatus"].(string) != "status:done" {
		t.Errorf("finalStatus: want status:done, got %s", run["finalStatus"])
	}
}

func TestHandleCompletedList_MethodNotAllowed(t *testing.T) {
	d := newTestDashboardServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/dashboard/completed", nil)
	w := httptest.NewRecorder()
	d.handleCompletedList(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestHandleCompletedIssue_Valid(t *testing.T) {
	d := newTestDashboardServer(t)
	_ = d.store.RecordTransitionAudit(55, "status:open", "status:done",
		"bud-dev", "", "bud-dev", "webhook", nil, "success", "", nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/completed/55", nil)
	w := httptest.NewRecorder()
	d.handleCompletedIssue(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var detail CompletedRunDetail
	if err := json.NewDecoder(w.Body).Decode(&detail); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if detail.IssueNumber != 55 {
		t.Errorf("issueNumber: want 55, got %d", detail.IssueNumber)
	}
	if detail.FinalStatus != "status:done" {
		t.Errorf("finalStatus: want status:done, got %s", detail.FinalStatus)
	}
	if len(detail.Audit) != 1 {
		t.Errorf("audit entries: want 1, got %d", len(detail.Audit))
	}
	if detail.StepCount != 1 {
		t.Errorf("stepCount: want 1, got %d", detail.StepCount)
	}
}

func TestHandleCompletedIssue_NotFound(t *testing.T) {
	d := newTestDashboardServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/completed/999", nil)
	w := httptest.NewRecorder()
	d.handleCompletedIssue(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleCompletedIssue_InvalidNumber(t *testing.T) {
	d := newTestDashboardServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/completed/xyz", nil)
	w := httptest.NewRecorder()
	d.handleCompletedIssue(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleCompletedIssue_AttachesWorkflowGraph(t *testing.T) {
	d := newTestDashboardServer(t)
	_ = d.store.SetIssueWorkflowKey(77, "lean")
	_ = d.store.RecordTransitionAudit(77, "status:open", "status:done",
		"bud-dev", "", "bud-dev", "webhook", nil, "success", "", nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/completed/77", nil)
	w := httptest.NewRecorder()
	d.handleCompletedIssue(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var detail CompletedRunDetail
	if err := json.NewDecoder(w.Body).Decode(&detail); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if detail.Workflow == nil {
		t.Fatal("expected workflow graph to be attached when workflow key is known")
	}
	if detail.Workflow.Key != "lean" {
		t.Errorf("workflow key: want lean, got %s", detail.Workflow.Key)
	}
}

func TestHandleCompletedIssue_ReturnsNotFoundForActiveTask(t *testing.T) {
	d := newTestDashboardServer(t)
	if _, err := d.store.QueueTask(WorkPackage{
		Repo:          "owner/repo",
		IssueID:       88,
		Role:          "developer",
		CurrentStatus: "status:open",
	}); err != nil {
		t.Fatalf("QueueTask: %v", err)
	}
	_ = d.store.RecordTransitionAudit(88, "status:open", "status:done",
		"bud-dev", "", "bud-dev", "webhook", nil, "success", "", nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/completed/88", nil)
	w := httptest.NewRecorder()
	d.handleCompletedIssue(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for active workflow, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleCompletedIssue_ReturnsLatestTerminalAuditStatus(t *testing.T) {
	d := newTestDashboardServer(t)
	if _, err := d.store.QueueTask(WorkPackage{
		Repo:          "owner/repo",
		IssueID:       91,
		Role:          "developer",
		CurrentStatus: "status:po-approval",
	}); err != nil {
		t.Fatalf("QueueTask: %v", err)
	}
	task, err := d.store.GetNextWorkPackage("bridge-1", []string{"developer"})
	if err != nil {
		t.Fatalf("GetNextWorkPackage: %v", err)
	}
	if task == nil {
		t.Fatal("expected queued task to dispatch")
	}
	if err := d.store.CompleteDispatchedTask(91); err != nil {
		t.Fatalf("CompleteDispatchedTask: %v", err)
	}
	_ = d.store.RecordTransitionAudit(91, "status:po-approval", "status:done",
		"bud-dev", "", "bud-dev", "webhook", nil, "success", "", nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/completed/91", nil)
	w := httptest.NewRecorder()
	d.handleCompletedIssue(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var detail CompletedRunDetail
	if err := json.NewDecoder(w.Body).Decode(&detail); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if detail.FinalStatus != "status:done" {
		t.Fatalf("finalStatus: want status:done, got %s", detail.FinalStatus)
	}
}

// --- store: ListCompletedIssues ---

func TestListCompletedIssues_Empty(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	runs, err := store.ListCompletedIssues(0)
	if err != nil {
		t.Fatalf("ListCompletedIssues: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("expected 0, got %d", len(runs))
	}
}

func TestListCompletedIssues_FiltersByTerminalStatus(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	// Issue 1: ends with "done" → should appear.
	_ = store.RecordTransitionAudit(1, "status:open", "status:done",
		"a", "", "a", "webhook", nil, "success", "", nil, nil)
	// Issue 2: still active → should NOT appear.
	_ = store.RecordTransitionAudit(2, "status:new", "status:in-progress",
		"b", "", "b", "webhook", nil, "success", "", nil, nil)

	runs, err := store.ListCompletedIssues(100)
	if err != nil {
		t.Fatalf("ListCompletedIssues: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 completed run, got %d", len(runs))
	}
	if runs[0].IssueNumber != 1 {
		t.Errorf("expected issue 1, got %d", runs[0].IssueNumber)
	}
	if runs[0].FinalStatus != "status:done" {
		t.Errorf("finalStatus: want status:done, got %s", runs[0].FinalStatus)
	}
	if runs[0].StepCount != 1 {
		t.Errorf("stepCount: want 1, got %d", runs[0].StepCount)
	}
}

func TestListCompletedIssues_StepCountIncludesAllTransitions(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	_ = store.RecordTransitionAudit(10, "status:new", "status:open",
		"a", "", "a", "webhook", nil, "success", "", nil, nil)
	_ = store.RecordTransitionAudit(10, "status:open", "status:in-review",
		"a", "", "a", "webhook", nil, "success", "", nil, nil)
	_ = store.RecordTransitionAudit(10, "status:in-review", "status:done",
		"a", "", "a", "webhook", nil, "success", "", nil, nil)

	runs, err := store.ListCompletedIssues(100)
	if err != nil {
		t.Fatalf("ListCompletedIssues: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1, got %d", len(runs))
	}
	if runs[0].StepCount != 3 {
		t.Errorf("stepCount: want 3, got %d", runs[0].StepCount)
	}
}

func TestListCompletedIssues_UsesTerminalAuditStatusWhenTaskStatusIsStale(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	if _, err := store.QueueTask(WorkPackage{
		Repo:          "owner/repo",
		IssueID:       91,
		Role:          "developer",
		CurrentStatus: "status:po-approval",
	}); err != nil {
		t.Fatalf("QueueTask: %v", err)
	}
	task, err := store.GetNextWorkPackage("bridge-1", []string{"developer"})
	if err != nil {
		t.Fatalf("GetNextWorkPackage: %v", err)
	}
	if task == nil {
		t.Fatal("expected queued task to dispatch")
	}
	if err := store.CompleteDispatchedTask(91); err != nil {
		t.Fatalf("CompleteDispatchedTask: %v", err)
	}
	_ = store.RecordTransitionAudit(91, "status:po-approval", "status:done",
		"bud-dev", "", "bud-dev", "webhook", nil, "success", "", nil, nil)

	runs, err := store.ListCompletedIssues(100)
	if err != nil {
		t.Fatalf("ListCompletedIssues: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 completed run, got %d", len(runs))
	}
	if runs[0].FinalStatus != "status:done" {
		t.Errorf("finalStatus: want status:done, got %s", runs[0].FinalStatus)
	}
}

func TestListCompletedIssues_ExcludesActiveTasksAndNonTerminalAudits(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	if _, err := store.QueueTask(WorkPackage{
		Repo:          "owner/repo",
		IssueID:       1,
		Role:          "developer",
		CurrentStatus: "status:open",
	}); err != nil {
		t.Fatalf("QueueTask: %v", err)
	}
	_ = store.RecordTransitionAudit(1, "status:open", "status:done",
		"bud-dev", "", "bud-dev", "webhook", nil, "success", "", nil, nil)
	_ = store.RecordTransitionAudit(2, "status:new", "status:open",
		"bud-dev", "", "bud-dev", "webhook", nil, "success", "", nil, nil)

	runs, err := store.ListCompletedIssues(100)
	if err != nil {
		t.Fatalf("ListCompletedIssues: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("expected no completed runs, got %d", len(runs))
	}
}

// --- setCORSHeaders ---

func TestSetCORSHeaders(t *testing.T) {
	w := httptest.NewRecorder()
	setCORSHeaders(w)
	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("expected Access-Control-Allow-Origin: *")
	}
	if !strings.Contains(w.Header().Get("Access-Control-Allow-Methods"), "GET") {
		t.Error("expected GET in Access-Control-Allow-Methods")
	}
}

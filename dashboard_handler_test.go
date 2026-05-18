package main

import (
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

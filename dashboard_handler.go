package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DashboardServer holds the dependencies for the dashboard HTTP server.
type DashboardServer struct {
	store     *Store
	workflows *WorkflowRegistry
	hub       *SSEHub
	logger    interface{ Printf(string, ...interface{}) }
}

// GitHubIssueSummary is a lightweight representation of a GitHub issue used by
// the dashboard. It is populated from the tasks table (not the GitHub API) to
// avoid rate-limit concerns.
type GitHubIssueSummary struct {
	Number        int         `json:"number"`
	Title         string      `json:"title"`
	Repo          string      `json:"repo"`
	URL           string      `json:"url"`
	CurrentStatus string      `json:"currentStatus"`
	Assignees     []string    `json:"assignees"`
	ActiveTask    *ActiveTask `json:"activeTask,omitempty"`
	UpdatedAt     string      `json:"updatedAt"`
}

// ActiveTask is the task currently queued or dispatched for an issue.
type ActiveTask struct {
	ID         int64  `json:"id"`
	Role       string `json:"role"`
	TaskStatus string `json:"taskStatus"`
	BridgeID   string `json:"bridgeId,omitempty"`
	CreatedAt  string `json:"createdAt"`
}

// WorkflowGraphResponse is the graph-ready representation of a workflow definition.
type WorkflowGraphResponse struct {
	ID          string      `json:"id"`
	Key         string      `json:"key"`
	Description string      `json:"description,omitempty"`
	Nodes       []GraphNode `json:"nodes"`
	Edges       []GraphEdge `json:"edges"`
}

// GraphNode represents a status node in the workflow graph.
type GraphNode struct {
	ID       string `json:"id"`
	Role     string `json:"role"`
	Category string `json:"category"`
	Terminal bool   `json:"terminal"`
	Label    string `json:"label"`
}

// GraphEdge represents a transition edge in the workflow graph.
type GraphEdge struct {
	ID           string   `json:"id"`
	Source       string   `json:"source"`
	Target       string   `json:"target"`
	AllowedRoles []string `json:"allowedRoles"`
	Guard        string   `json:"guard,omitempty"`
	Description  string   `json:"description,omitempty"`
}

// WorkflowSummary is a brief description of a workflow for the list endpoint.
type WorkflowSummary struct {
	ID          string `json:"id"`
	Key         string `json:"key"`
	StatusCount int    `json:"statusCount"`
	EdgeCount   int    `json:"edgeCount"`
}

func (d *DashboardServer) handleDashboardIssues(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	tasks, err := d.store.ListActiveTasksAllIssues(200)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Deduplicate by issue number — keep the most recent task per issue.
	seen := make(map[int]bool)
	var issues []GitHubIssueSummary
	for _, t := range tasks {
		if seen[t.IssueNumber] {
			continue
		}
		seen[t.IssueNumber] = true

		at := &ActiveTask{
			ID:         t.ID,
			Role:       t.Role,
			TaskStatus: t.Status,
			CreatedAt:  t.CreatedAt,
		}
		if t.BridgeID.Valid {
			at.BridgeID = t.BridgeID.String
		}

		assignees := []string{}
		if t.Assignee != "" {
			assignees = append(assignees, t.Assignee)
		}

		title, _, _ := d.store.GetIssueMetadata(t.IssueNumber, "_title")

		issues = append(issues, GitHubIssueSummary{
			Number:        t.IssueNumber,
			Title:         title,
			Repo:          t.Repo,
			URL:           fmt.Sprintf("https://github.com/%s/issues/%d", t.Repo, t.IssueNumber),
			CurrentStatus: t.CurrentStatus,
			Assignees:     assignees,
			ActiveTask:    at,
			UpdatedAt:     t.CreatedAt,
		})
	}

	if issues == nil {
		issues = []GitHubIssueSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"issues": issues})
}

func (d *DashboardServer) handleDashboardIssue(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Extract {number} from path /api/v1/dashboard/issues/{number}
	parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/"), "/")
	numberStr := parts[len(parts)-1]
	number, err := strconv.Atoi(numberStr)
	if err != nil || number <= 0 {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}

	tasks, err := d.store.ListTasksByIssue(number, 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	audit, err := d.store.ListTransitionAudit(number, 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"number": number,
		"tasks":  tasks,
		"audit":  audit,
	})
}

func (d *DashboardServer) handleDashboardTasks(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 100
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}

	tasks, err := d.store.ListRecentTasks(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if tasks == nil {
		tasks = []TaskRow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

func (d *DashboardServer) handleDashboardAudit(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 100
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}

	audit, err := d.store.ListRecentAudit(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if audit == nil {
		audit = []TransitionAuditRow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"audit": audit})
}

// handleDashboardStream implements GET /api/v1/dashboard/stream as a Server-Sent Events endpoint.
func (d *DashboardServer) handleDashboardStream(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// Send an initial "connected" event.
	fmt.Fprintf(w, "event: connected\ndata: {\"clients\":%d}\n\n", d.hub.ClientCount()+1)
	flusher.Flush()

	ch := make(chan []byte, 16)
	d.hub.Register(ch)
	defer d.hub.Unregister(ch)

	// Send heartbeat every 30s to keep the connection alive through proxies.
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		case msg, ok := <-ch:
			if !ok {
				return
			}
			var evt SSEEvent
			if err := json.Unmarshal(msg, &evt); err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, msg)
			flusher.Flush()
		}
	}
}

func (d *DashboardServer) handleWorkflowList(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if d.workflows == nil {
		writeJSON(w, http.StatusOK, map[string]any{"workflows": []WorkflowSummary{}})
		return
	}

	var summaries []WorkflowSummary
	for _, key := range d.workflows.Keys() {
		wd := d.workflows.Get(key)
		summaries = append(summaries, WorkflowSummary{
			ID:          wd.Workflow.ID,
			Key:         wd.Workflow.Key,
			StatusCount: len(wd.Statuses),
			EdgeCount:   len(wd.Transitions),
		})
	}

	if summaries == nil {
		summaries = []WorkflowSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"workflows": summaries})
}

func (d *DashboardServer) handleWorkflowGet(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/"), "/")
	id := parts[len(parts)-1]
	if id == "" {
		writeError(w, http.StatusBadRequest, "workflow id is required")
		return
	}

	if d.workflows == nil {
		writeError(w, http.StatusNotFound, "no workflows loaded")
		return
	}

	// Look up by key or ID — workflows.Get falls back to the default on a miss,
	// so we must verify the returned workflow actually matches the requested id.
	var wd *WorkflowDef
	for _, k := range d.workflows.Keys() {
		candidate := d.workflows.Get(k)
		if candidate.Workflow.ID == id || candidate.Workflow.Key == id {
			wd = candidate
			break
		}
	}
	if wd == nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}

	writeJSON(w, http.StatusOK, buildWorkflowGraph(wd))
}

// CompletedRunDetail contains the full history of a completed workflow run.
type CompletedRunDetail struct {
	IssueNumber int                    `json:"issueNumber"`
	Repo        string                 `json:"repo"`
	WorkflowKey string                 `json:"workflowKey"`
	FinalStatus string                 `json:"finalStatus"`
	CompletedAt string                 `json:"completedAt"`
	StepCount   int                    `json:"stepCount"`
	Audit       []TransitionAuditRow   `json:"audit"`
	Tasks       []TaskRow              `json:"tasks"`
	Workflow    *WorkflowGraphResponse `json:"workflow,omitempty"`
}

func (d *DashboardServer) handleCompletedList(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 100
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}

	runs, err := d.store.ListCompletedIssues(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"completed": runs})
}

func (d *DashboardServer) handleCompletedIssue(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/"), "/")
	numberStr := parts[len(parts)-1]
	number, err := strconv.Atoi(numberStr)
	if err != nil || number <= 0 {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}

	tasks, err := d.store.ListTasksByIssue(number, 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	activeTask, err := d.store.FindActiveTaskByIssue(number)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if activeTask != nil {
		writeError(w, http.StatusNotFound, "no completed run found for this issue")
		return
	}

	audit, err := d.store.ListTransitionAudit(number, 200)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if audit == nil {
		audit = []TransitionAuditRow{}
	}
	terminalAudit := latestSuccessfulCompletedAudit(audit)
	if terminalAudit == nil {
		writeError(w, http.StatusNotFound, "no completed run found for this issue")
		return
	}

	repo := ""
	if len(tasks) > 0 {
		// tasks is ordered DESC; index 0 is the most recent task.
		repo = tasks[0].Repo
	}
	if repo == "" {
		if storedRepo, ok, err := d.store.GetRepoForIssue(number); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		} else if ok {
			repo = storedRepo
		}
	}
	workflowKey, _, _ := d.store.GetIssueWorkflowKey(number)

	detail := CompletedRunDetail{
		IssueNumber: number,
		Repo:        repo,
		WorkflowKey: workflowKey,
		FinalStatus: terminalAudit.ToStatus,
		CompletedAt: terminalAudit.CreatedAt,
		StepCount:   countSuccessfulAuditRows(audit),
		Audit:       audit,
		Tasks:       tasks,
	}

	if workflowKey != "" && d.workflows != nil {
		for _, k := range d.workflows.Keys() {
			candidate := d.workflows.Get(k)
			if candidate.Workflow.Key == workflowKey || candidate.Workflow.ID == workflowKey {
				graph := buildWorkflowGraph(candidate)
				detail.Workflow = &graph
				break
			}
		}
	}

	writeJSON(w, http.StatusOK, detail)
}

func latestSuccessfulCompletedAudit(audit []TransitionAuditRow) *TransitionAuditRow {
	for i := range audit {
		if audit[i].Result == "success" && isCompletedWorkflowStatus(audit[i].ToStatus) {
			return &audit[i]
		}
		if audit[i].Result == "success" && !isCompletedWorkflowStatus(audit[i].ToStatus) {
			return nil
		}
	}
	return nil
}

func countSuccessfulAuditRows(audit []TransitionAuditRow) int {
	count := 0
	for _, row := range audit {
		if row.Result == "success" {
			count++
		}
	}
	return count
}

func isCompletedWorkflowStatus(status string) bool {
	return status == "status:done" || status == "status:rejected"
}

func (d *DashboardServer) handleCompletedOrDetail(w http.ResponseWriter, r *http.Request) {
	tail := r.URL.Path[len("/api/v1/dashboard/completed/"):]
	if tail == "" {
		d.handleCompletedList(w, r)
		return
	}
	d.handleCompletedIssue(w, r)
}

func buildWorkflowGraph(wd *WorkflowDef) WorkflowGraphResponse {
	nodes := make([]GraphNode, 0, len(wd.Statuses))
	for _, s := range wd.Statuses {
		nodes = append(nodes, GraphNode{
			ID:       s.ID,
			Role:     s.Role,
			Category: s.Category,
			Terminal: s.Terminal,
			Label:    s.ID,
		})
	}

	edges := make([]GraphEdge, 0, len(wd.Transitions))
	for _, t := range wd.Transitions {
		for _, from := range t.From {
			if from == "" {
				continue
			}
			edges = append(edges, GraphEdge{
				ID:           fmt.Sprintf("%s__%s", t.ID, from),
				Source:       from,
				Target:       t.To,
				AllowedRoles: t.AllowedRoles,
				Guard:        t.Guard,
				Description:  t.Description,
			})
		}
		// Bootstrap transitions (From == nil) have no source node.
		if len(t.From) == 0 && t.To != "" {
			edges = append(edges, GraphEdge{
				ID:           t.ID,
				Source:       "__bootstrap__",
				Target:       t.To,
				AllowedRoles: t.AllowedRoles,
				Description:  t.Description,
			})
		}
	}

	return WorkflowGraphResponse{
		ID:    wd.Workflow.ID,
		Key:   wd.Workflow.Key,
		Nodes: nodes,
		Edges: edges,
	}
}

func setCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}

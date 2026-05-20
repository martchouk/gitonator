package main

import (
	"context"
	"errors"
	"net/http"
)

// runDashboard starts the dashboard HTTP server on the configured address (DashboardAddr).
// It is a no-op when DashboardAddr is empty.
func (s *Server) runDashboard(ctx context.Context, hub *SSEHub) error {
	if s.cfg.DashboardAddr == "" {
		return nil
	}

	d := &DashboardServer{
		store:     s.store,
		workflows: s.workflows,
		hub:       hub,
		logger:    s.logger,
	}

	mux := http.NewServeMux()

	// Active issue listing and detail
	mux.HandleFunc("/api/v1/dashboard/issues/", d.handleDashboardIssueOrList)
	mux.HandleFunc("/api/v1/dashboard/issues", d.handleDashboardIssues)

	// Task and audit history
	mux.HandleFunc("/api/v1/dashboard/tasks", d.handleDashboardTasks)
	mux.HandleFunc("/api/v1/dashboard/audit", d.handleDashboardAudit)

	// Completed workflow runs
	mux.HandleFunc("/api/v1/dashboard/completed/", d.handleCompletedOrDetail)
	mux.HandleFunc("/api/v1/dashboard/completed", d.handleCompletedList)

	// SSE live stream
	mux.HandleFunc("/api/v1/dashboard/stream", d.handleDashboardStream)

	// Workflow graph API
	mux.HandleFunc("/api/v1/workflows/", d.handleWorkflowGet)
	mux.HandleFunc("/api/v1/workflows", d.handleWorkflowList)

	// Health check (same response as main server)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"service": "github-issue-orchestrator-dashboard",
		})
	})

	srv := &http.Server{
		Addr:    s.cfg.DashboardAddr,
		Handler: s.loggingMiddleware(mux),
	}

	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()

	s.logger.Printf("dashboard server listening on %s", s.cfg.DashboardAddr)
	err := srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return context.Canceled
	}
	return err
}

// handleDashboardIssueOrList routes /api/v1/dashboard/issues/{number} to the
// single-issue handler when a number is present in the path; otherwise falls
// back to the list handler (trailing slash with no number).
func (d *DashboardServer) handleDashboardIssueOrList(w http.ResponseWriter, r *http.Request) {
	// Path is /api/v1/dashboard/issues/<something>
	// Strip the prefix and check what remains.
	tail := r.URL.Path[len("/api/v1/dashboard/issues/"):]
	if tail == "" {
		d.handleDashboardIssues(w, r)
		return
	}
	d.handleDashboardIssue(w, r)
}

package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

func (s *Server) runHTTP(ctx context.Context) error {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/webhook/github", s.handleGitHubWebhook)

	// Bridge polling endpoint.
	mux.HandleFunc("/api/v1/work/next", s.handleWorkNext)
	mux.HandleFunc("/api/v1/work/fail", s.handleWorkFail)

	// MCP tool inspection and manual override (replaces the removed stdio interface).
	mux.HandleFunc("/mcp/tools/call", s.handleMCPToolsCall)
	mux.HandleFunc("/mcp/tools/list", s.handleMCPToolsList)

	srv := &http.Server{
		Addr:    s.cfg.HTTPAddr,
		Handler: s.loggingMiddleware(mux),
	}

	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()

	s.logger.Println("http server listening on", s.cfg.HTTPAddr)
	err := srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return context.Canceled
	}
	return err
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"service": "github-issue-orchestrator",
	})
}

// handleWorkNext implements GET /api/v1/work/next?roles=po,developer&bridge_id=my-bridge
// It atomically selects and marks dispatched the oldest queued task matching the requested roles.
func (s *Server) handleWorkNext(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeAgent(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	rolesRaw := strings.TrimSpace(r.URL.Query().Get("roles"))
	if rolesRaw == "" {
		writeError(w, http.StatusBadRequest, "roles query parameter is required")
		return
	}
	bridgeID := strings.TrimSpace(r.URL.Query().Get("bridge_id"))
	if bridgeID == "" {
		writeError(w, http.StatusBadRequest, "bridge_id query parameter is required")
		return
	}

	var roles []string
	for _, rr := range strings.Split(rolesRaw, ",") {
		rr = strings.TrimSpace(rr)
		if rr != "" {
			roles = append(roles, rr)
		}
	}
	if len(roles) == 0 {
		writeError(w, http.StatusBadRequest, "roles must contain at least one role")
		return
	}

	pkg, err := s.store.GetNextWorkPackage(bridgeID, roles)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if pkg != nil {
		s.logger.Printf("work claimed: bridge=%s roles=%s task=%d issue=%d role=%s assignee=%s",
			bridgeID, rolesRaw, pkg.ID, pkg.IssueID, pkg.Role, pkg.Assignee)
		if s.hub != nil {
			s.hub.Broadcast(SSEEvent{
				Type: "task_dispatched",
				Data: map[string]interface{}{
					"issue_number": pkg.IssueID,
					"task_id":      pkg.ID,
					"role":         pkg.Role,
					"bridge_id":    bridgeID,
					"status":       pkg.CurrentStatus,
					"assignee":     pkg.Assignee,
				},
			})
		}
	} else {
		s.debugf("work/next: bridge=%s roles=%s no work available", bridgeID, rolesRaw)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"task": pkg, // nil when no work is available
	})
}

// handleWorkFail implements POST /api/v1/work/fail.
// Bridges call it when an agent process exits unsuccessfully, so the task can
// be made available to another bridge immediately instead of waiting for stale recovery.
func (s *Server) handleWorkFail(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeAgent(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		TaskID    int64  `json:"task_id"`
		IssueID   int    `json:"issue_id"`
		BridgeID  string `json:"bridge_id"`
		Agent     string `json:"agent"`
		ExitCode  int    `json:"exit_code"`
		ErrorText string `json:"error_text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	req.BridgeID = strings.TrimSpace(req.BridgeID)
	req.Agent = strings.TrimSpace(req.Agent)
	req.ErrorText = truncateForLog(strings.TrimSpace(req.ErrorText), 4000)
	if req.TaskID <= 0 {
		writeError(w, http.StatusBadRequest, "task_id is required")
		return
	}
	if req.BridgeID == "" {
		writeError(w, http.StatusBadRequest, "bridge_id is required")
		return
	}
	if req.ErrorText == "" {
		req.ErrorText = "agent failed without error output"
	}

	requeued, err := s.store.RequeueDispatchedTask(req.TaskID, req.BridgeID, req.ErrorText)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.IssueID > 0 {
		_ = s.store.RecordFailure(req.IssueID, "agent_run", req.ErrorText, map[string]any{
			"task_id":   req.TaskID,
			"bridge_id": req.BridgeID,
			"agent":     req.Agent,
			"exit_code": req.ExitCode,
			"requeued":  requeued,
		})
	}
	s.logger.Printf("work failed: bridge=%s task=%d issue=%d agent=%s exit=%d requeued=%t error=%s",
		req.BridgeID, req.TaskID, req.IssueID, req.Agent, req.ExitCode, requeued, req.ErrorText)

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"requeued": requeued,
	})
}

func truncateForLog(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max]
}

// handleMCPToolsCall implements POST /mcp/tools/call for manual inspection and overrides.
func (s *Server) handleMCPToolsCall(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeAgent(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Arguments == nil {
		req.Arguments = json.RawMessage(`{}`)
	}

	s.debugf("tool call: name=%s", req.Name)
	result, err := s.callTool(r.Context(), req.Name, req.Arguments)
	if err != nil {
		s.debugf("tool call failed: name=%s err=%v", req.Name, err)
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	s.debugf("tool call ok: name=%s", req.Name)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": result})
}

// handleMCPToolsList implements GET /mcp/tools/list for discoverability.
func (s *Server) handleMCPToolsList(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeAgent(w, r) {
		return
	}
	tools := s.tools()
	s.debugf("tools/list: serving %d tools", len(tools))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "tools": tools})
}

func (s *Server) handleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Select workflow: ?workflow=lean (default) or ?workflow=full.
	// An explicit param is persisted per issue so later webhooks without the
	// param continue to use the same workflow instead of falling back to lean.
	wfKey := r.URL.Query().Get("workflow")
	var wd *WorkflowDef
	if s.workflows != nil {
		wd = s.workflows.Get(wfKey)
	}

	payload, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}

	if s.cfg.WebhookSecret != "" {
		sig := r.Header.Get("X-Hub-Signature-256")
		if !validateWebhookSignature(s.cfg.WebhookSecret, payload, sig) {
			writeError(w, http.StatusUnauthorized, "invalid signature")
			return
		}
	}

	deliveryID := strings.TrimSpace(r.Header.Get("X-GitHub-Delivery"))
	eventType := strings.TrimSpace(r.Header.Get("X-GitHub-Event"))
	if deliveryID == "" || eventType == "" {
		writeError(w, http.StatusBadRequest, "missing github headers")
		return
	}

	s.debugf("webhook received: delivery=%s event=%s query=%s payload_bytes=%d",
		deliveryID, eventType, r.URL.RawQuery, len(payload))
	s.debugf("webhook payload: delivery=%s\n%s", deliveryID, string(payload))

	processed, err := s.store.IsDeliveryProcessed(deliveryID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if processed {
		s.debugf("webhook: duplicate delivery ignored delivery=%s event=%s", deliveryID, eventType)
		writeJSON(w, http.StatusAccepted, map[string]any{
			"ok":      true,
			"message": "duplicate delivery ignored",
		})
		return
	}

	if err := s.store.RecordDelivery(deliveryID, eventType, payload); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := s.processWebhookPayload(r.Context(), eventType, deliveryID, payload, wd, wfKey); err != nil {
		_ = s.store.MarkDeliveryFailed(deliveryID, err.Error())
		_ = s.store.RecordFailure(0, "webhook", err.Error(), map[string]string{
			"delivery_id": deliveryID,
			"event_type":  eventType,
		})
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	_ = s.store.MarkDeliveryProcessed(deliveryID)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok":          true,
		"delivery_id": deliveryID,
	})
}

func (s *Server) processWebhookPayload(ctx context.Context, eventType, deliveryID string, payload []byte, wd *WorkflowDef, wfKey string) error {
	var env struct {
		Action  string `json:"action"`
		Issue   Issue  `json:"issue"`
		Comment struct {
			ID   int64      `json:"id"`
			Body string     `json:"body"`
			User GitHubUser `json:"user"`
		} `json:"comment"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}

	if err := json.Unmarshal(payload, &env); err != nil {
		return err
	}
	if env.Issue.Number == 0 {
		return nil
	}

	s.logger.Printf(
		"webhook delivery=%s event=%s action=%s issue=%d",
		deliveryID, eventType, env.Action, env.Issue.Number,
	)

	// Capture the real worker from issue_comment webhooks. When an agent posts its
	// completion comment, comment.user.login is GitHub's own authenticated identity —
	// more reliable than parsing the "Author:" header from the comment body.
	if eventType == "issue_comment" && env.Action == "created" &&
		env.Comment.User.Login != "" && s.store != nil {
		if err := s.store.SetTaskClaimedBy(env.Issue.Number, env.Comment.User.Login); err != nil {
			s.debugf("SetTaskClaimedBy: issue=%d login=%s err=%v",
				env.Issue.Number, env.Comment.User.Login, err)
		} else {
			s.debugf("SetTaskClaimedBy: issue=%d login=%s", env.Issue.Number, env.Comment.User.Login)
		}
	}

	// Store issue title and repo for dashboard display and future event processing.
	if s.store != nil {
		if env.Issue.Title != "" {
			_ = s.store.SetIssueMetadata(env.Issue.Number, "_title", env.Issue.Title)
		}
		if env.Repository.FullName != "" {
			_ = s.store.SetIssueMetadata(env.Issue.Number, "_repo", env.Repository.FullName)
		}
	}

	// Persist an explicit workflow key so future webhooks without ?workflow= reuse it.
	// If no explicit key was given, look up the stored one and override wd.
	if s.store != nil && s.workflows != nil {
		if wfKey != "" {
			_ = s.store.SetIssueWorkflowKey(env.Issue.Number, wfKey)
		} else {
			if stored, ok, _ := s.store.GetIssueWorkflowKey(env.Issue.Number); ok && stored != "" {
				wd = s.workflows.Get(stored)
			}
		}
	}

	_, err := s.processIssueWith(ctx, env.Issue.Number, env.Repository.FullName, wd)
	return err
}

func validateWebhookSignature(secret string, payload []byte, provided string) bool {
	if !strings.HasPrefix(provided, "sha256=") {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(provided))
}

func (s *Server) authorizeAgent(w http.ResponseWriter, r *http.Request) bool {
	if s.cfg.AgentSharedToken == "" {
		writeError(w, http.StatusInternalServerError, "agent auth not configured")
		return false
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return false
	}
	token := strings.TrimSpace(strings.TrimPrefix(auth, prefix))
	if token != s.cfg.AgentSharedToken {
		writeError(w, http.StatusUnauthorized, "invalid bearer token")
		return false
	}
	return true
}

func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.logger.Printf("%s %s from=%s", r.Method, r.URL.Path, r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"ok": false, "error": msg})
}

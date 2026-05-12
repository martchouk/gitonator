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
	} else {
		s.debugf("work/next: bridge=%s roles=%s no work available", bridgeID, rolesRaw)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"task": pkg, // nil when no work is available
	})
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

	if err := s.processWebhookPayload(r.Context(), eventType, deliveryID, payload); err != nil {
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

func (s *Server) processWebhookPayload(ctx context.Context, eventType, deliveryID string, payload []byte) error {
	var env struct {
		Action  string `json:"action"`
		Issue   Issue  `json:"issue"`
		Comment struct {
			ID   int64      `json:"id"`
			Body string     `json:"body"`
			User GitHubUser `json:"user"`
		} `json:"comment"`
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

	// Handle /approve comments for stakeholder-wait states.
	if eventType == "issue_comment" && (env.Action == "created" || env.Action == "edited") {
		handled, err := s.processApproveComment(
			ctx,
			env.Issue.Number,
			env.Comment.ID,
			env.Comment.User.Login,
			env.Comment.Body,
		)
		if err != nil {
			return err
		}
		if handled {
			// Approve transitions are applied inline; processIssue queues the next task.
		}
	}

	_, err := s.processIssue(ctx, env.Issue.Number)
	return err
}

// processApproveComment handles /approve comments in stakeholder-wait states.
// Verification that the commenter is the stakeholder is done here, not in validateTransition,
// to avoid GitHub API timing issues with RequiresStakeholderApprove.
func (s *Server) processApproveComment(ctx context.Context, issueNumber int, commentID int64, actor, body string) (bool, error) {
	if !containsApprove(body) {
		return false, nil
	}

	issue, _, err := s.loadIssueAndComments(ctx, issueNumber, 0)
	if err != nil {
		return false, err
	}

	ws := computeWorkflowState(issue, nil)

	toStatus, ok := approveTransitionTarget(ws.StatusLabel)
	if !ok {
		return false, nil
	}

	stakeholder := resolveStakeholder(issue)
	if actor != stakeholder {
		s.logger.Printf("approve comment ignored: actor=%s is not stakeholder=%s issue=%d", actor, stakeholder, issueNumber)
		return false, nil
	}

	fromStatus := ws.StatusLabel
	fromAssignee := currentAssigneeOfIssue(issue)

	currentLabels := labelsToStrings(issue.Labels)
	var nextLabels []string
	for _, l := range currentLabels {
		if !strings.HasPrefix(l, "status:") {
			nextLabels = append(nextLabels, l)
		}
	}
	nextLabels = append(nextLabels, toStatus)

	if _, err := s.gh.SetIssueLabels(ctx, issueNumber, nextLabels); err != nil {
		_ = s.store.RecordTransitionAudit(
			issueNumber, fromStatus, toStatus, fromAssignee, "", actor,
			"webhook_comment", &commentID, "failed", "set labels failed: "+err.Error(), nil, nil,
		)
		return true, err
	}

	_ = s.store.RecordTransitionAudit(
		issueNumber, fromStatus, toStatus, fromAssignee, "", actor,
		"webhook_comment", &commentID, "applied", "", nil, nil,
	)
	s.logger.Printf("approve transition applied: issue=%d from=%s to=%s actor=%s", issueNumber, fromStatus, toStatus, actor)
	return true, nil
}

// approveTransitionTarget maps a stakeholder-wait status to the status it transitions to
// when a valid /approve comment is received. Returns ("", false) for all other statuses.
func approveTransitionTarget(fromStatus string) (string, bool) {
	switch fromStatus {
	case "status:awaiting-stakeholder-approval":
		return "status:architect-analysis", true
	case "status:awaiting-final-stakeholder-approval":
		return "status:done", true
	default:
		return "", false
	}
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

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
	"strconv"
	"strings"
)

func (s *Server) runHTTP(ctx context.Context) error {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/webhook/github", s.handleGitHubWebhook)

	mux.HandleFunc("/api/v1/agent/tasks", s.handleAgentTasks)
	mux.HandleFunc("/api/v1/agent/tasks/", s.handleAgentTaskAction)
	mux.HandleFunc("/api/v1/agent/comment", s.handleAgentComment)

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

	// Comment-driven transitions first.
	if eventType == "issue_comment" && (env.Action == "created" || env.Action == "edited") {
		handled, err := s.processIssueCommentDirective(
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
			return nil
		}
	}

	_, err := s.processIssue(ctx, env.Issue.Number)
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

func (s *Server) handleAgentTasks(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeAgent(w, r) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		assignee := strings.TrimSpace(r.URL.Query().Get("assignee"))
		if assignee == "" {
			writeError(w, http.StatusBadRequest, "missing assignee")
			return
		}

		limit := 20
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil && v > 0 && v <= 200 {
				limit = v
			}
		}

		tasks, err := s.store.ListQueuedTasksForAssignee(assignee, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":    true,
			"tasks": tasks,
		})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleAgentTaskAction(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeAgent(w, r) {
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/v1/agent/tasks/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	taskID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || taskID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid task id")
		return
	}
	action := parts[1]

	var body struct {
		Agent    string                 `json:"agent"`
		Message  string                 `json:"message"`
		Result   map[string]any         `json:"result"`
		Metadata map[string]interface{} `json:"metadata"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	switch action {
	case "claim":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if err := s.store.ClaimTask(taskID, strings.TrimSpace(body.Agent)); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})

	case "heartbeat":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if err := s.store.HeartbeatTask(taskID, strings.TrimSpace(body.Agent), body.Message); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})

	case "complete":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if err := s.store.CompleteTask(taskID, strings.TrimSpace(body.Agent), body.Result, body.Message); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})

	case "fail":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if err := s.store.FailTask(taskID, strings.TrimSpace(body.Agent), body.Message, body.Result); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})

	default:
		writeError(w, http.StatusNotFound, "unknown action")
	}
}

func (s *Server) handleAgentComment(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeAgent(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var body struct {
		IssueNumber int    `json:"issue_number"`
		Body        string `json:"body"`
		Agent       string `json:"agent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.IssueNumber <= 0 {
		writeError(w, http.StatusBadRequest, "invalid issue_number")
		return
	}
	if strings.TrimSpace(body.Body) == "" {
		writeError(w, http.StatusBadRequest, "empty body")
		return
	}

	comment, err := s.gh.PostIssueComment(r.Context(), body.IssueNumber, body.Body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"comment": comment,
	})
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
	writeJSON(w, status, map[string]any{
		"ok":    false,
		"error": msg,
	})
}

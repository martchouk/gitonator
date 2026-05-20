package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Config struct {
	GitHubToken         string
	HTTPAddr            string
	DashboardAddr       string
	WebhookSecret       string
	SQLitePath          string
	AgentSharedToken    string
	StaleAfterSeconds   int
	RecoverEverySeconds int
	WorkflowsDir        string
}

type Server struct {
	cfg       Config
	ghFor     GitHubClientFactory
	store     *Store
	logger    *log.Logger
	debug     bool
	workflows *WorkflowRegistry
	hub       *SSEHub
	// issueMu stores a *sync.Mutex per issue number. Entries are never evicted because
	// the number of tracked issues is bounded and deletion would add complexity for
	// negligible benefit. See issueProcessLock in dispatch.go.
	issueMu sync.Map
}

func (s *Server) debugf(format string, args ...interface{}) {
	if s.debug {
		s.logger.Printf("DEBUG "+format, args...)
	}
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(1)
	}

	debug := strings.ToUpper(strings.TrimSpace(os.Getenv("LOG_LEVEL"))) == "DEBUG"
	logger := log.New(os.Stderr, "[github-mcp] ", log.LstdFlags|log.LUTC)

	store, err := OpenStore(cfg.SQLitePath)
	if err != nil {
		logger.Println("fatal:", err)
		os.Exit(1)
	}
	defer store.Close()

	workflows, err := LoadWorkflowRegistry(cfg.WorkflowsDir, "lean")
	if err != nil {
		logger.Println("fatal: load workflow registry:", err)
		os.Exit(1)
	}
	logger.Printf("workflows loaded: keys=%s default=lean", strings.Join(workflows.Keys(), ","))

	hub := newSSEHub()

	server := &Server{
		cfg: cfg,
		ghFor: func(repo string) GitHubAPI {
			parts := strings.SplitN(repo, "/", 2)
			var owner, repoName string
			if len(parts) == 2 {
				owner, repoName = parts[0], parts[1]
			}
			return &GitHubClient{
				baseURL: "https://api.github.com",
				token:   cfg.GitHubToken,
				owner:   owner,
				repo:    repoName,
			}
		},
		store:     store,
		logger:    logger,
		debug:     debug,
		workflows: workflows,
		hub:       hub,
	}

	logger.Printf("started: addr=%s sqlite=%s", cfg.HTTPAddr, cfg.SQLitePath)
	if debug {
		logger.Printf("DEBUG config: stale_after=%ds recover_every=%ds agent_auth=%v webhook_secret=%v",
			cfg.StaleAfterSeconds, cfg.RecoverEverySeconds,
			cfg.AgentSharedToken != "", cfg.WebhookSecret != "")
	}

	if strings.TrimSpace(cfg.HTTPAddr) == "" {
		logger.Println("fatal: HTTP_ADDR must not be empty")
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)

	go server.runRecoveryLoop(ctx)

	go func() {
		errCh <- server.runHTTP(ctx)
	}()

	if server.cfg.DashboardAddr != "" {
		go func() {
			if err := server.runDashboard(ctx, hub); err != nil && !errors.Is(err, context.Canceled) {
				logger.Printf("dashboard server error: %v", err)
			}
		}()
		logger.Printf("dashboard server enabled on %s", server.cfg.DashboardAddr)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Println("shutdown signal received:", sig)
		server.debugf("cancelling context and waiting for http server to drain")
		cancel()
		time.Sleep(1 * time.Second)

	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			logger.Println("fatal:", err)
			os.Exit(1)
		}
		logger.Println("http server stopped")
	}
}

func loadConfig() (Config, error) {
	cfg := Config{
		GitHubToken:         strings.TrimSpace(os.Getenv("GITHUB_TOKEN")),
		HTTPAddr:            defaultString(os.Getenv("HTTP_ADDR"), "127.0.0.1:7777"),
		DashboardAddr:       strings.TrimSpace(os.Getenv("DASHBOARD_ADDR")),
		WebhookSecret:       strings.TrimSpace(os.Getenv("WEBHOOK_SECRET")),
		SQLitePath:          defaultString(os.Getenv("SQLITE_PATH"), "orchestrator.db"),
		AgentSharedToken:    strings.TrimSpace(os.Getenv("AGENT_SHARED_TOKEN")),
		StaleAfterSeconds:   getEnvInt("STALE_AFTER_SECONDS", 900),
		RecoverEverySeconds: getEnvInt("RECOVER_EVERY_SECONDS", 30),
		WorkflowsDir:        defaultString(os.Getenv("WORKFLOWS_DIR"), "workflows"),
	}
	if cfg.GitHubToken == "" {
		return cfg, fmt.Errorf("GITHUB_TOKEN is required")
	}
	return cfg, nil
}

func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}

func getEnvInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

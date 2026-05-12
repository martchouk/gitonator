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
	"syscall"
	"time"
)

type Config struct {
	GitHubToken         string
	Owner               string
	Repo                string
	HTTPAddr            string
	WebhookSecret       string
	SQLitePath          string
	AgentSharedToken    string
	StaleAfterSeconds   int
	RecoverEverySeconds int
}

type Server struct {
	cfg    Config
	gh     *GitHubClient
	store  *Store
	logger *log.Logger
	debug  bool
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

	server := &Server{
		cfg: cfg,
		gh: &GitHubClient{
			baseURL: "https://api.github.com",
			token:   cfg.GitHubToken,
			owner:   cfg.Owner,
			repo:    cfg.Repo,
		},
		store:  store,
		logger: logger,
		debug:  debug,
	}

	logger.Printf("started: repo=%s/%s addr=%s sqlite=%s",
		cfg.Owner, cfg.Repo, cfg.HTTPAddr, cfg.SQLitePath)
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
		Owner:               strings.TrimSpace(os.Getenv("GITHUB_OWNER")),
		Repo:                strings.TrimSpace(os.Getenv("GITHUB_REPO")),
		HTTPAddr:            defaultString(os.Getenv("HTTP_ADDR"), "127.0.0.1:7777"),
		WebhookSecret:       strings.TrimSpace(os.Getenv("WEBHOOK_SECRET")),
		SQLitePath:          defaultString(os.Getenv("SQLITE_PATH"), "orchestrator.db"),
		AgentSharedToken:    strings.TrimSpace(os.Getenv("AGENT_SHARED_TOKEN")),
		StaleAfterSeconds:   getEnvInt("STALE_AFTER_SECONDS", 900),
		RecoverEverySeconds: getEnvInt("RECOVER_EVERY_SECONDS", 30),
	}
	if cfg.GitHubToken == "" {
		return cfg, fmt.Errorf("GITHUB_TOKEN is required")
	}
	if cfg.Owner == "" || cfg.Repo == "" {
		return cfg, fmt.Errorf("GITHUB_OWNER and GITHUB_REPO are required")
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

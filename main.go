package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"strconv"
)

type Config struct {
	GitHubToken          	string
	Owner                	string
	Repo                 	string
	Debug                	bool
	HTTPAddr             	string
	WebhookSecret       	string
	SQLitePath           	string
	DispatchDir          	string
	DispatchCommand      	string
	DispatchTmuxTemplate 	string
	StakeholderOverride  	string
	AgentSharedToken 	string
	StaleAfterSeconds   	int
	RecoverEverySeconds 	int
}

type Server struct {
	cfg    Config
	gh     *GitHubClient
	store  *Store
	logger *log.Logger
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(1)
	}

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
	}

	go server.runRecoveryLoop(ctx)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if strings.TrimSpace(cfg.HTTPAddr) != "" {
		go func() {
			if err := server.runHTTP(ctx); err != nil && !errors.Is(err, context.Canceled) {
				logger.Println("http server error:", err)
			}
		}()
	}

	if err := server.runStdio(ctx, os.Stdin, os.Stdout); err != nil {
		logger.Println("fatal:", err)
		os.Exit(1)
	}
}

func loadConfig() (Config, error) {
	cfg := Config{
		GitHubToken:          	strings.TrimSpace(os.Getenv("GITHUB_TOKEN")),
		Owner:                	strings.TrimSpace(os.Getenv("GITHUB_OWNER")),
		Repo:                 	strings.TrimSpace(os.Getenv("GITHUB_REPO")),
		Debug:                	strings.EqualFold(strings.TrimSpace(os.Getenv("LOG_LEVEL")), "DEBUG"),
		HTTPAddr:             	defaultString(os.Getenv("HTTP_ADDR"), "127.0.0.1:7777"),
		WebhookSecret:        	strings.TrimSpace(os.Getenv("WEBHOOK_SECRET")),
		SQLitePath:           	defaultString(os.Getenv("SQLITE_PATH"), "orchestrator.db"),
		DispatchDir:          	defaultString(os.Getenv("DISPATCH_DIR"), "./dispatch"),
		DispatchCommand:      	strings.TrimSpace(os.Getenv("DISPATCH_COMMAND")),
		DispatchTmuxTemplate: 	strings.TrimSpace(os.Getenv("DISPATCH_TMUX_TEMPLATE")),
		StakeholderOverride:  	strings.TrimSpace(os.Getenv("STAKEHOLDER_OVERRIDE")),
		AgentSharedToken:     	strings.TrimSpace(os.Getenv("AGENT_SHARED_TOKEN")),
		StaleAfterSeconds: 	getEnvInt("STALE_AFTER_SECONDS", 900),
		RecoverEverySeconds: 	getEnvInt("RECOVER_EVERY_SECONDS", 30),
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

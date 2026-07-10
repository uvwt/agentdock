package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/httpx"
	"github.com/uvwt/agentdock/internal/logx"
	"github.com/uvwt/agentdock/internal/mcp"
	"github.com/uvwt/agentdock/internal/tools"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "agentdock: %v\n", err)
		os.Exit(1)
	}
}

func run() error {

	cfg := config.FromEnv()
	flag.StringVar(&cfg.Host, "host", cfg.Host, "HTTP bind host")
	flag.IntVar(&cfg.Port, "port", cfg.Port, "HTTP bind port")
	flag.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "log level: debug, info, warn, error")
	flag.StringVar(&cfg.RecallEndpoint, "recall-endpoint", cfg.RecallEndpoint, "optional RecallDock HTTP endpoint, for example http://127.0.0.1:18777")
	flag.StringVar(&cfg.NexusEndpoint, "nexus-endpoint", cfg.NexusEndpoint, "optional NexusDock base URL for workflow templates")
	flag.BoolVar(&cfg.BrowserEnabled, "browser-enabled", cfg.BrowserEnabled, "expose optional browser automation tools")
	flag.BoolVar(&cfg.Stdio, "stdio", cfg.Stdio, "serve JSON-RPC over stdio")
	flag.Parse()
	if err := cfg.Normalize(); err != nil {
		return err
	}
	if err := cfg.ValidateAuth(); err != nil {
		return err
	}
	logx.Setup(cfg.LogLevel)
	slog.Info("server starting", "agentdock_home", cfg.AgentDockHome, "agentdock_default_dir", cfg.AgentDockDefaultDir, "path_model", config.PathModel, "host", cfg.Host, "port", cfg.Port, "stdio", cfg.Stdio, "log_level", cfg.LogLevel, "recall_enabled", cfg.RecallEndpoint != "", "nexus_enabled", cfg.NexusEndpoint != "", "browser_enabled", cfg.BrowserEnabled)
	runtime, err := tools.NewRuntime(cfg)
	if err != nil {
		return err
	}
	server := mcp.NewServer(runtime, cfg)
	if cfg.Stdio {
		return server.ServeStdio(os.Stdin, os.Stdout)
	}
	return httpx.Serve(server, cfg)
}

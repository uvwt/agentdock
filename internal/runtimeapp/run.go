package runtimeapp

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/httpx"
	"github.com/uvwt/agentdock/internal/logx"
	"github.com/uvwt/agentdock/internal/mcp"
	"github.com/uvwt/agentdock/internal/tools"
)

// Run boots the shared AgentDock core (runtime + MCP + HTTP).
// CLI and desktop entrypoints both call this so behavior stays unified.
// There is no operator dashboard: product UX is ChatGPT web + MCP.
func Run(ctx context.Context, cfg config.Config) error {
	if err := cfg.Normalize(); err != nil {
		return err
	}
	if err := cfg.ValidateAuth(); err != nil {
		return err
	}
	logx.Setup(cfg.LogLevel)
	slog.Info("server starting",
		"agentdock_home", cfg.AgentDockHome,
		"agentdock_default_dir", cfg.AgentDockDefaultDir,
		"path_model", config.PathModel,
		"host", cfg.Host,
		"port", cfg.Port,
		"stdio", cfg.Stdio,
		"log_level", cfg.LogLevel,
		"recall_enabled", cfg.NexusEndpoint != "",
		"browser_enabled", cfg.BrowserEnabled,
	)
	rt, err := tools.NewRuntime(cfg)
	if err != nil {
		return err
	}
	defer func() {
		if err := rt.Close(); err != nil {
			slog.Warn("runtime close failed", "error", err)
		}
	}()
	server := mcp.NewServer(rt, cfg)
	if cfg.Stdio {
		return serveStdio(ctx, server)
	}
	return httpx.Serve(ctx, server, cfg)
}

func serveStdio(ctx context.Context, server *mcp.Server) error {
	done := make(chan error, 1)
	go func() { done <- server.ServeStdio(os.Stdin, os.Stdout) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return nil
	}
}

// MCPURL returns the local MCP endpoint for operator messaging.
func MCPURL(cfg config.Config) string {
	host := cfg.Host
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%d/mcp", host, cfg.Port)
}

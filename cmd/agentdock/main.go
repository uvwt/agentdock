package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/httpx"
	"github.com/uvwt/agentdock/internal/logx"
	"github.com/uvwt/agentdock/internal/mcp"
	"github.com/uvwt/agentdock/internal/sandbox"
	"github.com/uvwt/agentdock/internal/tools"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "agentdock: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) >= 3 && os.Args[1] == "__landlock_exec" {
		return sandbox.ExecRestricted(os.Args[2])
	}

	cfg := config.FromEnv()
	flag.StringVar(&cfg.Workspace, "workspace", cfg.Workspace, "workspace root")
	flag.StringVar(&cfg.Host, "host", cfg.Host, "HTTP bind host")
	flag.IntVar(&cfg.Port, "port", cfg.Port, "HTTP bind port")
	flag.StringVar(&cfg.AuthToken, "auth-token", cfg.AuthToken, "optional bearer token")
	flag.StringVar(&cfg.ToolProfile, "tool-profile", cfg.ToolProfile, "tool profile")
	flag.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "log level: debug, info, warn, error")
	flag.StringVar(&cfg.SandboxMode, "sandbox-mode", cfg.SandboxMode, "command sandbox mode: landlock or none")
	flag.StringVar(&cfg.AgentDockDir, "agentdock-dir", cfg.AgentDockDir, "AgentDock control directory; absolute or workspace-relative")
	flag.StringVar(&cfg.ConnectorDir, "connector-dir", cfg.ConnectorDir, "workspace-relative connector directory")
	flag.StringVar(&cfg.ContextDir, "context-dir", cfg.ContextDir, "AgentDock-relative context directory")
	flag.BoolVar(&cfg.BrowserEnabled, "browser-enabled", cfg.BrowserEnabled, "expose optional browser automation tools")
	flag.StringVar(&cfg.BrowserRunnerDir, "browser-runner-dir", cfg.BrowserRunnerDir, "workspace-relative browser runner directory")
	flag.StringVar(&cfg.BrowserArtifactDir, "browser-artifact-dir", cfg.BrowserArtifactDir, "workspace-relative browser artifact directory")
	flag.BoolVar(&cfg.EnableViewImage, "enable-view-image", cfg.EnableViewImage, "expose view_image tool")
	flag.BoolVar(&cfg.Stdio, "stdio", cfg.Stdio, "serve JSON-RPC over stdio")
	flag.BoolVar(&cfg.DangerouslySkipAllPermissions, "dangerously-skip-all-permissions", cfg.DangerouslySkipAllPermissions, "auto-grant permission-gated operations")
	_ = flag.Bool("oauth-mode", false, "compatibility placeholder")
	flag.Parse()
	cfg.Normalize()
	logx.Setup(cfg.LogLevel)
	logx.Info("server starting", "workspace", cfg.Workspace, "host", cfg.Host, "port", cfg.Port, "stdio", cfg.Stdio, "tool_profile", cfg.ToolProfile, "log_level", cfg.LogLevel, "sandbox_mode", cfg.SandboxMode, "agent_dock_dir", cfg.AgentDockDir, "connector_dir", cfg.ConnectorDir, "context_dir", cfg.ContextDir, "browser_enabled", cfg.BrowserEnabled, "browser_runner_dir", cfg.BrowserRunnerDir)
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

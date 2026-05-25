package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/uvwt/coding-tools-mcp-go/internal/config"
	"github.com/uvwt/coding-tools-mcp-go/internal/httpx"
	"github.com/uvwt/coding-tools-mcp-go/internal/logx"
	"github.com/uvwt/coding-tools-mcp-go/internal/mcp"
	"github.com/uvwt/coding-tools-mcp-go/internal/sandbox"
	"github.com/uvwt/coding-tools-mcp-go/internal/tools"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "coding-tools-mcp: %v\n", err)
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
	flag.StringVar(&cfg.ConnectorDir, "connector-dir", cfg.ConnectorDir, "workspace-relative connector directory")
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
	logx.Info("server starting", "workspace", cfg.Workspace, "host", cfg.Host, "port", cfg.Port, "stdio", cfg.Stdio, "tool_profile", cfg.ToolProfile, "log_level", cfg.LogLevel, "sandbox_mode", cfg.SandboxMode, "connector_dir", cfg.ConnectorDir, "browser_enabled", cfg.BrowserEnabled, "browser_runner_dir", cfg.BrowserRunnerDir)
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

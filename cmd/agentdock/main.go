package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/httpx"
	"github.com/uvwt/agentdock/internal/logx"
	"github.com/uvwt/agentdock/internal/mcp"
	"github.com/uvwt/agentdock/internal/nexusagent"
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
	flag.StringVar(&cfg.Mode, "mode", cfg.Mode, "runtime mode: sandboxed or host")
	flag.StringVar(&cfg.Host, "host", cfg.Host, "HTTP bind host")
	flag.IntVar(&cfg.Port, "port", cfg.Port, "HTTP bind port")
	flag.StringVar(&cfg.AuthToken, "auth-token", cfg.AuthToken, "optional bearer token")
	flag.StringVar(&cfg.ToolProfile, "tool-profile", cfg.ToolProfile, "tool profile")
	flag.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "log level: debug, info, warn, error")
	flag.StringVar(&cfg.SandboxMode, "sandbox-mode", cfg.SandboxMode, "command sandbox mode: landlock or none")
	flag.StringVar(&cfg.PathPolicy, "path-policy", cfg.PathPolicy, "path policy: workspace or host")
	flag.StringVar(&cfg.AgentDockDir, "agentdock-dir", cfg.AgentDockDir, "AgentDock control directory; absolute or workspace-relative")
	flag.StringVar(&cfg.RecallEndpoint, "recall-endpoint", cfg.RecallEndpoint, "optional RecallDock HTTP endpoint, for example http://127.0.0.1:18777")
	flag.StringVar(&cfg.RecallEndpoint, "memory-endpoint", cfg.RecallEndpoint, "deprecated alias for --recall-endpoint")
	flag.StringVar(&cfg.RecallToken, "recall-token", cfg.RecallToken, "optional RecallDock bearer credential")
	flag.StringVar(&cfg.RecallToken, "memory-token", cfg.RecallToken, "deprecated alias for --recall-token")
	flag.IntVar(&cfg.RecallTimeoutMS, "recall-timeout-ms", cfg.RecallTimeoutMS, "RecallDock HTTP timeout in milliseconds")
	flag.IntVar(&cfg.RecallTimeoutMS, "memory-timeout-ms", cfg.RecallTimeoutMS, "deprecated alias for --recall-timeout-ms")
	flag.StringVar(&cfg.NexusEndpoint, "nexus-endpoint", cfg.NexusEndpoint, "optional AgentDock Nexus base URL")
	flag.StringVar(&cfg.NexusDeviceName, "nexus-device-name", cfg.NexusDeviceName, "AgentDock Nexus device display name")
	flag.StringVar(&cfg.NexusStateDir, "nexus-state-dir", cfg.NexusStateDir, "AgentDock Nexus local state directory")
	flag.IntVar(&cfg.NexusHeartbeatSeconds, "nexus-heartbeat-seconds", cfg.NexusHeartbeatSeconds, "AgentDock Nexus heartbeat interval seconds")
	flag.StringVar(&cfg.ArtifactTargetsJSON, "artifact-targets-json", cfg.ArtifactTargetsJSON, "JSON object mapping logical artifact targets to local directories")
	flag.BoolVar(&cfg.ArtifactFetchEnabled, "artifact-fetch-enabled", cfg.ArtifactFetchEnabled, "enable high-risk controlled absolute-path artifact fetch")
	flag.StringVar(&cfg.ArtifactFetchDenyJSON, "artifact-fetch-deny-json", cfg.ArtifactFetchDenyJSON, "JSON array of additional absolute path prefixes denied to artifact fetch")
	flag.BoolVar(&cfg.BrowserEnabled, "browser-enabled", cfg.BrowserEnabled, "expose optional browser automation tools")
	flag.StringVar(&cfg.BrowserRunnerDir, "browser-runner-dir", cfg.BrowserRunnerDir, "workspace-relative browser runner directory")
	flag.StringVar(&cfg.BrowserArtifactDir, "browser-artifact-dir", cfg.BrowserArtifactDir, "workspace-relative browser artifact directory")
	flag.BoolVar(&cfg.DesktopEnabled, "desktop-enabled", cfg.DesktopEnabled, "expose experimental macOS desktop automation tools")
	flag.StringVar(&cfg.DesktopArtifactDir, "desktop-artifact-dir", cfg.DesktopArtifactDir, "AgentDock-relative desktop artifact directory")
	flag.BoolVar(&cfg.EnableViewImage, "enable-view-image", cfg.EnableViewImage, "expose view_image tool")
	flag.BoolVar(&cfg.Stdio, "stdio", cfg.Stdio, "serve JSON-RPC over stdio")
	flag.BoolVar(&cfg.DangerouslySkipAllPermissions, "dangerously-skip-all-permissions", cfg.DangerouslySkipAllPermissions, "auto-grant permission-gated operations")
	_ = flag.Bool("oauth-mode", false, "compatibility placeholder")
	flag.Parse()
	cfg.Normalize()
	logx.Setup(cfg.LogLevel)
	slog.Info("server starting", "workspace", cfg.Workspace, "mode", cfg.Mode, "path_policy", cfg.PathPolicy, "host", cfg.Host, "port", cfg.Port, "stdio", cfg.Stdio, "tool_profile", cfg.ToolProfile, "log_level", cfg.LogLevel, "sandbox_mode", cfg.SandboxMode, "agent_dock_dir", cfg.AgentDockDir, "recall_enabled", cfg.RecallEndpoint != "", "nexus_enabled", cfg.NexusEndpoint != "", "browser_enabled", cfg.BrowserEnabled, "browser_runner_dir", cfg.BrowserRunnerDir, "desktop_enabled", cfg.DesktopEnabled)
	runtime, err := tools.NewRuntime(cfg)
	if err != nil {
		return err
	}
	server := mcp.NewServer(runtime, cfg)
	if cfg.Stdio {
		return server.ServeStdio(os.Stdin, os.Stdout)
	}
	if enabled, err := nexusagent.Start(context.Background(), cfg); err != nil {
		return err
	} else if enabled {
		slog.Info("nexus agent enabled", "endpoint", cfg.NexusEndpoint, "device_name", cfg.NexusDeviceName)
	}
	return httpx.Serve(server, cfg)
}

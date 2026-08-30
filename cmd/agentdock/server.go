package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/uvwt/agentdock/cmd/agentdock/internal/logx"
	"github.com/uvwt/agentdock/internal/app"
	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/desktopcontrol"
	"github.com/uvwt/agentdock/internal/desktopruntime"
	"github.com/uvwt/agentdock/internal/httpx"
	"github.com/uvwt/agentdock/internal/mcp"
	"github.com/uvwt/agentdock/internal/nexusbridge"
	"github.com/uvwt/agentdock/internal/selfupdate"
)

func runServer(ctx context.Context, args []string, stderr io.Writer) error {
	// 旧 Nexus 环境凭据已被配对身份取代。启动前主动清除，避免废弃密钥
	// 继续留在进程环境并被后续启动的本地工具继承。
	_ = os.Unsetenv("AGENTDOCK_NEXUS_ENDPOINT")
	_ = os.Unsetenv("AGENTDOCK_NEXUS_TOKEN")
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("agentdock", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "用法：")
		fmt.Fprintln(stderr, "  agentdock [服务参数]")
		fmt.Fprintln(stderr, "  agentdock --version")
		fmt.Fprintln(stderr, "  agentdock version [--json]")
		fmt.Fprintln(stderr, "  agentdock update [--check]")
		fmt.Fprintln(stderr, "  agentdock service <status|start|stop|restart|autostart> --runtime-root <目录>")
		fmt.Fprintln(stderr, "  agentdock tunnel <status|start|stop|restart|regenerate|configure|autostart> --runtime-root <目录>")
		fmt.Fprintln(stderr, "  agentdock skill bootstrap --bundle <目录>")
		fmt.Fprintln(stderr, "  agentdock nexus pair --endpoint <URL> --code <配对码> [--name <名称>]")
		fmt.Fprintln(stderr, "  agentdock nexus status [--json]")
		fmt.Fprintln(stderr, "\n服务参数：")
		flags.PrintDefaults()
	}
	flags.StringVar(&cfg.Host, "host", cfg.Host, "HTTP bind host")
	flags.IntVar(&cfg.Port, "port", cfg.Port, "HTTP bind port")
	flags.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "log level: debug, info, warn, error")
	flags.BoolVar(&cfg.BrowserEnabled, "browser-enabled", cfg.BrowserEnabled, "expose optional browser automation tools")
	flags.StringVar(&cfg.BrowserExecutablePath, "browser-executable-path", cfg.BrowserExecutablePath, "optional absolute Chrome, Chromium, or Edge executable path")
	flags.StringVar(&cfg.BrowserCDPURL, "browser-cdp-url", cfg.BrowserCDPURL, "optional existing Chromium CDP endpoint to attach")
	flags.BoolVar(&cfg.BrowserReuseExistingCDP, "browser-reuse-existing-cdp", cfg.BrowserReuseExistingCDP, "discover and reuse a unique local existing CDP browser before launching one")
	flags.BoolVar(&cfg.Stdio, "stdio", cfg.Stdio, "serve JSON-RPC over stdio")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("未知命令或参数：%s", flags.Arg(0))
	}
	if err := cfg.Normalize(); err != nil {
		return err
	}
	if err := cfg.ValidateAuth(); err != nil {
		return err
	}
	identity, identityErr := nexusbridge.Load(cfg.AgentDockHome)
	if identityErr == nil {
		// Nexus 地址和 Device Token 只来自配对身份，避免环境变量、控制面板与
		// 配对文件形成多套凭据来源和不明确的优先级。
		cfg.NexusEndpoint = identity.Endpoint
		cfg.NexusDeviceToken = identity.DeviceToken
	} else if identityErr != nil && !errors.Is(identityErr, os.ErrNotExist) {
		slog.Error("NexusDock device identity ignored", "error", identityErr)
	}
	logx.Setup(cfg.LogLevel)
	if err := selfupdate.RepairDesktopRuntimeIfNeeded(ctx, stderr); err != nil {
		// Windows v0.7.4 及更早版本只会替换 core。新版 core 启动时尝试补齐同版本控制面板，
		// 失败不应阻断 MCP 服务启动；保留明确日志并在下次启动继续重试。
		slog.Warn("desktop runtime repair skipped", "error", err)
	}
	slog.Info("server starting", "agentdock_home", cfg.AgentDockHome, "agentdock_default_dir", cfg.AgentDockDefaultDir, "path_model", config.PathModel, "host", cfg.Host, "port", cfg.Port, "stdio", cfg.Stdio, "log_level", cfg.LogLevel, "recall_enabled", cfg.NexusEndpoint != "", "nexus_enabled", cfg.NexusEndpoint != "", "browser_enabled", cfg.BrowserEnabled)
	runtime, err := app.NewRuntime(cfg)
	if err != nil {
		return err
	}
	defer func() {
		if err := runtime.Close(); err != nil {
			slog.Warn("runtime close failed", "error", err)
		}
	}()
	server := mcp.NewServer(runtime, cfg)
	if cfg.Stdio {
		return serveStdio(ctx, server)
	}
	nexusStatus := &nexusbridge.ConnectionState{}
	if identityErr == nil {
		go nexusbridge.NewClient(identity, server, runtime, nexusStatus).Run(ctx)
	}
	runtimeRoot := strings.TrimSpace(os.Getenv("AGENTDOCK_RUNTIME_ROOT"))
	if runtimeRoot == "" {
		return httpx.Serve(ctx, server, runtime, cfg)
	}

	// 桌面控制端点与 HTTP/MCP 服务共享同一生命周期；任一端点异常退出时，
	// 取消另一个端点并返回明确错误，避免后台只剩半套控制面。
	runtimeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 2)
	go func() { done <- httpx.Serve(runtimeCtx, server, runtime, cfg) }()
	go func() {
		done <- desktopcontrol.Serve(runtimeCtx, runtimeRoot, func(controlCtx context.Context, request desktopcontrol.Request) (any, error) {
			return desktopruntime.DispatchControlRequest(
				controlCtx,
				request,
				desktopruntime.ControlRuntimeStatus{NexusConnected: nexusStatus.Connected()},
			)
		})
	}()
	err = <-done
	cancel()
	<-done
	return err
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

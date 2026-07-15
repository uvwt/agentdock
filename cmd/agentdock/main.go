package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/uvwt/agentdock/internal/buildinfo"
	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/httpx"
	"github.com/uvwt/agentdock/internal/logx"
	"github.com/uvwt/agentdock/internal/mcp"
	"github.com/uvwt/agentdock/internal/selfupdate"
	"github.com/uvwt/agentdock/internal/tools"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "agentdock: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if handled, err := selfupdate.HandleInternalCommand(ctx, args); handled {
		return err
	}
	if len(args) == 1 && args[0] == "--version" {
		printVersion(stdout)
		return nil
	}
	if len(args) > 0 && args[0] == "update" {
		if len(args) != 1 {
			return errors.New("update 不接受额外参数")
		}
		return selfupdate.Run(ctx, stdout)
	}
	return runServer(ctx, args, stderr)
}

func printVersion(output io.Writer) {
	info := buildinfo.Current()
	fmt.Fprintf(output, "AgentDock v%s\n", strings.TrimPrefix(info.Version, "v"))
	fmt.Fprintf(output, "commit: %s\n", info.Commit)
	fmt.Fprintf(output, "built: %s\n", info.BuildDate)
	fmt.Fprintf(output, "go: %s\n", info.GoVersion)
	fmt.Fprintf(output, "platform: %s\n", info.Platform)
}

func runServer(ctx context.Context, args []string, stderr io.Writer) error {
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
		fmt.Fprintln(stderr, "  agentdock update")
		fmt.Fprintln(stderr, "\n服务参数：")
		flags.PrintDefaults()
	}
	flags.StringVar(&cfg.Host, "host", cfg.Host, "HTTP bind host")
	flags.IntVar(&cfg.Port, "port", cfg.Port, "HTTP bind port")
	flags.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "log level: debug, info, warn, error")
	flags.StringVar(&cfg.NexusEndpoint, "nexus-endpoint", cfg.NexusEndpoint, "optional NexusDock base URL for Recall memory and workflow APIs")
	flags.BoolVar(&cfg.BrowserEnabled, "browser-enabled", cfg.BrowserEnabled, "expose optional browser automation tools")
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
	logx.Setup(cfg.LogLevel)
	slog.Info("server starting", "agentdock_home", cfg.AgentDockHome, "agentdock_default_dir", cfg.AgentDockDefaultDir, "path_model", config.PathModel, "host", cfg.Host, "port", cfg.Port, "stdio", cfg.Stdio, "log_level", cfg.LogLevel, "recall_enabled", cfg.NexusEndpoint != "", "nexus_enabled", cfg.NexusEndpoint != "", "browser_enabled", cfg.BrowserEnabled)
	runtime, err := tools.NewRuntime(cfg)
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

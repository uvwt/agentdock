package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/uvwt/agentdock/internal/buildinfo"
	"github.com/uvwt/agentdock/internal/desktopruntime"
	"github.com/uvwt/agentdock/internal/selfupdate"
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
	if len(args) > 0 && args[0] == "version" {
		switch {
		case len(args) == 1:
			printVersion(stdout)
			return nil
		case len(args) == 2 && args[1] == "--json":
			return json.NewEncoder(stdout).Encode(buildinfo.Current())
		default:
			return errors.New("用法：agentdock version [--json]")
		}
	}
	if len(args) > 0 && args[0] == "update" {
		switch {
		case len(args) == 1:
			return selfupdate.Run(ctx, stdout)
		case len(args) == 2 && args[1] == "--check":
			result, err := selfupdate.Check(ctx)
			if err != nil {
				return err
			}
			return json.NewEncoder(stdout).Encode(result)
		case len(args) == 2 && args[1] == "--progress-json":
			// stdout 是稳定的机器事件流；人类可读日志单独走 stderr，桌面端无需解析文案。
			return selfupdate.RunWithProgress(ctx, stderr, stdout)
		case len(args) == 7 && args[1] == "--local-archive" && args[3] == "--checksum" && args[5] == "--target-version":
			// Setup 在 generation 布局建立后只负责分发离线 Release ZIP；真正的
			// stage/trial/commit/rollback 仍走与在线更新相同的 Go Update Engine。
			return selfupdate.RunLocalArchive(ctx, args[2], args[4], args[6], stdout)
		default:
			return errors.New("用法：agentdock update [--check|--progress-json|--local-archive <zip> --checksum <sha256> --target-version <version>]")
		}
	}
	if len(args) > 0 && args[0] == "service" {
		return runServiceCommand(ctx, args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "tunnel" {
		return desktopruntime.RunTunnelCommand(ctx, args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "config" {
		return desktopruntime.RunConfigCommand(ctx, args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "skill" {
		return runSkillCommand(ctx, args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "nexus" {
		return runNexusCommand(ctx, args[1:], stdout, stderr)
	}
	return runServer(ctx, args, stderr)
}

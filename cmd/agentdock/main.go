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
		default:
			return errors.New("用法：agentdock update [--check]")
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

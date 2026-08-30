package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/nexusbridge"
)

func runNexusCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return nexusCommandUsageError()
	}
	switch args[0] {
	case "pair":
		return runNexusPairCommand(ctx, args[1:], stdout, stderr)
	case "status":
		return runNexusStatusCommand(args[1:], stdout, stderr)
	default:
		return nexusCommandUsageError()
	}
}
func runNexusPairCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("agentdock nexus pair", flag.ContinueOnError)
	flags.SetOutput(stderr)
	endpoint := flags.String("endpoint", "", "NexusDock public base URL")
	code := flags.String("code", "", "one-time pairing code")
	name := flags.String("name", "", "device display name (defaults to hostname)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("用法：agentdock nexus pair --endpoint <URL> --code <配对码> [--name <名称>]")
	}
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	if err := cfg.Normalize(); err != nil {
		return err
	}
	identity, err := nexusbridge.Pair(ctx, cfg.AgentDockHome, nexusbridge.PairOptions{Endpoint: *endpoint, Code: *code, Name: *name})
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "AgentDock 已配对到 NexusDock，node_id=%s；重启 AgentDock 后自动连接。\n", identity.NodeID)
	return nil
}
func runNexusStatusCommand(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("agentdock nexus status", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonOutput := flags.Bool("json", false, "print machine-readable status")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return nexusCommandUsageError()
	}
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	if err := cfg.Normalize(); err != nil {
		return err
	}
	status, err := nexusbridge.ReadStatus(cfg.AgentDockHome)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return json.NewEncoder(stdout).Encode(status)
	}
	if !status.Paired {
		_, err = fmt.Fprintln(stdout, "AgentDock 尚未与 NexusDock 配对。")
		return err
	}
	_, err = fmt.Fprintf(stdout, "AgentDock 已配对到 %s，node_id=%s，Device Token 已保存。\n", status.Endpoint, status.NodeID)
	return err
}
func nexusCommandUsageError() error {
	return errors.New("用法：agentdock nexus <pair --endpoint <URL> --code <配对码> [--name <名称>] | status [--json]>")
}

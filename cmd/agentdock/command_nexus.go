package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

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
	home := flags.String("agentdock-home", "", "AgentDock state directory (must match Core AGENTDOCK_HOME)")
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
	if strings.TrimSpace(*home) != "" {
		cfg.AgentDockHome = strings.TrimSpace(*home)
	}
	if err := cfg.Normalize(); err != nil {
		return err
	}
	// Linux systemd 的 Core 往往不以当前终端账号运行；配对前提示实际路径，
	// 避免一次性配对码消耗后才发现凭据落入了登录用户的 HOME。
	if runtime.GOOS == "linux" && *home == "" && os.Getenv("AGENTDOCK_HOME") == "" {
		fmt.Fprintf(stderr, "注意：未指定 AgentDock 数据目录，配对将写入 %s；systemd 服务请先确认其 AGENTDOCK_HOME，并使用服务用户及 --agentdock-home 配对。\n", cfg.AgentDockHome)
	}
	identity, err := nexusbridge.Pair(ctx, cfg.AgentDockHome, nexusbridge.PairOptions{Endpoint: *endpoint, Code: *code, Name: *name})
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "NexusDock 配对凭据已保存，node_id=%s（后台服务连接状态需另行验证）。\n", identity.NodeID)
	fmt.Fprintf(stdout, "设备凭据位置：%s；Core 必须使用相同的 AGENTDOCK_HOME。\n", filepath.Join(cfg.AgentDockHome, "nexus", "device.json"))
	return nil
}
func runNexusStatusCommand(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("agentdock nexus status", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonOutput := flags.Bool("json", false, "print machine-readable status")
	home := flags.String("agentdock-home", "", "AgentDock state directory (must match Core AGENTDOCK_HOME)")
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
	if strings.TrimSpace(*home) != "" {
		cfg.AgentDockHome = strings.TrimSpace(*home)
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
		_, err = fmt.Fprintf(stdout, "AgentDock 尚未与 NexusDock 配对（当前数据目录：%s）。\n", cfg.AgentDockHome)
		return err
	}
	_, err = fmt.Fprintf(stdout, "AgentDock 已配对到 %s，node_id=%s，Device Token 已保存（数据目录：%s）。\n", status.Endpoint, status.NodeID, cfg.AgentDockHome)
	return err
}
func nexusCommandUsageError() error {
	return errors.New("用法：agentdock nexus <pair --endpoint <URL> --code <配对码> [--name <名称>] [--agentdock-home <目录>] | status [--json] [--agentdock-home <目录>]>")
}

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
	"time"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/desktopruntime"
	"github.com/uvwt/agentdock/internal/nexusbridge"
)

type nexusPairJSONOutput struct {
	Paired            bool   `json:"paired"`
	Endpoint          string `json:"endpoint"`
	NodeID            string `json:"node_id"`
	DeviceID          string `json:"device_id"`
	AgentDockHome     string `json:"agentdock_home"`
	IdentityPath      string `json:"identity_path"`
	ServiceRestarted  bool   `json:"service_restarted"`
	RestartSkipped    bool   `json:"restart_skipped,omitempty"`
	ManualRestartHint string `json:"manual_restart_hint,omitempty"`
}

func runNexusCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return nexusCommandUsageError()
	}
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprintln(stdout, nexusCommandUsageError().Error())
		return nil
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
	runtimeRoot := flags.String("runtime-root", desktopruntime.DefaultRuntimeRoot(), "AgentDock service runtime configuration directory")
	noRestart := flags.Bool("no-restart", false, "skip automatic service restart after pairing")
	jsonOutput := flags.Bool("json", false, "print machine-readable pairing result")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return nexusCommandUsageError()
	}

	explicitHome := flagWasProvided(flags, "agentdock-home")
	explicitRuntimeRoot := flagWasProvided(flags, "runtime-root")
	agentDockHome, binding, fromServiceRuntime, err := resolveNexusAgentDockHome(*home, explicitHome, *runtimeRoot, explicitRuntimeRoot)
	if err != nil {
		return err
	}

	// Linux systemd/OpenRC 的 Core 常以独立服务账号运行；仅当未能从服务配置解析出
	// AGENTDOCK_HOME 且调用方也未显式指定目录时才给出回退警告，避免误消费一次性配对码。
	if runtime.GOOS == "linux" && !explicitHome && strings.TrimSpace(os.Getenv("AGENTDOCK_HOME")) == "" && !fromServiceRuntime {
		fmt.Fprintf(stderr, "注意：未能读取系统服务配置且未指定 AgentDock 数据目录，配对将写入 %s；若使用 systemd/OpenRC 托管服务，请使用 sudo 执行或显式传入 --agentdock-home。\n", agentDockHome)
	}

	identity, err := nexusbridge.Pair(ctx, agentDockHome, nexusbridge.PairOptions{
		Endpoint: *endpoint,
		Code:     *code,
		Name:     *name,
	})
	if err != nil {
		return err
	}

	identityFile := filepath.Join(agentDockHome, "nexus", "device.json")
	if binding.MatchesAgentDockHome(agentDockHome) {
		if err := binding.SyncAgentDockHomeOwnership(
			agentDockHome,
			filepath.Dir(identityFile),
			identityFile,
		); err != nil {
			return fmt.Errorf("同步 NexusDock 设备身份属主: %w", err)
		}
	}

	// 配对码是一次性消费资源；凭据落盘成功后，即使自动重启因权限不足失败，
	// 也必须保留成功退出码并输出明确的手动重启指引，避免误导用户重跑已失效的配对码。
	restarted, restartMessage, manualHint := maybeRestartLinuxServiceAfterPair(ctx, agentDockHome, binding, *noRestart)

	if *jsonOutput {
		return json.NewEncoder(stdout).Encode(nexusPairJSONOutput{
			Paired:            true,
			Endpoint:          identity.Endpoint,
			NodeID:            identity.NodeID,
			DeviceID:          identity.DeviceID,
			AgentDockHome:     agentDockHome,
			IdentityPath:      identityFile,
			ServiceRestarted:  restarted,
			RestartSkipped:    *noRestart,
			ManualRestartHint: manualHint,
		})
	}

	if restarted {
		fmt.Fprintf(stdout, "NexusDock 配对凭据已保存，node_id=%s，%s。\n", identity.NodeID, restartMessage)
	} else {
		fmt.Fprintf(stdout, "NexusDock 配对凭据已保存，node_id=%s（后台服务连接状态需另行验证）。\n", identity.NodeID)
	}
	fmt.Fprintf(stdout, "设备凭据位置：%s；Core 必须使用相同的 AGENTDOCK_HOME。\n", identityFile)
	if !restarted && restartMessage != "" {
		fmt.Fprintln(stdout, restartMessage)
	}
	return nil
}

func runNexusStatusCommand(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("agentdock nexus status", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonOutput := flags.Bool("json", false, "print machine-readable status")
	home := flags.String("agentdock-home", "", "AgentDock state directory (must match Core AGENTDOCK_HOME)")
	runtimeRoot := flags.String("runtime-root", desktopruntime.DefaultRuntimeRoot(), "AgentDock service runtime configuration directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return nexusCommandUsageError()
	}

	explicitHome := flagWasProvided(flags, "agentdock-home")
	explicitRuntimeRoot := flagWasProvided(flags, "runtime-root")
	agentDockHome, _, _, err := resolveNexusAgentDockHome(*home, explicitHome, *runtimeRoot, explicitRuntimeRoot)
	if err != nil {
		return err
	}

	status, err := nexusbridge.ReadStatus(agentDockHome)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return json.NewEncoder(stdout).Encode(status)
	}
	if !status.Paired {
		_, err = fmt.Fprintf(stdout, "AgentDock 尚未与 NexusDock 配对（当前数据目录：%s）。\n", agentDockHome)
		return err
	}
	_, err = fmt.Fprintf(stdout, "AgentDock 已配对到 %s，node_id=%s，Device Token 已保存（数据目录：%s）。\n", status.Endpoint, status.NodeID, agentDockHome)
	return err
}

func resolveNexusAgentDockHome(
	flagHome string,
	explicitHome bool,
	runtimeRoot string,
	explicitRuntimeRoot bool,
) (string, desktopruntime.LinuxServiceBinding, bool, error) {
	binding, err := desktopruntime.DetectLinuxServiceBinding(runtimeRoot)
	if err != nil && (explicitRuntimeRoot || !errors.Is(err, os.ErrPermission)) {
		return "", desktopruntime.LinuxServiceBinding{}, false, err
	}

	cfg, err := config.FromEnv()
	if err != nil {
		return "", desktopruntime.LinuxServiceBinding{}, false, err
	}

	fromServiceRuntime := false
	switch {
	case explicitHome:
		if strings.TrimSpace(flagHome) == "" {
			return "", desktopruntime.LinuxServiceBinding{}, false, errors.New("--agentdock-home 不能为空")
		}
		cfg.AgentDockHome = strings.TrimSpace(flagHome)
	case strings.TrimSpace(os.Getenv("AGENTDOCK_HOME")) != "":
		cfg.AgentDockHome = strings.TrimSpace(os.Getenv("AGENTDOCK_HOME"))
	case binding.Configured && strings.TrimSpace(binding.AgentDockHome) != "":
		cfg.AgentDockHome = strings.TrimSpace(binding.AgentDockHome)
		fromServiceRuntime = true
	}

	if err := cfg.Normalize(); err != nil {
		return "", desktopruntime.LinuxServiceBinding{}, false, err
	}
	return cfg.AgentDockHome, binding, fromServiceRuntime, nil
}

func maybeRestartLinuxServiceAfterPair(
	ctx context.Context,
	agentDockHome string,
	binding desktopruntime.LinuxServiceBinding,
	noRestart bool,
) (restarted bool, message string, manualHint string) {
	if !binding.ManagedService() || !binding.MatchesAgentDockHome(agentDockHome) {
		return false, "", ""
	}
	if noRestart {
		return false, "", ""
	}
	manualCmd := manualLinuxServiceRestartCommand(binding)
	restartCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := desktopruntime.RestartService(restartCtx, binding.RuntimeRoot); err != nil {
		return false,
			fmt.Sprintf("未能自动重启服务 %s（%v）；请手动执行：%s", binding.ServiceName, err, manualCmd),
			manualCmd
	}
	return true, fmt.Sprintf("已自动重启服务 %s", binding.ServiceName), ""
}

func manualLinuxServiceRestartCommand(binding desktopruntime.LinuxServiceBinding) string {
	switch binding.ServiceManager {
	case "openrc":
		return fmt.Sprintf("sudo rc-service %s restart", binding.ServiceName)
	default:
		return fmt.Sprintf("sudo systemctl restart %s", binding.ServiceName)
	}
}

func flagWasProvided(fs *flag.FlagSet, name string) bool {
	provided := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			provided = true
		}
	})
	return provided
}

func nexusCommandUsageError() error {
	return errors.New("用法：agentdock nexus <pair --endpoint <URL> --code <配对码> [--name <名称>] [--agentdock-home <目录>] [--runtime-root <目录>] [--no-restart] [--json] | status [--json] [--agentdock-home <目录>] [--runtime-root <目录>]>")
}

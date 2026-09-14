package desktopruntime

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/uvwt/agentdock/internal/desktopcontrol"
)

// ServiceStatus 是桌面端和 CLI 共享的结构化运行状态。
// 生命周期操作统一返回 JSON，避免桌面端继续解析 PowerShell 文本。
type ServiceStatus struct {
	Running        bool `json:"running"`
	Healthy        bool `json:"healthy"`
	StartupEnabled bool `json:"startup_enabled"`
	NexusConnected bool `json:"nexus_connected"`
}

type serviceCommandResult struct {
	Action    string `json:"action"`
	Completed bool   `json:"completed"`
}

func RunServiceCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return serviceCommandUsageError()
	}

	switch args[0] {
	case "status":
		runtimeRoot, err := parseRuntimeRoot("agentdock service status", args[1:], stderr)
		if err != nil {
			return err
		}
		var status ServiceStatus
		err = desktopcontrol.Call(ctx, runtimeRoot, "service.status", controlActionParams{RuntimeRoot: runtimeRoot}, &status)
		if err != nil {
			status, err = platformServiceStatus(ctx, runtimeRoot)
		}
		if err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(status)
	case "start", "stop", "restart":
		action := args[0]
		runtimeRoot, err := parseRuntimeRoot("agentdock service "+action, args[1:], stderr)
		if err != nil {
			return err
		}
		if err := platformServiceAction(ctx, runtimeRoot, action); err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(serviceCommandResult{Action: action, Completed: true})
	case "autostart":
		flags := flag.NewFlagSet("agentdock service autostart", flag.ContinueOnError)
		flags.SetOutput(stderr)
		runtimeRoot := flags.String("runtime-root", "", "AgentDock 桌面运行目录")
		component := flags.String("component", "", "启动组件：core 或 tray")
		enabled := flags.String("enabled", "", "是否启用：true 或 false")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*runtimeRoot) == "" {
			return errors.New("用法：agentdock service autostart --runtime-root <目录> --component <core|tray> --enabled <true|false>")
		}
		if *component != "core" && *component != "tray" {
			return errors.New("service autostart 的 component 必须是 core 或 tray")
		}
		var shouldEnable bool
		switch strings.ToLower(strings.TrimSpace(*enabled)) {
		case "true":
			shouldEnable = true
		case "false":
			shouldEnable = false
		default:
			return errors.New("service autostart 的 enabled 必须是 true 或 false")
		}
		if err := platformSetAutostart(ctx, *runtimeRoot, *component, shouldEnable); err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(serviceCommandResult{Action: "autostart", Completed: true})
	case "task-start":
		flags := flag.NewFlagSet("agentdock service task-start", flag.ContinueOnError)
		flags.SetOutput(stderr)
		taskName := flags.String("task-name", "", "Windows Scheduled Task 名称")
		expectedUserSID := flags.String("expected-user-sid", "", "期望的 InteractiveToken 用户 SID")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*taskName) == "" {
			return errors.New("用法：agentdock service task-start --task-name <名称> [--expected-user-sid <SID>]")
		}
		if err := platformStartScheduledTask(ctx, *taskName, *expectedUserSID); err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(serviceCommandResult{Action: "task-start", Completed: true})
	default:
		return serviceCommandUsageError()
	}
}

func PrepareCoreEnvironment(runtimeRoot string) error {
	if strings.TrimSpace(runtimeRoot) == "" {
		return errors.New("runtime-root 不能为空")
	}
	return platformPrepareCoreEnvironment(runtimeRoot)
}

func parseRuntimeRoot(name string, args []string, stderr io.Writer) (string, error) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	runtimeRoot := flags.String("runtime-root", "", "AgentDock 桌面运行目录")
	if err := flags.Parse(args); err != nil {
		return "", err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*runtimeRoot) == "" {
		return "", fmt.Errorf("用法：%s --runtime-root <目录>", name)
	}
	return *runtimeRoot, nil
}

func serviceCommandUsageError() error {
	return errors.New("用法：agentdock service <status|start|stop|restart|autostart|task-start> ...")
}

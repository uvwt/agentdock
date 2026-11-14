package desktopruntime

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
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
	if args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		printServiceCommandUsage(stdout)
		return nil
	}

	switch args[0] {
	case "status":
		ctx, cancel := context.WithTimeout(ctx, desktopStatusTimeout)
		defer cancel()
		runtimeRoot, err := parseRuntimeRoot("agentdock service status", args[1:], stderr)
		if err != nil {
			return err
		}
		var status ServiceStatus
		err = readControlStatus(ctx, runtimeRoot, "service.status", &status)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
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
		runtimeRoot := flags.String("runtime-root", DefaultRuntimeRoot(), "AgentDock 运行配置目录")
		component := flags.String("component", "", "启动组件：core 或 tray")
		enabled := flags.String("enabled", "", "是否启用：true 或 false")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*runtimeRoot) == "" {
			return errors.New("用法：agentdock service autostart [--runtime-root <目录>] --component <core|tray> --enabled <true|false>")
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

// LinuxServiceBinding 描述 Linux 系统服务与 Core 数据目录的绑定关系。
// 手动执行 agentdock nexus pair / status 时据此定位服务真实的 AGENTDOCK_HOME，
// 并在配对完成后重启对应的托管服务使新凭据生效。
type LinuxServiceBinding struct {
	Configured     bool
	RuntimeRoot    string
	ServiceManager string
	ServiceName    string
	AgentDockHome  string
	ownerUID       int
	ownerGID       int
}

func (b LinuxServiceBinding) ManagedService() bool {
	return b.Configured &&
		(b.ServiceManager == "systemd" || b.ServiceManager == "openrc") &&
		strings.TrimSpace(b.ServiceName) != ""
}

func (b LinuxServiceBinding) MatchesAgentDockHome(agentDockHome string) bool {
	if !b.Configured || strings.TrimSpace(agentDockHome) == "" || strings.TrimSpace(b.AgentDockHome) == "" {
		return false
	}
	target, err := filepath.Abs(strings.TrimSpace(agentDockHome))
	if err != nil {
		target = filepath.Clean(strings.TrimSpace(agentDockHome))
	}
	bound, err := filepath.Abs(strings.TrimSpace(b.AgentDockHome))
	if err != nil {
		bound = filepath.Clean(strings.TrimSpace(b.AgentDockHome))
	}
	return target == bound
}

func RestartService(ctx context.Context, runtimeRoot string) error {
	if strings.TrimSpace(runtimeRoot) == "" {
		runtimeRoot = DefaultRuntimeRoot()
	}
	return platformServiceAction(ctx, strings.TrimSpace(runtimeRoot), "restart")
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
	runtimeRoot := flags.String("runtime-root", DefaultRuntimeRoot(), "AgentDock 运行配置目录")
	if err := flags.Parse(args); err != nil {
		return "", err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*runtimeRoot) == "" {
		return "", fmt.Errorf("无法确定 AgentDock 运行配置目录；请设置 AGENTDOCK_RUNTIME_ROOT 或使用 %s --runtime-root <目录>", name)
	}
	return strings.TrimSpace(*runtimeRoot), nil
}

func printServiceCommandUsage(w io.Writer) {
	fmt.Fprintln(w, "用法：")
	fmt.Fprintln(w, "  agentdock service status [--runtime-root <目录>]")
	fmt.Fprintln(w, "  agentdock service <start|stop|restart> [--runtime-root <目录>]")
	fmt.Fprintln(w, "  agentdock service autostart [--runtime-root <目录>] --component <core|tray> --enabled <true|false>")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "运行配置目录优先级：--runtime-root > AGENTDOCK_RUNTIME_ROOT > 平台默认目录。")
}

func serviceCommandUsageError() error {
	return errors.New("用法：agentdock service <status|start|stop|restart|autostart|task-start> ...")
}

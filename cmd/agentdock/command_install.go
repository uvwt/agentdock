package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/uvwt/agentdock/internal/installer"
)

func runInstallCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 && args[0] == "--engine-ready" {
		// 脚本用这段固定输出判断“这是真正的 Go Engine”，避免测试替身或旧 binary 的 `exit 0` 误触发。
		fmt.Fprintln(stdout, "agentdock-installer-engine")
		return nil
	}
	if len(args) > 0 && args[0] == "inspect" {
		return runInstallInspect(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "abandon" {
		return runInstallAbandon(ctx, args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "commit" {
		return runInstallCommit(ctx, args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "prepare-windows-legacy" {
		return runInstallPrepareWindowsLegacy(ctx, args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "detach-engine" {
		return runInstallDetachEngine(ctx, args[1:], stdout, stderr)
	}

	flags := flag.NewFlagSet("agentdock install", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "用法：")
		fmt.Fprintln(stderr, "  agentdock install --install-root <目录> [--payload-dir <目录>|--binary <文件>]")
		fmt.Fprintln(stderr, "  agentdock install --repair --install-root <目录>")
		fmt.Fprintln(stderr, "  agentdock install inspect --state-root <目录> [--require-committed] [--require-version <版本>]")
		fmt.Fprintln(stderr, "  agentdock install abandon --install-root <目录> [--transaction-id <ID>] [--rollback-failed]")
		fmt.Fprintln(stderr, "  --rollback-failed 表示 OS adapter 回滚失败，写入 failed/external_rollback_failed 并阻断后续自动 install")
		fmt.Fprintln(stderr, "  agentdock install commit --install-root <目录> [--transaction-id <ID>]")
		fmt.Fprintln(stderr, "  agentdock install prepare-windows-legacy --install-root <目录> --legacy-version <版本> --legacy-core <文件> --legacy-tray <文件> --payload-dir <目录>")
		fmt.Fprintln(stderr, "  agentdock install detach-engine --output <临时文件>")
		fmt.Fprintln(stderr, "  agentdock install --engine-ready")
	}

	var request installer.Request
	repair := flags.Bool("repair", false, "修复已有安装")
	flags.StringVar(&request.InstallRoot, "install-root", "", "安装根目录")
	flags.StringVar(&request.RuntimeRoot, "runtime-root", "", "运行配置目录，默认与 install-root 相同")
	flags.StringVar(&request.PayloadDir, "payload-dir", "", "已解压的 Release 载荷目录")
	flags.StringVar(&request.BinaryPath, "binary", "", "已就位的 agentdock 二进制")
	flags.StringVar(&request.SkillBundle, "skill-bundle", "", "核心 Skill Bundle 目录")
	flags.StringVar(&request.Version, "version", "", "目标版本")
	flags.StringVar(&request.Channel, "channel", "official", "安装通道")
	// host/port 默认必须是“未指定”，不能写成 127.0.0.1:8765。
	// 否则 update/repair 省略这两个标志时会把用户已有监听地址覆盖掉。
	flags.StringVar(&request.Host, "host", "", "监听地址；省略则保留已有值")
	flags.IntVar(&request.Port, "port", 0, "监听端口；省略则保留已有值")
	flags.StringVar(&request.LogLevel, "log-level", "", "日志级别；省略则保留已有值")
	flags.StringVar(&request.TunnelMode, "tunnel-mode", "", "Tunnel 模式：none、quick 或 named；省略则保留已有值")
	flags.StringVar(&request.ServerURL, "server-url", "", "Named Tunnel HTTPS Origin")
	flags.StringVar(&request.TunnelToken, "tunnel-token", "", "Cloudflare Tunnel Token")
	flags.StringVar(&request.TunnelTokenFile, "token-file", "", "Tunnel Token 文件")
	flags.StringVar(&request.LiveBinary, "live-binary", "", "生产二进制最终路径；macOS CLI 为 ~/.local/bin/agentdock")
	flags.StringVar(&request.CloudflaredPath, "cloudflared", "", "cloudflared 路径")
	flags.StringVar(&request.ServiceName, "service-name", "agentdock", "服务名")
	flags.StringVar(&request.ServiceUser, "service-user", "", "服务用户")
	flags.StringVar(&request.ServiceGroup, "service-group", "", "服务组")
	flags.StringVar(&request.ServiceManager, "service-manager", "auto", "systemd、openrc、none 或 auto")
	flags.StringVar(&request.DataDir, "data-dir", "", "数据目录")
	flags.StringVar(&request.SystemdDir, "systemd-dir", "", "systemd unit 目录")
	flags.StringVar(&request.OpenRCDir, "openrc-dir", "", "OpenRC 脚本目录")
	flags.StringVar(&request.LaunchAgentsDir, "launch-agents-dir", "", "LaunchAgents 目录")
	authToken := flags.String("auth-token", "", "Bearer Token；省略则保留已有值，首次安装才生成")
	oauthPassword := flags.String("oauth-password", "", "OAuth 密码；省略则保留已有值")
	oauthSecret := flags.String("oauth-token-secret", "", "OAuth 签名密钥；省略则保留已有值")
	rotateOAuth := flags.Bool("rotate-oauth", false, "强制轮换 OAuth 密码和签名密钥")
	flags.StringVar(&request.PrivilegeMode, "privilege-mode", "", "Windows privilege mode")
	flags.StringVar(&request.AgentDockHome, "agentdock-home", "", "AGENTDOCK_HOME")
	flags.StringVar(&request.AgentDockDefaultDir, "agentdock-default-dir", "", "AGENTDOCK_DEFAULT_DIR")
	flags.StringVar(&request.TaskName, "task-name", "", "Windows Scheduled Task 名称")
	flags.StringVar(&request.StartupValueName, "startup-value-name", "", "Windows Core Run 名称")
	flags.StringVar(&request.TrayStartupValueName, "tray-startup-value-name", "", "Windows Tray Run 名称")
	flags.StringVar(&request.CloudflaredStartupValueName, "cloudflared-startup-value-name", "", "Windows Tunnel Run 名称")
	noStart := flags.Bool("no-start", false, "只写入布局，不启动服务")
	skipHealth := flags.Bool("skip-health", false, "跳过 health-check")
	skipSkills := flags.Bool("skip-skills", false, "跳过 Skill bootstrap")
	registerService := flags.Bool("register-service", false, "写入 LaunchAgent 或系统服务")
	deferCommit := flags.Bool("defer-commit", false, "完成 verify 后停在 trial，由调用方再执行 install commit")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("未知参数：%s", flags.Arg(0))
	}
	if *repair {
		request.Action = installer.ActionRepair
	}
	specified := map[string]bool{}
	flags.Visit(func(flag *flag.Flag) { specified[flag.Name] = true })
	if specified["auth-token"] {
		request.AuthToken = installer.Specified(*authToken)
	}
	if specified["oauth-password"] {
		request.OAuthPassword = installer.Specified(*oauthPassword)
	}
	if specified["oauth-token-secret"] {
		request.OAuthTokenSecret = installer.Specified(*oauthSecret)
	}
	request.RotateOAuth = *rotateOAuth
	request.StartService = !*noStart
	request.SkipHealth = *skipHealth
	request.SkipSkills = *skipSkills
	request.RegisterService = *registerService
	request.DeferCommit = *deferCommit
	if dir := strings.TrimSpace(os.Getenv("AGENTDOCK_SYSTEMD_DIR")); request.SystemdDir == "" && dir != "" {
		request.SystemdDir = dir
	}
	if dir := strings.TrimSpace(os.Getenv("AGENTDOCK_OPENRC_DIR")); request.OpenRCDir == "" && dir != "" {
		request.OpenRCDir = dir
	}

	result, err := installer.Engine{}.Run(ctx, request)
	if err != nil {
		if result.TransactionID != "" {
			_ = json.NewEncoder(stdout).Encode(result)
		}
		return err
	}
	return json.NewEncoder(stdout).Encode(result)
}

func runUninstallCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("agentdock uninstall", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var request installer.Request
	request.Action = installer.ActionUninstall
	flags.StringVar(&request.InstallRoot, "install-root", "", "安装根目录")
	flags.StringVar(&request.RuntimeRoot, "runtime-root", "", "运行配置目录")
	flags.StringVar(&request.ServiceName, "service-name", "agentdock", "服务名")
	flags.StringVar(&request.ServiceManager, "service-manager", "auto", "服务管理器")
	flags.StringVar(&request.SystemdDir, "systemd-dir", "", "systemd unit 目录")
	flags.StringVar(&request.OpenRCDir, "openrc-dir", "", "OpenRC 脚本目录")
	flags.StringVar(&request.LaunchAgentsDir, "launch-agents-dir", "", "LaunchAgents 目录")
	flags.StringVar(&request.TaskName, "task-name", "", "Windows Scheduled Task 名称")
	purgeConfig := flags.Bool("purge-config", false, "同时删除配置")
	purgeData := flags.Bool("purge-data", false, "删除程序、配置和数据")
	deferCommit := flags.Bool("defer-commit", false, "停在 trial；OS adapter 完成 Task/Registry 后再 install commit。Engine committed 不是整个产品已卸载")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(request.InstallRoot) == "" {
		return errors.New("用法：agentdock uninstall --install-root <目录> [--purge-config|--purge-data] [--defer-commit]")
	}
	request.PurgeConfig = *purgeConfig
	request.PurgeData = *purgeData
	request.DeferCommit = *deferCommit
	if *purgeData {
		request.PurgeConfig = true
	}
	result, err := installer.Engine{}.Run(ctx, request)
	if err != nil {
		if result.TransactionID != "" {
			_ = json.NewEncoder(stdout).Encode(result)
		}
		return err
	}
	return json.NewEncoder(stdout).Encode(result)
}

func runInstallInspect(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("agentdock install inspect", flag.ContinueOnError)
	flags.SetOutput(stderr)
	stateRoot := flags.String("state-root", "", "安装状态目录")
	requireCommitted := flags.Bool("require-committed", false, "要求事务已提交")
	requireVersion := flags.String("require-version", "", "要求已安装版本")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*stateRoot) == "" {
		return errors.New("用法：agentdock install inspect --state-root <目录> [--require-committed] [--require-version <版本>]")
	}
	inspection, err := installer.Inspect(*stateRoot)
	if err != nil {
		return err
	}
	if *requireCommitted || strings.TrimSpace(*requireVersion) != "" {
		if err := installer.AssertCommitted(inspection, *requireVersion); err != nil {
			return err
		}
		if err := installer.AssertManifestPresent(inspection); err != nil {
			return err
		}
	}
	return json.NewEncoder(stdout).Encode(inspection)
}

func runInstallAbandon(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("agentdock install abandon", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var request installer.Request
	request.Action = installer.ActionAbandon
	flags.StringVar(&request.InstallRoot, "install-root", "", "安装根目录")
	flags.StringVar(&request.RuntimeRoot, "runtime-root", "", "运行配置目录")
	flags.StringVar(&request.TransactionID, "transaction-id", "", "要撤销的 install 事务 ID")
	rollbackFailed := flags.Bool("rollback-failed", false, "OS adapter 回滚失败，写入 failed/external_rollback_failed 并阻断后续自动 install")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(request.InstallRoot) == "" {
		return errors.New("用法：agentdock install abandon --install-root <目录> [--transaction-id <ID>] [--rollback-failed]")
	}
	request.RollbackFailed = *rollbackFailed
	result, err := installer.Engine{}.Run(ctx, request)
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(result)
}

func runInstallCommit(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("agentdock install commit", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var request installer.Request
	request.Action = installer.ActionCommit
	flags.StringVar(&request.InstallRoot, "install-root", "", "安装根目录")
	flags.StringVar(&request.RuntimeRoot, "runtime-root", "", "运行配置目录")
	flags.StringVar(&request.TransactionID, "transaction-id", "", "要提交的 install 事务 ID")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(request.InstallRoot) == "" {
		return errors.New("用法：agentdock install commit --install-root <目录> [--transaction-id <ID>]")
	}
	result, err := installer.Engine{}.Run(ctx, request)
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(result)
}

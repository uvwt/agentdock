package scripts

import (
	"os"
	"strings"
	"testing"
)

// 本文件是 Linux 安装链路的架构契约测试。前身 install_linux_test.go 因文件名
// 的 GOOS 约束只在 Linux CI 编译，Mini 门禁看不到它，legacy 状态机删除后
// 三个用例全部失效。现在改为全平台可跑：Linux 专属行为（降权、run_root）
// 通过对实现文件的内容断言约束，Engine env 契约的单元测试在
// internal/installer/env_test.go。

// legacy 的 Nexus 设备代理凭据必须在安装时从 env 清除；清理责任已随 env
// 文件的所有权一起移交给 Go Installer Engine（单元测试见
// internal/installer/env_test.go）。这里断言 shell 侧不再有 legacy 交互。
func TestInstallLinuxShellHasNoLegacyNexusHandling(t *testing.T) {
	shell, err := os.ReadFile("../install/install-linux-platform.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"NexusDock API 是否需要 token？",
		"AGENTDOCK_NEXUS_TOKEN=",
		"AGENTDOCK_NEXUS_DEVICE_NAME",
		"nexus_token",
	} {
		if strings.Contains(string(shell), forbidden) {
			t.Fatalf("install-linux-platform.sh still contains legacy Nexus handling: %s", forbidden)
		}
	}
}

// 引擎调用必须在 root 下执行（/opt、/etc、unit 目录），而 skill bootstrap
// 的 service-user 降权由引擎内部完成——两边缺一都会破坏 Linux 身份模型。
func TestInstallLinuxEngineRunsAsRootAndDropsForSkillState(t *testing.T) {
	shell, err := os.ReadFile("../install/install-linux-platform.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(shell)
	for _, want := range []string{
		"apply_linux_with_go_installer",
		"sudo --preserve-env=AGENTDOCK_AUTH_TOKEN,AGENTDOCK_CLOUDFLARE_TUNNEL_TOKEN,AGENTDOCK_OAUTH_PASSWORD,AGENTDOCK_OAUTH_TOKEN_SECRET",
		// engine 失败必须终止，不允许回退 legacy 状态机
		"已禁止回退 legacy 实现",
		// 终端摘要消费 Engine 之后的最终公网地址，不得使用调用前的旧变量
		`read_env_assignment "$env_file" AGENTDOCK_SERVER_URL`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("install-linux-platform.sh missing engine privilege/summary contract %q", want)
		}
	}
	for _, forbidden := range []string{
		"run_as_service_user",
		"make_core_skill_bundle_readable",
		"write_env_file ",
		"write_systemd_unit",
		"write_openrc_service",
		"write_runtime_manifest",
		"configure_cloudflared",
		`--auth-token "$token"`,
		`--tunnel-token "$tunnel_token"`,
		`--oauth-password "$oauth_password"`,
		`--oauth-token-secret "$oauth_token_secret"`,
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("install-linux-platform.sh still implements legacy shell state machine: %s", forbidden)
		}
	}

	drop, err := os.ReadFile("../../internal/installer/skill_bootstrap_linux.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"os.Geteuid() != 0",
		"syscall.Credential",
		"exec.CommandContext(ctx, executable",
		"skill\", \"bootstrap\", \"--bundle\"",
		"AGENTDOCK_HOME=",
	} {
		if !strings.Contains(string(drop), want) {
			t.Fatalf("skill bootstrap privilege drop missing %q", want)
		}
	}
}

// systemd/OpenRC/LaunchAgent 单元由引擎模板生成，运行入口必须是
// service launch-core / tunnel launch（沿用旧测试的守护命令契约）。
func TestInstallLinuxEngineUnitsUseManagedRuntimeCommands(t *testing.T) {
	units, err := os.ReadFile("../../internal/installer/units.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(units)
	for _, want := range []string{
		"service launch-core --runtime-root %s",
		"tunnel launch --runtime-root %s",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("engine unit template missing managed runtime command %q", want)
		}
	}
}

package scripts

import (
	"os"
	"strings"
	"testing"
)

func TestDockerComposeBoundsAgentDockAndTunnelLogs(t *testing.T) {
	data, err := os.ReadFile("../../docker-compose.yml")
	if err != nil {
		t.Fatalf("read docker-compose.yml: %v", err)
	}
	compose := string(data)
	if got := strings.Count(compose, `max-size: "10m"`); got != 3 {
		t.Fatalf("max-size policy occurrences=%d, want 3", got)
	}
	if got := strings.Count(compose, `max-file: "5"`); got != 3 {
		t.Fatalf("max-file policy occurrences=%d, want 3", got)
	}
}

func TestLinuxOpenRCUsesAgentDockManagedRotatingLogs(t *testing.T) {
	// OpenRC unit 由 Go Installer Engine 生成（internal/installer/units.go），
	// 平台脚本不再保留第二份单元模板；日志治理策略断言跟着权威实现走。
	data, err := os.ReadFile("../../internal/installer/units.go")
	if err != nil {
		t.Fatalf("read internal/installer/units.go: %v", err)
	}
	template := string(data)
	// core 与 tunnel 两个 OpenRC unit 都必须走托管日志目录，直接输出到 /dev/null。
	for _, want := range []string{
		`log_dir="/var/log/%s"`,
		`output_log="/dev/null"`,
		`error_log="/dev/null"`,
		`checkpath -d -m 0750`,
	} {
		if got := strings.Count(template, want); got != 2 {
			t.Fatalf("OpenRC template policy %q occurrences=%d, want 2 (core+tunnel)", want, got)
		}
	}
	for _, legacy := range []string{
		`output_log="/var/log/`,
		`error_log="/var/log/`,
	} {
		if strings.Contains(template, legacy) {
			t.Fatalf("OpenRC still writes unbounded legacy log directly: %s", legacy)
		}
	}
}

func TestLinuxSystemdKeepsJournaldLogging(t *testing.T) {
	data, err := os.ReadFile("../install/install-linux-platform.sh")
	if err != nil {
		t.Fatalf("read install-linux-platform.sh: %v", err)
	}
	script := string(data)
	if !strings.Contains(script, `systemd) printf 'sudo journalctl -u %s -n 100 --no-pager'`) {
		t.Fatal("systemd log command must continue to use journald")
	}
	for _, forbidden := range []string{"StandardOutput=append:", "StandardError=append:"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("systemd must not add duplicate file logging: %s", forbidden)
		}
	}
}

func TestDesktopHostsDoNotBypassManagedRotation(t *testing.T) {
	files := map[string][]string{
		"../../internal/desktopruntime/service_windows.go": {
			`os.O_APPEND`,
			`agentdock.out.log`,
		},
		"../../desktop/windows/control-panel/Services/RuntimeService.cs": {
			`FileMode.Append`,
			`Path.Combine(LogsDirectory, "agentdock.err.log")`,
		},
		"../install/manage-windows.ps1": {
			`RedirectStandardOutput $CloudflaredStdoutPath`,
			`RedirectStandardError $CloudflaredStderrPath`,
		},
	}
	for path, forbidden := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, value := range forbidden {
			if strings.Contains(string(data), value) {
				t.Fatalf("%s still bypasses managed log rotation with %q", path, value)
			}
		}
	}

	manager, err := os.ReadFile("../install/manage-windows.ps1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manager), `& $AgentDockBinary tunnel start --runtime-root $RuntimeRoot`) {
		t.Fatal("Windows compatibility tunnel launcher must delegate to native tunnel lifecycle")
	}

	// macOS LaunchAgent 由 Go Installer Engine 生成（internal/installer/units.go）；
	// core 与 tunnel 两个 plist 各自把 stdout/stderr 重定向到 /dev/null。
	plistTemplate, err := os.ReadFile("../../internal/installer/units.go")
	if err != nil {
		t.Fatalf("read internal/installer/units.go: %v", err)
	}
	if got := strings.Count(string(plistTemplate), `<string>/dev/null</string>`); got != 4 {
		t.Fatalf("macOS LaunchAgent null redirections in engine template=%d, want 4", got)
	}
}

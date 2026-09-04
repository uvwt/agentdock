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
	data, err := os.ReadFile("../install/install-linux-platform.sh")
	if err != nil {
		t.Fatalf("read install-linux-platform.sh: %v", err)
	}
	script := string(data)
	for _, want := range []string{
		`log_dir="/var/log/${service_name}"`,
		`log_dir="/var/log/${tunnel_service_name}"`,
		`output_log="/dev/null"`,
		`error_log="/dev/null"`,
		`/var/log/%s/agentdock.err.log`,
		`/var/log/%s/cloudflared.out.log`,
		`/var/log/%s/cloudflared.err.log`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("OpenRC logging policy missing %q", want)
		}
	}
	for _, legacy := range []string{
		`output_log="/var/log/${service_name}.log"`,
		`error_log="/var/log/${service_name}.err"`,
		`output_log="/var/log/${tunnel_service_name}.log"`,
		`error_log="/var/log/${tunnel_service_name}.err"`,
	} {
		if strings.Contains(script, legacy) {
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

	macInstaller, err := os.ReadFile("../install/install-macos-platform.sh")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(macInstaller), `<string>/dev/null</string>`); got != 4 {
		t.Fatalf("macOS LaunchAgent null redirections=%d, want 4", got)
	}
}

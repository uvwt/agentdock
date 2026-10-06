package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnifiedInstallerEntryOwnsUnixBootstrap(t *testing.T) {
	for _, path := range []string{
		"../install/install.sh",
		"../install/install.ps1",
	} {
		if info, err := os.Stat(path); err != nil {
			t.Fatalf("required installer file %s: %v", path, err)
		} else if !info.Mode().IsRegular() {
			t.Fatalf("required installer path is not a regular file: %s", path)
		}
	}

	for _, legacyPath := range []string{
		"../install/install-linux-platform.sh",
		"../install/install-macos-platform.sh",
		"../install/uninstall-linux.sh",
		"../install/uninstall-macos.sh",
		"install-linux.sh",
		"install-linux-bootstrap.sh",
		"install-macos.sh",
		"install-windows.ps1",
	} {
		if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
			t.Fatalf("legacy installer entry must not exist: %s", legacyPath)
		}
	}

	data, err := os.ReadFile("../install/install.sh")
	if err != nil {
		t.Fatalf("read install.sh: %v", err)
	}
	entry := string(data)
	menu := "NexusDock 远程连接：\n1) 官方服务（推荐）\n2) 自托管\n3) 暂不连接"
	if !strings.Contains(entry, menu) {
		t.Fatalf("install.sh missing exact NexusDock menu: %q", menu)
	}
	for _, want := range []string{
		`max_attempts=60`,
		`正在获取 Quick Tunnel 公网地址...`,
		`agentdock_${PLATFORM}_${ARCH}.tar.gz`,
		`CLOUDFLARED_PATH="${AGENTDOCK_CLOUDFLARED_BINARY:-}"`,
		"install --engine-ready",
		"AGENTDOCK_INSTALLER_BASE_URL",
		"verify_checksum",
	} {
		if !strings.Contains(entry, want) {
			t.Fatalf("install.sh missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"install-linux-platform.sh",
		"install-macos-platform.sh",
		"uninstall-linux.sh",
		"uninstall-macos.sh",
		"AGENTDOCK_USE_LOCAL_PLATFORM_INSTALLER",
		"AGENTDOCK_CLOUDFLARED_RELEASE_BASE_URL",
		"cloudflared/releases/latest",
		"install_cloudflared",
	} {
		if strings.Contains(entry, forbidden) {
			t.Fatalf("install.sh still depends on platform installer asset %q", forbidden)
		}
	}
}

func TestInstallerDefaultsUseNexusDockDistribution(t *testing.T) {
	unixData, err := os.ReadFile("../install/install.sh")
	if err != nil {
		t.Fatalf("read install.sh: %v", err)
	}
	for _, want := range []string{
		`DEFAULT_BASE_URL="https://download.nexusdock.co/latest"`,
	} {
		if !strings.Contains(string(unixData), want) {
			t.Fatalf("install.sh missing canonical distribution URL %q", want)
		}
	}
	for _, forbidden := range []string{
		"VERSIONED_RELEASE_BASE_URL",
		"AGENTDOCK_RELEASE_VERSION",
		"--version latest|vX.Y.Z",
		"RELEASE_VERSION",
	} {
		if strings.Contains(string(unixData), forbidden) {
			t.Fatalf("install.sh must only install the current stable release; found %q", forbidden)
		}
	}

	windowsData, err := os.ReadFile("../install/install.ps1")
	if err != nil {
		t.Fatalf("read install.ps1: %v", err)
	}
	if want := "$defaultReleaseBaseUrl = 'https://download.nexusdock.co/latest'"; !strings.Contains(string(windowsData), want) {
		t.Fatalf("install.ps1 missing canonical distribution URL %q", want)
	}
	for _, forbidden := range []string{
		"[string] $Version = 'latest'",
		"$versionedReleaseBaseUrl",
		"RequestedVersion",
	} {
		if strings.Contains(string(windowsData), forbidden) {
			t.Fatalf("install.ps1 must only install the current stable release; found %q", forbidden)
		}
	}
}

func TestUnifiedInstallerFreshFlowOrdersCoreNexusThenCloudflare(t *testing.T) {
	data, err := os.ReadFile("../install/install.sh")
	if err != nil {
		t.Fatalf("read install.sh: %v", err)
	}
	entry := string(data)
	for _, want := range []string{
		`OFFICIAL_NEXUS_ENDPOINT="${AGENTDOCK_NEXUS_OFFICIAL_ENDPOINT:-https://mcp.nexusdock.co}"`,
		`OFFICIAL_NEXUS_DEVICES_URL="${AGENTDOCK_NEXUS_OFFICIAL_DEVICES_URL:-https://mcp.nexusdock.co/workspace/devices}"`,
		`prompt_value '配对码'`,
		`\n打开 %s 获取 NexusDock 配对码\n`,
		`run_install_engine install "$CORE_TUNNEL_MODE"`,
		`configure_nexus`,
		`choose_tunnel_mode`,
		`install_linux_cli_link`,
		`.installer-onboarding`,
		`write_onboarding_stage nexus`,
		`write_onboarding_stage tunnel`,
		`clear_onboarding_stage`,
	} {
		if !strings.Contains(entry, want) {
			t.Fatalf("install.sh missing fresh-flow contract %q", want)
		}
	}

	core := strings.Index(entry, `run_install_engine install "$CORE_TUNNEL_MODE"`)
	nexus := strings.Index(entry[core:], "\n  configure_nexus\n")
	tunnel := strings.Index(entry[core:], "\n      choose_tunnel_mode\n")
	tunnelRepair := strings.Index(entry[core:], `run_install_engine repair "$TUNNEL_MODE" "$CLOUDFLARED_PATH"`)
	if core < 0 || nexus < 0 || tunnel < 0 || tunnelRepair < 0 {
		t.Fatal("fresh installer flow markers are incomplete")
	}
	if !(nexus < tunnel && tunnel < tunnelRepair) {
		t.Fatalf("fresh installer order must be Core -> Nexus -> Tunnel choice -> component-backed repair; offsets nexus=%d tunnel=%d repair=%d", nexus, tunnel, tunnelRepair)
	}
}

func TestDesktopRuntimeSurfacesDoNotUseLegacyLaunchers(t *testing.T) {
	trayData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "tray", "app_windows.go"))
	if err != nil {
		t.Fatalf("read Windows tray: %v", err)
	}
	tray := string(trayData)
	for _, want := range []string{
		"runNativeAgentDock",
		"procSetClipboardData",
		"procShellExecuteW",
		`"tunnel", "regenerate"`,
		`"service", "restart"`,
	} {
		if !strings.Contains(tray, want) {
			t.Fatalf("Windows tray missing native runtime behavior %q", want)
		}
	}
	for _, forbidden := range []string{
		"powershell.exe",
		"startPowerShellScript",
		"AgentDockLauncher",
		"CloudflaredLauncher",
	} {
		if strings.Contains(tray, forbidden) {
			t.Fatalf("Windows tray still depends on legacy launcher %q", forbidden)
		}
	}

	selfUpdateData, err := os.ReadFile(filepath.Join("..", "..", "internal", "selfupdate", "service_darwin.go"))
	if err != nil {
		t.Fatalf("read macOS self-update service adapter: %v", err)
	}
	selfUpdate := string(selfUpdateData)
	for _, want := range []string{
		`"ProgramArguments.0": paths.binary`,
		`"ProgramArguments.2": "launch-core"`,
		`"ProgramArguments.4": paths.runtimeRoot`,
	} {
		if !strings.Contains(selfUpdate, want) {
			t.Fatalf("macOS self-update adapter missing native LaunchAgent contract %q", want)
		}
	}
	for _, forbidden := range []string{"start-agentdock.sh", "startScript"} {
		if strings.Contains(selfUpdate, forbidden) {
			t.Fatalf("macOS self-update adapter still depends on legacy launcher %q", forbidden)
		}
	}
}

package desktopruntime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadForBinary(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "bin", "agentdock.exe")
	if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{
  "schema_version": 1,
  "agentdock_binary": "` + filepath.ToSlash(binary) + `",
  "tray_binary": "` + filepath.ToSlash(filepath.Join(root, "bin", "agentdock-tray.exe")) + `",
  "agentdock_launcher": "` + filepath.ToSlash(filepath.Join(root, "start-agentdock.ps1")) + `",
  "cloudflared_binary": "` + filepath.ToSlash(filepath.Join(root, "bin", "cloudflared.exe")) + `",
  "startup_value_name": "AgentDockCore",
  "tray_startup_value_name": "AgentDockTray",
  "cloudflared_startup_value_name": "AgentDockCloudflared",
  "host": "127.0.0.1",
  "port": 8765,
  "local_mcp_url": "http://127.0.0.1:8765/mcp",
  "tunnel_mode": "none",
  "install_channel": "setup"
}`
	if err := os.WriteFile(PathForBinary(binary), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadForBinary(binary)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.HealthURL(); got != "http://127.0.0.1:8765/healthz" {
		t.Fatalf("unexpected health URL: %s", got)
	}
	if loaded.StartupValueName != "AgentDockCore" || loaded.TrayStartupValueName != "AgentDockTray" {
		t.Fatalf("startup names were not loaded: %#v", loaded)
	}
	if got := filepath.Base(loaded.CloudflaredBinary); got != "cloudflared.exe" {
		t.Fatalf("unexpected cloudflared binary: %s", got)
	}
}

func TestLoadRebasesPathsFromRecordedInstallRoot(t *testing.T) {
	recordedRoot := filepath.Join(t.TempDir(), "logical-install")
	runtimeRoot := filepath.Join(t.TempDir(), "redirected-install")
	binDir := filepath.Join(runtimeRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(binDir, "agentdock.exe"),
		filepath.Join(binDir, "agentdock-tray.exe"),
		filepath.Join(binDir, "cloudflared.exe"),
		filepath.Join(runtimeRoot, "start-agentdock.ps1"),
		filepath.Join(runtimeRoot, "start-cloudflared.ps1"),
	} {
		if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	manifest := Manifest{
		SchemaVersion:       SchemaVersion,
		InstallRoot:         recordedRoot,
		AgentDockBinary:     filepath.Join(recordedRoot, "bin", "agentdock.exe"),
		TrayBinary:          filepath.Join(recordedRoot, "bin", "agentdock-tray.exe"),
		AgentDockLauncher:   filepath.Join(recordedRoot, "start-agentdock.ps1"),
		CloudflaredBinary:   filepath.Join(recordedRoot, "bin", "cloudflared.exe"),
		CloudflaredLauncher: filepath.Join(recordedRoot, "start-cloudflared.ps1"),
		Host:                "127.0.0.1",
		Port:                8765,
		LocalMCPURL:         "http://127.0.0.1:8765/mcp",
		TunnelMode:          "none",
	}
	manifestPath := filepath.Join(runtimeRoot, "runtime.json")
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	for name, pair := range map[string][2]string{
		"install_root":         {loaded.InstallRoot, runtimeRoot},
		"agentdock_binary":     {loaded.AgentDockBinary, filepath.Join(binDir, "agentdock.exe")},
		"tray_binary":          {loaded.TrayBinary, filepath.Join(binDir, "agentdock-tray.exe")},
		"agentdock_launcher":   {loaded.AgentDockLauncher, filepath.Join(runtimeRoot, "start-agentdock.ps1")},
		"cloudflared_binary":   {loaded.CloudflaredBinary, filepath.Join(binDir, "cloudflared.exe")},
		"cloudflared_launcher": {loaded.CloudflaredLauncher, filepath.Join(runtimeRoot, "start-cloudflared.ps1")},
	} {
		if !samePath(pair[0], pair[1]) {
			t.Fatalf("%s = %q, want %q", name, pair[0], pair[1])
		}
	}
}

func TestLoadFallsBackWhenRecordedBinaryIsMissing(t *testing.T) {
	runtimeRoot := t.TempDir()
	binDir := filepath.Join(runtimeRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	corePath := filepath.Join(binDir, "agentdock.exe")
	trayPath := filepath.Join(binDir, "agentdock-tray.exe")
	for _, path := range []string{corePath, trayPath} {
		if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	manifest := Manifest{
		SchemaVersion:   SchemaVersion,
		InstallRoot:     runtimeRoot,
		AgentDockBinary: filepath.Join(runtimeRoot, "missing", "agentdock.exe"),
		TrayBinary:      filepath.Join(runtimeRoot, "missing", "agentdock-tray.exe"),
		Host:            "127.0.0.1",
		Port:            8765,
		LocalMCPURL:     "http://127.0.0.1:8765/mcp",
		TunnelMode:      "none",
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(runtimeRoot, "runtime.json")
	if err := os.WriteFile(manifestPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadForBinary(corePath)
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(loaded.AgentDockBinary, corePath) {
		t.Fatalf("agentdock_binary = %q, want %q", loaded.AgentDockBinary, corePath)
	}
	if !samePath(loaded.TrayBinary, trayPath) {
		t.Fatalf("tray_binary = %q, want %q", loaded.TrayBinary, trayPath)
	}
}

func TestLoadKeepsExistingExternalManagedPath(t *testing.T) {
	runtimeRoot := t.TempDir()
	binDir := filepath.Join(runtimeRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	corePath := filepath.Join(binDir, "agentdock.exe")
	if err := os.WriteFile(corePath, []byte("core"), 0o600); err != nil {
		t.Fatal(err)
	}
	externalRoot := t.TempDir()
	externalCloudflared := filepath.Join(externalRoot, "cloudflared.exe")
	if err := os.WriteFile(externalCloudflared, []byte("cloudflared"), 0o600); err != nil {
		t.Fatal(err)
	}

	manifest := Manifest{
		SchemaVersion:     SchemaVersion,
		InstallRoot:       runtimeRoot,
		AgentDockBinary:   corePath,
		CloudflaredBinary: externalCloudflared,
		Host:              "127.0.0.1",
		Port:              8765,
		LocalMCPURL:       "http://127.0.0.1:8765/mcp",
		TunnelMode:        "none",
	}
	manifestPath := filepath.Join(runtimeRoot, "runtime.json")
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(loaded.CloudflaredBinary, externalCloudflared) {
		t.Fatalf("existing external cloudflared path was rewritten: %q", loaded.CloudflaredBinary)
	}
}

func TestLoadKeepsMissingExternalCloudflaredPath(t *testing.T) {
	runtimeRoot := t.TempDir()
	binDir := filepath.Join(runtimeRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	corePath := filepath.Join(binDir, "agentdock.exe")
	bundledCloudflared := filepath.Join(binDir, "cloudflared.exe")
	for path, content := range map[string]string{
		corePath:           "core",
		bundledCloudflared: "bundled",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	externalCloudflared := filepath.Join(t.TempDir(), "missing-cloudflared.exe")
	manifest := Manifest{
		SchemaVersion:     SchemaVersion,
		InstallRoot:       runtimeRoot,
		AgentDockBinary:   corePath,
		CloudflaredBinary: externalCloudflared,
		Host:              "127.0.0.1",
		Port:              8765,
		LocalMCPURL:       "http://127.0.0.1:8765/mcp",
		TunnelMode:        "none",
	}
	manifestPath := filepath.Join(runtimeRoot, "runtime.json")
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(loaded.CloudflaredBinary, externalCloudflared) {
		t.Fatalf("missing external cloudflared path was replaced: %q", loaded.CloudflaredBinary)
	}
}

func TestManifestAllowsQuickModeWithoutPublicURL(t *testing.T) {
	manifest := Manifest{
		SchemaVersion:   SchemaVersion,
		AgentDockBinary: filepath.Join(t.TempDir(), "agentdock.exe"),
		Host:            "127.0.0.1",
		Port:            8765,
		LocalMCPURL:     "http://127.0.0.1:8765/mcp",
		TunnelMode:      "quick",
	}
	if err := manifest.Validate(); err != nil {
		t.Fatalf("quick mode must allow an empty public URL before cloudflared is ready: %v", err)
	}
}

func TestManifestRejectsPublicURLInLocalMode(t *testing.T) {
	manifest := Manifest{
		SchemaVersion:   SchemaVersion,
		AgentDockBinary: filepath.Join(t.TempDir(), "agentdock.exe"),
		Host:            "127.0.0.1",
		Port:            8765,
		LocalMCPURL:     "http://127.0.0.1:8765/mcp",
		TunnelMode:      "none",
		PublicURL:       "https://example.com",
	}
	if err := manifest.Validate(); err == nil {
		t.Fatal("expected local mode with public URL to fail")
	}
}

func TestManifestElevatedModeRequiresScheduledTask(t *testing.T) {
	manifest := Manifest{
		SchemaVersion:   SchemaVersion,
		AgentDockBinary: filepath.Join(t.TempDir(), "agentdock.exe"),
		PrivilegeMode:   "elevated",
		Host:            "127.0.0.1",
		Port:            8765,
		LocalMCPURL:     "http://127.0.0.1:8765/mcp",
		TunnelMode:      "none",
	}
	if err := manifest.Validate(); err == nil {
		t.Fatal("expected elevated mode without a scheduled task to fail")
	}

	manifest.AgentDockTaskName = "AgentDock"
	if err := manifest.Validate(); err != nil {
		t.Fatalf("elevated manifest should be valid: %v", err)
	}
	if !manifest.UsesScheduledTask() {
		t.Fatal("elevated manifest should use Task Scheduler")
	}
}

func TestManifestKeepsLegacyStandardModeCompatible(t *testing.T) {
	manifest := Manifest{
		SchemaVersion:   SchemaVersion,
		AgentDockBinary: filepath.Join(t.TempDir(), "agentdock.exe"),
		Host:            "127.0.0.1",
		Port:            8765,
		LocalMCPURL:     "http://127.0.0.1:8765/mcp",
		TunnelMode:      "none",
	}
	if err := manifest.Validate(); err != nil {
		t.Fatalf("legacy manifest without privilege fields should remain valid: %v", err)
	}
	if manifest.UsesScheduledTask() {
		t.Fatal("legacy manifest must remain a standard launcher installation")
	}
}

func TestSaveManifestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.json")
	manifest := Manifest{
		SchemaVersion:               SchemaVersion,
		AgentDockHome:               filepath.Join(filepath.Dir(path), "state"),
		AgentDockDefaultDir:         filepath.Join(filepath.Dir(path), "workspace"),
		AgentDockBinary:             filepath.Join(filepath.Dir(path), "bin", "agentdock.exe"),
		TrayBinary:                  filepath.Join(filepath.Dir(path), "bin", "agentdock-tray.exe"),
		CloudflaredBinary:           filepath.Join(filepath.Dir(path), "bin", "cloudflared.exe"),
		StartupValueName:            "AgentDockCoreTest",
		CloudflaredStartupValueName: "AgentDockTunnelTest",
		Host:                        "127.0.0.1",
		Port:                        28765,
		LocalMCPURL:                 "http://127.0.0.1:28765/mcp",
		TunnelMode:                  "quick",
		PublicURL:                   "https://native-test.trycloudflare.com",
		InstallChannel:              "setup",
	}
	if err := Save(path, manifest); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PublicURL != manifest.PublicURL || loaded.CloudflaredStartupValueName != manifest.CloudflaredStartupValueName ||
		loaded.AgentDockHome != manifest.AgentDockHome || loaded.AgentDockDefaultDir != manifest.AgentDockDefaultDir {
		t.Fatalf("manifest round trip mismatch: %#v", loaded)
	}
}

func TestManifestRejectsRelativeRuntimeStatePaths(t *testing.T) {
	manifest := Manifest{
		SchemaVersion:       SchemaVersion,
		AgentDockHome:       "relative-state",
		AgentDockDefaultDir: filepath.Join(t.TempDir(), "workspace"),
		AgentDockBinary:     filepath.Join(t.TempDir(), "agentdock.exe"),
		Host:                "127.0.0.1",
		Port:                8765,
		LocalMCPURL:         "http://127.0.0.1:8765/mcp",
		TunnelMode:          "none",
	}
	if err := manifest.Validate(); err == nil {
		t.Fatal("expected relative agentdock_home to be rejected")
	}
}

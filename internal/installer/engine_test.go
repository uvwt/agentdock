package installer

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/uvwt/agentdock/internal/desktopruntime"
	"github.com/uvwt/agentdock/internal/envstore"
	"github.com/uvwt/agentdock/internal/updateengine"
)

func TestEngineInstallsLinuxLayoutWithoutStartingServices(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux layout is exercised on Unix CI")
	}
	root := t.TempDir()
	installRoot := filepath.Join(root, "opt")
	runtimeRoot := filepath.Join(root, "etc")
	systemdDir := filepath.Join(root, "systemd")
	payload := filepath.Join(root, "payload")
	if err := os.MkdirAll(filepath.Join(payload, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payload, "bin", "agentdock"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	skills := filepath.Join(payload, "share", "agentdock", "core-skills")
	if err := os.MkdirAll(skills, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skills, "manifest.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	request := Request{
		InstallRoot:    installRoot,
		RuntimeRoot:    runtimeRoot,
		PayloadDir:     payload,
		Version:        "v1.2.3",
		Host:           "127.0.0.1",
		Port:           8765,
		ServiceName:    "agentdock",
		ServiceUser:    "agentdock",
		ServiceGroup:   "agentdock",
		ServiceManager: "systemd",
		SystemdDir:     systemdDir,
		DataDir:        filepath.Join(root, "data"),
		SkipHealth:     true,
		StartService:   false,
		SkipSkills:     true,
	}
	journal := newJournal(runtimeRoot, "layout")
	staged, err := stageUnixPayload(request, journal)
	if err != nil {
		t.Fatal(err)
	}
	activated, err := activateLinux(context.Background(), request, staged)
	if err != nil {
		t.Fatal(err)
	}
	if activated.LocalMCPURL != "http://127.0.0.1:8765/mcp" {
		t.Fatalf("mcp url=%s", activated.LocalMCPURL)
	}
	live := filepath.Join(installRoot, "bin", "agentdock")
	if !fileExists(live) {
		t.Fatal("live binary missing")
	}
	if !fileExists(filepath.Join(installRoot, "versions", "v1.2.3", "agentdock")) {
		t.Fatal("staged generation binary missing")
	}

	envPath := filepath.Join(runtimeRoot, "agentdock.env")
	values, err := envstore.ParseFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if values["AGENTDOCK_HOST"] != "127.0.0.1" || values["AGENTDOCK_PORT"] != "8765" {
		t.Fatalf("env=%v", values)
	}
	if values["AGENTDOCK_AUTH_TOKEN"] == "" {
		t.Fatal("auth token was not generated")
	}

	unit, err := os.ReadFile(filepath.Join(systemdDir, "agentdock.service"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(unit)
	if !strings.Contains(text, "service launch-core --runtime-root "+runtimeRoot) {
		t.Fatalf("unit missing native launch-core: %s", text)
	}

	result, err := Engine{}.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != updateengine.StateCommitted {
		t.Fatalf("state=%s", result.State)
	}
	if err := RequireInspection(runtimeRoot, "v1.2.3"); err != nil {
		t.Fatal(err)
	}
}

func TestUnixActivationFailureRestoresPreviousBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix rollback is exercised on Unix CI")
	}
	root := t.TempDir()
	installRoot := filepath.Join(root, "opt")
	runtimeRoot := filepath.Join(root, "etc")
	payload1 := filepath.Join(root, "payload1", "bin")
	payload2 := filepath.Join(root, "payload2", "bin")
	if err := os.MkdirAll(payload1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(payload2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payload1, "agentdock"), []byte("version-one"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payload2, "agentdock"), []byte("version-two"), 0o755); err != nil {
		t.Fatal(err)
	}
	base := Request{
		InstallRoot:    installRoot,
		RuntimeRoot:    runtimeRoot,
		SkipHealth:     true,
		StartService:   false,
		SkipSkills:     true,
		ServiceManager: "none",
	}
	first := base
	first.PayloadDir = filepath.Dir(payload1)
	first.Version = "v1.0.0"
	if _, err := (Engine{}).Run(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(installRoot, "bin", "agentdock")
	got, err := os.ReadFile(live)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "version-one" {
		t.Fatalf("v1 live=%q", got)
	}

	envPath := filepath.Join(runtimeRoot, "agentdock.env")
	if err := os.Remove(envPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(envPath, 0o700); err != nil {
		t.Fatal(err)
	}

	second := base
	second.PayloadDir = filepath.Dir(payload2)
	second.Version = "v2.0.0"
	result, err := Engine{}.Run(context.Background(), second)
	if err == nil {
		t.Fatal("expected v2 activation to fail")
	}
	if result.State != updateengine.StateRolledBack {
		t.Fatalf("state=%s failure=%v", result.State, result.Failure)
	}
	got, err = os.ReadFile(live)
	if err != nil {
		t.Fatalf("old binary missing after reported rollback: %v", err)
	}
	if string(got) != "version-one" {
		t.Fatalf("rollback restored %q, want version-one", got)
	}
	if result.ActiveVersion != "v1.0.0" {
		t.Fatalf("rolled_back active_version=%s, want v1.0.0 (running version), target=%s", result.ActiveVersion, result.Version)
	}
	if result.Version != "v2.0.0" {
		t.Fatalf("failed target version should remain v2.0.0, got %s", result.Version)
	}
	if result.Healthy {
		t.Fatal("rolled_back result must not keep trial Healthy=true")
	}
	if result.Phase != PhaseRollback {
		t.Fatalf("rolled_back phase=%s, want rollback", result.Phase)
	}
}

func TestRepairUsesExistingLiveBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix repair path")
	}
	root := t.TempDir()
	installRoot := filepath.Join(root, "opt")
	runtimeRoot := filepath.Join(root, "etc")
	payload := filepath.Join(root, "payload", "bin")
	if err := os.MkdirAll(payload, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payload, "agentdock"), []byte("installed"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := (Engine{}).Run(context.Background(), Request{
		InstallRoot:    installRoot,
		RuntimeRoot:    runtimeRoot,
		PayloadDir:     filepath.Dir(payload),
		Version:        "v1.0.0",
		SkipHealth:     true,
		StartService:   false,
		SkipSkills:     true,
		ServiceManager: "none",
	}); err != nil {
		t.Fatal(err)
	}
	result, err := (Engine{}).Run(context.Background(), Request{
		Action:         ActionRepair,
		InstallRoot:    installRoot,
		RuntimeRoot:    runtimeRoot,
		SkipHealth:     true,
		StartService:   false,
		SkipSkills:     true,
		ServiceManager: "none",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != updateengine.StateCommitted {
		t.Fatalf("repair state=%s", result.State)
	}
}

func TestSkillFailureAfterActivateRestoresSourceAsActiveVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix rollback is exercised on Unix CI")
	}
	root := t.TempDir()
	installRoot := filepath.Join(root, "opt")
	runtimeRoot := filepath.Join(root, "etc")
	payload1 := filepath.Join(root, "payload1", "bin")
	payload2 := filepath.Join(root, "payload2", "bin")
	if err := os.MkdirAll(payload1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(payload2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payload1, "agentdock"), []byte("version-one"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payload2, "agentdock"), []byte("version-two"), 0o755); err != nil {
		t.Fatal(err)
	}
	base := Request{
		InstallRoot:    installRoot,
		RuntimeRoot:    runtimeRoot,
		SkipHealth:     true,
		StartService:   false,
		SkipSkills:     true,
		ServiceManager: "none",
		AgentDockHome:  filepath.Join(root, "home"),
	}
	first := base
	first.PayloadDir = filepath.Dir(payload1)
	first.Version = "v1.0.0"
	if _, err := (Engine{}).Run(context.Background(), first); err != nil {
		t.Fatal(err)
	}

	badBundle := filepath.Join(root, "not-a-bundle")
	if err := os.WriteFile(badBundle, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := base
	second.PayloadDir = filepath.Dir(payload2)
	second.Version = "v2.0.0"
	second.SkipSkills = false
	second.SkillBundle = badBundle
	result, err := (Engine{}).Run(context.Background(), second)
	if err == nil {
		t.Fatal("expected v2 skill bootstrap to fail")
	}
	if result.State != updateengine.StateRolledBack {
		t.Fatalf("state=%s failure=%v", result.State, result.Failure)
	}
	if result.ActiveVersion != "v1.0.0" {
		t.Fatalf("active_version=%s want v1.0.0 after skill failure rollback", result.ActiveVersion)
	}
	got, err := os.ReadFile(filepath.Join(installRoot, "bin", "agentdock"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "version-one" {
		t.Fatalf("live=%q", got)
	}

	third := base
	third.PayloadDir = filepath.Dir(payload1)
	third.Version = "v3.0.0"
	if _, err := (Engine{}).Run(context.Background(), third); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(runtimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := store.ReadTransaction()
	if err != nil {
		t.Fatal(err)
	}
	if tx.SourceVersion != "v1.0.0" {
		t.Fatalf("next install source_version=%s, want v1.0.0 not failed v2", tx.SourceVersion)
	}
}

func TestWriteCoreEnvironmentPreservesHostPortWhenUnspecified(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agentdock.env")
	if err := writeCoreEnvironment(path, Request{
		Host:      "127.0.0.2",
		Port:      18888,
		LogLevel:  "info",
		AuthToken: Specified("stable-token"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := writeCoreEnvironment(path, Request{AuthToken: Specified("stable-token")}); err != nil {
		t.Fatal(err)
	}
	values, err := envstore.ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if values["AGENTDOCK_HOST"] != "127.0.0.2" || values["AGENTDOCK_PORT"] != "18888" {
		t.Fatalf("unspecified host/port overwrote existing listen address: %v", values)
	}
}

func TestWriteCoreEnvironmentQuickTunnelWithoutURLDoesNotEnableOAuth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agentdock.env")
	first := Request{
		Host:       "127.0.0.1",
		Port:       8765,
		TunnelMode: "named",
		ServerURL:  "https://agent.example.test",
		AuthToken:  Specified("stable-token"),
	}
	if err := writeCoreEnvironment(path, first); err != nil {
		t.Fatal(err)
	}
	firstValues, err := envstore.ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	password := firstValues["AGENTDOCK_OAUTH_PASSWORD"]
	secret := firstValues["AGENTDOCK_OAUTH_TOKEN_SECRET"]

	quick := first
	quick.ServerURL = ""
	quick.TunnelMode = "quick"
	if err := writeCoreEnvironment(path, quick); err != nil {
		t.Fatal(err)
	}
	values, err := envstore.ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if values["AGENTDOCK_OAUTH_ENABLED"] != "false" {
		t.Fatalf("quick tunnel without origin must not enable oauth: %v", values)
	}
	if values["AGENTDOCK_SERVER_URL"] != "" {
		t.Fatalf("quick tunnel without origin must clear server url, got %q", values["AGENTDOCK_SERVER_URL"])
	}
	if values["AGENTDOCK_OAUTH_PASSWORD"] != password || values["AGENTDOCK_OAUTH_TOKEN_SECRET"] != secret {
		t.Fatal("switching to quick tunnel must not rotate oauth credentials")
	}
}

func TestWriteCoreEnvironmentPreservesOAuthOnRepair(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agentdock.env")
	first := Request{
		Host:       "127.0.0.1",
		Port:       8765,
		LogLevel:   "info",
		TunnelMode: "named",
		ServerURL:  "https://agent.example.test",
		AuthToken:  Specified("stable-token"),
	}
	if err := writeCoreEnvironment(path, first); err != nil {
		t.Fatal(err)
	}
	firstValues, err := envstore.ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	password := firstValues["AGENTDOCK_OAUTH_PASSWORD"]
	secret := firstValues["AGENTDOCK_OAUTH_TOKEN_SECRET"]
	if password == "" || secret == "" {
		t.Fatal("first public install must generate oauth credentials")
	}

	repair := first
	repair.Host = "127.0.0.2"
	if err := writeCoreEnvironment(path, repair); err != nil {
		t.Fatal(err)
	}
	secondValues, err := envstore.ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if secondValues["AGENTDOCK_OAUTH_PASSWORD"] != password || secondValues["AGENTDOCK_OAUTH_TOKEN_SECRET"] != secret {
		t.Fatalf("oauth rotated on repair: %v", secondValues)
	}
	if secondValues["AGENTDOCK_HOST"] != "127.0.0.2" {
		t.Fatal("host should update while oauth is preserved")
	}
	if secondValues["AGENTDOCK_BROWSER_ENABLED"] != "" {
		t.Fatal("unexpected browser key")
	}
}

func TestWriteCoreEnvironmentPreservesUnmanagedKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agentdock.env")
	if err := os.WriteFile(path, []byte("AGENTDOCK_HOST=127.0.0.9\nAGENTDOCK_BROWSER_ENABLED=true\nAGENTDOCK_NEXUS_TOKEN=legacy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeCoreEnvironment(path, Request{
		Host:      "127.0.0.1",
		Port:      8765,
		LogLevel:  "info",
		AuthToken: Specified("stable-token"),
	}); err != nil {
		t.Fatal(err)
	}
	values, err := envstore.ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if values["AGENTDOCK_HOST"] != "127.0.0.1" {
		t.Fatalf("host=%s", values["AGENTDOCK_HOST"])
	}
	if values["AGENTDOCK_BROWSER_ENABLED"] != "true" {
		t.Fatal("unmanaged browser setting was dropped")
	}
	if _, ok := values["AGENTDOCK_NEXUS_TOKEN"]; ok {
		t.Fatal("legacy Nexus token must be removed")
	}
	if values["AGENTDOCK_AUTH_TOKEN"] != "stable-token" {
		t.Fatalf("token=%s", values["AGENTDOCK_AUTH_TOKEN"])
	}
}

func TestWindowsGenerationLayoutAndManifest(t *testing.T) {
	root := t.TempDir()
	payload := filepath.Join(root, "payload")
	if err := os.MkdirAll(filepath.Join(payload, "share", "agentdock", "core-skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(payload, "wsl-helper"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"agentdock.exe":                             "core",
		"agentdock-tray.exe":                        "tray",
		"agentdock-arbiter.exe":                     "arbiter",
		"agentdock-shim.exe":                        "shim",
		"agentdock-tray-shim.exe":                   "tray-shim",
		"manage-windows.ps1":                        "manager",
		"share/agentdock/core-skills/manifest.json": "{}",
		"wsl-helper/manifest.json":                  "{}",
	} {
		path := filepath.Join(payload, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	request := Request{
		InstallRoot:                 filepath.Join(root, "runtime"),
		RuntimeRoot:                 filepath.Join(root, "runtime"),
		PayloadDir:                  payload,
		Version:                     "v0.9.0",
		Host:                        "127.0.0.1",
		Port:                        8765,
		TunnelMode:                  "none",
		AgentDockHome:               filepath.Join(root, "home", ".agentdock"),
		AgentDockDefaultDir:         filepath.Join(root, "home", "AgentDock"),
		PrivilegeMode:               "standard",
		TaskName:                    "AgentDockMustNotLeak",
		StartupValueName:            "AgentDockE2E",
		TrayStartupValueName:        "AgentDockTrayE2E",
		CloudflaredStartupValueName: "AgentDockCloudflaredE2E",
		SkipHealth:                  true,
		StartService:                false,
	}
	journal := newJournal(request.InstallRoot, "windows-test")
	staged, err := stageWindowsPayload(request, journal)
	if err != nil {
		t.Fatal(err)
	}
	activated, err := activateWindows(context.Background(), request, staged)
	if err != nil {
		t.Fatal(err)
	}
	if activated.ActiveVersion != "v0.9.0" {
		t.Fatalf("active=%s", activated.ActiveVersion)
	}
	layout, err := updateengine.NewWindowsLayout(request.InstallRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !fileExists(layout.GenerationCore("v0.9.0")) {
		t.Fatal("generation core missing")
	}
	manifest, err := desktopruntime.Load(filepath.Join(request.InstallRoot, "runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.AgentDockTaskName != "" {
		t.Fatalf("standard install leaked scheduled task name: %q", manifest.AgentDockTaskName)
	}
	if manifest.StartupValueName != "AgentDockE2E" ||
		manifest.TrayStartupValueName != "AgentDockTrayE2E" ||
		manifest.CloudflaredStartupValueName != "AgentDockCloudflaredE2E" {
		t.Fatalf("custom Windows startup identity was not preserved: %+v", manifest)
	}
	store, err := updateengine.NewStore(request.InstallRoot)
	if err != nil {
		t.Fatal(err)
	}
	active, err := store.ReadActive()
	if err != nil {
		t.Fatal(err)
	}
	if active.ActiveVersion != "v0.9.0" || active.State != updateengine.StateTrial {
		t.Fatalf("first publish active=%s state=%s, want trial until install commit", active.ActiveVersion, active.State)
	}
	if active.TransactionID != "windows-test" {
		t.Fatalf("trial pointer transaction=%s, want windows-test", active.TransactionID)
	}
	if err := commitWindowsActivePointer(request.InstallRoot, "windows-test"); err != nil {
		t.Fatal(err)
	}
	active, err = store.ReadActive()
	if err != nil {
		t.Fatal(err)
	}
	if active.ActiveVersion != "v0.9.0" || active.State != updateengine.StateCommitted {
		t.Fatalf("commit pointer active=%s state=%s", active.ActiveVersion, active.State)
	}

	repair := request
	repair.PrivilegeMode = ""
	repair.TaskName = ""
	repair.StartupValueName = ""
	repair.TrayStartupValueName = ""
	repair.CloudflaredStartupValueName = ""
	repairJournal := newJournal(repair.InstallRoot, "windows-repair")
	repairStaged, err := stageWindowsPayload(repair, repairJournal)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := activateWindows(context.Background(), repair, repairStaged); err != nil {
		t.Fatal(err)
	}
	manifest, err = desktopruntime.Load(filepath.Join(request.InstallRoot, "runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.AgentDockTaskName != "" || manifest.PrivilegeMode != "standard" {
		t.Fatalf("standard repair restored a scheduled task identity: task=%q privilege=%q", manifest.AgentDockTaskName, manifest.PrivilegeMode)
	}
	if manifest.StartupValueName != "AgentDockE2E" ||
		manifest.TrayStartupValueName != "AgentDockTrayE2E" ||
		manifest.CloudflaredStartupValueName != "AgentDockCloudflaredE2E" {
		t.Fatalf("repair lost custom Windows startup identity: %+v", manifest)
	}
}

func TestWindowsDoesNotRestageOwnedGeneration(t *testing.T) {
	root := t.TempDir()
	payload := filepath.Join(root, "payload")
	if err := os.MkdirAll(payload, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"agentdock.exe":         "core-v1",
		"agentdock-tray.exe":    "tray-v1",
		"agentdock-arbiter.exe": "arbiter-v1",
	} {
		if err := os.WriteFile(filepath.Join(payload, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	request := Request{
		InstallRoot:  filepath.Join(root, "runtime"),
		RuntimeRoot:  filepath.Join(root, "runtime"),
		PayloadDir:   payload,
		Version:      "v1.0.0",
		Host:         "127.0.0.1",
		Port:         8765,
		TunnelMode:   "none",
		SkipHealth:   true,
		StartService: false,
	}
	if _, err := stageWindowsPayload(request, newJournal(request.InstallRoot, "owned-1")); err != nil {
		t.Fatal(err)
	}
	if err := commitWindowsActivePointer(request.InstallRoot, "owned-1"); err != nil {
		t.Fatal(err)
	}
	layout, err := updateengine.NewWindowsLayout(request.InstallRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payload, "agentdock.exe"), []byte("core-v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	staged, err := stageWindowsPayload(request, newJournal(request.InstallRoot, "owned-2"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(layout.GenerationCore("v1.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "core-v1" {
		t.Fatalf("owned generation was restaged: %q", got)
	}
	if staged.Binary != layout.GenerationCore("v1.0.0") {
		t.Fatalf("attach binary=%s", staged.Binary)
	}
}

func TestActivateWindowsDoesNotRewriteActiveVersion(t *testing.T) {
	root := t.TempDir()
	payload := filepath.Join(root, "payload")
	if err := os.MkdirAll(payload, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"agentdock.exe", "agentdock-tray.exe", "agentdock-arbiter.exe"} {
		if err := os.WriteFile(filepath.Join(payload, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	request := Request{
		InstallRoot:         filepath.Join(root, "runtime"),
		RuntimeRoot:         filepath.Join(root, "runtime"),
		PayloadDir:          payload,
		Version:             "v1.0.0",
		Host:                "127.0.0.1",
		Port:                8765,
		TunnelMode:          "none",
		AgentDockHome:       filepath.Join(root, "home"),
		AgentDockDefaultDir: filepath.Join(root, "workspace"),
		SkipHealth:          true,
		StartService:        false,
	}
	journal := newJournal(request.InstallRoot, "pointer")
	staged, err := stageWindowsPayload(request, journal)
	if err != nil {
		t.Fatal(err)
	}
	store, err := updateengine.NewStore(request.InstallRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WriteActive(updateengine.ActiveVersion{
		SchemaVersion: updateengine.SchemaVersion,
		ActiveVersion: "v0.9.0",
		State:         updateengine.StateCommitted,
	}); err != nil {
		t.Fatal(err)
	}
	request.Version = "v1.0.0"
	activated, err := activateWindows(context.Background(), request, staged)
	if err != nil {
		t.Fatal(err)
	}
	if activated.ActiveVersion != "v0.9.0" {
		t.Fatalf("activate rewrote result active_version=%s", activated.ActiveVersion)
	}
	active, err := store.ReadActive()
	if err != nil {
		t.Fatal(err)
	}
	if active.ActiveVersion != "v0.9.0" {
		t.Fatalf("activate rewrote active-version.json to %s", active.ActiveVersion)
	}
}

func TestWindowsRepairWithoutPayloadUsesExistingGeneration(t *testing.T) {
	root := t.TempDir()
	installRoot := filepath.Join(root, "runtime")
	layout, err := updateengine.NewWindowsLayout(installRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := layout.EnsureBase(); err != nil {
		t.Fatal(err)
	}
	version := "v0.9.0"
	if err := os.MkdirAll(layout.GenerationDir(version), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.GenerationCore(version), []byte("core"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.GenerationTray(version), []byte("tray"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.GenerationArbiter(version), []byte("arbiter"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.CoreShim(), []byte("shim"), 0o644); err != nil {
		t.Fatal(err)
	}

	request := Request{
		Action:       ActionRepair,
		InstallRoot:  installRoot,
		RuntimeRoot:  installRoot,
		Version:      version,
		BinaryPath:   layout.CoreShim(),
		SkipHealth:   true,
		StartService: false,
		SkipSkills:   true,
	}
	journal := newJournal(installRoot, "windows-repair")
	staged, err := stageWindowsPayload(request, journal)
	if err != nil {
		t.Fatal(err)
	}
	if staged.Binary != layout.GenerationCore(version) {
		t.Fatalf("repair staged binary=%s, want generation core", staged.Binary)
	}
	if staged.WindowsLayout == nil {
		t.Fatal("repair must keep Windows layout")
	}
}

func TestLinuxTunnelUnitIsSnapshottedInJournal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux unit journal is exercised on Unix CI")
	}
	root := t.TempDir()
	request := Request{
		InstallRoot:    filepath.Join(root, "opt"),
		RuntimeRoot:    filepath.Join(root, "etc"),
		SystemdDir:     filepath.Join(root, "systemd"),
		ServiceName:    "agentdock",
		ServiceManager: "systemd",
		TunnelMode:     "named",
		ServerURL:      "https://agent.example.test",
		Version:        "v1.0.0",
	}
	if err := os.MkdirAll(filepath.Join(request.InstallRoot, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(request.InstallRoot, "bin", "agentdock"), []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	journal := newJournal(request.RuntimeRoot, "tunnel-journal")
	staged := stagedInstall{
		Binary:     filepath.Join(request.InstallRoot, "bin", "agentdock"),
		LiveBinary: filepath.Join(request.InstallRoot, "bin", "agentdock"),
		Journal:    journal,
	}
	if _, err := activateLinux(context.Background(), request, staged); err != nil {
		t.Fatal(err)
	}
	unit := filepath.Join(request.SystemdDir, "agentdock-cloudflared.service")
	if !fileExists(unit) {
		t.Fatal("tunnel unit was not written")
	}
	found := false
	for _, backup := range journal.Backups {
		if backup.Original == unit {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("tunnel unit %s was not snapshotted in rollback journal", unit)
	}
}

func TestAbandonRewritesCommittedStateToRolledBack(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("abandon is exercised on Unix CI")
	}
	root := t.TempDir()
	installRoot := filepath.Join(root, "opt")
	runtimeRoot := filepath.Join(root, "etc")
	payload := filepath.Join(root, "payload", "bin")
	if err := os.MkdirAll(payload, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payload, "agentdock"), []byte("v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := (Engine{}).Run(context.Background(), Request{
		InstallRoot:    installRoot,
		RuntimeRoot:    runtimeRoot,
		PayloadDir:     filepath.Dir(payload),
		Version:        "v1.0.0",
		SkipHealth:     true,
		StartService:   false,
		SkipSkills:     true,
		ServiceManager: "none",
	}); err != nil {
		t.Fatal(err)
	}
	abandoned, err := (Engine{}).Run(context.Background(), Request{
		Action:      ActionAbandon,
		InstallRoot: installRoot,
		RuntimeRoot: runtimeRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	if abandoned.State != updateengine.StateRolledBack {
		t.Fatalf("abandon state=%s", abandoned.State)
	}
	if abandoned.Phase != PhaseRollback {
		t.Fatalf("abandon phase=%s, want rollback", abandoned.Phase)
	}
	if abandoned.ActiveVersion != "" {
		t.Fatalf("fresh install abandon must clear active_version, got %s", abandoned.ActiveVersion)
	}
	if abandoned.Healthy {
		t.Fatal("abandon must not keep trial Healthy")
	}
	if abandoned.Failure == nil || abandoned.Failure.Code != "abandoned" {
		t.Fatalf("abandon failure=%v", abandoned.Failure)
	}
	if got := existingVersion(Request{InstallRoot: installRoot, RuntimeRoot: runtimeRoot}); got != "" {
		t.Fatalf("next source_version=%s, want empty after abandoning a fresh install", got)
	}

	if _, err := (Engine{}).Run(context.Background(), Request{
		InstallRoot:    installRoot,
		RuntimeRoot:    runtimeRoot,
		PayloadDir:     filepath.Dir(payload),
		Version:        "v2.0.0",
		SkipHealth:     true,
		StartService:   false,
		SkipSkills:     true,
		ServiceManager: "none",
	}); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(runtimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := store.ReadTransaction()
	if err != nil {
		t.Fatal(err)
	}
	if tx.SourceVersion != "" {
		t.Fatalf("next install source_version=%s, want empty not abandoned v1.0.0", tx.SourceVersion)
	}
}

func TestNormalizeRequestRejectsUnsafeVersionPath(t *testing.T) {
	_, err := normalizeRequest(Request{InstallRoot: t.TempDir(), Version: `0.8.3\\..\\evil`})
	if err == nil {
		t.Fatal("unsafe version path must be rejected before generation path construction")
	}
}

func TestAbandonRefreshesAlreadyRolledBackProjection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("result reprojection is exercised on Unix CI")
	}
	root := t.TempDir()
	installRoot := filepath.Join(root, "opt")
	runtimeRoot := filepath.Join(root, "etc")
	if err := os.MkdirAll(runtimeRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtimeRoot, "agentdock.env"), []byte(
		"AGENTDOCK_HOST=127.0.0.1\n"+
			"AGENTDOCK_PORT=8765\n"+
			"AGENTDOCK_SERVER_URL=https://new-quick.example.test\n",
	), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(runtimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	txID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	completedAt := now
	transaction := Transaction{
		SchemaVersion:   SchemaVersion,
		TransactionID:   txID,
		Platform:        "linux",
		Action:          ActionInstall,
		SourceVersion:   "v1.0.0",
		TargetVersion:   "v2.0.0",
		ActiveVersion:   "v1.0.0",
		FallbackVersion: "v1.0.0",
		State:           updateengine.StateRolledBack,
		Phase:           PhaseRollback,
		InstallRoot:     installRoot,
		RuntimeRoot:     runtimeRoot,
		StartedAt:       now,
		UpdatedAt:       now,
		CompletedAt:     &completedAt,
		Failure:         &updateengine.Failure{Code: "activate_failed", Message: "target failed", At: now},
	}
	if err := store.WriteTransaction(transaction); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteResult(Result{
		SchemaVersion:   SchemaVersion,
		TransactionID:   txID,
		Platform:        "linux",
		Action:          ActionInstall,
		State:           updateengine.StateRolledBack,
		Phase:           PhaseRollback,
		Version:         "v2.0.0",
		ActiveVersion:   "v1.0.0",
		FallbackVersion: "v1.0.0",
		PublicURL:       "https://old-quick.example.test",
		LocalMCPURL:     "http://127.0.0.1:9999/mcp",
		Failure:         transaction.Failure,
		StartedAt:       now,
		CompletedAt:     now,
	}); err != nil {
		t.Fatal(err)
	}

	refreshed, err := (Engine{}).Run(context.Background(), Request{
		Action:        ActionAbandon,
		InstallRoot:   installRoot,
		RuntimeRoot:   runtimeRoot,
		TransactionID: txID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.PublicURL != "https://new-quick.example.test" {
		t.Fatalf("public_url=%q, want final restored runtime", refreshed.PublicURL)
	}
	if refreshed.LocalMCPURL != "http://127.0.0.1:8765/mcp" {
		t.Fatalf("local_mcp_url=%q", refreshed.LocalMCPURL)
	}
	if refreshed.Failure == nil || refreshed.Failure.Code != "activate_failed" {
		t.Fatalf("rollback failure provenance changed: %#v", refreshed.Failure)
	}
	current, err := store.ReadCurrentResult()
	if err != nil {
		t.Fatal(err)
	}
	if current.PublicURL != refreshed.PublicURL || current.LocalMCPURL != refreshed.LocalMCPURL {
		t.Fatalf("persisted result not refreshed: %#v", current)
	}
}

func TestDeferCommitStaysTrialUntilCommit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("two-phase commit is exercised on Unix CI")
	}
	root := t.TempDir()
	installRoot := filepath.Join(root, "opt")
	runtimeRoot := filepath.Join(root, "etc")
	payload := filepath.Join(root, "payload", "bin")
	if err := os.MkdirAll(payload, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payload, "agentdock"), []byte("v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	prepared, err := (Engine{}).Run(context.Background(), Request{
		InstallRoot:    installRoot,
		RuntimeRoot:    runtimeRoot,
		PayloadDir:     filepath.Dir(payload),
		Version:        "v1.0.0",
		SkipHealth:     true,
		StartService:   false,
		SkipSkills:     true,
		ServiceManager: "none",
		DeferCommit:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.State != updateengine.StateTrial {
		t.Fatalf("defer-commit state=%s, want trial", prepared.State)
	}
	if prepared.Phase != PhaseCommit {
		t.Fatalf("defer-commit phase=%s, want commit (ready to finalize)", prepared.Phase)
	}

	committed, err := (Engine{}).Run(context.Background(), Request{
		Action:      ActionCommit,
		InstallRoot: installRoot,
		RuntimeRoot: runtimeRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	if committed.State != updateengine.StateCommitted {
		t.Fatalf("commit state=%s", committed.State)
	}
	if committed.Phase != PhaseCommit {
		t.Fatalf("commit phase=%s", committed.Phase)
	}
}

func TestAbandonTrialAndRollbackFailed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("abandon is exercised on Unix CI")
	}
	root := t.TempDir()
	installRoot := filepath.Join(root, "opt")
	runtimeRoot := filepath.Join(root, "etc")
	payload := filepath.Join(root, "payload", "bin")
	if err := os.MkdirAll(payload, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payload, "agentdock"), []byte("v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	base := Request{
		InstallRoot:    installRoot,
		RuntimeRoot:    runtimeRoot,
		PayloadDir:     filepath.Dir(payload),
		Version:        "v1.0.0",
		SkipHealth:     true,
		StartService:   false,
		SkipSkills:     true,
		ServiceManager: "none",
		DeferCommit:    true,
	}
	if _, err := (Engine{}).Run(context.Background(), base); err != nil {
		t.Fatal(err)
	}

	abandoned, err := (Engine{}).Run(context.Background(), Request{
		Action:      ActionAbandon,
		InstallRoot: installRoot,
		RuntimeRoot: runtimeRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	if abandoned.State != updateengine.StateRolledBack || abandoned.Phase != PhaseRollback {
		t.Fatalf("trial abandon state=%s phase=%s", abandoned.State, abandoned.Phase)
	}

	failedRoot := t.TempDir()
	failedInstall := filepath.Join(failedRoot, "opt")
	failedRuntime := filepath.Join(failedRoot, "etc")
	failedPayload := filepath.Join(failedRoot, "payload", "bin")
	if err := os.MkdirAll(failedPayload, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(failedPayload, "agentdock"), []byte("v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	failedReq := base
	failedReq.InstallRoot = failedInstall
	failedReq.RuntimeRoot = failedRuntime
	failedReq.PayloadDir = filepath.Dir(failedPayload)
	if _, err := (Engine{}).Run(context.Background(), failedReq); err != nil {
		t.Fatal(err)
	}
	failed, err := (Engine{}).Run(context.Background(), Request{
		Action:         ActionAbandon,
		InstallRoot:    failedInstall,
		RuntimeRoot:    failedRuntime,
		RollbackFailed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if failed.State != updateengine.StateFailed {
		t.Fatalf("rollback-failed state=%s, want failed", failed.State)
	}
	if failed.Phase != PhaseRollback {
		t.Fatalf("rollback-failed phase=%s, want rollback", failed.Phase)
	}
	if failed.Failure == nil || failed.Failure.Code != FailureExternalRollbackFailed {
		t.Fatalf("rollback-failed failure=%v", failed.Failure)
	}
	if failed.Healthy {
		t.Fatal("rollback-failed must not stay healthy")
	}
}

func TestInterruptedTrialIsRolledBackBeforeNextInstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix rollback journal is exercised on Unix CI")
	}
	root := t.TempDir()
	installRoot := filepath.Join(root, "opt")
	runtimeRoot := filepath.Join(root, "etc")
	writePayload := func(name, body string) string {
		dir := filepath.Join(root, name, "bin")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "agentdock"), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		return filepath.Dir(dir)
	}
	base := Request{
		InstallRoot:    installRoot,
		RuntimeRoot:    runtimeRoot,
		SkipHealth:     true,
		StartService:   false,
		SkipSkills:     true,
		ServiceManager: "none",
	}
	first := base
	first.PayloadDir = writePayload("p1", "version-one")
	first.Version = "v1.0.0"
	if _, err := (Engine{}).Run(context.Background(), first); err != nil {
		t.Fatal(err)
	}

	second := base
	second.PayloadDir = writePayload("p2", "version-two")
	second.Version = "v2.0.0"
	second.DeferCommit = true
	if _, err := (Engine{}).Run(context.Background(), second); err != nil {
		t.Fatal(err)
	}

	third := base
	third.PayloadDir = writePayload("p3", "version-three")
	third.Version = "v3.0.0"
	result, err := (Engine{}).Run(context.Background(), third)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != updateengine.StateCommitted {
		t.Fatalf("v3 state=%s", result.State)
	}
	store, err := NewStore(runtimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := store.ReadTransaction()
	if err != nil {
		t.Fatal(err)
	}
	if tx.SourceVersion != "v1.0.0" {
		t.Fatalf("v3 source_version=%s, want v1.0.0 not interrupted v2 trial", tx.SourceVersion)
	}
	got, err := os.ReadFile(filepath.Join(installRoot, "bin", "agentdock"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "version-three" {
		t.Fatalf("live=%q", got)
	}
}

func TestMissingJournalAfterActivateBlocksEveryRetry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix rollback journal is exercised on Unix CI")
	}
	root := t.TempDir()
	installRoot := filepath.Join(root, "opt")
	runtimeRoot := filepath.Join(root, "etc")
	payload := filepath.Join(root, "payload", "bin")
	if err := os.MkdirAll(payload, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payload, "agentdock"), []byte("version-one"), 0o755); err != nil {
		t.Fatal(err)
	}
	base := Request{
		InstallRoot:    installRoot,
		RuntimeRoot:    runtimeRoot,
		PayloadDir:     filepath.Dir(payload),
		SkipHealth:     true,
		StartService:   false,
		SkipSkills:     true,
		ServiceManager: "none",
	}
	first := base
	first.Version = "v1.0.0"
	if _, err := (Engine{}).Run(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installRoot, "bin", "agentdock"), []byte("version-two"), 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(runtimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.WriteTransaction(Transaction{
		SchemaVersion: SchemaVersion,
		TransactionID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Platform:      "linux",
		Action:        ActionInstall,
		SourceVersion: "v1.0.0",
		TargetVersion: "v2.0.0",
		ActiveVersion: "v2.0.0",
		State:         updateengine.StateTrial,
		Phase:         PhaseActivate,
		InstallRoot:   installRoot,
		RuntimeRoot:   runtimeRoot,
		StartedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatal(err)
	}
	third := base
	third.Version = "v3.0.0"
	if err := os.WriteFile(filepath.Join(payload, "agentdock"), []byte("version-three"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := (Engine{}).Run(context.Background(), third); err == nil {
		t.Fatal("first retry must refuse an interrupted activate without journal")
	}
	if _, err := (Engine{}).Run(context.Background(), third); err == nil {
		t.Fatal("second retry must still refuse; failed without journal is not handled")
	}
	got, err := os.ReadFile(filepath.Join(installRoot, "bin", "agentdock"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) == "version-three" {
		t.Fatal("v3 must not install over an unrestorable v2 trial")
	}
}

func TestCommitDoesNotReturnStaleCommittedResult(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("commit binding is exercised on Unix CI")
	}
	root := t.TempDir()
	installRoot := filepath.Join(root, "opt")
	runtimeRoot := filepath.Join(root, "etc")
	payload := filepath.Join(root, "payload", "bin")
	if err := os.MkdirAll(payload, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payload, "agentdock"), []byte("v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	base := Request{
		InstallRoot:    installRoot,
		RuntimeRoot:    runtimeRoot,
		PayloadDir:     filepath.Dir(payload),
		SkipHealth:     true,
		StartService:   false,
		SkipSkills:     true,
		ServiceManager: "none",
	}
	first := base
	first.Version = "v1.0.0"
	v1, err := (Engine{}).Run(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(runtimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := os.ReadFile(store.CurrentResultPath())
	if err != nil {
		t.Fatal(err)
	}

	second := base
	second.Version = "v2.0.0"
	second.DeferCommit = true
	trial, err := (Engine{}).Run(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}
	if trial.State != updateengine.StateTrial {
		t.Fatalf("v2 state=%s", trial.State)
	}
	if err := os.WriteFile(store.CurrentResultPath(), stale, 0o600); err != nil {
		t.Fatal(err)
	}

	committed, err := (Engine{}).Run(context.Background(), Request{
		Action:      ActionCommit,
		InstallRoot: installRoot,
		RuntimeRoot: runtimeRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	if committed.TransactionID != trial.TransactionID {
		t.Fatalf("commit returned stale transaction %s, want %s (not v1 %s)", committed.TransactionID, trial.TransactionID, v1.TransactionID)
	}
	if committed.State != updateengine.StateCommitted || committed.Version != "v2.0.0" {
		t.Fatalf("commit state=%s version=%s", committed.State, committed.Version)
	}

	_, err = (Engine{}).Run(context.Background(), Request{
		Action:        ActionCommit,
		InstallRoot:   installRoot,
		RuntimeRoot:   runtimeRoot,
		TransactionID: "ffffffffffffffffffffffffffffffff",
	})
	if err == nil {
		t.Fatal("commit must reject a mismatched transaction id")
	}
}

func TestFailedRollbackDoesNotBecomeNextSource(t *testing.T) {
	root := t.TempDir()
	runtimeRoot := filepath.Join(root, "etc")
	store, err := NewStore(runtimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	tx := Transaction{
		SchemaVersion: SchemaVersion,
		TransactionID: "deadbeefdeadbeefdeadbeefdeadbeef",
		Platform:      "linux",
		Action:        ActionInstall,
		SourceVersion: "v1.0.0",
		TargetVersion: "v2.0.0",
		ActiveVersion: "v2.0.0",
		State:         updateengine.StateFailed,
		Phase:         PhaseRollback,
		InstallRoot:   filepath.Join(root, "opt"),
		RuntimeRoot:   runtimeRoot,
		StartedAt:     now,
		UpdatedAt:     now,
		Failure:       &updateengine.Failure{Code: "rollback_failed", Message: "stop failed", At: now},
	}
	if err := store.WriteTransaction(tx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Complete(tx, updateengine.StateFailed, Result{
		ActiveVersion: "v2.0.0",
		Version:       "v2.0.0",
		Failure:       tx.Failure,
	}); err != nil {
		t.Fatal(err)
	}
	got := existingVersion(Request{InstallRoot: tx.InstallRoot, RuntimeRoot: runtimeRoot})
	if got != "v1.0.0" {
		t.Fatalf("existingVersion=%s after rollback_failed, want v1.0.0 not failed v2", got)
	}
}

func TestExistingVersionReadsCommittedGenerationPointer(t *testing.T) {
	root := t.TempDir()
	installRoot := filepath.Join(root, "runtime")
	if err := os.MkdirAll(installRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := updateengine.NewStore(installRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WriteActive(updateengine.ActiveVersion{
		SchemaVersion: updateengine.SchemaVersion,
		ActiveVersion: "v0.8.3",
		State:         updateengine.StateCommitted,
	}); err != nil {
		t.Fatal(err)
	}
	got := existingVersion(Request{InstallRoot: installRoot, RuntimeRoot: filepath.Join(root, "missing-installer-store")})
	if got != "v0.8.3" {
		t.Fatalf("existingVersion=%s, want v0.8.3 from active-version.json", got)
	}
}

func TestWaitNamedTunnelReadyRequiresRunningProcess(t *testing.T) {
	root := t.TempDir()
	request := Request{
		InstallRoot:    root,
		RuntimeRoot:    root,
		TunnelMode:     "named",
		ServerURL:      "https://named.example.test",
		ServiceName:    "agentdock",
		ServiceManager: "none",
	}
	if err := waitTunnelReady(context.Background(), request, 200*time.Millisecond); err == nil {
		t.Fatal("named tunnel without a running cloudflared must not be ready")
	}
}

func TestWaitWindowsQuickTunnelReadyUsesRuntimeFiles(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	request := Request{RuntimeRoot: root, TunnelMode: "quick"}
	if err := waitWindowsQuickTunnelReady(ctx, request, 200*time.Millisecond); err == nil {
		t.Fatal("empty Windows runtime must not be ready")
	}
	url := "https://fresh.trycloudflare.com"
	if err := os.WriteFile(filepath.Join(root, "quick-tunnel-url.txt"), []byte(url+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "agentdock.env"), []byte("AGENTDOCK_SERVER_URL="+url+"\nAGENTDOCK_OAUTH_ENABLED=true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := waitWindowsQuickTunnelReady(ctx, request, 200*time.Millisecond); err == nil {
		t.Fatal("agentdock.env must not make Windows Quick Tunnel ready")
	}
	if err := os.WriteFile(filepath.Join(root, "server-url.txt"), []byte(url+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := desktopruntime.Manifest{
		SchemaVersion:   desktopruntime.SchemaVersion,
		AgentDockBinary: filepath.Join(root, "bin", "agentdock.exe"),
		Host:            "127.0.0.1",
		Port:            8765,
		LocalMCPURL:     "http://127.0.0.1:8765/mcp",
		TunnelMode:      "quick",
		PublicURL:       url,
	}
	if err := desktopruntime.Save(filepath.Join(root, "runtime.json"), manifest); err != nil {
		t.Fatal(err)
	}
	if err := waitWindowsQuickTunnelReady(ctx, request, time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestWaitQuickTunnelReadyRequiresURLAndOAuth(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows Quick readiness uses server-url.txt/runtime.json and is covered separately")
	}
	root := t.TempDir()
	ctx := context.Background()
	request := Request{RuntimeRoot: root, TunnelMode: "quick"}
	if err := waitTunnelReady(ctx, request, 200*time.Millisecond); err == nil {
		t.Fatal("empty runtime must not be ready")
	}
	if err := os.WriteFile(filepath.Join(root, "agentdock.env"), []byte("AGENTDOCK_HOST=127.0.0.1\nAGENTDOCK_PORT=8765\nAGENTDOCK_OAUTH_ENABLED=false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "quick-tunnel-url.txt"), []byte("https://fresh.trycloudflare.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := waitTunnelReady(ctx, request, 200*time.Millisecond); err == nil {
		t.Fatal("oauth still false must not be ready")
	}
	if err := os.WriteFile(filepath.Join(root, "agentdock.env"), []byte("AGENTDOCK_HOST=127.0.0.1\nAGENTDOCK_PORT=8765\nAGENTDOCK_SERVER_URL=https://fresh.trycloudflare.com\nAGENTDOCK_OAUTH_ENABLED=true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := waitTunnelReady(ctx, request, time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestLinuxAutostartRestoreDisablesPreviouslyDisabledUnits(t *testing.T) {
	if bin, action := linuxAutostartRestore("systemd", false); bin != "systemctl" || action != "disable" {
		t.Fatalf("systemd previously-disabled restore=%s %s", bin, action)
	}
	if _, action := linuxAutostartRestore("systemd", true); action != "enable" {
		t.Fatalf("systemd previously-enabled restore=%s", action)
	}
	if bin, action := linuxAutostartRestore("openrc", false); bin != "rc-update" || action != "del" {
		t.Fatalf("openrc previously-disabled restore=%s %s", bin, action)
	}
}

func TestJournalRestoreReportsAutostartDisableFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux autostart restore is exercised on Unix")
	}
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "systemctl"), []byte("#!/bin/sh\nexit 23\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	root := t.TempDir()
	journal := newJournal(root, "disable-fail")
	if err := journal.NoteService(journalService{
		Manager:    "systemd",
		Name:       "agentdock.service",
		WasActive:  false,
		WasEnabled: false,
	}); err != nil {
		t.Fatal(err)
	}
	err := journal.Restore(context.Background(), Request{InstallRoot: root, RuntimeRoot: root})
	if err == nil {
		t.Fatal("systemctl disable exit 23 must fail rollback, not be recorded as rolled_back")
	}
}

func TestJournalRestoreReportsStopFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux service stop is exercised on Unix")
	}
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "systemctl"), []byte("#!/bin/sh\nif [ \"$1\" = stop ]; then exit 23; fi\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	root := t.TempDir()
	journal := newJournal(root, "stop-fail")
	if err := journal.NoteService(journalService{
		Manager:    "systemd",
		Name:       "agentdock.service",
		WasActive:  true,
		WasEnabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	err := journal.Restore(context.Background(), Request{InstallRoot: root, RuntimeRoot: root})
	if err == nil {
		t.Fatal("systemctl stop exit 23 must fail rollback")
	}
}

func TestStartLinuxServicesReportsEnableFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux enable is exercised on Unix")
	}
	binDir := t.TempDir()
	script := "#!/bin/sh\nif [ \"$1\" = enable ] && [ \"$2\" != --now ]; then\n  exit 23\nfi\nexit 0\n"
	if err := os.WriteFile(filepath.Join(binDir, "systemctl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	root := t.TempDir()
	journal := newJournal(root, "enable-fail")
	err := startLinuxServices(context.Background(), Request{
		InstallRoot:    root,
		RuntimeRoot:    root,
		ServiceName:    "agentdock",
		ServiceManager: "systemd",
	}, journal)
	if err == nil {
		t.Fatal("systemctl enable exit 23 must fail start, not commit")
	}
}

func TestLinuxUnitsKeepManagedLogging(t *testing.T) {
	dir := t.TempDir()
	unit := filepath.Join(dir, "agentdock.service")
	if err := writeSystemdUnit(unit, "agentdock", "agentdock", "agentdock", "/opt/agentdock", "/etc/agentdock/agentdock.env", "/etc/agentdock"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(unit)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, forbidden := range []string{"StandardOutput=append:", "StandardError=append:"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("systemd logging regression: %s", forbidden)
		}
	}
}

func TestUninstallStopsUnitsThenPurgesWithoutRecreatingState(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("systemd 卸载语义只在 Unix 上验证；Windows 不能触碰宿主计划任务")
	}
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "systemctl"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	root := t.TempDir()
	installRoot := filepath.Join(root, "opt")
	runtimeRoot := filepath.Join(root, "etc")
	systemdDir := filepath.Join(root, "systemd")
	if err := os.MkdirAll(installRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(runtimeRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(systemdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtimeRoot, "agentdock.env"), []byte("AGENTDOCK_HOST=127.0.0.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(systemdDir, "agentdock.service"), []byte("unit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Engine{}.Run(context.Background(), Request{
		Action:         ActionUninstall,
		InstallRoot:    installRoot,
		RuntimeRoot:    runtimeRoot,
		ServiceName:    "agentdock",
		ServiceManager: "auto",
		SystemdDir:     systemdDir,
		PurgeConfig:    true,
		PurgeData:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if fileExists(filepath.Join(runtimeRoot, "agentdock.env")) {
		t.Fatal("env file should be purged")
	}
	if fileExists(filepath.Join(systemdDir, "agentdock.service")) {
		t.Fatal("systemd unit should be removed")
	}
	if dirExists(installRoot) {
		t.Fatal("purge-data must not leave install-root")
	}
	if dirExists(filepath.Join(runtimeRoot, "install")) {
		t.Fatal("purge-data must not recreate install journal")
	}
}

func TestCommitPointerFailureIsNotObservableCommitted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod on install-root injects pointer write failure on Unix")
	}
	root := t.TempDir()
	installRoot := filepath.Join(root, "opt")
	runtimeRoot := filepath.Join(root, "etc")
	payload := writeUnixPayload(t, root, "p1", "version-one")
	prepared, err := (Engine{}).Run(context.Background(), Request{
		InstallRoot:    installRoot,
		RuntimeRoot:    runtimeRoot,
		PayloadDir:     payload,
		Version:        "v1.0.0",
		SkipHealth:     true,
		StartService:   false,
		SkipSkills:     true,
		ServiceManager: "none",
		DeferCommit:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	writeWindowsTrialPointer(t, installRoot, "v1.0.0", prepared.TransactionID)
	if err := os.Chmod(installRoot, 0o555); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(installRoot, 0o755) }()

	committed, err := (Engine{}).Run(context.Background(), Request{
		Action:        ActionCommit,
		InstallRoot:   installRoot,
		RuntimeRoot:   runtimeRoot,
		TransactionID: prepared.TransactionID,
	})
	if err == nil {
		t.Fatal("pointer write failure must fail commit")
	}
	if committed.State == updateengine.StateCommitted {
		t.Fatalf("returned state=%s, pointer failure must not be observable committed", committed.State)
	}
	_, tx, current := readInstallStore(t, runtimeRoot)
	if tx.State == updateengine.StateCommitted || current.State == updateengine.StateCommitted {
		t.Fatalf("disk state=%s result=%s, transaction/result must stay trial", tx.State, current.State)
	}
	if !fileExists(journalFile(runtimeRoot, prepared.TransactionID)) {
		t.Fatal("journal must remain so recovery can finish or roll back")
	}

	if err := os.Chmod(installRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	finished, err := (Engine{}).Run(context.Background(), Request{
		Action:        ActionCommit,
		InstallRoot:   installRoot,
		RuntimeRoot:   runtimeRoot,
		TransactionID: prepared.TransactionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if finished.State != updateengine.StateCommitted {
		t.Fatalf("retry commit state=%s", finished.State)
	}
	store, err := updateengine.NewStore(installRoot)
	if err != nil {
		t.Fatal(err)
	}
	active, err := store.ReadActive()
	if err != nil {
		t.Fatal(err)
	}
	if active.State != updateengine.StateCommitted || active.TransactionID != prepared.TransactionID {
		t.Fatalf("recovered pointer state=%s tx=%s", active.State, active.TransactionID)
	}
}

func TestRecoverCompletesWhenPointerAlreadyCommitted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix payload install is exercised on Unix CI")
	}
	root := t.TempDir()
	installRoot := filepath.Join(root, "opt")
	runtimeRoot := filepath.Join(root, "etc")
	v1 := writeUnixPayload(t, root, "p1", "version-one")
	prepared, err := (Engine{}).Run(context.Background(), unixInstallRequest(installRoot, runtimeRoot, v1, "v1.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	if prepared.State != updateengine.StateCommitted {
		t.Fatalf("v1 state=%s", prepared.State)
	}

	v2payload := writeUnixPayload(t, root, "p2", "version-two")
	trial, err := (Engine{}).Run(context.Background(), Request{
		InstallRoot:    installRoot,
		RuntimeRoot:    runtimeRoot,
		PayloadDir:     v2payload,
		Version:        "v2.0.0",
		SkipHealth:     true,
		StartService:   false,
		SkipSkills:     true,
		ServiceManager: "none",
		DeferCommit:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	writeWindowsTrialPointer(t, installRoot, "v2.0.0", trial.TransactionID)
	if err := commitWindowsActivePointer(installRoot, trial.TransactionID); err != nil {
		t.Fatal(err)
	}
	_, tx, _ := readInstallStore(t, runtimeRoot)
	if tx.State != updateengine.StateTrial {
		t.Fatalf("simulated crash must leave trial, got %s", tx.State)
	}

	v3payload := writeUnixPayload(t, root, "p3", "version-three")
	next, err := (Engine{}).Run(context.Background(), unixInstallRequest(installRoot, runtimeRoot, v3payload, "v3.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	if next.State != updateengine.StateCommitted {
		t.Fatalf("v3 state=%s", next.State)
	}
	_, tx, _ = readInstallStore(t, runtimeRoot)
	if tx.SourceVersion != "v2.0.0" {
		t.Fatalf("v3 source_version=%s, want v2.0.0 from recovered pointer commit, not rollback", tx.SourceVersion)
	}
}

func TestReleaseTrialPointerFailureIsRollbackFailed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod on install-root injects pointer unlink failure on Unix")
	}
	root := t.TempDir()
	installRoot := filepath.Join(root, "opt")
	runtimeRoot := filepath.Join(root, "etc")
	if err := os.MkdirAll(installRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(runtimeRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(runtimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	tx := Transaction{
		SchemaVersion: SchemaVersion,
		TransactionID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Platform:      "linux",
		Action:        ActionInstall,
		SourceVersion: "",
		TargetVersion: "v1.0.0",
		State:         updateengine.StateTrial,
		Phase:         PhaseActivate,
		InstallRoot:   installRoot,
		RuntimeRoot:   runtimeRoot,
		StartedAt:     now,
		UpdatedAt:     now,
	}
	if err := store.WriteTransaction(tx); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(runtimeRoot, "marker.txt")
	if err := os.WriteFile(marker, []byte("known-good"), 0o600); err != nil {
		t.Fatal(err)
	}
	journal := newJournal(runtimeRoot, tx.TransactionID)
	if err := journal.Snapshot(marker); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("trial"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeWindowsTrialPointer(t, installRoot, "v1.0.0", tx.TransactionID)
	if err := os.Chmod(installRoot, 0o555); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(installRoot, 0o755) }()

	result, err := (Engine{}).Run(context.Background(), unixInstallRequest(installRoot, runtimeRoot, writeUnixPayload(t, root, "p2", "v2"), "v2.0.0"))
	if err == nil {
		t.Fatal("trial pointer cleanup failure must block the next install")
	}
	if result.State == updateengine.StateRolledBack {
		t.Fatal("pointer cleanup failure must not be recorded as rolled_back")
	}
	if result.Failure == nil || result.Failure.Code != FailureRollbackFailed {
		t.Fatalf("failure=%v, want rollback_failed", result.Failure)
	}
	if !fileExists(journalFile(runtimeRoot, tx.TransactionID)) {
		t.Fatal("journal must be kept as recovery evidence")
	}
	_, stored, current := readInstallStore(t, runtimeRoot)
	if stored.State == updateengine.StateRolledBack || current.State == updateengine.StateRolledBack {
		t.Fatal("disk state must not be rolled_back after pointer cleanup failure")
	}

	if err := os.Chmod(installRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	next, err := (Engine{}).Run(context.Background(), unixInstallRequest(installRoot, runtimeRoot, writeUnixPayload(t, root, "p3", "v3"), "v3.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	if next.State != updateengine.StateCommitted {
		t.Fatalf("after pointer cleanup, next install state=%s", next.State)
	}
}

func TestExternalRollbackFailedBlocksNextInstallUntilAbandon(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("abandon recovery is exercised on Unix CI")
	}
	root := t.TempDir()
	installRoot := filepath.Join(root, "opt")
	runtimeRoot := filepath.Join(root, "etc")
	payload := writeUnixPayload(t, root, "p1", "v1")
	if _, err := (Engine{}).Run(context.Background(), Request{
		InstallRoot:    installRoot,
		RuntimeRoot:    runtimeRoot,
		PayloadDir:     payload,
		Version:        "v1.0.0",
		SkipHealth:     true,
		StartService:   false,
		SkipSkills:     true,
		ServiceManager: "none",
		DeferCommit:    true,
	}); err != nil {
		t.Fatal(err)
	}
	failed, err := (Engine{}).Run(context.Background(), Request{
		Action:         ActionAbandon,
		InstallRoot:    installRoot,
		RuntimeRoot:    runtimeRoot,
		RollbackFailed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if failed.State != updateengine.StateFailed || failed.Failure == nil || failed.Failure.Code != FailureExternalRollbackFailed {
		t.Fatalf("state=%s failure=%v", failed.State, failed.Failure)
	}

	if _, err := (Engine{}).Run(context.Background(), unixInstallRequest(installRoot, runtimeRoot, writeUnixPayload(t, root, "p2", "v2"), "v2.0.0")); err == nil {
		t.Fatal("next Engine.Run must stay blocked after external_rollback_failed")
	}

	if _, err := (Engine{}).Run(context.Background(), Request{
		Action:      ActionAbandon,
		InstallRoot: installRoot,
		RuntimeRoot: runtimeRoot,
	}); err != nil {
		t.Fatal(err)
	}
	next, err := (Engine{}).Run(context.Background(), unixInstallRequest(installRoot, runtimeRoot, writeUnixPayload(t, root, "p3", "v3"), "v3.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	if next.State != updateengine.StateCommitted {
		t.Fatalf("explicit abandon must unblock install, state=%s", next.State)
	}
}

func TestRolledBackResultDoesNotKeepFailedTrialURLs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix rollback result projection is exercised on Unix CI")
	}
	root := t.TempDir()
	installRoot := filepath.Join(root, "opt")
	runtimeRoot := filepath.Join(root, "etc")
	v1 := writeUnixPayload(t, root, "p1", "version-one")
	first := unixInstallRequest(installRoot, runtimeRoot, v1, "v1.0.0")
	first.Host = "127.0.0.1"
	first.Port = 8765
	first.TunnelMode = "named"
	first.ServerURL = "https://v1.example.test"
	if _, err := (Engine{}).Run(context.Background(), first); err != nil {
		t.Fatal(err)
	}

	badBundle := filepath.Join(root, "not-a-bundle")
	if err := os.WriteFile(badBundle, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	v2 := writeUnixPayload(t, root, "p2", "version-two")
	second := unixInstallRequest(installRoot, runtimeRoot, v2, "v2.0.0")
	second.Host = "127.0.0.1"
	second.Port = 9876
	second.TunnelMode = "named"
	second.ServerURL = "https://v2.example.test"
	second.SkipSkills = false
	second.SkillBundle = badBundle
	second.PrivilegeMode = "elevated"
	second.AgentDockHome = filepath.Join(root, "home")
	result, err := (Engine{}).Run(context.Background(), second)
	if err == nil {
		t.Fatal("expected v2 skill bootstrap to fail")
	}
	if result.State != updateengine.StateRolledBack {
		t.Fatalf("state=%s", result.State)
	}
	if result.PublicURL == "https://v2.example.test" || result.LocalMCPURL == "http://127.0.0.1:9876/mcp" || result.PrivilegeMode == "elevated" {
		t.Fatalf("rolled_back result kept failed trial urls: public=%q mcp=%q privilege=%q", result.PublicURL, result.LocalMCPURL, result.PrivilegeMode)
	}
	_, _, current := readInstallStore(t, runtimeRoot)
	if current.PublicURL == "https://v2.example.test" || current.LocalMCPURL == "http://127.0.0.1:9876/mcp" || current.PrivilegeMode == "elevated" {
		t.Fatalf("result.json kept failed trial urls: public=%q mcp=%q privilege=%q", current.PublicURL, current.LocalMCPURL, current.PrivilegeMode)
	}
	if current.PublicURL != "https://v1.example.test" {
		t.Fatalf("restored public_url=%q, want v1 origin", current.PublicURL)
	}
	if current.LocalMCPURL != "http://127.0.0.1:8765/mcp" {
		t.Fatalf("restored local_mcp_url=%q, want v1 listen address", current.LocalMCPURL)
	}
}

func TestProjectRestoredResultClearsFailedTrialProjection(t *testing.T) {
	got := projectRestoredResult(Result{
		PublicURL:     "https://v2.example.test",
		LocalMCPURL:   "http://127.0.0.1:9876/mcp",
		PrivilegeMode: "elevated",
		Healthy:       true,
		ActiveVersion: "v2.0.0",
	}, Transaction{SourceVersion: "v1.0.0"}, Request{}, false)
	if got.PublicURL != "" || got.LocalMCPURL != "" || got.PrivilegeMode != "" || got.Healthy {
		t.Fatalf("failed trial projection leaked: public=%q mcp=%q privilege=%q healthy=%v", got.PublicURL, got.LocalMCPURL, got.PrivilegeMode, got.Healthy)
	}
	if got.ActiveVersion != "v1.0.0" {
		t.Fatalf("active_version=%s, want source v1.0.0", got.ActiveVersion)
	}
}

func TestCorruptRollbackJournalAfterCommitDoesNotBlockNextInstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix payload install is exercised on Unix CI")
	}
	root := t.TempDir()
	installRoot := filepath.Join(root, "opt")
	runtimeRoot := filepath.Join(root, "etc")
	v1 := writeUnixPayload(t, root, "p1", "version-one")
	first, err := (Engine{}).Run(context.Background(), unixInstallRequest(installRoot, runtimeRoot, v1, "v1.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	if first.State != updateengine.StateCommitted {
		t.Fatalf("v1 state=%s", first.State)
	}
	journalDir := filepath.Join(runtimeRoot, "install", "rollback", first.TransactionID)
	if err := os.MkdirAll(journalDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(journalDir, "journal.json"), []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}

	v2 := writeUnixPayload(t, root, "p2", "version-two")
	second, err := (Engine{}).Run(context.Background(), unixInstallRequest(installRoot, runtimeRoot, v2, "v2.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	if second.State != updateengine.StateCommitted {
		t.Fatalf("corrupt leftover journal must not block next install, state=%s err=%v", second.State, err)
	}
}

func TestWindowsCrossVersionInstallerOwnsTrialWithFallback(t *testing.T) {
	root := t.TempDir()
	installRoot := filepath.Join(root, "runtime")
	makePayload := func(name, body string) string {
		payload := filepath.Join(root, name)
		if err := os.MkdirAll(payload, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, file := range []string{"agentdock.exe", "agentdock-tray.exe", "agentdock-arbiter.exe"} {
			if err := os.WriteFile(filepath.Join(payload, file), []byte(body+"-"+file), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		return payload
	}
	v1 := Request{InstallRoot: installRoot, RuntimeRoot: installRoot, PayloadDir: makePayload("p1", "v1"), Version: "v1.0.0"}
	if _, err := stageWindowsPayload(v1, newJournal(installRoot, "tx-v1")); err != nil {
		t.Fatal(err)
	}
	if err := commitWindowsActivePointer(installRoot, "tx-v1"); err != nil {
		t.Fatal(err)
	}

	v2 := v1
	v2.PayloadDir = makePayload("p2", "v2")
	v2.Version = "v2.0.0"
	staged, err := stageWindowsPayload(v2, newJournal(installRoot, "tx-v2"))
	if err != nil {
		t.Fatal(err)
	}
	layout, _ := updateengine.NewWindowsLayout(installRoot)
	if staged.Binary != layout.GenerationCore("v2.0.0") {
		t.Fatalf("cross-version stage attached old generation: %s", staged.Binary)
	}
	store, _ := updateengine.NewStore(installRoot)
	active, err := store.ReadActive()
	if err != nil {
		t.Fatal(err)
	}
	if active.State != updateengine.StateTrial || active.ActiveVersion != "v2.0.0" || active.FallbackVersion != "v1.0.0" || active.TransactionID != "tx-v2" {
		t.Fatalf("cross-version trial=%+v", active)
	}
	if err := commitWindowsActivePointer(installRoot, "tx-v2"); err != nil {
		t.Fatal(err)
	}
	if err := releaseWindowsTrialPointer(installRoot, "tx-v2"); err != nil {
		t.Fatal(err)
	}
	active, err = store.ReadActive()
	if err != nil {
		t.Fatal(err)
	}
	if active.State != updateengine.StateCommitted || active.ActiveVersion != "v1.0.0" || active.TransactionID != "" {
		t.Fatalf("outer rollback did not restore fallback: %+v", active)
	}
}

func TestSnapshotWindowsRuntimeStateRecordsWasActive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix harness uses an executable shell fixture; Windows behavior is covered by native E2E")
	}
	root := t.TempDir()
	payload := filepath.Join(root, "payload")
	if err := os.MkdirAll(payload, 0o755); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(payload, "agentdock.exe")
	script := `#!/bin/sh
if [ "$1" = service ] && [ "$2" = status ]; then echo '{"running":true,"healthy":true,"startup_enabled":true}'; exit 0; fi
if [ "$1" = tunnel ] && [ "$2" = status ]; then echo '{"mode":"quick","running":true,"ready":true,"startup_enabled":true}'; exit 0; fi
exit 2
`
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	runtimeRoot := filepath.Join(root, "runtime")
	if err := os.MkdirAll(runtimeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtimeRoot, "runtime.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal := newJournal(runtimeRoot, "snapshot-running")
	request := Request{InstallRoot: runtimeRoot, RuntimeRoot: runtimeRoot, PayloadDir: payload, StartService: true}
	if err := snapshotWindowsRuntimeState(request, journal); err != nil {
		t.Fatal(err)
	}
	if len(journal.Services) != 2 || !journal.Services[0].WasActive || !journal.Services[1].WasActive {
		t.Fatalf("runtime snapshot=%+v", journal.Services)
	}
}

package installer

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/uvwt/agentdock/internal/updateengine"
)

func TestUninstallSystemctlDisableFailureIsNotCommitted(t *testing.T) {
	if runtime.GOOS == "windows" {
		withStubCommands(t, map[string]string{"systemctl": "if /I \"%1\"==\"disable\" if /I \"%2\"==\"--now\" (echo Failed to disable unit: exit 23& exit /b 23)\r\nexit /b 0"})
	} else {
		withStubCommands(t, map[string]string{"systemctl": "if [ \"$1\" = disable ] && [ \"$2\" = --now ]; then echo 'Failed to disable unit: exit 23'; exit 23; fi; exit 0"})
	}
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
	unit := filepath.Join(systemdDir, "agentdock.service")
	if err := os.WriteFile(unit, []byte("unit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Engine{}.Run(context.Background(), Request{
		Action:          ActionUninstall,
		InstallRoot:     installRoot,
		RuntimeRoot:     runtimeRoot,
		ServiceName:     "agentdock",
		ServiceManager:  "systemd",
		SystemdDir:      systemdDir,
		LaunchAgentsDir: filepath.Join(root, "agents"),
	})
	if err == nil {
		t.Fatal("systemctl disable --now exit 23 must fail uninstall")
	}
	if result.State == updateengine.StateCommitted {
		t.Fatalf("state=%s, disable failure must not commit", result.State)
	}
	if result.Failure == nil || result.Failure.Code != FailureUninstallFailed {
		t.Fatalf("failure=%v", result.Failure)
	}
	if !fileExists(unit) {
		t.Fatal("unit must remain after stop/disable failure; uninstall must not keep deleting")
	}
}

func TestUninstallAbsentSystemdUnitIsCommitted(t *testing.T) {
	if runtime.GOOS == "windows" {
		withStubCommands(t, map[string]string{"systemctl": "echo Unit file agentdock.service does not exist.& exit /b 1"})
	} else {
		withStubCommands(t, map[string]string{"systemctl": "echo 'Failed to disable unit: Unit file agentdock.service does not exist.'; exit 1"})
	}
	root := t.TempDir()
	result, err := Engine{}.Run(context.Background(), Request{
		Action:          ActionUninstall,
		InstallRoot:     filepath.Join(root, "opt"),
		RuntimeRoot:     filepath.Join(root, "etc"),
		ServiceName:     "agentdock",
		ServiceManager:  "systemd",
		SystemdDir:      filepath.Join(root, "systemd"),
		LaunchAgentsDir: filepath.Join(root, "agents"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != updateengine.StateCommitted {
		t.Fatalf("absent unit must be idempotent, state=%s failure=%v", result.State, result.Failure)
	}
}

func TestUninstallPurgeConfigFailureIsNotCommitted(t *testing.T) {
	withStubCommands(t, map[string]string{"systemctl": "exit 0"})
	root := t.TempDir()
	installRoot := filepath.Join(root, "opt")
	runtimeRoot := filepath.Join(root, "etc")
	if err := os.MkdirAll(installRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(runtimeRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	envDir := filepath.Join(runtimeRoot, "agentdock.env")
	if err := os.Mkdir(envDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(envDir, "nested"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := Engine{}.Run(context.Background(), Request{
		Action:          ActionUninstall,
		InstallRoot:     installRoot,
		RuntimeRoot:     runtimeRoot,
		ServiceManager:  "none",
		LaunchAgentsDir: filepath.Join(root, "agents"),
		PurgeConfig:     true,
	})
	if err == nil {
		t.Fatal("purge-config must fail when a config path cannot be removed")
	}
	if result.State == updateengine.StateCommitted {
		t.Fatalf("state=%s, purge-config failure must not commit", result.State)
	}
	if !dirExists(envDir) {
		t.Fatal("failed purge must leave the remaining config in place")
	}
}

func TestUninstallPurgeDataFailureIsNotCommitted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix directory permissions are used to inject RemoveAll failure")
	}
	withStubCommands(t, map[string]string{"systemctl": "exit 0"})
	root := t.TempDir()
	installRoot := filepath.Join(root, "opt")
	hold := filepath.Join(root, "hold")
	runtimeRoot := filepath.Join(hold, "etc")
	if err := os.MkdirAll(installRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(runtimeRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtimeRoot, "agentdock.env"), []byte("AGENTDOCK_HOST=127.0.0.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(hold, 0o555); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(hold, 0o755) }()

	result, err := Engine{}.Run(context.Background(), Request{
		Action:          ActionUninstall,
		InstallRoot:     installRoot,
		RuntimeRoot:     runtimeRoot,
		ServiceManager:  "none",
		LaunchAgentsDir: filepath.Join(root, "agents"),
		PurgeData:       true,
	})
	if err == nil {
		t.Fatal("purge-data RemoveAll failure must fail uninstall")
	}
	if result.State == updateengine.StateCommitted {
		t.Fatalf("state=%s, purge-data failure must not commit", result.State)
	}
	if !dirExists(runtimeRoot) {
		t.Fatal("failed purge-data must leave the runtime directory")
	}
}

func TestUninstallDoesNotPolluteNextSourceVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix payload install is exercised on Unix CI")
	}
	root := t.TempDir()
	installRoot := filepath.Join(root, "opt")
	runtimeRoot := filepath.Join(root, "etc")
	agents := filepath.Join(root, "agents")
	v1 := writeUnixPayload(t, root, "p1", "version-one")
	if _, err := (Engine{}).Run(context.Background(), unixInstallRequest(installRoot, runtimeRoot, v1, "v1.0.0")); err != nil {
		t.Fatal(err)
	}

	uninstalled, err := (Engine{}).Run(context.Background(), Request{
		Action:          ActionUninstall,
		InstallRoot:     installRoot,
		RuntimeRoot:     runtimeRoot,
		ServiceManager:  "none",
		LaunchAgentsDir: agents,
	})
	if err != nil {
		t.Fatal(err)
	}
	if uninstalled.State != updateengine.StateCommitted {
		t.Fatalf("uninstall state=%s", uninstalled.State)
	}

	v2 := writeUnixPayload(t, root, "p2", "version-two")
	second, err := (Engine{}).Run(context.Background(), unixInstallRequest(installRoot, runtimeRoot, v2, "v2.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	if second.State != updateengine.StateCommitted {
		t.Fatalf("reinstall state=%s", second.State)
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
		t.Fatalf("source_version=%s after uninstall, want v1.0.0 not unknown", tx.SourceVersion)
	}
}

func TestIdempotentAbsenceClassification(t *testing.T) {
	tests := []struct {
		name   string
		bin    string
		args   []string
		output string
		want   bool
	}{
		{name: "systemd missing unit", bin: "systemctl", output: "Failed to disable unit: Unit file agentdock.service does not exist.", want: true},
		{name: "systemd not loaded", bin: "systemctl", output: "Failed to stop agentdock.service: Unit agentdock.service not loaded.", want: true},
		{name: "systemd exit 23", bin: "systemctl", output: "Failed to disable unit: exit 23", want: false},
		{name: "systemd dependency", bin: "systemctl", output: "Failed to disable unit: dependency not found", want: false},
		{name: "openrc missing", bin: "rc-service", output: "* service agentdock does not exist", want: true},
		{name: "launchctl missing", bin: "launchctl", output: "Could not find specified service", want: true},
		{name: "launchctl not privileged", bin: "launchctl", output: "Not privileged to print service.", want: false},
		{name: "launchctl operation not permitted", bin: "launchctl", output: "Operation not permitted", want: false},
		{name: "schtasks missing", bin: "schtasks.exe", args: []string{"/Change", "/TN", "AgentDock", "/DISABLE"}, output: "ERROR: The specified task name was not found.", want: true},
		{name: "schtasks not running", bin: "schtasks", args: []string{"/End", "/TN", "AgentDock"}, output: "ERROR: The task is not currently running.", want: true},
		{name: "schtasks disable not running is not absence", bin: "schtasks", args: []string{"/Change", "/TN", "AgentDock", "/DISABLE"}, output: "ERROR: The task is not currently running.", want: false},
		{name: "access denied", bin: "systemctl", output: "Failed to disable unit: Access denied", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := isIdempotentAbsence(test.bin, test.args, []byte(test.output), errExit(1))
			if got != test.want {
				t.Fatalf("got %v want %v", got, test.want)
			}
		})
	}
}

func TestUninstallDarwinPrintFaultDoesNotDeletePlist(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("launchctl classification is exercised on Unix")
	}
	withStubCommands(t, map[string]string{
		"launchctl": "echo 'Not privileged to print service.'; exit 1",
	})
	root := t.TempDir()
	agents := filepath.Join(root, "agents")
	if err := os.MkdirAll(agents, 0o755); err != nil {
		t.Fatal(err)
	}
	plist := filepath.Join(agents, darwinCLICoreLabel+".plist")
	if err := os.WriteFile(plist, []byte("plist\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := uninstallDarwin(context.Background(), Request{LaunchAgentsDir: agents})
	if err == nil {
		t.Fatal("launchctl print permission failure must fail uninstall")
	}
	if !fileExists(plist) {
		t.Fatal("launchctl fault must not delete plist as if the service were absent")
	}
}

func TestUninstallDarwinAbsentServiceDeletesPlist(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("launchctl classification is exercised on Unix")
	}
	withStubCommands(t, map[string]string{
		"launchctl": "echo 'Could not find specified service'; exit 1",
	})
	root := t.TempDir()
	agents := filepath.Join(root, "agents")
	if err := os.MkdirAll(agents, 0o755); err != nil {
		t.Fatal(err)
	}
	plist := filepath.Join(agents, darwinCLICoreLabel+".plist")
	if err := os.WriteFile(plist, []byte("plist\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := uninstallDarwin(context.Background(), Request{LaunchAgentsDir: agents}); err != nil {
		t.Fatal(err)
	}
	if fileExists(plist) {
		t.Fatal("classified absence should delete leftover plist")
	}
}

func TestUninstallDeferCommitIsNotProductUninstalled(t *testing.T) {
	withStubCommands(t, map[string]string{"systemctl": "exit 0"})
	root := t.TempDir()
	installRoot := filepath.Join(root, "opt")
	runtimeRoot := filepath.Join(root, "etc")
	if err := os.MkdirAll(installRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(runtimeRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	result, err := (Engine{}).Run(context.Background(), Request{
		Action:          ActionUninstall,
		InstallRoot:     installRoot,
		RuntimeRoot:     runtimeRoot,
		ServiceManager:  "none",
		LaunchAgentsDir: filepath.Join(root, "agents"),
		DeferCommit:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.State == updateengine.StateCommitted {
		t.Fatal("deferred uninstall must not look like the whole product is already gone")
	}
	if result.State != updateengine.StateTrial {
		t.Fatalf("deferred uninstall state=%s, want trial", result.State)
	}
	_, tx, current := readInstallStore(t, runtimeRoot)
	if tx.State == updateengine.StateCommitted || current.State == updateengine.StateCommitted {
		t.Fatal("disk uninstall state must stay trial until adapter commit")
	}

	committed, err := (Engine{}).Run(context.Background(), Request{
		Action:        ActionCommit,
		InstallRoot:   installRoot,
		RuntimeRoot:   runtimeRoot,
		TransactionID: result.TransactionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if committed.State != updateengine.StateCommitted || committed.Action != ActionUninstall {
		t.Fatalf("adapter commit state=%s action=%s", committed.State, committed.Action)
	}
}

type stubExitError struct{ code int }

func (err stubExitError) Error() string { return "exit status 1" }

func errExit(code int) error { return stubExitError{code: code} }

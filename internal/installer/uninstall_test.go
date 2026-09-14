package installer

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/desktopruntime"
	"github.com/uvwt/agentdock/internal/updateengine"
)

func TestWindowsManagedTaskNameRequiresOwnedElevatedManifest(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "runtime.json")
	manifest := desktopruntime.Manifest{
		SchemaVersion:     1,
		AgentDockBinary:   filepath.Join(root, "agentdock.exe"),
		PrivilegeMode:     "standard",
		AgentDockTaskName: "AgentDock",
		Host:              "127.0.0.1",
		Port:              8765,
		LocalMCPURL:       "http://127.0.0.1:8765/mcp",
		TunnelMode:        "none",
	}
	if err := desktopruntime.Save(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}
	request := Request{RuntimeRoot: root}
	if got := windowsManagedTaskName(request); got != "" {
		t.Fatalf("standard install unexpectedly owns scheduled task %q", got)
	}

	manifest.PrivilegeMode = "elevated"
	manifest.AgentDockTaskName = "AgentDock-E2E"
	if err := desktopruntime.Save(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}
	if got := windowsManagedTaskName(request); got != "AgentDock-E2E" {
		t.Fatalf("elevated manifest task=%q, want AgentDock-E2E", got)
	}

	request.TaskName = "Explicit-Task"
	if got := windowsManagedTaskName(request); got != "Explicit-Task" {
		t.Fatalf("explicit task=%q, want Explicit-Task", got)
	}
}

func TestUninstallWindowsWithoutOwnedTaskDoesNotTouchTaskScheduler(t *testing.T) {
	stub := "exit 23"
	if runtime.GOOS == "windows" {
		stub = "exit /b 23"
	}
	withStubCommands(t, map[string]string{"schtasks": stub})
	root := t.TempDir()
	manifest := desktopruntime.Manifest{
		SchemaVersion:     1,
		AgentDockBinary:   filepath.Join(root, "agentdock.exe"),
		PrivilegeMode:     "standard",
		AgentDockTaskName: "AgentDock",
		Host:              "127.0.0.1",
		Port:              8765,
		LocalMCPURL:       "http://127.0.0.1:8765/mcp",
		TunnelMode:        "none",
	}
	if err := desktopruntime.Save(filepath.Join(root, "runtime.json"), manifest); err != nil {
		t.Fatal(err)
	}
	if err := uninstallWindows(context.Background(), Request{InstallRoot: root, RuntimeRoot: root}); err != nil {
		t.Fatalf("standard uninstall touched Task Scheduler: %v", err)
	}
}

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

// destructive cleanup 失败重跑时 stable binary 可能已被清理：uninstall 必须重绑定
// 同一个 trial 事务，让 txid-scoped detached helper 的 commit 仍然有效。
func TestUninstallRebindsPendingTrialTransaction(t *testing.T) {
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
	uninstallRequest := Request{
		Action:          ActionUninstall,
		InstallRoot:     installRoot,
		RuntimeRoot:     runtimeRoot,
		ServiceManager:  "none",
		LaunchAgentsDir: filepath.Join(root, "agents"),
		DeferCommit:     true,
	}
	first, err := (Engine{}).Run(context.Background(), uninstallRequest)
	if err != nil {
		t.Fatal(err)
	}
	second, err := (Engine{}).Run(context.Background(), uninstallRequest)
	if err != nil {
		t.Fatal(err)
	}
	if second.TransactionID != first.TransactionID {
		t.Fatalf("pending uninstall trial was replaced: %s -> %s", first.TransactionID, second.TransactionID)
	}
	committed, err := (Engine{}).Run(context.Background(), Request{
		Action:        ActionCommit,
		InstallRoot:   installRoot,
		RuntimeRoot:   runtimeRoot,
		TransactionID: first.TransactionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if committed.State != updateengine.StateCommitted {
		t.Fatalf("rebound trial commit state=%s", committed.State)
	}
}

// 计划任务名必须由 engine Result 单一来源返回，adapter 不得再自行解析 runtime.json 判定。
func TestUninstallResultCarriesManagedTaskName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows manifest resolution is exercised on Windows CI")
	}
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
	manifest := desktopruntime.Manifest{
		SchemaVersion:     1,
		InstallRoot:       installRoot,
		AgentDockBinary:   filepath.Join(installRoot, "agentdock.exe"),
		PrivilegeMode:     "elevated",
		AgentDockTaskName: "AgentDock",
		Host:              "127.0.0.1",
		Port:              8765,
		LocalMCPURL:       "http://127.0.0.1:8765/mcp",
		TunnelMode:        "none",
	}
	if err := desktopruntime.Save(filepath.Join(runtimeRoot, "runtime.json"), manifest); err != nil {
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
	if result.TaskName != "AgentDock" {
		t.Fatalf("uninstall result task_name=%q, want AgentDock", result.TaskName)
	}
}

// uninstall trial 重入必须冻结事务意图：purge 标志漂移必须拒绝，
// 不能在旧事务上执行比首次承诺更强或更弱的清理。
func TestUninstallRebindRejectsPurgeIntentDrift(t *testing.T) {
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
	uninstallRequest := Request{
		Action:          ActionUninstall,
		InstallRoot:     installRoot,
		RuntimeRoot:     runtimeRoot,
		ServiceManager:  "none",
		LaunchAgentsDir: filepath.Join(root, "agents"),
		DeferCommit:     true,
	}
	first, err := (Engine{}).Run(context.Background(), uninstallRequest)
	if err != nil {
		t.Fatal(err)
	}

	// purge-data 升级：同一 runtime root 下第二次请求要求更强清理，必须拒绝。
	drifted := uninstallRequest
	drifted.PurgeData = true
	if _, err := (Engine{}).Run(context.Background(), drifted); err == nil {
		t.Fatal("purge-data drift must be rejected on a pending uninstall trial")
	} else if !strings.Contains(err.Error(), "清理意图不匹配") {
		t.Fatalf("drift error should name the intent mismatch: %v", err)
	}

	store, err := NewStore(runtimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := store.ReadTransaction()
	if err != nil {
		t.Fatal(err)
	}
	if tx.State != updateengine.StateTrial || tx.TransactionID != first.TransactionID {
		t.Fatalf("rejected drift must leave the original trial untouched: state=%s tx=%s", tx.State, tx.TransactionID)
	}

	// 参数一致的 retry 仍然重入并提交。
	second, err := (Engine{}).Run(context.Background(), uninstallRequest)
	if err != nil {
		t.Fatal(err)
	}
	if second.TransactionID != first.TransactionID {
		t.Fatalf("matching retry must rebind: %s -> %s", first.TransactionID, second.TransactionID)
	}
}

// install-root 漂移同样是意图漂移：不允许在同一 runtime root 下换一个目录执行清理。
func TestUninstallRebindRejectsInstallRootDrift(t *testing.T) {
	withStubCommands(t, map[string]string{"systemctl": "exit 0"})
	root := t.TempDir()
	runtimeRoot := filepath.Join(root, "etc")
	installRootA := filepath.Join(root, "opt-a")
	installRootB := filepath.Join(root, "opt-b")
	if err := os.MkdirAll(runtimeRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	base := Request{
		Action:          ActionUninstall,
		RuntimeRoot:     runtimeRoot,
		ServiceManager:  "none",
		LaunchAgentsDir: filepath.Join(root, "agents"),
		DeferCommit:     true,
	}
	firstRequest := base
	firstRequest.InstallRoot = installRootA
	if _, err := (Engine{}).Run(context.Background(), firstRequest); err != nil {
		t.Fatal(err)
	}

	drifted := base
	drifted.InstallRoot = installRootB
	if _, err := (Engine{}).Run(context.Background(), drifted); err == nil {
		t.Fatal("install-root drift must be rejected on a pending uninstall trial")
	} else if !strings.Contains(err.Error(), "install-root 意图不匹配") {
		t.Fatalf("drift error should name install-root mismatch: %v", err)
	}

	store, err := NewStore(runtimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := store.ReadTransaction()
	if err != nil {
		t.Fatal(err)
	}
	if tx.State != updateengine.StateTrial || tx.InstallRoot != installRootA {
		t.Fatalf("rejected drift must keep the original trial: state=%s install_root=%s", tx.State, tx.InstallRoot)
	}
}

type stubExitError struct{ code int }

func (err stubExitError) Error() string { return "exit status 1" }

func errExit(code int) error { return stubExitError{code: code} }

// purge-data 的用户目录只能来自 Request；宿主进程里的 AGENTDOCK_HOME 不能成为隐式删除目标。
func TestPurgeInstallDataIgnoresAmbientAgentDockHome(t *testing.T) {
	root := t.TempDir()
	installRoot := filepath.Join(root, "install")
	runtimeRoot := filepath.Join(root, "runtime")
	agentDockHome := filepath.Join(root, "isolated", ".agentdock")
	defaultDir := filepath.Join(root, "isolated", "AgentDock")
	hostHome := filepath.Join(root, "production", ".agentdock")
	for _, dir := range []string{installRoot, runtimeRoot, agentDockHome, defaultDir, hostHome} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	marker := filepath.Join(hostHome, "must-survive")
	if err := os.WriteFile(marker, []byte("production"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTDOCK_HOME", hostHome)

	request, err := normalizeRequest(Request{
		Action:              ActionUninstall,
		InstallRoot:         installRoot,
		RuntimeRoot:         runtimeRoot,
		PurgeData:           true,
		AgentDockHome:       agentDockHome,
		AgentDockDefaultDir: defaultDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := purgeInstallData(request); err != nil {
		t.Fatal(err)
	}
	for _, removed := range []string{installRoot, runtimeRoot, agentDockHome, defaultDir} {
		if _, err := os.Stat(removed); !os.IsNotExist(err) {
			t.Fatalf("purge-data should remove explicit target %s, stat err=%v", removed, err)
		}
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "production" {
		t.Fatalf("ambient AGENTDOCK_HOME was touched: data=%q err=%v", data, err)
	}
}

func TestNormalizeRequestRejectsDangerousPurgeDataTarget(t *testing.T) {
	root := t.TempDir()
	filesystemRoot := string(filepath.Separator)
	if runtime.GOOS == "windows" {
		filesystemRoot = filepath.VolumeName(root) + string(filepath.Separator)
	}
	for _, test := range []struct {
		name        string
		installRoot string
		runtimeRoot string
	}{
		{name: "install root", installRoot: filesystemRoot, runtimeRoot: filepath.Join(root, "runtime")},
		{name: "runtime root", installRoot: filepath.Join(root, "install"), runtimeRoot: filesystemRoot},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizeRequest(Request{
				Action:      ActionUninstall,
				InstallRoot: test.installRoot,
				RuntimeRoot: test.runtimeRoot,
				PurgeData:   true,
			})
			if err == nil || !strings.Contains(err.Error(), "根目录") {
				t.Fatalf("dangerous managed purge root must be rejected, got %v", err)
			}
		})
	}

	_, err := normalizeRequest(Request{
		Action:        ActionUninstall,
		InstallRoot:   filepath.Join(root, "install"),
		RuntimeRoot:   filepath.Join(root, "runtime"),
		PurgeData:     true,
		AgentDockHome: filesystemRoot,
	})
	if err == nil || !strings.Contains(err.Error(), "根目录") {
		t.Fatalf("filesystem root purge target must be rejected, got %v", err)
	}

	accountHome := "/Users/example"
	if runtime.GOOS == "linux" {
		accountHome = "/home/example"
	} else if runtime.GOOS == "windows" {
		accountHome = `C:\Users\example`
	}
	_, err = normalizeRequest(Request{
		Action:        ActionUninstall,
		InstallRoot:   filepath.Join(root, "install"),
		RuntimeRoot:   filepath.Join(root, "runtime"),
		PurgeData:     true,
		AgentDockHome: accountHome,
	})
	if err == nil || !strings.Contains(err.Error(), "危险路径") {
		t.Fatalf("whole account home purge target must be rejected, got %v", err)
	}

	_, err = normalizeRequest(Request{
		Action:              ActionUninstall,
		InstallRoot:         filepath.Join(root, "install"),
		RuntimeRoot:         filepath.Join(root, "runtime"),
		PurgeData:           true,
		AgentDockDefaultDir: root,
	})
	if err == nil || !strings.Contains(err.Error(), "installer root") {
		t.Fatalf("purge target containing installer roots must be rejected, got %v", err)
	}
}

func TestUninstallIntentFreezesPurgeDataPaths(t *testing.T) {
	root := t.TempDir()
	transaction := Transaction{
		TransactionID:       "0123456789abcdef0123456789abcdef",
		InstallRoot:         filepath.Join(root, "install"),
		RuntimeRoot:         filepath.Join(root, "runtime"),
		PurgeConfig:         true,
		PurgeData:           true,
		AgentDockHome:       filepath.Join(root, "state"),
		AgentDockDefaultDir: filepath.Join(root, "workspace"),
	}
	request := Request{
		InstallRoot:         transaction.InstallRoot,
		RuntimeRoot:         transaction.RuntimeRoot,
		PurgeConfig:         true,
		PurgeData:           true,
		AgentDockHome:       transaction.AgentDockHome,
		AgentDockDefaultDir: transaction.AgentDockDefaultDir,
	}
	if err := ensureUninstallIntentMatches(transaction, request); err != nil {
		t.Fatalf("matching cleanup intent rejected: %v", err)
	}

	drifted := request
	drifted.AgentDockHome = filepath.Join(root, "other-state")
	if err := ensureUninstallIntentMatches(transaction, drifted); err == nil || !strings.Contains(err.Error(), "agentdock-home") {
		t.Fatalf("agentdock-home drift must be rejected, got %v", err)
	}

	drifted = request
	drifted.AgentDockDefaultDir = filepath.Join(root, "other-workspace")
	if err := ensureUninstallIntentMatches(transaction, drifted); err == nil || !strings.Contains(err.Error(), "agentdock-default-dir") {
		t.Fatalf("agentdock-default-dir drift must be rejected, got %v", err)
	}
}

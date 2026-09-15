//go:build windows

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/fs/processlock"
	"github.com/uvwt/agentdock/internal/updateengine"
)

func TestTrayRequiresWaitOnlyDetachesNormalBackgroundLaunches(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "no arguments", want: false},
		{name: "background", args: []string{"--background"}, want: false},
		{name: "background case insensitive", args: []string{" --BACKGROUND "}, want: false},
		{name: "task admin", args: []string{"--task-admin", "prepare-elevated"}, want: true},
		{name: "version", args: []string{"--version"}, want: true},
		{name: "help", args: []string{"--help"}, want: true},
		{name: "background plus management argument", args: []string{"--background", "--start-core"}, want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := trayRequiresWait(test.args); got != test.want {
				t.Fatalf("trayRequiresWait(%q) = %v, want %v", test.args, got, test.want)
			}
		})
	}
}

func TestCoreLaunchRequiresParentLifetimeOnlyForServiceHost(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "service host", args: []string{"service", "launch-core", "--runtime-root", `C:\AgentDock`}, want: true},
		{name: "service host case insensitive", args: []string{" SERVICE ", " LAUNCH-CORE "}, want: true},
		{name: "service status", args: []string{"service", "status"}},
		{name: "version", args: []string{"version", "--json"}},
		{name: "empty"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := coreLaunchRequiresParentLifetime(test.args); got != test.want {
				t.Fatalf("coreLaunchRequiresParentLifetime(%q) = %v, want %v", test.args, got, test.want)
			}
		})
	}
}

func TestSetupRuntimeHostPreservesExitCodeAndDiagnostics(t *testing.T) {
	comspec := os.Getenv("COMSPEC")
	if strings.TrimSpace(comspec) == "" {
		comspec = `C:\Windows\System32\cmd.exe`
	}
	stdoutPath := filepath.Join(t.TempDir(), "stdout.log")
	stderrPath := filepath.Join(t.TempDir(), "stderr.log")
	errorPath := filepath.Join(t.TempDir(), "launcher-error.log")
	encode := func(value string) string {
		return base64.StdEncoding.EncodeToString([]byte(value))
	}

	exitCode, err := runSetupRuntimeHost([]string{
		"--file-b64", encode(comspec),
		"--args-b64", encode(`/d /s /c "echo runtime-host-stdout & echo runtime-host-stderr 1>&2 & exit 7"`),
		"--wait",
		"--stdout-b64", encode(stdoutPath),
		"--stderr-b64", encode(stderrPath),
		"--error-b64", encode(errorPath),
	})
	if err != nil {
		t.Fatalf("runSetupRuntimeHost() error = %v", err)
	}
	if exitCode != 7 {
		t.Fatalf("runSetupRuntimeHost() exit code = %d, want 7", exitCode)
	}
	stdout, err := os.ReadFile(stdoutPath)
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := os.ReadFile(stderrPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stdout), "runtime-host-stdout") {
		t.Fatalf("stdout = %q", stdout)
	}
	if !strings.Contains(string(stderr), "runtime-host-stderr") {
		t.Fatalf("stderr = %q", stderr)
	}
	if _, err := os.Stat(errorPath); !os.IsNotExist(err) {
		t.Fatalf("launcher error file exists after a normal child exit: %v", err)
	}
}

func TestReplaceWindowsEnvironmentIsCaseInsensitive(t *testing.T) {
	environment := replaceWindowsEnvironment(
		[]string{"Path=C:\\Windows", "agentdock_home=old", "OTHER=value"},
		"AGENTDOCK_HOME",
		`C:\Users\Test\.agentdock`,
	)
	joined := strings.Join(environment, "\n")
	if strings.Contains(strings.ToLower(joined), "agentdock_home=old") {
		t.Fatalf("old environment value survived: %q", environment)
	}
	if !strings.Contains(joined, `AGENTDOCK_HOME=C:\Users\Test\.agentdock`) {
		t.Fatalf("replacement environment value missing: %q", environment)
	}
}

func TestInstallerOwnsActiveTrialOnlyWhileMatchingTransactionIsLive(t *testing.T) {
	root := t.TempDir()
	active := updateengine.ActiveVersion{
		SchemaVersion:   updateengine.SchemaVersion,
		ActiveVersion:   "v0.8.2",
		FallbackVersion: "v0.8.3",
		State:           updateengine.StateTrial,
		TransactionID:   "installer-trial",
	}
	transaction := installerTrialTransaction{
		TransactionID: "installer-trial",
		Platform:      "windows",
		Action:        "install",
		SourceVersion: "v0.8.3",
		TargetVersion: "v0.8.2",
		State:         updateengine.StateTrial,
		InstallRoot:   root,
	}
	writeInstallerTrialTransaction(t, root, transaction)

	lock, err := processlock.Acquire(context.Background(), filepath.Join(root, "install", "transaction.lock"))
	if err != nil {
		t.Fatal(err)
	}
	owned, err := installerOwnsActiveTrial(root, active)
	if err != nil {
		t.Fatal(err)
	}
	if !owned {
		t.Fatal("live matching Installer transaction should own the trial generation")
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}

	owned, err = installerOwnsActiveTrial(root, active)
	if err != nil {
		t.Fatal(err)
	}
	if owned {
		t.Fatal("stale Installer transaction without the live lock must not own the trial generation")
	}
}

func TestInstallerOwnsActiveTrialRejectsMismatchedAuthority(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*installerTrialTransaction)
	}{
		{name: "transaction", mutate: func(tx *installerTrialTransaction) { tx.TransactionID = "other" }},
		{name: "platform", mutate: func(tx *installerTrialTransaction) { tx.Platform = "linux" }},
		{name: "action", mutate: func(tx *installerTrialTransaction) { tx.Action = "uninstall" }},
		{name: "state", mutate: func(tx *installerTrialTransaction) { tx.State = updateengine.StateCommitted }},
		{name: "target", mutate: func(tx *installerTrialTransaction) { tx.TargetVersion = "v0.8.4" }},
		{name: "source", mutate: func(tx *installerTrialTransaction) { tx.SourceVersion = "v0.8.1" }},
		{name: "root", mutate: func(tx *installerTrialTransaction) { tx.InstallRoot = filepath.Join(tx.InstallRoot, "other") }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			active := updateengine.ActiveVersion{
				SchemaVersion:   updateengine.SchemaVersion,
				ActiveVersion:   "v0.8.2",
				FallbackVersion: "v0.8.3",
				State:           updateengine.StateTrial,
				TransactionID:   "installer-trial",
			}
			transaction := installerTrialTransaction{
				TransactionID: "installer-trial",
				Platform:      "windows",
				Action:        "install",
				SourceVersion: "v0.8.3",
				TargetVersion: "v0.8.2",
				State:         updateengine.StateTrial,
				InstallRoot:   root,
			}
			test.mutate(&transaction)
			writeInstallerTrialTransaction(t, root, transaction)
			lock, err := processlock.Acquire(context.Background(), filepath.Join(root, "install", "transaction.lock"))
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Release()

			owned, err := installerOwnsActiveTrial(root, active)
			if err != nil {
				t.Fatal(err)
			}
			if owned {
				t.Fatal("mismatched Installer transaction must not authorize the trial generation")
			}
		})
	}
}

func TestResolveActiveAllowsLiveInstallerTrialWithoutUpdateTransaction(t *testing.T) {
	root := t.TempDir()
	store, err := updateengine.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	active := updateengine.ActiveVersion{
		SchemaVersion:   updateengine.SchemaVersion,
		ActiveVersion:   "v0.8.2",
		FallbackVersion: "v0.8.3",
		State:           updateengine.StateTrial,
		TransactionID:   "installer-trial",
	}
	if err := store.WriteActive(active); err != nil {
		t.Fatal(err)
	}
	writeInstallerTrialTransaction(t, root, installerTrialTransaction{
		TransactionID: "installer-trial",
		Platform:      "windows",
		Action:        "install",
		SourceVersion: "v0.8.3",
		TargetVersion: "v0.8.2",
		State:         updateengine.StateTrial,
		InstallRoot:   root,
	})
	lock, err := processlock.Acquire(context.Background(), filepath.Join(root, "install", "transaction.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	layout, err := updateengine.NewWindowsLayout(root)
	if err != nil {
		t.Fatal(err)
	}

	got, err := resolveActiveWithRecovery(root, store, layout)
	if err != nil {
		t.Fatal(err)
	}
	if got.ActiveVersion != active.ActiveVersion || got.TransactionID != active.TransactionID {
		t.Fatalf("resolved active = %#v, want %#v", got, active)
	}
}

func writeInstallerTrialTransaction(t *testing.T, root string, transaction installerTrialTransaction) {
	t.Helper()
	data, err := json.MarshalIndent(transaction, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "install", "transaction.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

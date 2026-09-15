//go:build darwin

package selfupdate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/uvwt/agentdock/internal/updateengine"
)

func TestManagedDesktopUpdateFallsBackForPreArbiterTarget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	coordinationDir := filepath.Join(home, "Library", "Application Support", "AgentDock")
	if err := os.MkdirAll(coordinationDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(coordinationDir, "update-services.json"), []byte(`{"schema_version":1,"core_enabled":false,"tunnel_enabled":false}`), 0o600); err != nil {
		t.Fatal(err)
	}

	sourceApp := filepath.Join(t.TempDir(), "AgentDock.app")
	targetApp := filepath.Join(t.TempDir(), "AgentDock.app")
	for _, app := range []string{sourceApp, targetApp} {
		if err := os.MkdirAll(filepath.Join(app, "Contents", "Helpers"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sourceArbiter := filepath.Join(sourceApp, "Contents", "Helpers", "agentdock-arbiter")
	if err := os.WriteFile(sourceArbiter, []byte("source arbiter"), 0o700); err != nil {
		t.Fatal(err)
	}

	_, handled, err := applyManagedDesktopOnlyUpdate(context.Background(), applyRequest{
		DesktopTargetPath: sourceApp,
		DesktopStagedPath: targetApp,
	})
	if err != nil {
		t.Fatalf("capability fallback returned an error: %v", err)
	}
	if handled {
		t.Fatal("pre-Arbiter target unexpectedly entered the managed transaction engine")
	}
}

func TestCleanupMacOSUpdateArtifactsKeepsDurableTerminalResult(t *testing.T) {
	root := t.TempDir()
	trialPath := filepath.Join(root, ".AgentDock.app.trial.tx-test")
	arbiterDir := filepath.Join(root, "update", "arbiters", "tx-test")
	handoffPath := filepath.Join(root, "update-handoff.json")
	serviceStatePath := filepath.Join(root, "update-services.json")
	terminalResultPath := filepath.Join(root, "update", "result.json")

	for _, directory := range []string{trialPath, arbiterDir, filepath.Dir(terminalResultPath)} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{
		filepath.Join(trialPath, "source-marker"),
		filepath.Join(arbiterDir, "agentdock-arbiter"),
		handoffPath,
		serviceStatePath,
		terminalResultPath,
	} {
		if err := os.WriteFile(path, []byte("evidence\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cleanupMacOSUpdateArtifacts(trialPath, arbiterDir, handoffPath, serviceStatePath)

	for _, path := range []string{trialPath, arbiterDir, handoffPath, serviceStatePath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("cleanup artifact still exists: %s (err=%v)", path, err)
		}
	}
	if data, err := os.ReadFile(terminalResultPath); err != nil {
		t.Fatalf("durable terminal result was removed: %v", err)
	} else if string(data) != "evidence\n" {
		t.Fatalf("durable terminal result changed: %q", data)
	}
}

func TestCleanupMacOSUpdateArtifactsTerminalPolicy(t *testing.T) {
	for _, test := range []struct {
		name        string
		state       updateengine.State
		wantCleanup bool
	}{
		{name: "committed", state: updateengine.StateCommitted, wantCleanup: true},
		{name: "rolled back", state: updateengine.StateRolledBack, wantCleanup: true},
		{name: "failed rollback", state: updateengine.StateFailed, wantCleanup: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			trialPath := filepath.Join(root, ".AgentDock.app.trial.tx-test")
			arbiterDir := filepath.Join(root, "update", "arbiters", "tx-test")
			handoffPath := filepath.Join(root, "update-handoff.json")
			serviceStatePath := filepath.Join(root, "update-services.json")
			for _, directory := range []string{trialPath, arbiterDir} {
				if err := os.MkdirAll(directory, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			for _, path := range []string{
				filepath.Join(trialPath, "source-marker"),
				filepath.Join(arbiterDir, "agentdock-arbiter"),
				handoffPath,
				serviceStatePath,
			} {
				if err := os.WriteFile(path, []byte("evidence\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			cleanupMacOSUpdateArtifactsForTerminalResult(
				updateengine.Result{State: test.state},
				trialPath,
				arbiterDir,
				handoffPath,
				serviceStatePath,
			)

			for _, path := range []string{trialPath, arbiterDir, handoffPath, serviceStatePath} {
				_, err := os.Stat(path)
				if test.wantCleanup && !os.IsNotExist(err) {
					t.Fatalf("safe terminal artifact still exists: %s (err=%v)", path, err)
				}
				if !test.wantCleanup && err != nil {
					t.Fatalf("failed rollback evidence was removed: %s (err=%v)", path, err)
				}
			}
		})
	}
}

//go:build darwin

package updateplatform

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/uvwt/agentdock/internal/updateengine"
)

func TestProcessIDsFromPSOutputPreservesExecutablePathsWithSpaces(t *testing.T) {
	executable := "/Users/Test User/Applications/AgentDock.app/Contents/MacOS/AgentDock"
	output := []byte("  123 " + executable + " --background\n" +
		"  124 /Applications/AgentDock.app/Contents/MacOS/AgentDock --background\n" +
		"  125 " + executable + "-helper\n")

	if got := processIDsFromPSOutput(output, executable); !slices.Equal(got, []int{123}) {
		t.Fatalf("process ids = %v", got)
	}
}

func TestMacOSOpenArgumentsPreserveRuntimeHomeOverrides(t *testing.T) {
	t.Setenv("HOME", "/tmp/agentdock-home")
	t.Setenv("CFFIXED_USER_HOME", "/tmp/agentdock-home")
	t.Setenv("AGENTDOCK_SKIP_LOGIN_ITEM_CONFIGURATION", "1")

	args := macOSOpenArguments("/tmp/AgentDock.app")
	for _, want := range []string{
		"-g",
		"-n",
		"HOME=/tmp/agentdock-home",
		"CFFIXED_USER_HOME=/tmp/agentdock-home",
		"AGENTDOCK_SKIP_LOGIN_ITEM_CONFIGURATION=1",
		"/tmp/AgentDock.app",
		"--background",
	} {
		if !slices.Contains(args, want) {
			t.Fatalf("macOS open args %q missing %q", args, want)
		}
	}
}

func TestDarwinVerifyTrialTreatsRequiresApprovalAsWarning(t *testing.T) {
	root := t.TempDir()
	driver, err := NewDarwinDriver(root)
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := updateengine.NewTransaction("darwin", "0.8.3", "0.8.4")
	if err != nil {
		t.Fatal(err)
	}
	apps := filepath.Join(root, "apps")
	handoffPath := filepath.Join(root, "update-handoff.json")
	transaction.MacOS = &updateengine.MacOSPlan{
		TargetAppPath:  filepath.Join(apps, "AgentDock.app"),
		TrialAppPath:   filepath.Join(apps, ".AgentDock.app.trial"),
		HandoffPath:    handoffPath,
		ResultPath:     filepath.Join(root, "update-result.json"),
		HealthURL:      "http://127.0.0.1:1/healthz",
		CoreWasEnabled: true,
		TunnelEnabled:  true,
	}
	data := []byte(fmt.Sprintf(
		`{"schema_version":1,"transaction_id":%q,"target_version":%q,"core_registration":"requires_approval","tunnel_registration":"requires_approval"}`,
		transaction.TransactionID,
		transaction.TargetVersion,
	))
	if err := os.WriteFile(handoffPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	warnings, err := driver.VerifyTrial(context.Background(), transaction)
	if err != nil {
		t.Fatalf("VerifyTrial() error = %v", err)
	}
	for _, want := range []string{
		"AgentDock Core requires background-item approval in System Settings.",
		"AgentDock Tunnel requires background-item approval in System Settings.",
	} {
		if !slices.Contains(warnings, want) {
			t.Fatalf("VerifyTrial() warnings = %q, want %q", warnings, want)
		}
	}
}

func TestMacOSDesignatedRequirementClassification(t *testing.T) {
	adHoc, err := parseMacOSDesignatedRequirement("Executable=/tmp/AgentDock.app/Contents/MacOS/AgentDock\n# designated => cdhash H\"52e5516b23b677349b172aac371d864180b9c37c\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if !isLegacyAdHocRequirement(adHoc) {
		t.Fatalf("ad-hoc requirement was not classified as legacy: %q", adHoc)
	}

	certificate, err := parseMacOSDesignatedRequirement("Executable=/tmp/AgentDock.app/Contents/MacOS/AgentDock\ndesignated => identifier \"com.uvwt.agentdock\" and certificate leaf = H\"62bdaeee2f8cda5d0d1de81438825b88ed97c988\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if isLegacyAdHocRequirement(certificate) {
		t.Fatalf("certificate requirement was classified as ad-hoc: %q", certificate)
	}
	if certificate != `identifier "com.uvwt.agentdock" and certificate leaf = H"62bdaeee2f8cda5d0d1de81438825b88ed97c988"` {
		t.Fatalf("certificate requirement = %q", certificate)
	}

	if _, err := parseMacOSDesignatedRequirement("Executable=/tmp/AgentDock.app\n"); err == nil {
		t.Fatal("missing designated requirement unexpectedly parsed")
	}
}

func TestDarwinVerifyTrialTreatsUnavailableTunnelAsWarning(t *testing.T) {
	root := t.TempDir()
	driver, err := NewDarwinDriver(root)
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := updateengine.NewTransaction("darwin", "0.8.3", "0.8.4")
	if err != nil {
		t.Fatal(err)
	}
	apps := filepath.Join(root, "apps")
	handoffPath := filepath.Join(root, "update-handoff.json")
	transaction.MacOS = &updateengine.MacOSPlan{
		TargetAppPath: filepath.Join(apps, "AgentDock.app"),
		TrialAppPath:  filepath.Join(apps, ".AgentDock.app.trial"),
		HandoffPath:   handoffPath,
		ResultPath:    filepath.Join(root, "update-result.json"),
		TunnelEnabled: true,
	}
	data := []byte(fmt.Sprintf(
		`{"schema_version":1,"transaction_id":%q,"target_version":%q,"core_registration":"disabled","tunnel_registration":"unavailable"}`,
		transaction.TransactionID,
		transaction.TargetVersion,
	))
	if err := os.WriteFile(handoffPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	warnings, err := driver.VerifyTrial(context.Background(), transaction)
	if err != nil {
		t.Fatalf("VerifyTrial() error = %v", err)
	}
	want := "AgentDock Tunnel registration was not ready after update: unavailable"
	if !slices.Contains(warnings, want) {
		t.Fatalf("VerifyTrial() warnings = %q, want %q", warnings, want)
	}
}

func TestRestoreMacOSRollbackAppRestoresMissingActiveFromSourceSlot(t *testing.T) {
	root := t.TempDir()
	targetPath := filepath.Join(root, "AgentDock.app")
	trialPath := filepath.Join(root, ".AgentDock.app.trial")
	if err := os.MkdirAll(trialPath, 0o755); err != nil {
		t.Fatal(err)
	}
	markerPath := filepath.Join(trialPath, "source-marker")
	if err := os.WriteFile(markerPath, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := restoreMacOSRollbackApp(targetPath, trialPath, "", "0.8.3", "0.8.3", "0.8.4"); err != nil {
		t.Fatalf("restoreMacOSRollbackApp() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetPath, "source-marker")); err != nil {
		t.Fatalf("restored source marker: %v", err)
	}
	if _, err := os.Stat(trialPath); !os.IsNotExist(err) {
		t.Fatalf("rollback slot still exists after restore: %v", err)
	}
}

func TestRestoreMacOSRollbackAppRejectsUnprovenMissingActive(t *testing.T) {
	root := t.TempDir()
	targetPath := filepath.Join(root, "AgentDock.app")
	trialPath := filepath.Join(root, ".AgentDock.app.trial")
	if err := os.MkdirAll(trialPath, 0o755); err != nil {
		t.Fatal(err)
	}

	err := restoreMacOSRollbackApp(targetPath, trialPath, "", "0.8.2", "0.8.3", "0.8.4")
	if err == nil {
		t.Fatal("unproven rollback slot unexpectedly restored")
	}
	if _, statErr := os.Stat(trialPath); statErr != nil {
		t.Fatalf("unproven rollback slot was modified: %v", statErr)
	}
}

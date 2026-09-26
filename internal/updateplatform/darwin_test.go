//go:build darwin

package updateplatform

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
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
	want := []string{"AgentDock Core requires background-item approval in System Settings."}
	if !slices.Equal(warnings, want) {
		t.Fatalf("VerifyTrial() warnings = %q, want only Core warning %q", warnings, want)
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

func TestMacOSSigningIdentityWarning(t *testing.T) {
	adHocOld := "cdhash H\"52e5516b23b677349b172aac371d864180b9c37c\""
	adHocNew := "cdhash H\"672587ba3924923108e8c491f5716a6cf2394bcd\""
	certificate := "identifier \"com.uvwt.agentdock\" and certificate leaf = H\"62bdaeee2f8cda5d0d1de81438825b88ed97c988\""

	if warning := macOSSigningIdentityWarning(adHocOld, adHocOld); warning != "" {
		t.Fatalf("unchanged ad-hoc requirement warning = %q", warning)
	}
	if warning := macOSSigningIdentityWarning(certificate, certificate); warning != "" {
		t.Fatalf("unchanged certificate requirement warning = %q", warning)
	}
	if warning := macOSSigningIdentityWarning(certificate, adHocNew); warning != "" {
		t.Fatalf("certificate downgrade is blocked before VerifyTrial; warning = %q", warning)
	}
	if warning := macOSSigningIdentityWarning(adHocOld, adHocNew); warning == "" {
		t.Fatal("changed ad-hoc requirement must warn about TCC reauthorization")
	}
	if warning := macOSSigningIdentityWarning(adHocOld, certificate); warning == "" {
		t.Fatal("ad-hoc to certificate migration must warn about one-time TCC reauthorization")
	}
}

func TestDarwinVerifyTrialWarnsWhenAdHocIdentityChanges(t *testing.T) {
	root := t.TempDir()
	driver, err := NewDarwinDriver(root)
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := updateengine.NewTransaction("darwin", "0.8.3", "0.8.4")
	if err != nil {
		t.Fatal(err)
	}

	sourcePath := filepath.Join(root, "AgentDock.source")
	targetPath := filepath.Join(root, "AgentDock.target")
	writeAdHocSignedFixture(t, sourcePath, "/bin/echo")
	writeAdHocSignedFixture(t, targetPath, "/bin/cat")

	handoffPath := filepath.Join(root, "update-handoff.json")
	transaction.MacOS = &updateengine.MacOSPlan{
		TargetAppPath: targetPath,
		TrialAppPath:  sourcePath,
		HandoffPath:   handoffPath,
		ResultPath:    filepath.Join(root, "update-result.json"),
	}
	data := []byte(fmt.Sprintf(
		`{"schema_version":1,"transaction_id":%q,"target_version":%q,"core_registration":"disabled","tunnel_registration":"disabled"}`,
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
	if len(warnings) != 1 || !strings.Contains(warnings[0], "ad-hoc signed macOS build") {
		t.Fatalf("VerifyTrial() warnings = %q, want ad-hoc TCC reauthorization warning", warnings)
	}
}

func TestValidateMacOSSigningContinuityAllowsAdHocMigrationAndWarns(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source")
	targetPath := filepath.Join(root, "target")

	writeAdHocSignedFixture(t, sourcePath, "/bin/echo")
	writeAdHocSignedFixture(t, targetPath, "/bin/cat")

	if err := validateMacOSSigningContinuity(context.Background(), sourcePath, targetPath); err != nil {
		t.Fatalf("ad-hoc migration must remain update-compatible: %v", err)
	}

	sourceRequirement, err := macOSDesignatedRequirement(context.Background(), sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	targetRequirement, err := macOSDesignatedRequirement(context.Background(), targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if !isLegacyAdHocRequirement(sourceRequirement) || !isLegacyAdHocRequirement(targetRequirement) {
		t.Fatalf("expected ad-hoc requirements, source=%q target=%q", sourceRequirement, targetRequirement)
	}
	if sourceRequirement == targetRequirement {
		t.Fatalf("fixtures unexpectedly share an ad-hoc requirement: %q", sourceRequirement)
	}
	if warning := macOSSigningIdentityWarning(sourceRequirement, targetRequirement); warning == "" {
		t.Fatal("changed ad-hoc identity must warn about TCC reauthorization")
	}
}

func writeAdHocSignedFixture(t *testing.T, path, binary string) {
	t.Helper()
	if output, err := exec.Command("/bin/cp", binary, path).CombinedOutput(); err != nil {
		t.Fatalf("copy %s: %v: %s", binary, err, output)
	}
	if output, err := exec.Command(
		"/usr/bin/codesign",
		"--force",
		"--sign", "-",
		"--identifier", "com.uvwt.agentdock",
		path,
	).CombinedOutput(); err != nil {
		t.Fatalf("ad-hoc sign %s: %v: %s", path, err, output)
	}
}

func TestDarwinVerifyTrialDoesNotWarnForUnavailableTunnel(t *testing.T) {
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
	if len(warnings) != 0 {
		t.Fatalf("VerifyTrial() warnings = %q, Tunnel/public readiness must not decorate update completion", warnings)
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

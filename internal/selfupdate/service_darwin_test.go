//go:build darwin

package selfupdate

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestStandardMacOSPaths(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "Users", "Agent Dock Test")
	paths := standardMacOSPaths(home)

	want := map[string]string{
		"binary":       filepath.Join(home, ".local", "bin", "agentdock"),
		"service env":  filepath.Join(home, "Library", "Application Support", "AgentDock", "agentdock.env"),
		"start script": filepath.Join(home, "Library", "Application Support", "AgentDock", "start-agentdock.sh"),
		"launch agent": filepath.Join(home, "Library", "LaunchAgents", "com.uvwt.agentdock.plist"),
		"work dir":     filepath.Join(home, "AgentDock"),
		"backup dir":   filepath.Join(home, ".agentdock", "backups", "bin"),
		"stdout log":   filepath.Join(home, "Library", "Logs", "AgentDock", "agentdock.out.log"),
		"stderr log":   filepath.Join(home, "Library", "Logs", "AgentDock", "agentdock.err.log"),
	}
	got := map[string]string{
		"binary":       paths.binary,
		"service env":  paths.serviceEnv,
		"start script": paths.startScript,
		"launch agent": paths.launchAgent,
		"work dir":     paths.workDir,
		"backup dir":   paths.backupDir,
		"stdout log":   paths.stdoutLog,
		"stderr log":   paths.stderrLog,
	}
	for label, expected := range want {
		if got[label] != expected {
			t.Fatalf("%s path = %s, want %s", label, got[label], expected)
		}
	}
}

func TestPlatformHealthCandidatesReadsMacOSAgentDockEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	appSupport := filepath.Join(home, "Library", "Application Support", "AgentDock")
	if err := os.MkdirAll(appSupport, 0o700); err != nil {
		t.Fatal(err)
	}
	serviceEnv := []byte("AGENTDOCK_HOST=0.0.0.0\nAGENTDOCK_PORT='18766'\n")
	if err := os.WriteFile(filepath.Join(appSupport, "agentdock.env"), serviceEnv, 0o600); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(home, ".local", "bin", "agentdock")
	candidates := platformHealthCandidates(context.Background(), target)
	want := "http://127.0.0.1:18766/healthz"
	if !slices.Contains(candidates, want) {
		t.Fatalf("health candidates %v do not contain %s", candidates, want)
	}
}

func TestParseLaunchdPID(t *testing.T) {
	output := "gui/501/com.uvwt.agentdock = {\n\tstate = running\n\tpid = 43210\n}\n"
	if got := parseLaunchdPID(output); got != 43210 {
		t.Fatalf("unexpected PID: %d", got)
	}
	if got := parseLaunchdPID("state = exited\n"); got != 0 {
		t.Fatalf("unexpected PID for stopped service: %d", got)
	}
}

func TestPlatformBackupPathUsesAgentDockStateDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	target := filepath.Join(home, ".local", "bin", "agentdock")

	backup, err := platformBackupPath(target)
	if err != nil {
		t.Fatal(err)
	}
	backupDir := filepath.Join(home, ".agentdock", "backups", "bin")
	if filepath.Dir(backup) != backupDir {
		t.Fatalf("backup path %s is not under %s", backup, backupDir)
	}
	info, err := os.Stat(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("backup directory mode = %o, want 700", info.Mode().Perm())
	}
}

func TestContainsCodeSignIdentifierRequiresExactLine(t *testing.T) {
	output := "Executable=/tmp/agentdock\nIdentifier=com.local.agentdock\nAuthority=AgentDock Local Code Signing\n"
	if !containsCodeSignIdentifier(output, "com.local.agentdock") {
		t.Fatal("expected identifier was not found")
	}
	if containsCodeSignIdentifier(output, "com.local") {
		t.Fatal("partial identifier must not match")
	}
}

package installer

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func stubLaunchctl(t *testing.T, loaded bool) (stateFile, callsFile string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("launchctl rollback contract is exercised on Unix")
	}

	stateDir := t.TempDir()
	stateFile = filepath.Join(stateDir, "loaded")
	callsFile = filepath.Join(stateDir, "calls")
	if loaded {
		if err := os.WriteFile(stateFile, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("TEST_LAUNCHCTL_STATE", stateFile)
	t.Setenv("TEST_LAUNCHCTL_CALLS", callsFile)
	withStubCommands(t, map[string]string{
		"launchctl": `
case "$1" in
  print)
    if [ -f "$TEST_LAUNCHCTL_STATE" ]; then
      echo 'state = running'
      exit 0
    fi
    echo 'Could not find specified service' >&2
    exit 1
    ;;
  bootout)
    echo "bootout $2" >> "$TEST_LAUNCHCTL_CALLS"
    rm -f "$TEST_LAUNCHCTL_STATE"
    ;;
  bootstrap)
    echo "bootstrap $2 $3" >> "$TEST_LAUNCHCTL_CALLS"
    : > "$TEST_LAUNCHCTL_STATE"
    ;;
  kickstart)
    echo "kickstart $2 $3" >> "$TEST_LAUNCHCTL_CALLS"
    ;;
  *) exit 2 ;;
esac
`,
	})
	return stateFile, callsFile
}

func readCalls(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatal(err)
	}
	return string(data)
}

func TestDarwinRollbackDoesNotRestartUntouchedOldJob(t *testing.T) {
	stateFile, callsFile := stubLaunchctl(t, true)
	service := journalService{
		Manager:       "launchd",
		Name:          darwinCLICoreLabel,
		Domain:        "gui/501",
		Plist:         "/tmp/com.uvwt.agentdock.plist",
		WasActive:     true,
		StopAttempted: true,
	}

	if err := stopJournalService(context.Background(), Request{}, service); err != nil {
		t.Fatal(err)
	}
	if err := restoreJournalService(context.Background(), Request{}, service); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stateFile); err != nil {
		t.Fatalf("old launchd job should stay loaded: %v", err)
	}
	if calls := readCalls(t, callsFile); calls != "" {
		t.Fatalf("rollback touched an old job after the initial bootout failed: %q", calls)
	}
}

func TestDarwinRollbackCleansPossiblyLoadedNewJob(t *testing.T) {
	stateFile, callsFile := stubLaunchctl(t, true)
	service := journalService{
		Manager:       "launchd",
		Name:          darwinCLICoreLabel,
		Domain:        "gui/501",
		Plist:         "/tmp/com.uvwt.agentdock.plist",
		LoadAttempted: true,
	}

	if err := stopJournalService(context.Background(), Request{}, service); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stateFile); !os.IsNotExist(err) {
		t.Fatalf("possibly loaded new launchd job should be removed, stat err=%v", err)
	}
	calls := readCalls(t, callsFile)
	if !strings.Contains(calls, "bootout gui/501/"+darwinCLICoreLabel) {
		t.Fatalf("rollback did not boot out the possibly loaded new job: %q", calls)
	}
}

func TestDarwinRollbackRestoresOldJobAfterSuccessfulStop(t *testing.T) {
	stateFile, callsFile := stubLaunchctl(t, false)
	service := journalService{
		Manager:       "launchd",
		Name:          darwinCLICoreLabel,
		Domain:        "gui/501",
		Plist:         "/tmp/com.uvwt.agentdock.plist",
		WasActive:     true,
		StopAttempted: true,
	}

	if err := restoreJournalService(context.Background(), Request{}, service); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stateFile); err != nil {
		t.Fatalf("old launchd job should be restored: %v", err)
	}
	calls := readCalls(t, callsFile)
	if !strings.Contains(calls, "bootstrap gui/501 /tmp/com.uvwt.agentdock.plist") ||
		!strings.Contains(calls, "kickstart -k gui/501/"+darwinCLICoreLabel) {
		t.Fatalf("rollback did not restore the old launchd job: %q", calls)
	}
}

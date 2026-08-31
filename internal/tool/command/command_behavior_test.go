package command

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExecCommandDoesNotFilterCommandContent(t *testing.T) {
	rt, _ := newCommandTestService(t)
	command := `printf 'shell=%s network=%s\n' "$(printf expansion)" "https://example.test"`
	if runtime.GOOS == "windows" {
		command = `Write-Output "shell=expansion network=https://example.test"`
	}
	result, err := rt.execArgs(context.Background(), map[string]any{
		"cmd":            command,
		"yield_time_ms":  15000,
		"timeout_ms":     15000,
		"execution_mode": "sync",
	})
	if err != nil {
		t.Fatalf("exec_command should not reject command content: %v", err)
	}
	if result["status"] != "exited" || !strings.Contains(result["stdout"].(string), "shell=expansion network=https://example.test") {
		t.Fatalf("unexpected command result: %#v", result)
	}
}

func TestExecCommandForwardsConfiguredHostEnv(t *testing.T) {
	rt, cfg := newCommandTestService(t)
	t.Setenv("AGENTDOCK_TEST_HOST_PASSTHROUGH", "host-forwarded")
	cfg.CommandEnvFromEnv = map[string]string{
		"AGENTDOCK_TEST_CHILD_PASSTHROUGH": "AGENTDOCK_TEST_HOST_PASSTHROUGH",
	}

	command := `printf '%s' "$AGENTDOCK_TEST_CHILD_PASSTHROUGH"`
	if runtime.GOOS == "windows" {
		command = `[Console]::Out.Write($env:AGENTDOCK_TEST_CHILD_PASSTHROUGH)`
	}
	result, err := rt.execArgs(context.Background(), map[string]any{
		"cmd":            command,
		"yield_time_ms":  15000,
		"timeout_ms":     15000,
		"execution_mode": "sync",
	})
	if err != nil {
		t.Fatalf("exec_command should forward configured host env: %v", err)
	}
	if result["status"] != "exited" || result["stdout"].(string) != "host-forwarded" {
		t.Fatalf("configured host env was not forwarded: %#v", result)
	}
}

func TestExecCommandForwardsExplicitEnv(t *testing.T) {
	rt, _ := newCommandTestService(t)
	command := `printf '%s' "$AGENTDOCK_TEST_EXEC_ENV"`
	if runtime.GOOS == "windows" {
		command = `[Console]::Out.Write($env:AGENTDOCK_TEST_EXEC_ENV)`
	}
	result, err := rt.execArgs(context.Background(), map[string]any{
		"cmd":            command,
		"env":            map[string]any{"AGENTDOCK_TEST_EXEC_ENV": "forwarded"},
		"yield_time_ms":  15000,
		"timeout_ms":     15000,
		"execution_mode": "sync",
	})
	if err != nil {
		t.Fatalf("exec_command should accept explicit env: %v", err)
	}
	if result["status"] != "exited" || result["stdout"].(string) != "forwarded" {
		t.Fatalf("explicit env was not forwarded: %#v", result)
	}
}

func TestCommandEnvReportsTempDirectoryFailure(t *testing.T) {
	rt, cfg := newCommandTestService(t)
	root := cfg.AgentDockDefaultDir
	blocked := filepath.Join(root, "blocked-home")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.AgentDockHome = blocked
	if _, err := rt.CommandEnv("", nil); err == nil || !strings.Contains(err.Error(), "create command temp directory") {
		t.Fatalf("commandEnv() error = %v, want temp-directory error", err)
	}
}

func TestExecCommandForwardsStdinAndClosesPipe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test command uses POSIX cat")
	}
	rt, _ := newCommandTestService(t)
	result, err := rt.execArgs(context.Background(), map[string]any{
		"cmd":            "cat",
		"stdin":          "input-line\n",
		"yield_time_ms":  5000,
		"timeout_ms":     5000,
		"execution_mode": "sync",
	})
	if err != nil {
		t.Fatalf("execCommand() error = %v", err)
	}
	if result["status"] != "exited" || result["stdout"] != "input-line\n" {
		t.Fatalf("result = %#v", result)
	}
}

func TestExecCommandReportsClosedStdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test command uses POSIX shell")
	}
	rt, _ := newCommandTestService(t)
	largeInput := strings.Repeat("x", 8<<20)
	_, err := rt.execArgs(context.Background(), map[string]any{
		"cmd":            "exec 0<&-; sleep 1",
		"stdin":          largeInput,
		"yield_time_ms":  5000,
		"timeout_ms":     5000,
		"execution_mode": "sync",
	})
	if err == nil || !strings.Contains(err.Error(), "write command stdin") {
		t.Fatalf("execCommand() error = %v, want stdin write failure", err)
	}
}

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

func TestExecCommandOmitsSandboxFieldAcrossExecutionModes(t *testing.T) {
	svc, _ := newCommandTestService(t)
	defer svc.Close()

	fastCmd := "printf 'fast-ok'"
	slowCmd := "sleep 2"
	if runtime.GOOS == "windows" {
		fastCmd = "[Console]::Out.Write('fast-ok')"
		slowCmd = "Start-Sleep -Seconds 2"
	}

	assertNoSandbox := func(t *testing.T, result Result) {
		t.Helper()
		if _, exists := result["sandbox"]; exists {
			t.Fatalf("exec_command result should not contain sandbox field: %#v", result)
		}
	}
	assertCommonFields := func(t *testing.T, result Result) {
		t.Helper()
		for _, key := range []string{
			"session_id", "status", "stdout", "stderr", "elapsed_ms", "timed_out", "terminal",
			"stdout_output_bytes", "stderr_output_bytes", "stdout_total_bytes", "stderr_total_bytes",
			"stdout_dropped_bytes", "stderr_dropped_bytes", "stdout_omitted_bytes", "stderr_omitted_bytes",
			"stdout_output_lines", "stderr_output_lines", "stdout_truncated", "stderr_truncated",
		} {
			if _, exists := result[key]; !exists {
				t.Fatalf("exec_command result missing expected field %q: %#v", key, result)
			}
		}
	}

	t.Run("sync_exited", func(t *testing.T) {
		result, err := svc.execArgs(context.Background(), map[string]any{
			"cmd":            fastCmd,
			"execution_mode": "sync",
			"timeout_ms":     15000,
		})
		if err != nil {
			t.Fatalf("exec_command sync error = %v", err)
		}
		assertNoSandbox(t, result)
		assertCommonFields(t, result)
		if result["status"] != "exited" || result["stdout"] != "fast-ok" || result["exit_code"] != 0 || result["command_ok"] != true {
			t.Fatalf("unexpected sync result: %#v", result)
		}
	})

	t.Run("auto_exited", func(t *testing.T) {
		result, err := svc.execArgs(context.Background(), map[string]any{
			"cmd":            fastCmd,
			"execution_mode": "auto",
			"yield_time_ms":  15000,
			"timeout_ms":     15000,
		})
		if err != nil {
			t.Fatalf("exec_command auto(exited) error = %v", err)
		}
		assertNoSandbox(t, result)
		assertCommonFields(t, result)
		if result["status"] != "exited" || result["stdout"] != "fast-ok" || result["exit_code"] != 0 || result["command_ok"] != true {
			t.Fatalf("unexpected auto(exited) result: %#v", result)
		}
	})

	t.Run("async_running", func(t *testing.T) {
		result, err := svc.execArgs(context.Background(), map[string]any{
			"cmd":            slowCmd,
			"execution_mode": "async",
			"timeout_ms":     15000,
		})
		if err != nil {
			t.Fatalf("exec_command async error = %v", err)
		}
		sessionID, _ := result["session_id"].(string)
		defer func() {
			_, _ = svc.actArgs(map[string]any{"action": "kill", "session_id": sessionID})
		}()
		assertNoSandbox(t, result)
		assertCommonFields(t, result)
		if result["status"] != "running" || result["session_reason"] != "explicit_async" || result["observe_after_ms"] != 1000 {
			t.Fatalf("unexpected async result: %#v", result)
		}
	})

	t.Run("auto_running", func(t *testing.T) {
		result, err := svc.execArgs(context.Background(), map[string]any{
			"cmd":            slowCmd,
			"execution_mode": "auto",
			"yield_time_ms":  10,
			"timeout_ms":     15000,
		})
		if err != nil {
			t.Fatalf("exec_command auto(running) error = %v", err)
		}
		sessionID, _ := result["session_id"].(string)
		defer func() {
			_, _ = svc.actArgs(map[string]any{"action": "kill", "session_id": sessionID})
		}()
		assertNoSandbox(t, result)
		assertCommonFields(t, result)
		if result["status"] != "running" || result["session_reason"] != "foreground_threshold_exceeded" || result["observe_after_ms"] != 1000 {
			t.Fatalf("unexpected auto(running) result: %#v", result)
		}
	})
}

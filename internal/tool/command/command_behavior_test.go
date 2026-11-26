package command

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/tool/command/session"
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
		for _, key := range []string{"session_id", "status", "stdout", "elapsed_ms"} {
			if _, exists := result[key]; !exists {
				t.Fatalf("exec_command result missing expected field %q: %#v", key, result)
			}
		}
		// 正常无截断、无 stderr、非超时且默认 pipes 的命令不应输出冗余默认字段和零值统计。
		for _, key := range []string{
			"stderr", "timed_out", "terminal",
			"stdout_output_bytes", "stderr_output_bytes", "stdout_total_bytes", "stderr_total_bytes",
			"stdout_dropped_bytes", "stderr_dropped_bytes", "stdout_omitted_bytes", "stderr_omitted_bytes",
			"stdout_output_lines", "stderr_output_lines", "stdout_truncated", "stderr_truncated",
		} {
			if _, exists := result[key]; exists {
				t.Fatalf("exec_command normal result should omit redundant field %q: %#v", key, result)
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

func TestSnapshotResultIncludesConditionalFieldsOnlyWhenNeeded(t *testing.T) {
	t.Run("normal_short_output_omits_noise", func(t *testing.T) {
		result := snapshotResult(session.Snapshot{
			SessionID:         "session-1",
			Status:            "exited",
			Stdout:            "ok\n",
			Stderr:            "",
			ElapsedMS:         12,
			TimedOut:          false,
			Terminal:          "pipes",
			StdoutOutputBytes: 3,
			StdoutTotalBytes:  3,
			Completed:         true,
			ExitCode:          0,
			CommandOK:         true,
		})
		for _, key := range []string{
			"stderr", "timed_out", "terminal",
			"stdout_truncated", "stdout_output_bytes", "stdout_total_bytes", "stdout_dropped_bytes", "stdout_omitted_bytes", "stdout_output_lines",
			"stderr_truncated", "stderr_output_bytes", "stderr_total_bytes", "stderr_dropped_bytes", "stderr_omitted_bytes", "stderr_output_lines",
		} {
			if _, exists := result[key]; exists {
				t.Fatalf("normal snapshot should omit %q: %#v", key, result)
			}
		}
		if result["stdout"] != "ok\n" || result["exit_code"] != 0 || result["command_ok"] != true {
			t.Fatalf("unexpected normal snapshot result: %#v", result)
		}
	})

	t.Run("truncation_timeout_and_non_default_terminal", func(t *testing.T) {
		result := snapshotResult(session.Snapshot{
			SessionID:          "session-2",
			Status:             "timeout",
			Stdout:             "head",
			Stderr:             "",
			ElapsedMS:          500,
			TimedOut:           true,
			Terminal:           "conpty",
			StdoutOutputBytes:  4,
			StdoutTotalBytes:   120,
			StdoutDroppedBytes: 20,
			StdoutOmittedBytes: 96,
			StdoutTruncated:    true,
			StderrOutputBytes:  0,
			StderrTotalBytes:   64,
			StderrDroppedBytes: 64,
			Completed:          true,
			ExitCode:           -1,
			CommandOK:          false,
		})
		if result["timed_out"] != true || result["terminal"] != "conpty" {
			t.Fatalf("missing timed_out or terminal: %#v", result)
		}
		if result["stdout_truncated"] != true || result["stdout_output_bytes"] != 4 || result["stdout_total_bytes"] != 120 ||
			result["stdout_dropped_bytes"] != 20 || result["stdout_omitted_bytes"] != 96 {
			t.Fatalf("unexpected stdout truncation metrics: %#v", result)
		}
		// stderr 即使本次片段为空，只要发生丢弃/截断也必须保留 stderr 及对应统计，避免调用方误判无错误输出。
		if _, exists := result["stderr"]; !exists {
			t.Fatalf("truncated stderr should preserve stderr key: %#v", result)
		}
		if result["stderr_truncated"] != true || result["stderr_output_bytes"] != 0 || result["stderr_total_bytes"] != 64 || result["stderr_dropped_bytes"] != 64 {
			t.Fatalf("unexpected stderr truncation metrics: %#v", result)
		}
		if _, exists := result["stderr_omitted_bytes"]; exists {
			t.Fatalf("zero stderr_omitted_bytes should be omitted: %#v", result)
		}
		for _, removed := range []string{"stdout_output_lines", "stderr_output_lines"} {
			if _, exists := result[removed]; exists {
				t.Fatalf("removed line-count field %q should not be present: %#v", removed, result)
			}
		}
	})
}

func TestExecCommandIncludesConditionalFieldsOnTruncationStderrAndTimeout(t *testing.T) {
	svc, _ := newCommandTestService(t)
	defer svc.Close()

	t.Run("stdout_truncated", func(t *testing.T) {
		cmd := "printf '0123456789abcdef0123456789abcdef'"
		if runtime.GOOS == "windows" {
			cmd = "[Console]::Out.Write('0123456789abcdef0123456789abcdef')"
		}
		result, err := svc.execArgs(context.Background(), map[string]any{
			"cmd":              cmd,
			"execution_mode":   "sync",
			"timeout_ms":       15000,
			"max_output_bytes": 10,
		})
		if err != nil {
			t.Fatalf("exec_command truncated error = %v", err)
		}
		if result["stdout_truncated"] != true || result["stdout_output_bytes"] != 10 || result["stdout_total_bytes"] != 32 || result["stdout_omitted_bytes"] != 22 {
			t.Fatalf("unexpected stdout truncation fields: %#v", result)
		}
		for _, absent := range []string{"stdout_dropped_bytes", "stderr", "stderr_truncated", "timed_out", "terminal", "stdout_output_lines", "stderr_output_lines"} {
			if _, exists := result[absent]; exists {
				t.Fatalf("truncated stdout result should omit %q: %#v", absent, result)
			}
		}
	})

	t.Run("non_empty_stderr_without_truncation", func(t *testing.T) {
		cmd := "printf 'warn-msg' >&2"
		if runtime.GOOS == "windows" {
			cmd = "[Console]::Error.Write('warn-msg')"
		}
		result, err := svc.execArgs(context.Background(), map[string]any{
			"cmd":            cmd,
			"execution_mode": "sync",
			"timeout_ms":     15000,
		})
		if err != nil {
			t.Fatalf("exec_command stderr error = %v", err)
		}
		if result["stderr"] != "warn-msg" {
			t.Fatalf("stderr = %#v, want warn-msg", result["stderr"])
		}
		for _, absent := range []string{"stderr_truncated", "stderr_output_bytes", "stderr_total_bytes", "stdout_truncated", "timed_out", "terminal"} {
			if _, exists := result[absent]; exists {
				t.Fatalf("untruncated stderr result should omit %q: %#v", absent, result)
			}
		}
	})

	t.Run("timeout_sets_timed_out_true", func(t *testing.T) {
		cmd := "sleep 2"
		if runtime.GOOS == "windows" {
			cmd = "Start-Sleep -Seconds 2"
		}
		result, err := svc.execArgs(context.Background(), map[string]any{
			"cmd":            cmd,
			"execution_mode": "sync",
			"timeout_ms":     200,
		})
		if err != nil {
			t.Fatalf("exec_command timeout error = %v", err)
		}
		if result["status"] != "timeout" || result["timed_out"] != true || result["command_ok"] != false {
			t.Fatalf("unexpected timeout result: %#v", result)
		}
	})
}

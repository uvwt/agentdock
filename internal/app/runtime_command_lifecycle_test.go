package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestRuntimeCloseStopsRunningCommandSessions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test command uses POSIX shell syntax")
	}
	rt, root := newCodeToolsRuntime(t)
	marker := filepath.Join(root, "must-not-be-created")
	started, err := rt.Call(context.Background(), "exec_command", map[string]any{
		"cmd":            `sleep 1; printf done > "$MARKER"`,
		"env":            map[string]any{"MARKER": marker},
		"execution_mode": "async",
		"timeout_ms":     60000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if started["status"] != "running" {
		t.Fatalf("exec_command did not start asynchronously: %#v", started)
	}

	if err := rt.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}

	time.Sleep(1200 * time.Millisecond)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("runtime close left command alive; marker stat err=%v", err)
	}
}

func TestRuntimeCloseCancelsForegroundCommandAndRejectsNewStarts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test command uses POSIX shell syntax")
	}
	rt, root := newCodeToolsRuntime(t)
	startedMarker := filepath.Join(root, "started")
	callDone := make(chan error, 1)
	go func() {
		_, err := rt.Call(context.Background(), "exec_command", map[string]any{
			"cmd":            `printf started > "$STARTED_MARKER"; sleep 30`,
			"env":            map[string]any{"STARTED_MARKER": startedMarker},
			"execution_mode": "sync",
			"timeout_ms":     60000,
		})
		callDone <- err
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(startedMarker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("foreground command did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := rt.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case err := <-callDone:
		if err != nil {
			t.Fatalf("foreground exec returned error after close: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("foreground command did not return after runtime close")
	}

	_, err := rt.Call(context.Background(), "exec_command", map[string]any{"cmd": "echo must-not-run"})
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "RUNTIME_CLOSING" {
		t.Fatalf("post-close exec error = %#v, want RUNTIME_CLOSING", err)
	}
}

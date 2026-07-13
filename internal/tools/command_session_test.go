package tools

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/uvwt/agentdock/internal/session"
)

func TestCommandOutputLimit(t *testing.T) {
	for _, test := range []struct {
		name string
		args map[string]any
		want int
	}{
		{name: "default", args: nil, want: 65536},
		{name: "negative", args: map[string]any{"max_output_bytes": -1}, want: 65536},
		{name: "custom", args: map[string]any{"max_output_bytes": 1024}, want: 1024},
		{name: "capped", args: map[string]any{"max_output_bytes": maxCommandOutputBytes + 1}, want: maxCommandOutputBytes},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := commandOutputLimit(test.args); got != test.want {
				t.Fatalf("commandOutputLimit() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestExecCommandRejectsNonPositiveTimeout(t *testing.T) {
	runtime, _ := newCodeToolsRuntime(t)
	for _, timeout := range []int{-1, 0} {
		_, err := runtime.execCommand(context.Background(), map[string]any{"cmd": "true", "timeout_ms": timeout})
		var toolErr *ToolError
		if !errors.As(err, &toolErr) || toolErr.Code != "INVALID_TIMEOUT" {
			t.Fatalf("timeout_ms=%d error = %#v", timeout, err)
		}
	}
}

func TestListSessionsKeepsCompletedResultAvailable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test command uses POSIX shell syntax")
	}
	runtime, _ := newCodeToolsRuntime(t)
	started, err := runtime.execCommand(context.Background(), map[string]any{
		"cmd":           "sleep 0.05; printf 'completed-output'",
		"yield_time_ms": 1,
		"timeout_ms":    2000,
	})
	if err != nil {
		t.Fatalf("execCommand() error = %v", err)
	}
	if started["status"] != "running" {
		t.Fatalf("initial status = %#v, want running", started["status"])
	}
	sessionID, _ := started["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("initial result missing session_id: %#v", started)
	}

	session, ok := runtime.sessions.Get(sessionID)
	if !ok {
		t.Fatalf("session %q was not stored", sessionID)
	}
	select {
	case <-session.Done:
	case <-time.After(time.Second):
		t.Fatal("command did not complete")
	}
	listed, err := runtime.listSessions()
	if err != nil {
		t.Fatalf("listSessions() error = %v", err)
	}
	items, _ := listed["sessions"].([]map[string]any)
	if len(items) != 1 || items[0]["session_id"] != sessionID || items[0]["status"] != "exited" {
		t.Fatalf("listed sessions = %#v, want completed session", listed["sessions"])
	}

	result, err := runtime.sessionStatus(map[string]any{"session_id": sessionID})
	if err != nil {
		t.Fatalf("sessionStatus() error = %v", err)
	}
	if result["status"] != "exited" || result["stdout"] != "completed-output" {
		t.Fatalf("completed result = %#v", result)
	}
	if _, ok := runtime.sessions.Get(sessionID); ok {
		t.Fatal("session remained stored after final result was consumed")
	}
}

func TestKillCompletedSessionReturnsActualStatus(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test command uses POSIX shell syntax")
	}
	runtime, _ := newCodeToolsRuntime(t)
	started, err := runtime.execCommand(context.Background(), map[string]any{
		"cmd": "sleep 0.02; printf 'already-done'", "yield_time_ms": 1, "timeout_ms": 2000,
	})
	if err != nil {
		t.Fatalf("execCommand() error = %v", err)
	}
	sessionID, _ := started["session_id"].(string)
	stored, ok := runtime.sessions.Get(sessionID)
	if !ok {
		t.Fatalf("session %q was not stored", sessionID)
	}
	select {
	case <-stored.Done:
	case <-time.After(time.Second):
		t.Fatal("command did not complete")
	}

	result, err := runtime.killSession(map[string]any{"session_id": sessionID})
	if err != nil {
		t.Fatalf("killSession() error = %v", err)
	}
	if result["status"] != "exited" || result["stdout"] != "already-done" {
		t.Fatalf("completed kill result = %#v", result)
	}
}

func TestKillAllSessionsKeepsCompletedStatus(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test command uses POSIX shell syntax")
	}
	runtime, _ := newCodeToolsRuntime(t)
	started, err := runtime.execCommand(context.Background(), map[string]any{
		"cmd": "sleep 0.02", "yield_time_ms": 1, "timeout_ms": 2000,
	})
	if err != nil {
		t.Fatalf("execCommand() error = %v", err)
	}
	sessionID, _ := started["session_id"].(string)
	stored, ok := runtime.sessions.Get(sessionID)
	if !ok {
		t.Fatalf("session %q was not stored", sessionID)
	}
	select {
	case <-stored.Done:
	case <-time.After(time.Second):
		t.Fatal("command did not complete")
	}
	result, err := runtime.killAllSessions(nil)
	if err != nil {
		t.Fatalf("killAllSessions() error = %v", err)
	}
	items := result["sessions"].([]map[string]any)
	if len(items) != 1 || items[0]["session_id"] != sessionID || items[0]["status"] != "exited" {
		t.Fatalf("kill_all result = %#v", result)
	}
}

func TestKillSessionWaitsForProcessExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test command uses POSIX shell syntax")
	}
	runtime, _ := newCodeToolsRuntime(t)
	started, err := runtime.execCommand(context.Background(), map[string]any{
		"cmd":           "sleep 10",
		"yield_time_ms": 1,
		"timeout_ms":    20000,
	})
	if err != nil {
		t.Fatalf("execCommand() error = %v", err)
	}
	sessionID, _ := started["session_id"].(string)
	session, ok := runtime.sessions.Get(sessionID)
	if !ok {
		t.Fatalf("session %q was not stored", sessionID)
	}

	result, err := runtime.killSession(map[string]any{"session_id": sessionID})
	if err != nil {
		t.Fatalf("killSession() error = %v", err)
	}
	select {
	case <-session.Done:
	default:
		t.Fatal("killSession() returned before process completion")
	}
	if result["status"] != "killed" {
		t.Fatalf("kill result status = %#v", result["status"])
	}
	if _, ok := result["exit_code"]; !ok {
		t.Fatalf("kill result missing exit_code: %#v", result)
	}
	if _, ok := runtime.sessions.Get(sessionID); ok {
		t.Fatal("killed session remained stored")
	}
}

func TestWaitForSessionsCompletionUsesSharedDeadline(t *testing.T) {
	sessions := make([]*session.Session, 0, 10)
	for index := range 10 {
		sessions = append(sessions, &session.Session{ID: fmt.Sprintf("session-%d", index), Done: make(chan struct{})})
	}
	started := time.Now()
	completed, timedOut := waitForSessionsCompletion(sessions, 50*time.Millisecond)
	elapsed := time.Since(started)
	if len(completed) != 0 || len(timedOut) != len(sessions) {
		t.Fatalf("completed=%d timed_out=%d", len(completed), len(timedOut))
	}
	if elapsed >= 300*time.Millisecond {
		t.Fatalf("shared timeout took %s; appears to be applied per session", elapsed)
	}
}

func TestKillAllSessionsWaitsForEveryProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test command uses POSIX shell syntax")
	}
	runtime, _ := newCodeToolsRuntime(t)
	sessions := make([]*session.Session, 0, 2)
	for range 2 {
		started, err := runtime.execCommand(context.Background(), map[string]any{
			"cmd": "sleep 10", "yield_time_ms": 1, "timeout_ms": 20000,
		})
		if err != nil {
			t.Fatalf("execCommand() error = %v", err)
		}
		sessionID, _ := started["session_id"].(string)
		stored, ok := runtime.sessions.Get(sessionID)
		if !ok {
			t.Fatalf("session %q was not stored", sessionID)
		}
		sessions = append(sessions, stored)
	}

	result, err := runtime.killAllSessions(nil)
	if err != nil {
		t.Fatalf("killAllSessions() error = %v", err)
	}
	if result["count"] != 2 {
		t.Fatalf("killAllSessions() count = %#v, want 2", result["count"])
	}
	for _, stored := range sessions {
		select {
		case <-stored.Done:
		default:
			t.Fatalf("session %s still running after kill_all", stored.ID)
		}
		if _, ok := runtime.sessions.Get(stored.ID); ok {
			t.Fatalf("session %s remained stored after kill_all", stored.ID)
		}
	}
}

func TestSessionActWriteAfterCompletionReturnsFinalOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test command uses POSIX shell syntax")
	}
	runtime, _ := newCodeToolsRuntime(t)
	started, err := runtime.execCommand(context.Background(), map[string]any{
		"cmd": "sleep 0.05; printf 'final-output'", "yield_time_ms": 1, "timeout_ms": 2000,
	})
	if err != nil {
		t.Fatalf("execCommand() error = %v", err)
	}
	sessionID, _ := started["session_id"].(string)
	stored, ok := runtime.sessions.Get(sessionID)
	if !ok {
		t.Fatalf("session %q was not stored", sessionID)
	}
	select {
	case <-stored.Done:
	case <-time.After(time.Second):
		t.Fatal("command did not complete")
	}

	result, err := runtime.writeStdin(map[string]any{"session_id": sessionID, "chars": "late-input"})
	if err != nil {
		t.Fatalf("writeStdin() error = %v", err)
	}
	if result["status"] != "exited" || result["stdout"] != "final-output" {
		t.Fatalf("final result = %#v", result)
	}
	if _, ok := runtime.sessions.Get(sessionID); ok {
		t.Fatal("completed session remained stored")
	}
}

func TestSessionActWritesInputAndReturnsFinalOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test command uses POSIX shell syntax")
	}
	runtime, _ := newCodeToolsRuntime(t)
	started, err := runtime.execCommand(context.Background(), map[string]any{
		"cmd":           "IFS= read -r line; printf 'received:%s' \"$line\"",
		"tty":           true,
		"yield_time_ms": 1,
		"timeout_ms":    2000,
	})
	if err != nil {
		t.Fatalf("execCommand() error = %v", err)
	}
	sessionID, _ := started["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("initial result missing session_id: %#v", started)
	}
	result, err := runtime.writeStdin(map[string]any{"session_id": sessionID, "chars": "hello\n"})
	if err != nil {
		t.Fatalf("writeStdin() error = %v", err)
	}

	var stdout strings.Builder
	if value, _ := result["stdout"].(string); value != "" {
		stdout.WriteString(value)
	}
	deadline := time.Now().Add(time.Second)
	for result["status"] != "exited" && time.Now().Before(deadline) {
		result, err = runtime.sessionStatus(map[string]any{"session_id": sessionID})
		if err != nil {
			if !strings.Contains(err.Error(), "session not found") {
				t.Fatalf("sessionStatus() error = %v", err)
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if value, _ := result["stdout"].(string); value != "" {
			stdout.WriteString(value)
		}
		if result["status"] != "exited" {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if result["status"] != "exited" || stdout.String() != "received:hello" {
		t.Fatalf("final result = %#v, stdout = %q", result, stdout.String())
	}
}

func TestExecCommandRejectsWhenRunningSessionLimitIsReached(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	for i := 0; i < maxConcurrentCommandSessions; i++ {
		rt.sessions.Add(&session.Session{
			ID:        fmt.Sprintf("running-%02d", i),
			StartedAt: time.Now(),
			Done:      make(chan struct{}),
		})
	}
	_, err := rt.execCommand(context.Background(), map[string]any{"cmd": "must-not-start"})
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "SESSION_LIMIT_REACHED" {
		t.Fatalf("execCommand() error = %#v, want SESSION_LIMIT_REACHED", err)
	}
}

func TestRuntimeCloseStopsRunningCommandSessions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test command uses POSIX shell syntax")
	}
	rt, _ := newCodeToolsRuntime(t)
	started, err := rt.execCommand(context.Background(), map[string]any{
		"cmd": "sleep 30", "yield_time_ms": 1, "timeout_ms": 60000,
	})
	if err != nil {
		t.Fatal(err)
	}
	sessionID := started["session_id"].(string)
	stored, ok := rt.sessions.Get(sessionID)
	if !ok {
		t.Fatalf("session %q was not stored", sessionID)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case <-stored.Done:
	case <-time.After(2 * time.Second):
		t.Fatal("runtime close did not stop the command session")
	}
	if _, ok := rt.sessions.Get(sessionID); ok {
		t.Fatal("runtime close left the session in the store")
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestRuntimeCloseCancelsCommandBeforeItIsStored(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test command uses POSIX sleep")
	}
	rt, _ := newCodeToolsRuntime(t)
	callDone := make(chan error, 1)
	go func() {
		_, err := rt.execCommand(context.Background(), map[string]any{
			"cmd":             "sleep 30",
			"yield_time_ms":   30000,
			"timeout_ms":      60000,
			"wait_until_exit": true,
		})
		callDone <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	for rt.sessions.ReservationCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if rt.sessions.ReservationCount() == 0 {
		t.Fatal("command did not enter the startup reservation window")
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case err := <-callDone:
		if err != nil {
			t.Fatalf("execCommand() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("starting command did not return after runtime close")
	}
	if got := rt.sessions.ReservationCount(); got != 0 {
		t.Fatalf("reservation count = %d, want 0", got)
	}
	if listed := rt.sessions.List(); len(listed) != 0 {
		t.Fatalf("sessions after close = %#v", listed)
	}

	_, err := rt.execCommand(context.Background(), map[string]any{"cmd": "echo must-not-run"})
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "RUNTIME_CLOSING" {
		t.Fatalf("post-close exec error = %#v, want RUNTIME_CLOSING", err)
	}
}

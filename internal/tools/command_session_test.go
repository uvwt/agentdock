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
	if _, err := runtime.writeStdin(map[string]any{"session_id": sessionID, "chars": "hello\n"}); err != nil {
		t.Fatalf("writeStdin() error = %v", err)
	}

	var result Result
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		result, err = runtime.sessionStatus(map[string]any{"session_id": sessionID})
		if err == nil && result["status"] == "exited" {
			break
		}
		if err != nil && !strings.Contains(err.Error(), "session not found") {
			t.Fatalf("sessionStatus() error = %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if result["status"] != "exited" || result["stdout"] != "received:hello" {
		t.Fatalf("final result = %#v", result)
	}
}

package command

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestExecBlockedInitialStdinStillReturnsControllableSession(t *testing.T) {
	command := "sleep 10"
	if runtime.GOOS == "windows" {
		command = "Start-Sleep -Seconds 10"
	}
	for _, mode := range []string{"async", "auto", "sync"} {
		t.Run(mode, func(t *testing.T) {
			service, _ := newCommandTestService(t)
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			timeout, yield := 2500, 0
			start := time.Now()
			result, err := service.Exec(ctx, ExecRequest{
				Cmd: command, Stdin: strings.Repeat("x", 256<<10),
				ExecutionMode: mode, TimeoutMS: &timeout, YieldTimeMS: &yield,
			})
			if err != nil {
				t.Fatalf("Exec returned stdin error before a session: %v", err)
			}
			if elapsed := time.Since(start); elapsed >= 2*time.Second {
				t.Errorf("Exec blocked for %s waiting for stdin", elapsed)
			}
			id, ok := result["session_id"].(string)
			if !ok || id == "" || result["status"] != "running" {
				t.Fatalf("result = %#v, want controllable running session", result)
			}
			if _, err := service.Act(SessionActRequest{Action: "kill", SessionID: id}); err != nil {
				t.Fatalf("kill blocked-input session: %v", err)
			}
			if service.sessions.ReservationCount() != 0 {
				t.Fatal("command reservation was not released")
			}
		})
	}
}

func TestExecInitialStdinReachesEOFAndPreservesFullInput(t *testing.T) {
	command := "cat"
	if runtime.GOOS == "windows" {
		command = "[Console]::Out.Write([Console]::In.ReadToEnd())"
	}
	service, _ := newCommandTestService(t)
	input := strings.Repeat("input-line\n", 10000)
	timeout, maxOutput := 10000, len(input)
	result, err := service.Exec(context.Background(), ExecRequest{
		Cmd: command, Stdin: input, ExecutionMode: "sync", TimeoutMS: &timeout, MaxOutputBytes: &maxOutput,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["status"] != "exited" || result["stdout"] != input || result["command_ok"] != true {
		t.Fatalf("result status=%v command_ok=%v stdout bytes=%v, want complete input", result["status"], result["command_ok"], result["stdout_total_bytes"])
	}
}

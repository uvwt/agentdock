package app

import (
	"context"
	"runtime"
	"testing"
	"time"
)

func TestSessionReadRuntimeContractAndRetry(t *testing.T) {
	rt := newRuntimeValidationTestRuntime(t)
	cmd := "printf replay-marker"
	if runtime.GOOS == "windows" {
		cmd = "[Console]::Out.Write('replay-marker')"
	}
	started, err := rt.Call(context.Background(), "exec_command", map[string]any{"cmd": cmd, "execution_mode": "async"})
	if err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"action": "read", "session_id": started["session_id"], "stdout_offset": 0, "stderr_offset": 0, "max_output_bytes": 6}
	deadline := time.Now().Add(5 * time.Second)
	for {
		page, err := rt.Call(context.Background(), "session_observe", args)
		if err != nil {
			t.Fatal(err)
		}
		assertToolResultMatchestestOutputSchema(t, "session_observe", page)
		if page["status"] == "exited" {
			if page["stdout"] != "replay" || page["stdout_next_offset"] != 6 || page["stdout_truncated"] != true {
				t.Fatalf("page = %#v", page)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("command did not complete")
		}
		time.Sleep(10 * time.Millisecond)
	}
	retry, err := rt.Call(context.Background(), "session_observe", args)
	if err != nil || retry["stdout"] != "replay" {
		t.Fatalf("retry = %#v, %v", retry, err)
	}
	args["stdout_offset"] = 6
	last, err := rt.Call(context.Background(), "session_observe", args)
	if err != nil || last["stdout"] != "-marke" {
		t.Fatalf("next page = %#v, %v", last, err)
	}
	assertToolResultMatchestestOutputSchema(t, "session_observe", last)
}

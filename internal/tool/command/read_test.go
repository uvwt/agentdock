package command

import (
	"context"
	"runtime"
	"testing"
)

func TestReadSessionRetainsCompletedResultForRetry(t *testing.T) {
	svc, _ := newCommandTestService(t)
	cmd := "printf 'first-second'; printf 'error' >&2; exit 7"
	if runtime.GOOS == "windows" {
		cmd = "[Console]::Out.Write('first-second'); [Console]::Error.Write('error'); exit 7"
	}
	result, err := svc.Exec(context.Background(), ExecRequest{Cmd: cmd, ExecutionMode: "async"})
	if err != nil {
		t.Fatal(err)
	}
	id := result["session_id"].(string)
	s, _ := svc.sessions.Get(id)
	<-s.Done
	limit := 5
	request := SessionObserveRequest{Action: "read", SessionID: id, MaxOutputBytes: &limit}
	for range 2 {
		page, err := svc.Observe(request)
		if err != nil || page["stdout"] != "first" || page["stderr"] != "error" || page["status"] != "exited" || page["exit_code"] != 7 || page["command_error"] == nil {
			t.Fatalf("read completed output = %#v, %v", page, err)
		}
	}
	offset := 5
	request.StdoutOffset, request.StderrOffset = &offset, &offset
	page, err := svc.Observe(request)
	if err != nil || page["stdout"] != "-seco" || page["stdout_next_offset"] != 10 || page["stderr"] != "" {
		t.Fatalf("second page = %#v, %v", page, err)
	}
	if _, exists := svc.sessions.Get(id); !exists {
		t.Fatal("read removed the completed session")
	}
	legacy, err := svc.Observe(SessionObserveRequest{Action: "status", SessionID: id})
	if err != nil || legacy["stdout"] != "first-second" {
		t.Fatalf("legacy status output = %#v, %v", legacy, err)
	}
	if _, err := svc.Observe(request); err == nil {
		t.Fatal("read found a session removed by legacy status")
	}
}

func TestObserveRejectsOffsetsOutsideRead(t *testing.T) {
	svc, _ := newCommandTestService(t)
	zero := 0
	for _, action := range []string{"", "list", "status"} {
		if _, err := svc.Observe(SessionObserveRequest{Action: action, StdoutOffset: &zero}); err == nil {
			t.Fatalf("action=%q accepted an ignored output offset", action)
		}
	}
}

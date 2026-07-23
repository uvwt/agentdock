//go:build !windows

package session

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestStartCommandWithTTYRunsDirectCommandFactory(t *testing.T) {
	s, _, err := StartCommandWithTTY(
		context.Background(),
		func(ctx context.Context) *exec.Cmd {
			return exec.CommandContext(ctx, "/bin/sh", "-c", "printf direct-factory")
		},
		5*time.Second,
		false,
		nil,
	)
	if err != nil {
		t.Fatalf("StartCommandWithTTY() error = %v", err)
	}
	defer s.Cancel()
	select {
	case <-s.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("direct command factory did not finish")
	}
	result := s.Snapshot("exited", 1024)
	if result["exit_code"] != 0 || result["stdout"] != "direct-factory" {
		t.Fatalf("direct command result = %#v", result)
	}
}

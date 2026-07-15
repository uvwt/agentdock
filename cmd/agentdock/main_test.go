package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
)

func TestRunPrintsVersionWithoutLoadingServerConfiguration(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run(context.Background(), []string{"--version"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "AgentDock v"+strings.TrimPrefix(config.Version, "v")) || !strings.Contains(stdout.String(), "platform:") {
		t.Fatalf("unexpected version output: %s", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}

func TestRunRejectsUnexpectedUpdateArguments(t *testing.T) {
	err := run(context.Background(), []string{"update", "--check"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "不接受额外参数") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	err := run(context.Background(), []string{"unknown"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "未知命令或参数") {
		t.Fatalf("unexpected error: %v", err)
	}
}

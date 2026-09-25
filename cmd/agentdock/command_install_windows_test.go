//go:build windows

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyDetachedEngineCopiesBytes(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.exe")
	destination := filepath.Join(root, "detached", "engine.exe")
	want := []byte("detached-engine-test")
	if err := os.WriteFile(source, want, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyDetachedEngine(source, destination); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("detached Engine bytes=%q, want %q", got, want)
	}
}

func TestDetachEngineRequiresOutput(t *testing.T) {
	err := runInstallDetachEngine(context.Background(), nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("detach-engine without --output must fail")
	}
}

func TestDetachEngineHandshakeUsesNoConsolePolicy(t *testing.T) {
	source, err := os.ReadFile("command_install_windows.go")
	if err != nil {
		t.Fatalf("read command_install_windows.go: %v", err)
	}
	text := string(source)
	anchor := "exec.CommandContext(verifyCtx, destination, \"install\", \"--engine-ready\")"
	start := strings.Index(text, anchor)
	if start < 0 {
		t.Fatalf("command_install_windows.go missing %q", anchor)
	}
	end := start + 400
	if end > len(text) {
		end = len(text)
	}
	if !strings.Contains(text[start:end], "processcontrol.ConfigureBackground(command)") {
		t.Fatal("detached Installer Engine handshake must use the Windows no-console policy")
	}
}

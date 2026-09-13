//go:build windows

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
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

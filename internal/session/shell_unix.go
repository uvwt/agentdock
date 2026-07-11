//go:build darwin || linux

package session

import (
	"context"
	"os"
	"os/exec"
)

func shellCommand(ctx context.Context, command string) *exec.Cmd {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	return exec.CommandContext(ctx, shell, "-c", command)
}

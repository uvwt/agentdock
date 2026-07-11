//go:build darwin || linux

package session

import (
	"context"
	"io"
	"os/exec"
)

func startInteractiveRunner(_ context.Context, _ *exec.Cmd, _, _ io.Writer) (commandRunner, bool, error) {
	return nil, false, nil
}

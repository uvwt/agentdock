//go:build windows

package installer

import (
	"context"

	"github.com/uvwt/agentdock/internal/desktopruntime"
)

func cloudflaredProcessRunningAtPath(_ context.Context, path string) bool {
	running, err := desktopruntime.BinaryProcessRunning(path)
	return err == nil && running
}

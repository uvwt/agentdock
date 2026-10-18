package desktopruntime

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunTunnelCommandRejectsUnknownAction(t *testing.T) {
	err := RunTunnelCommand(context.Background(), []string{"unknown"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "agentdock tunnel") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunTunnelCommandHelpShowsOptionalRuntimeRoot(t *testing.T) {
	var stdout bytes.Buffer
	if err := RunTunnelCommand(context.Background(), []string{"-h"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if output := stdout.String(); !strings.Contains(output, "agentdock tunnel <start|stop|restart|regenerate> [--runtime-root <目录>]") ||
		!strings.Contains(output, "AGENTDOCK_RUNTIME_ROOT") {
		t.Fatalf("unexpected help output: %s", output)
	}
}

func TestRunTunnelCommandValidatesConfigureModeBeforePlatformAccess(t *testing.T) {
	t.Setenv("AGENTDOCK_RUNTIME_ROOT", t.TempDir())
	err := RunTunnelCommand(
		context.Background(),
		[]string{"configure", "--mode", "invalid"},
		&bytes.Buffer{},
		&bytes.Buffer{},
	)
	if err == nil || !strings.Contains(err.Error(), "mode") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunTunnelCommandValidatesAutostartBooleanBeforePlatformAccess(t *testing.T) {
	t.Setenv("AGENTDOCK_RUNTIME_ROOT", t.TempDir())
	err := RunTunnelCommand(
		context.Background(),
		[]string{"autostart", "--enabled", "yes"},
		&bytes.Buffer{},
		&bytes.Buffer{},
	)
	if err == nil || !strings.Contains(err.Error(), "enabled") {
		t.Fatalf("unexpected error: %v", err)
	}
}

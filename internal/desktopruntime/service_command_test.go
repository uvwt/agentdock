package desktopruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestRunServiceCommandRejectsUnknownAction(t *testing.T) {
	err := RunServiceCommand(context.Background(), []string{"unknown"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "agentdock service") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseRuntimeRootUsesEnvironmentDefaultAndExplicitOverride(t *testing.T) {
	environmentRoot := t.TempDir()
	t.Setenv("AGENTDOCK_RUNTIME_ROOT", environmentRoot)

	got, err := parseRuntimeRoot("agentdock service status", nil, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if got != environmentRoot {
		t.Fatalf("runtime root = %q, want environment root %q", got, environmentRoot)
	}

	explicitRoot := t.TempDir()
	got, err = parseRuntimeRoot("agentdock service status", []string{"--runtime-root", explicitRoot}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if got != explicitRoot {
		t.Fatalf("runtime root = %q, want explicit root %q", got, explicitRoot)
	}
}

func TestRunServiceCommandHelpShowsOptionalRuntimeRoot(t *testing.T) {
	var stdout bytes.Buffer
	if err := RunServiceCommand(context.Background(), []string{"-h"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if output := stdout.String(); !strings.Contains(output, "agentdock service <start|stop|restart> [--runtime-root <目录>]") ||
		!strings.Contains(output, "AGENTDOCK_RUNTIME_ROOT") {
		t.Fatalf("unexpected help output: %s", output)
	}
}

func TestRunServiceCommandValidatesAutostartArgumentsBeforePlatformAccess(t *testing.T) {
	t.Setenv("AGENTDOCK_RUNTIME_ROOT", t.TempDir())
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{
			name: "component",
			args: []string{"autostart", "--component", "tunnel", "--enabled", "true"},
			want: "component",
		},
		{
			name: "enabled",
			args: []string{"autostart", "--component", "core", "--enabled", "yes"},
			want: "enabled",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := RunServiceCommand(context.Background(), test.args, &bytes.Buffer{}, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestRunServiceCommandTaskStartRequiresTaskName(t *testing.T) {
	err := RunServiceCommand(context.Background(), []string{"task-start"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--task-name") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestServiceStatusJSONIncludesNexusConnection(t *testing.T) {
	data, err := json.Marshal(ServiceStatus{NexusConnected: true})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["nexus_connected"] != true {
		t.Fatalf("nexus_connected = %#v, want true", decoded["nexus_connected"])
	}
}

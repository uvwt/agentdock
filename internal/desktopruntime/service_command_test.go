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

func TestRunServiceCommandRequiresRuntimeRoot(t *testing.T) {
	err := RunServiceCommand(context.Background(), []string{"start"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--runtime-root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunServiceCommandValidatesAutostartArgumentsBeforePlatformAccess(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{
			name: "component",
			args: []string{"autostart", "--runtime-root", t.TempDir(), "--component", "tunnel", "--enabled", "true"},
			want: "component",
		},
		{
			name: "enabled",
			args: []string{"autostart", "--runtime-root", t.TempDir(), "--component", "core", "--enabled", "yes"},
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

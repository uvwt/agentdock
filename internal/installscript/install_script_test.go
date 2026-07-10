package installscript

import (
	"os"
	"strings"
	"testing"
)

func TestInstallLinuxWritesExplicitNexusDockWorkflowToken(t *testing.T) {
	data, err := os.ReadFile("../../scripts/install-linux.sh")
	if err != nil {
		t.Fatalf("read install-linux.sh: %v", err)
	}
	script := string(data)
	checks := []string{
		"local nexus_token=\"${10}\"",
		"printf 'AGENTDOCK_NEXUS_TOKEN=%s\\n' \"$nexus_token\"",
		"NexusDock workflow API 是否需要 token？",
		"nexus_token=\"$(prompt_secret 'NexusDock workflow token')\"",
	}
	for _, want := range checks {
		if !strings.Contains(script, want) {
			t.Fatalf("install-linux.sh missing NexusDock workflow token handling: %s", want)
		}
	}
	for _, removed := range []string{"AGENTDOCK_NEXUS_DEVICE_NAME", "AGENTDOCK_NEXUS_HEARTBEAT_SECONDS", "Nexus 设备名"} {
		if strings.Contains(script, removed) {
			t.Fatalf("install-linux.sh still contains removed device-agent config %q", removed)
		}
	}
}

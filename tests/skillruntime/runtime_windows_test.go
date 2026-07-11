//go:build windows

package skillruntime_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/uvwt/agentdock/internal/skillruntime"
	"github.com/uvwt/agentdock/internal/skillstate"
)

func TestWindowsInstallActivateAndRunPowerShellSkill(t *testing.T) {
	root := t.TempDir()
	state, err := skillstate.New(filepath.Join(root, "skills"))
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `apiVersion: agentdock.dev/v1
kind: Skill
metadata:
  name: windows-integration
  version: 1.0.0
  displayName: Windows Integration
  description: Windows PowerShell integration test
spec:
  runtime: powershell
  entrypoint: run.ps1
  operations:
    - name: echo
      description: Echo JSON input
      inputSchema: {"type":"object","required":["message"],"properties":{"message":{"type":"string"}},"additionalProperties":false}
      outputSchema: {"type":"object","required":["message"],"properties":{"message":{"type":"string"}},"additionalProperties":false}
      timeoutSeconds: 10
  compatibility:
    platforms: [windows]
    architectures: [` + runtime.GOARCH + `]
    agentdock: ">=1.0.0"
  permissions:
    filesystem: []
    network: []
    env: []
    commands: []
`
	if err := os.WriteFile(filepath.Join(source, "agentdock.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	document := "---\nname: windows-integration\ndescription: Verify the Windows PowerShell Skill runtime.\n---\n\n# Windows Integration\n"
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	script := `$payload = [Console]::In.ReadToEnd() | ConvertFrom-Json
@{ message = $payload.message } | ConvertTo-Json -Compress
`
	if err := os.WriteFile(filepath.Join(source, "run.ps1"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}

	runtimeInstance, err := skillruntime.New(state, nil)
	if err != nil {
		t.Fatal(err)
	}
	installed, err := runtimeInstance.Install(context.Background(), skillruntime.InstallRequest{
		Source: source, Activate: true, Channel: skillstate.ChannelStable, ConfirmedNoEnv: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if installed.Skill != "windows-integration" || installed.Version != "1.0.0" {
		t.Fatalf("installed = %#v", installed)
	}
	result, err := runtimeInstance.Run(context.Background(), skillruntime.RunRequest{
		Skill: "windows-integration", Operation: "echo", Input: json.RawMessage(`{"message":"windows-ok"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || string(result.Output) != `{"message":"windows-ok"}` {
		t.Fatalf("result = %#v", result)
	}
}

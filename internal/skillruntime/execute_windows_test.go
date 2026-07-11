//go:build windows

package skillruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/uvwt/agentdock/internal/skillstate"
)

func TestWindowsPowerShellSkillRunsEndToEnd(t *testing.T) {
	root := t.TempDir()
	state, err := skillstate.New(filepath.Join(root, "skills"))
	if err != nil {
		t.Fatal(err)
	}
	packageDir, err := state.InstalledPath("windows-test", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(packageDir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{
		APIVersion: ManifestAPIVersion,
		Kind:       ManifestKind,
		Metadata: Metadata{
			Name: "windows-test", Version: "1.0.0", DisplayName: "Windows Test", Description: "PowerShell runtime test",
		},
		Spec: Spec{
			Runtime: RuntimePowerShell, Entrypoint: "run.ps1",
			Operations: []Operation{{
				Name: "echo", Description: "Echo input", TimeoutSeconds: 10,
				InputSchema: map[string]any{
					"type": "object", "required": []string{"message"},
					"properties":           map[string]any{"message": map[string]any{"type": "string"}},
					"additionalProperties": false,
				},
				OutputSchema: map[string]any{
					"type": "object", "required": []string{"message"},
					"properties":           map[string]any{"message": map[string]any{"type": "string"}},
					"additionalProperties": false,
				},
			}},
			Compatibility: Compatibility{Platforms: []string{"windows"}, Architectures: []string{runtime.GOARCH}, AgentDock: ">=1.0.0"},
			Permissions:   Permissions{},
		},
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "agentdock.yaml"), manifestData, 0o600); err != nil {
		t.Fatal(err)
	}
	document := "---\nname: windows-test\ndescription: Run the Windows PowerShell Skill runtime test.\n---\n\n# Windows Test\n"
	if err := os.WriteFile(filepath.Join(packageDir, "SKILL.md"), []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	script := `$raw = [Console]::In.ReadToEnd()
$payload = $raw | ConvertFrom-Json
@{ message = $payload.message } | ConvertTo-Json -Compress
`
	if err := os.WriteFile(filepath.Join(packageDir, "run.ps1"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := state.Activate(context.Background(), "windows-test", "1.0.0", skillstate.ChannelStable); err != nil {
		t.Fatal(err)
	}
	runtimeInstance, err := New(state, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtimeInstance.Run(context.Background(), RunRequest{
		Skill: "windows-test", Operation: "echo", Input: json.RawMessage(`{"message":"中文消息"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || string(result.Output) != `{"message":"中文消息"}` {
		t.Fatalf("result = %#v", result)
	}
}

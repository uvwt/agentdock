package skillruntime

import (
	"runtime"
	"testing"
)

func TestParseManifestV1(t *testing.T) {
	manifest, err := ParseManifest([]byte(`
apiVersion: agentdock.dev/v1
kind: Skill
metadata:
  name: demo-skill
  version: 1.0.0
  displayName: Demo
  description: Demo skill
spec:
  entrypoint: run.sh
  operations:
    - name: echo
      description: Echo input
      inputSchema: {"type":"object","required":["message"],"properties":{"message":{"type":"string"}},"additionalProperties":false}
      outputSchema: {"type":"object","required":["message"],"properties":{"message":{"type":"string"}},"additionalProperties":false}
      timeoutSeconds: 5
  compatibility:
    platforms: [` + runtime.GOOS + `]
    architectures: [` + runtime.GOARCH + `]
    agentdock: ">=1.0.0"
  permissions:
    filesystem: []
    network: []
    secrets: []
    commands: [sh]
`))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Metadata.Name != "demo-skill" || len(manifest.Spec.Operations) != 1 {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}
}

func TestValidateJSONRejectsUnknownProperty(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}}, "additionalProperties": false}
	if err := ValidateJSON(schema, []byte(`{"ok":true,"extra":1}`)); err == nil {
		t.Fatal("expected validation error")
	}
}

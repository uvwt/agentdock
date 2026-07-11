package skillruntime

import (
	"runtime"
	"strings"
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
  runtime: binary
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
    env:
      - name: DEMO_BASE_URL
        kind: plain
      - name: DEMO_API_TOKEN
        kind: secret
    commands: [sh]
`))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Metadata.Name != "demo-skill" || len(manifest.Spec.Operations) != 1 {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}
	if len(manifest.Spec.Permissions.Env) != 2 {
		t.Fatalf("permissions.env count = %d, want 2", len(manifest.Spec.Permissions.Env))
	}
}

func TestParseManifestRejectsInvalidEnvDeclarations(t *testing.T) {
	_, err := ParseManifest([]byte(`
apiVersion: agentdock.dev/v1
kind: Skill
metadata:
  name: demo-skill
  version: 1.0.0
  displayName: Demo
  description: Demo skill
spec:
  runtime: binary
  entrypoint: run.sh
  operations:
    - name: echo
      description: Echo input
      inputSchema: {"type":"object","additionalProperties":false}
      outputSchema: {"type":"object","additionalProperties":false}
      timeoutSeconds: 5
  compatibility:
    platforms: [` + runtime.GOOS + `]
    architectures: [` + runtime.GOARCH + `]
    agentdock: ">=1.0.0"
  permissions:
    filesystem: []
    network: []
    env:
      - name: DEMO_TOKEN
        kind: token
      - name: DEMO_TOKEN
        kind: secret
    commands: [sh]
`))
	if err == nil {
		t.Fatal("expected invalid env declaration error")
	}
	message := err.Error()
	for _, want := range []string{
		"spec.permissions.env[0].kind: must be plain or secret",
		"spec.permissions.env[1].name: duplicate env name",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("error %q does not contain %q", message, want)
		}
	}
}

func TestParseManifestRejectsDeprecatedSecretsField(t *testing.T) {
	_, err := ParseManifest([]byte(`
apiVersion: agentdock.dev/v1
kind: Skill
metadata:
  name: demo-skill
  version: 1.0.0
  displayName: Demo
  description: Demo skill
spec:
  runtime: binary
  entrypoint: run.sh
  operations:
    - name: echo
      description: Echo input
      inputSchema: {"type":"object","additionalProperties":false}
      outputSchema: {"type":"object","additionalProperties":false}
      timeoutSeconds: 5
  compatibility:
    platforms: [` + runtime.GOOS + `]
    architectures: [` + runtime.GOARCH + `]
    agentdock: ">=1.0.0"
  permissions:
    filesystem: []
    network: []
    env:
      - name: DEMO_TOKEN
        kind: secret
    secrets: [DEMO_TOKEN]
    commands: [sh]
`))
	if err == nil {
		t.Fatal("expected deprecated secrets field error")
	}
	want := "spec.permissions.secrets: deprecated; declare secret variables in spec.permissions.env with kind=secret"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err.Error(), want)
	}
}

func TestValidateJSONRejectsUnknownProperty(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}}, "additionalProperties": false}
	if err := ValidateJSON(schema, []byte(`{"ok":true,"extra":1}`)); err == nil {
		t.Fatal("expected validation error")
	}
}

func FuzzParseManifestDoesNotPanic(f *testing.F) {
	f.Add([]byte(""))
	f.Add([]byte("apiVersion: agentdock.dev/v1\nkind: Skill\n"))
	f.Add([]byte("spec:\n  operations:\n    - name: test\n      inputSchema: {}\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		_, _ = ParseManifest(data)
	})
}

func TestWindowsCompatibilityRequiresExplicitRuntime(t *testing.T) {
	manifest := Manifest{
		APIVersion: ManifestAPIVersion,
		Kind:       ManifestKind,
		Metadata: Metadata{
			Name: "demo-skill", Version: "1.0.0", DisplayName: "Demo", Description: "Demo skill",
		},
		Spec: Spec{
			Entrypoint: "run.py",
			Operations: []Operation{{
				Name: "status", Description: "Status", TimeoutSeconds: 5,
				InputSchema: map[string]any{"type": "object"}, OutputSchema: map[string]any{"type": "object"},
			}},
			Compatibility: Compatibility{Platforms: []string{"windows"}, Architectures: []string{"amd64"}},
			Permissions:   Permissions{},
		},
	}
	if err := ValidateManifest(manifest); err == nil || !strings.Contains(err.Error(), "spec.runtime: is required") {
		t.Fatalf("ValidateManifest() error = %v", err)
	}
	manifest.Spec.Runtime = RuntimePython
	if err := ValidateManifest(manifest); err != nil {
		t.Fatalf("ValidateManifest() with explicit runtime: %v", err)
	}
}

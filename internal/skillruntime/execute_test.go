package skillruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/uvwt/agentdock/internal/skillstate"
)

func TestRunTruncatesLargeJSONWithoutFailing(t *testing.T) {
	rt := newTestRuntime(t, `#!/bin/sh
printf '{"ok":true,"items":["'
i=0
while [ "$i" -lt 400 ]; do
  printf 'abcdefghij'
  i=$((i + 1))
done
printf '"]}\n'
`)

	result, err := rt.Run(context.Background(), RunRequest{
		Skill:     "output-test",
		Operation: "run",
		Input:     json.RawMessage(`{}`),
		Timeout:   10 * time.Second,
		MaxOutput: 512,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected successful result: %#v", result)
	}
	if !result.Truncated || !result.StdoutTruncated || result.StderrTruncated {
		t.Fatalf("unexpected truncation flags: %#v", result)
	}
	if result.StdoutBytes <= 512 {
		t.Fatalf("expected stdout bytes above limit, got %d", result.StdoutBytes)
	}
	if len(result.Stdout) > 512 {
		t.Fatalf("stdout exceeds limit: %d", len(result.Stdout))
	}
	if len(result.Output) > 512 {
		t.Fatalf("output preview exceeds limit: %d", len(result.Output))
	}
	if !json.Valid(result.Output) {
		t.Fatalf("output preview is not valid JSON: %q", result.Output)
	}
}

func TestRunTruncatesStderrButKeepsValidOutput(t *testing.T) {
	rt := newTestRuntime(t, `#!/bin/sh
i=0
while [ "$i" -lt 100 ]; do
  printf 'stderr-data' >&2
  i=$((i + 1))
done
printf '{"ok":true}\n'
`)

	result, err := rt.Run(context.Background(), RunRequest{
		Skill:     "output-test",
		Operation: "run",
		Input:     json.RawMessage(`{}`),
		Timeout:   10 * time.Second,
		MaxOutput: 128,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !result.OK || !result.Truncated || result.StdoutTruncated || !result.StderrTruncated {
		t.Fatalf("unexpected result: %#v", result)
	}
	if string(result.Output) != `{"ok":true}` {
		t.Fatalf("unexpected output: %s", result.Output)
	}
	if len(result.Stderr) > 128 {
		t.Fatalf("stderr exceeds limit: %d", len(result.Stderr))
	}
}

func TestRunPreservesSmallJSON(t *testing.T) {
	rt := newTestRuntime(t, "#!/bin/sh\nprintf '{\"ok\":true,\"message\":\"done\"}\\n'\n")

	result, err := rt.Run(context.Background(), RunRequest{
		Skill:     "output-test",
		Operation: "run",
		Input:     json.RawMessage(`{}`),
		Timeout:   10 * time.Second,
		MaxOutput: 1024,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !result.OK || result.Truncated {
		t.Fatalf("unexpected result: %#v", result)
	}
	var output map[string]any
	if err := json.Unmarshal(result.Output, &output); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if got, ok := output["ok"].(bool); !ok || !got {
		t.Fatalf("unexpected ok field: %#v", output["ok"])
	}
	if got, ok := output["message"].(string); !ok || got != "done" {
		t.Fatalf("unexpected message field: %#v", output["message"])
	}
	if _, ok := output["truncated"]; ok {
		t.Fatalf("small output should not use truncated fallback: %s", result.Output)
	}
}

func newTestRuntime(t *testing.T, script string) *Runtime {
	t.Helper()
	root := t.TempDir()
	state, err := skillstate.New(filepath.Join(root, "skills"))
	if err != nil {
		t.Fatal(err)
	}
	packageDir, err := state.InstalledPath("output-test", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(packageDir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"apiVersion": ManifestAPIVersion,
		"kind":       ManifestKind,
		"metadata": map[string]any{
			"name":        "output-test",
			"version":     "1.0.0",
			"displayName": "Output Test",
			"description": "Output test skill",
		},
		"spec": map[string]any{
			"entrypoint": "run.sh",
			"operations": []any{map[string]any{
				"name":        "run",
				"description": "Run output test",
				"inputSchema": map[string]any{"type": "object", "additionalProperties": false},
				"outputSchema": map[string]any{
					"type":                 "object",
					"properties":           map[string]any{"ok": map[string]any{"type": "boolean"}},
					"required":             []string{"ok"},
					"additionalProperties": true,
				},
				"timeoutSeconds": 10,
			}},
			"compatibility": map[string]any{
				"platforms":     []string{runtime.GOOS},
				"architectures": []string{runtime.GOARCH},
				"agentdock":     ">=1.0.0",
			},
			"permissions": map[string]any{
				"filesystem": []string{},
				"network":    []string{},
				"secrets":    []string{},
				"commands":   []string{"sh"},
			},
		},
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "agentdock.yaml"), manifestData, 0o600); err != nil {
		t.Fatal(err)
	}
	entrypoint := filepath.Join(packageDir, "run.sh")
	if err := os.WriteFile(entrypoint, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := state.Activate(context.Background(), "output-test", "1.0.0", skillstate.ChannelStable); err != nil {
		t.Fatal(err)
	}
	rt, err := New(state, nil)
	if err != nil {
		t.Fatal(err)
	}
	return rt
}

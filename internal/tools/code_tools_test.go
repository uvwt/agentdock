package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/uvwt/agentdock/internal/config"
)

func newCodeToolsRuntime(t *testing.T) (*Runtime, string) {
	t.Helper()
	root := t.TempDir()
	cfg := config.Config{
		Workspace:       root,
		ToolProfile:     config.ProfileFull,
		Mode:            config.ModeSandboxed,
		PathPolicy:      config.PathPolicyWorkspace,
		AgentDockDir:    "AgentDock",
		EnableViewImage: true,
	}
	cfg.Normalize()
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return rt, root
}

func TestServerInfoRecommendsCompactRecallBootstrap(t *testing.T) {
	root := t.TempDir()
	cfg := config.Config{
		Workspace:       root,
		ToolProfile:     config.ProfileFull,
		Mode:            config.ModeSandboxed,
		PathPolicy:      config.PathPolicyWorkspace,
		AgentDockDir:    "AgentDock",
		RecallEndpoint:  "http://127.0.0.1:18777",
		RecallTimeoutMS: 30000,
	}
	cfg.Normalize()
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	info := rt.serverInfo()
	args := info["recall_bootstrap_args"].(map[string]any)
	if _, ok := args["max_bytes"]; ok {
		t.Fatalf("server_info should not recommend explicit max_bytes because that disables compact bootstrap defaults: %#v", args)
	}
	if _, ok := args["project"]; ok {
		t.Fatalf("server_info should not recommend project because recall_bootstrap hides project selection: %#v", args)
	}
}

func TestServerInfoReportsOAuthAuthEnabled(t *testing.T) {
	root := t.TempDir()
	cfg := config.Config{
		Workspace:      root,
		ToolProfile:    config.ProfileFull,
		Mode:           config.ModeSandboxed,
		PathPolicy:     config.PathPolicyWorkspace,
		AgentDockDir:   "AgentDock",
		OAuthServerURL: "https://auth.example.test",
	}
	cfg.Normalize()
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	info := rt.serverInfo()
	if info["auth_enabled"] != true {
		t.Fatalf("auth_enabled = %#v, want true", info["auth_enabled"])
	}
}

func TestBrowserRunnerReceivesPayloadEnvWithoutSkipPermissions(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is required for browser runner")
	}
	root := t.TempDir()
	runnerDir := filepath.Join(root, "AgentDock", "browser-runner")
	if err := os.MkdirAll(runnerDir, 0o700); err != nil {
		t.Fatal(err)
	}
	script := `const payload = JSON.parse(process.env.BROWSER_RUNNER_PAYLOAD || "{}");
process.stdout.write(JSON.stringify({
  ok: Boolean(process.env.BROWSER_RUNNER_PAYLOAD),
  operation: payload.operation,
  workspace: payload.workspace,
  artifact_dir: payload.artifact_dir,
  env_workspace: process.env.WORKSPACE,
  artifact_env: process.env.BROWSER_ARTIFACT_DIR
}));`
	if err := os.WriteFile(filepath.Join(runnerDir, "browser-runner.js"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Workspace:                     root,
		ToolProfile:                   config.ProfileFull,
		Mode:                          config.ModeSandboxed,
		PathPolicy:                    config.PathPolicyWorkspace,
		AgentDockDir:                  "AgentDock",
		BrowserEnabled:                true,
		BrowserRunnerDir:              "browser-runner",
		BrowserArtifactDir:            "browser-artifacts",
		DangerouslySkipAllPermissions: false,
	}
	cfg.Normalize()
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	result, err := rt.Call(context.Background(), "browser_snapshot", map[string]any{"timeout_ms": 5000})
	if err != nil {
		t.Fatal(err)
	}
	if result["ok"] != true {
		t.Fatalf("browser runner did not receive payload: %#v", result)
	}
	if result["operation"] != "snapshot" {
		t.Fatalf("operation = %#v, want snapshot", result["operation"])
	}
	if result["workspace"] == "" || result["workspace"] != result["env_workspace"] {
		t.Fatalf("workspace env mismatch: %#v", result)
	}
	if result["artifact_env"] == "" || result["artifact_env"] != result["artifact_dir"] {
		t.Fatalf("artifact env mismatch: %#v", result)
	}
}

func TestReadFileReturnsNextStartLineOnTruncation(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("第一行\n第二行\n第三行\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := rt.readFile(map[string]any{"path": "notes.txt", "max_bytes": 13})
	if err != nil {
		t.Fatal(err)
	}
	content := result["content"].(string)
	if !utf8.ValidString(content) {
		t.Fatalf("content is invalid UTF-8: %q", content)
	}
	if result["truncated"] != true || result["truncated_reason"] != "max_bytes" {
		t.Fatalf("expected truncation metadata, got %#v", result)
	}
	if _, ok := result["next_start_line"].(int); !ok {
		t.Fatalf("expected next_start_line, got %#v", result)
	}
}

func TestEditFileReplacesSingleMatch(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc main() {}\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	result, err := rt.editFile(map[string]any{"path": "main.go", "old": "func main() {}", "new": "func main() { println(\"ok\") }"})
	if err != nil {
		t.Fatal(err)
	}
	if result["changed"] != true || result["matches"] != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "println") {
		t.Fatalf("file was not edited: %s", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("mode = %v, want 0640", got)
	}
}

func TestEditFileDryRunDoesNotWrite(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("alpha\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := rt.editFile(map[string]any{"path": "main.go", "old": "alpha", "new": "beta", "dry_run": true})
	if err != nil {
		t.Fatal(err)
	}
	if result["changed"] != true || !strings.Contains(result["diff_preview"].(string), "beta") {
		t.Fatalf("unexpected dry-run result: %#v", result)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "alpha\n" {
		t.Fatalf("dry-run wrote file: %q", data)
	}
}

func TestEditFileRejectsUnexpectedMatchCounts(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("alpha\nalpha\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.editFile(map[string]any{"path": "main.go", "old": "alpha", "new": "beta"}); err == nil {
		t.Fatalf("expected multi-match error")
	}
	if _, err := rt.editFile(map[string]any{"path": "main.go", "old": "missing", "new": "beta"}); err == nil {
		t.Fatalf("expected zero-match error")
	}
}

func TestEditFileReplaceAll(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("alpha\nalpha\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.editFile(map[string]any{"path": "main.go", "old": "alpha", "new": "beta", "replace_all": true, "expected_matches": 2}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "beta\nbeta\n" {
		t.Fatalf("replace_all result = %q", data)
	}
}

func TestEditFileRejectsBinaryAndInvalidUTF8(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	if err := os.WriteFile(filepath.Join(root, "bin.dat"), []byte{0, 1, 2}, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.editFile(map[string]any{"path": "bin.dat", "old": "x", "new": "y"}); err == nil {
		t.Fatalf("expected binary rejection")
	}
	if err := os.WriteFile(filepath.Join(root, "bad.txt"), []byte{0xff, 'x'}, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.editFile(map[string]any{"path": "bad.txt", "old": "x", "new": "y"}); err == nil {
		t.Fatalf("expected invalid UTF-8 rejection")
	}
}

func TestSearchTextGoFallbackIncludesColumnsAndContext(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("one\nTwo needle\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := rt.ws.ResolveExisting(".")
	if err != nil {
		t.Fatal(err)
	}
	result, err := rt.searchTextGo(p, searchOptions{Query: "needle", MaxResults: 10, ContextLines: 1})
	if err != nil {
		t.Fatal(err)
	}
	matches := result["matches"].([]map[string]any)
	if len(matches) != 1 {
		t.Fatalf("matches = %#v", matches)
	}
	if matches[0]["column"] != 5 || matches[0]["match_text"] != "needle" {
		t.Fatalf("missing column/match_text: %#v", matches[0])
	}
	if matches[0]["context_start_line"] != 1 || matches[0]["context_end_line"] != 3 {
		t.Fatalf("missing context range: %#v", matches[0])
	}
}

func TestParseRGJSONIncludesColumnsAndContext(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	abs := filepath.Join(root, "main.go")
	output := strings.Join([]string{
		`{"type":"context","data":{"path":{"text":"` + abs + `"},"lines":{"text":"before\n"},"line_number":1}}`,
		`{"type":"match","data":{"path":{"text":"` + abs + `"},"lines":{"text":"needle here\n"},"line_number":2,"submatches":[{"match":{"text":"needle"},"start":0,"end":6}]}}`,
		`{"type":"context","data":{"path":{"text":"` + abs + `"},"lines":{"text":"after\n"},"line_number":3}}`,
	}, "\n")
	matches, truncated, ok := rt.parseRGJSON([]byte(output), searchOptions{Query: "needle", MaxResults: 10, ContextLines: 1})
	if !ok || truncated || len(matches) != 1 {
		t.Fatalf("parseRGJSON = matches=%#v truncated=%v ok=%v", matches, truncated, ok)
	}
	if !strings.HasSuffix(matches[0]["path"].(string), "main.go") || matches[0]["column"] != 1 || matches[0]["match_text"] != "needle" {
		t.Fatalf("missing rg fields: %#v", matches[0])
	}
	if matches[0]["context_start_line"] != 1 || matches[0]["context_end_line"] != 3 {
		t.Fatalf("missing rg context range: %#v", matches[0])
	}
}

func TestApplyEnvelopePatchDryRunAndDiagnostics(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("alpha\nbeta\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	patch := "*** Begin Patch\n*** Update File: main.go\n@@\n-alpha\n+ALPHA\n*** End Patch"
	result, err := rt.applyPatch(context.Background(), map[string]any{"patch": patch, "dry_run": true})
	if err != nil {
		t.Fatal(err)
	}
	if result["dry_run"] != true || !strings.Contains(result["diff_preview"].(string), "ALPHA") {
		t.Fatalf("unexpected patch dry-run: %#v", result)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "alpha\nbeta\n" {
		t.Fatalf("dry-run wrote file: %q", data)
	}

	_, err = rt.applyPatch(context.Background(), map[string]any{"patch": "*** Begin Patch\n*** Update File: main.go\n@@\n-missing\n+value\n*** End Patch"})
	if err == nil {
		t.Fatalf("expected context diagnostic")
	}
	if toolErr, ok := err.(*ToolError); !ok || toolErr.Details["diagnostic"] == nil {
		t.Fatalf("missing diagnostic: %#v", err)
	}
}

func TestApplyUnifiedDiffDryRunDoesNotWrite(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("alpha\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	patch := "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-alpha\n+beta\n"
	result, err := rt.applyPatch(context.Background(), map[string]any{"patch": patch, "dry_run": true})
	if err != nil {
		t.Fatal(err)
	}
	if result["dry_run"] != true || result["insertions"] != 1 || result["deletions"] != 1 {
		t.Fatalf("unexpected unified dry-run result: %#v", result)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "alpha\n" {
		t.Fatalf("dry-run wrote file: %q", data)
	}
}

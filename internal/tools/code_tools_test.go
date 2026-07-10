package tools

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/publicartifacts"
	"github.com/uvwt/agentdock/internal/taskstate"
)

func newCodeToolsRuntime(t *testing.T) (*Runtime, string) {
	t.Helper()
	root := t.TempDir()
	cfg := config.Config{
		AgentDockDefaultDir: root, AgentDockHome: filepath.Join(root, ".agentdock"),
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	server := newWorkflowTemplateNexusTestServer(t, rt.tasks)
	rt.cfg.NexusEndpoint = server.URL
	return rt, root
}

func newWorkflowTemplateNexusTestServer(t *testing.T, _ *taskstate.Store) *httptest.Server {
	t.Helper()
	templates := map[string]taskstate.Template{}
	key := func(id, version string) string { return id + "@" + version }
	write := func(w http.ResponseWriter, payload map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}
	listSummaries := func(status taskstate.TemplateStatus) []map[string]any {
		items := []map[string]any{}
		for _, template := range templates {
			if status != "" && template.Status != status {
				continue
			}
			items = append(items, compactTemplateSummary(template))
		}
		return items
	}
	matchTemplates := func(goal, device, taskType string) []taskstate.TemplateCandidate {
		query := strings.ToLower(goal + " " + device + " " + taskType)
		candidates := []taskstate.TemplateCandidate{}
		for _, template := range templates {
			if template.Status != taskstate.TemplateActive {
				continue
			}
			score := 0
			reasons := []string{}
			for _, keyword := range template.Match.Keywords {
				if keyword != "" && strings.Contains(query, strings.ToLower(keyword)) {
					score += 15
					reasons = append(reasons, "keyword:"+keyword)
				}
			}
			if taskType != "" && template.Match.Type == taskType {
				score += 80
				reasons = append(reasons, "type:"+taskType)
			}
			if device != "" {
				for _, candidateDevice := range template.Match.Devices {
					if candidateDevice == device {
						score += 5
						reasons = append(reasons, "device:"+device)
					}
				}
			}
			if score > 0 {
				candidates = append(candidates, taskstate.TemplateCandidate{ID: template.ID, Version: template.Version, Score: score, Reason: strings.Join(reasons, ", ")})
			}
		}
		return candidates
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/workflow-templates", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		items := listSummaries(taskstate.TemplateStatus(r.URL.Query().Get("status")))
		write(w, map[string]any{"ok": true, "items": items, "templates": items, "count": len(items)})
	})
	mux.HandleFunc("/v1/workflow-templates/drafts", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Template taskstate.Template `json:"template"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			write(w, map[string]any{"error": map[string]any{"message": err.Error()}})
			return
		}
		template := req.Template
		template.Status = taskstate.TemplateDraft
		template.Hash = ""
		template.PublishedAt = nil
		template.RetiredAt = nil
		templates[key(template.ID, template.Version)] = template
		write(w, map[string]any{"ok": true, "template": template, "template_summary": compactTemplateSummary(template)})
	})
	mux.HandleFunc("/v1/workflow-templates/match", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Goal, Device, Type string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		candidates := matchTemplates(req.Goal, req.Device, req.Type)
		result := map[string]any{"ok": true, "action": "match", "candidates": candidates, "count": len(candidates)}
		for key, value := range templateMatchRecommendation(candidates) {
			result[key] = value
		}
		write(w, result)
	})
	mux.HandleFunc("/v1/workflow-templates/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/workflow-templates/"), "/"), "/")
		if len(parts) < 2 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		id, version := parts[0], parts[1]
		template, ok := templates[key(id, version)]
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			write(w, map[string]any{"error": map[string]any{"message": "template not found"}})
			return
		}
		if r.Method == http.MethodPost && len(parts) == 3 {
			switch parts[2] {
			case "validate":
			case "publish":
				template.Status = taskstate.TemplateActive
				template.Hash = "sha256:" + template.ID + "@" + template.Version
				templates[key(id, version)] = template
			case "retire":
				template.Status = taskstate.TemplateRetired
				templates[key(id, version)] = template
			default:
				w.WriteHeader(http.StatusBadRequest)
				write(w, map[string]any{"error": map[string]any{"message": "unknown action"}})
				return
			}
		} else if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		write(w, map[string]any{"ok": true, "template": template, "template_summary": compactTemplateSummary(template)})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestViewImageDefaultsToSignedURLWithoutBase64(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	imagePath := filepath.Join(root, "tiny.png")
	writeTinyPNG(t, imagePath)

	result, err := rt.Call(context.Background(), "view_image", map[string]any{"path": "tiny.png", "format": "png"})
	if err != nil {
		t.Fatal(err)
	}
	if result["return_mode"] != "url" {
		t.Fatalf("return_mode = %#v, want url", result["return_mode"])
	}
	if _, ok := result["inline"]; ok {
		t.Fatalf("default view_image returned inline image data: %#v", result)
	}
	for _, forbidden := range []string{"data_base64", "data_url", "_mcp_image_base64"} {
		if _, ok := result[forbidden]; ok {
			t.Fatalf("default view_image returned %s: %#v", forbidden, result)
		}
	}
	imageObject, ok := result["image"].(map[string]any)
	if !ok {
		t.Fatalf("image object missing: %#v", result)
	}
	rawURL, _ := imageObject["url"].(string)
	if !strings.Contains(rawURL, "/artifacts/public/") || !strings.Contains(rawURL, "sig=") {
		t.Fatalf("signed url missing: %#v", imageObject)
	}
	body, status := downloadPublicArtifact(t, rt, rawURL)
	if status != http.StatusOK {
		t.Fatalf("download status = %d body=%q", status, body)
	}
	if len(body) == 0 {
		t.Fatalf("downloaded image is empty")
	}
}

func TestBrowserScreenshotIsPublishedAndScratchFieldsAreRemoved(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is required for browser runner")
	}
	root := t.TempDir()
	runnerDir := filepath.Join(root, ".agentdock", "browser-runner")
	if err := os.MkdirAll(runnerDir, 0o700); err != nil {
		t.Fatal(err)
	}
	script := `const fs = require('fs');
const path = require('path');
const payload = JSON.parse(process.env.BROWSER_RUNNER_PAYLOAD || '{}');
const dir = path.join(payload.artifact_dir, 'screenshots');
fs.mkdirSync(dir, { recursive: true });
const file = 'snapshot-test.png';
const screenshotPath = path.join(dir, file);
fs.writeFileSync(screenshotPath, Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADUlEQVR4nGJgYGD4DwABBAEAgh7R8QAAAABJRU5ErkJggg==', 'base64'));
process.stdout.write(JSON.stringify({ ok: true, operation: payload.operation, screenshot_path: screenshotPath, screenshot_file: file, artifact: { path: screenshotPath } }));`
	if err := os.WriteFile(filepath.Join(runnerDir, "browser-runner.js"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		AgentDockDefaultDir: root,
		AgentDockHome:       filepath.Join(root, ".agentdock"),
		BrowserEnabled:      true,
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	result, err := rt.Call(context.Background(), "browser_snapshot", map[string]any{"timeout_ms": 5000})
	if err != nil {
		t.Fatal(err)
	}
	if result["screenshot_return_mode"] != "url" {
		t.Fatalf("screenshot_return_mode = %#v, want url", result["screenshot_return_mode"])
	}
	if _, ok := result["stdout"]; ok {
		t.Fatalf("parsed browser output should not echo raw stdout: %#v", result)
	}
	for _, forbidden := range []string{"artifact", "screenshot_path", "screenshot_file", "screenshot_artifact_id", "image_base64", "screenshot_base64"} {
		if _, ok := result[forbidden]; ok {
			t.Fatalf("browser result kept scratch field %s: %#v", forbidden, result)
		}
	}
	screenshot, ok := result["screenshot"].(map[string]any)
	if !ok {
		t.Fatalf("screenshot object missing: %#v", result)
	}
	rawURL, _ := screenshot["url"].(string)
	if !strings.Contains(rawURL, "/artifacts/public/") || !strings.Contains(rawURL, "sig=") {
		t.Fatalf("signed screenshot url missing: %#v", screenshot)
	}
	body, status := downloadPublicArtifact(t, rt, rawURL)
	if status != http.StatusOK || len(body) == 0 {
		t.Fatalf("download status = %d len=%d", status, len(body))
	}
}

func writeTinyPNG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, img); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func downloadPublicArtifact(t *testing.T, rt *Runtime, rawURL string) ([]byte, int) {
	t.Helper()
	store := publicartifacts.New(rt.cfg.AgentDockHome, rt.cfg.OAuthServerURL, rt.cfg.Port)
	req := httptest.NewRequest(http.MethodGet, rawURL, nil)
	recorder := httptest.NewRecorder()
	store.ServeHTTP(recorder, req, "/artifacts/public/")
	return recorder.Body.Bytes(), recorder.Code
}

func TestServerInfoRecommendsCompactRecallBootstrap(t *testing.T) {
	root := t.TempDir()
	cfg := config.Config{
		AgentDockDefaultDir: root, AgentDockHome: filepath.Join(root, ".agentdock"),
		RecallEndpoint: "http://127.0.0.1:18777",
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
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

func TestServerInfoServerURLAloneDoesNotEnableAuth(t *testing.T) {
	root := t.TempDir()
	cfg := config.Config{
		AgentDockDefaultDir: root, AgentDockHome: filepath.Join(root, ".agentdock"),
		OAuthServerURL: "https://auth.example.test",
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	info := rt.serverInfo()
	if info["auth_enabled"] != false {
		t.Fatalf("auth_enabled = %#v, want false", info["auth_enabled"])
	}
}

func TestServerInfoReportsOAuthClientIDAuthEnabled(t *testing.T) {
	root := t.TempDir()
	cfg := config.Config{
		AgentDockDefaultDir: root, AgentDockHome: filepath.Join(root, ".agentdock"),
		OAuthClientID: "client-id", OAuthServerURL: "https://auth.example.test",
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	info := rt.serverInfo()
	if info["auth_enabled"] != true {
		t.Fatalf("auth_enabled = %#v, want true", info["auth_enabled"])
	}
}

func TestBrowserRunnerReceivesPayloadEnv(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is required for browser runner")
	}
	root := t.TempDir()
	runnerDir := filepath.Join(root, ".agentdock", "browser-runner")
	if err := os.MkdirAll(runnerDir, 0o700); err != nil {
		t.Fatal(err)
	}
	script := `const payload = JSON.parse(process.env.BROWSER_RUNNER_PAYLOAD || "{}");
process.stdout.write(JSON.stringify({
  ok: Boolean(process.env.BROWSER_RUNNER_PAYLOAD),
  operation: payload.operation,
  default_dir: payload.default_dir,
  artifact_dir: payload.artifact_dir,
  env_default_dir: process.env.AGENTDOCK_DEFAULT_DIR,
  artifact_env: process.env.BROWSER_ARTIFACT_DIR
}));`
	if err := os.WriteFile(filepath.Join(runnerDir, "browser-runner.js"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		AgentDockDefaultDir: root,
		AgentDockHome:       filepath.Join(root, ".agentdock"),
		BrowserEnabled:      true,
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
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
	if result["default_dir"] == "" || result["default_dir"] != result["env_default_dir"] {
		t.Fatalf("default dir env mismatch: %#v", result)
	}
	if result["artifact_env"] == "" || result["artifact_env"] != result["artifact_dir"] {
		t.Fatalf("artifact env mismatch: %#v", result)
	}
}

func TestExecCommandDoesNotFilterCommandContent(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	result, err := rt.execCommand(context.Background(), map[string]any{
		"cmd":             `printf 'shell=%s network=%s\n' "$(printf expansion)" "https://example.test"`,
		"yield_time_ms":   5000,
		"timeout_ms":      5000,
		"wait_until_exit": true,
	})
	if err != nil {
		t.Fatalf("exec_command should not reject command content: %v", err)
	}
	if result["status"] != "exited" || !strings.Contains(result["stdout"].(string), "shell=expansion network=https://example.test") {
		t.Fatalf("unexpected command result: %#v", result)
	}
}

func TestExecCommandForwardsExplicitEnv(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	result, err := rt.execCommand(context.Background(), map[string]any{
		"cmd":             `printf '%s' "$AGENTDOCK_TEST_EXEC_ENV"`,
		"env":             map[string]any{"AGENTDOCK_TEST_EXEC_ENV": "forwarded"},
		"yield_time_ms":   5000,
		"timeout_ms":      5000,
		"wait_until_exit": true,
	})
	if err != nil {
		t.Fatalf("exec_command should accept explicit env: %v", err)
	}
	if result["status"] != "exited" || result["stdout"].(string) != "forwarded" {
		t.Fatalf("explicit env was not forwarded: %#v", result)
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

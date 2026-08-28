package app

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/taskstate"
	tooltask "github.com/uvwt/agentdock/internal/tool/task"
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
	rt.cfg.NexusDeviceToken = "test-device-token"
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
			items = append(items, tooltask.CompactTemplateSummary(template))
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
	mux.HandleFunc("/v1/workflow-templates/publish", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Template taskstate.Template `json:"template"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			write(w, map[string]any{"error": map[string]any{"message": err.Error()}})
			return
		}
		template := req.Template
		if _, exists := templates[key(template.ID, template.Version)]; exists {
			w.WriteHeader(http.StatusConflict)
			write(w, map[string]any{"error": map[string]any{"message": "published template version is immutable"}})
			return
		}
		for existingKey, existing := range templates {
			if existing.ID == template.ID && existing.Status == taskstate.TemplateActive {
				existing.Status = taskstate.TemplateRetired
				templates[existingKey] = existing
			}
		}
		template.Status = taskstate.TemplateActive
		template.Hash = "sha256:" + template.ID + "@" + template.Version
		template.RetiredAt = nil
		templates[key(template.ID, template.Version)] = template
		write(w, map[string]any{"ok": true, "template": template, "template_summary": tooltask.CompactTemplateSummary(template)})
	})
	mux.HandleFunc("/v1/workflow-templates/vector-index", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		write(w, map[string]any{"ok": true, "available": false, "vector_index_status": "missing"})
	})
	mux.HandleFunc("/v1/workflow-templates/match", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Goal, Device, Type string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		candidates := matchTemplates(req.Goal, req.Device, req.Type)
		result := map[string]any{"ok": true, "action": "match", "candidates": candidates, "count": len(candidates)}
		for key, value := range tooltask.TemplateMatchRecommendation(candidates) {
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
		if r.Method == http.MethodPost && len(parts) == 3 && parts[2] == "retire" {
			if template.Status != taskstate.TemplateActive {
				w.WriteHeader(http.StatusBadRequest)
				write(w, map[string]any{"error": map[string]any{"message": "template is not active"}})
				return
			}
			template.Status = taskstate.TemplateRetired
			templates[key(id, version)] = template
		} else if r.Method != http.MethodGet || len(parts) != 2 {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		write(w, map[string]any{"ok": true, "template": template, "template_summary": tooltask.CompactTemplateSummary(template)})
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-device-token" {
			t.Fatalf("Authorization = %q", got)
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestViewImageLoadsPathAsMCPImage(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	imagePath := filepath.Join(root, "tiny.png")
	writeTinyPNG(t, imagePath)

	result, err := rt.Call(context.Background(), "view_image", map[string]any{"path": "tiny.png", "format": "png"})
	if err != nil {
		t.Fatal(err)
	}
	assertToolResultMatchesOutputSchema(t, "view_image", result)
	assertMCPImagePayload(t, result)
	source, ok := result["source"].(map[string]any)
	if !ok || source["type"] != "path" || source["path"] != "tiny.png" {
		t.Fatalf("path source = %#v", result["source"])
	}
	if _, ok := result["return_mode"]; ok {
		t.Fatalf("view_image should not expose return_mode: %#v", result)
	}
	if _, ok := result["inline"]; ok {
		t.Fatalf("view_image should not expose inline Base64 metadata: %#v", result)
	}
}

func TestViewImageLoadsHTTPURLAsMCPImage(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	imagePath := filepath.Join(root, "remote.png")
	writeTinyPNG(t, imagePath)
	imageBytes, err := os.ReadFile(imagePath)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(imageBytes)
	}))
	t.Cleanup(server.Close)

	result, err := rt.Call(context.Background(), "view_image", map[string]any{"url": server.URL + "/remote.png", "format": "png"})
	if err != nil {
		t.Fatal(err)
	}
	assertMCPImagePayload(t, result)
	source, ok := result["source"].(map[string]any)
	if !ok || source["type"] != "url" || source["url"] != server.URL+"/remote.png" {
		t.Fatalf("url source = %#v", result["source"])
	}
}

func assertMCPImagePayload(t *testing.T, result Result) {
	t.Helper()
	data, ok := result["_mcp_image_base64"].(string)
	if !ok || data == "" {
		t.Fatalf("MCP image Base64 missing: %#v", result)
	}
	if result["_mcp_image_mime_type"] != "image/png" {
		t.Fatalf("MCP image mime type = %#v", result["_mcp_image_mime_type"])
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

func TestExecCommandDoesNotFilterCommandContent(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	command := `printf 'shell=%s network=%s\n' "$(printf expansion)" "https://example.test"`
	if runtime.GOOS == "windows" {
		command = `Write-Output "shell=expansion network=https://example.test"`
	}
	result, err := rt.execCommand(context.Background(), map[string]any{
		"cmd":            command,
		"yield_time_ms":  15000,
		"timeout_ms":     15000,
		"execution_mode": "sync",
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
	command := `printf '%s' "$AGENTDOCK_TEST_EXEC_ENV"`
	if runtime.GOOS == "windows" {
		command = `[Console]::Out.Write($env:AGENTDOCK_TEST_EXEC_ENV)`
	}
	result, err := rt.execCommand(context.Background(), map[string]any{
		"cmd":            command,
		"env":            map[string]any{"AGENTDOCK_TEST_EXEC_ENV": "forwarded"},
		"yield_time_ms":  15000,
		"timeout_ms":     15000,
		"execution_mode": "sync",
	})
	if err != nil {
		t.Fatalf("exec_command should accept explicit env: %v", err)
	}
	if result["status"] != "exited" || result["stdout"].(string) != "forwarded" {
		t.Fatalf("explicit env was not forwarded: %#v", result)
	}
}

func TestCommandEnvReportsTempDirectoryFailure(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	blocked := filepath.Join(root, "blocked-home")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt.cfg.AgentDockHome = blocked
	if _, err := rt.commandEnv("", nil); err == nil || !strings.Contains(err.Error(), "create command temp directory") {
		t.Fatalf("commandEnv() error = %v, want temp-directory error", err)
	}
}

func TestExecCommandForwardsStdinAndClosesPipe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test command uses POSIX cat")
	}
	rt, _ := newCodeToolsRuntime(t)
	result, err := rt.execCommand(context.Background(), map[string]any{
		"cmd":            "cat",
		"stdin":          "input-line\n",
		"yield_time_ms":  5000,
		"timeout_ms":     5000,
		"execution_mode": "sync",
	})
	if err != nil {
		t.Fatalf("execCommand() error = %v", err)
	}
	if result["status"] != "exited" || result["stdout"] != "input-line\n" {
		t.Fatalf("result = %#v", result)
	}
}

func TestExecCommandReportsClosedStdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test command uses POSIX shell")
	}
	rt, _ := newCodeToolsRuntime(t)
	largeInput := strings.Repeat("x", 8<<20)
	_, err := rt.execCommand(context.Background(), map[string]any{
		"cmd":            "exec 0<&-; sleep 1",
		"stdin":          largeInput,
		"yield_time_ms":  5000,
		"timeout_ms":     5000,
		"execution_mode": "sync",
	})
	if err == nil || !strings.Contains(err.Error(), "write command stdin") {
		t.Fatalf("execCommand() error = %v, want stdin write failure", err)
	}
}

func TestReadFileReturnsNextStartLineOnTruncation(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("第一行\n第二行\n第三行\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := rt.readFile(context.Background(), map[string]any{"path": "notes.txt", "max_bytes": 13})
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
	t.Setenv("PATH", t.TempDir())
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
	if got := info.Mode().Perm(); runtime.GOOS != "windows" && got != 0o640 {
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
	result, err := rt.searchTextGo(context.Background(), p, searchOptions{Query: "needle", MaxResults: 10, ContextLines: 1})
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
	escapedAbs := strings.ReplaceAll(abs, `\`, `\\`)
	output := strings.Join([]string{
		`{"type":"context","data":{"path":{"text":"` + escapedAbs + `"},"lines":{"text":"before\n"},"line_number":1}}`,
		`{"type":"match","data":{"path":{"text":"` + escapedAbs + `"},"lines":{"text":"needle here\n"},"line_number":2,"submatches":[{"match":{"text":"needle"},"start":0,"end":6}]}}`,
		`{"type":"context","data":{"path":{"text":"` + escapedAbs + `"},"lines":{"text":"after\n"},"line_number":3}}`,
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

func TestReadFileUsesCanonicalEncodingErrorCode(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	path := filepath.Join(root, "invalid-utf8.txt")
	if err := os.WriteFile(path, []byte{0xff, 0xfe}, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := rt.readFile(context.Background(), map[string]any{"path": "invalid-utf8.txt"})
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "ENCODING_UNSUPPORTED" {
		t.Fatalf("readFile() error = %#v, want ENCODING_UNSUPPORTED", err)
	}
}

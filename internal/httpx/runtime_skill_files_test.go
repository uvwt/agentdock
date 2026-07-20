package httpx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/auth"
	"github.com/uvwt/agentdock/internal/mcp"
	"github.com/uvwt/agentdock/internal/tools"
)

func TestRuntimeAPISkillFilesStayInsideActivePackage(t *testing.T) {
	cfg := testConfig(t)
	runtime, err := tools.NewRuntime(cfg)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}

	sourceDir := filepath.Join(cfg.AgentDockDefaultDir, "demo-skill")
	if err := os.MkdirAll(filepath.Join(sourceDir, "references"), 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	writeTestFile(t, filepath.Join(sourceDir, "SKILL.md"), "---\nname: demo-skill\ndescription: Demo Skill\nversion: 0.1.0\n---\n\n# Demo Skill\n")
	writeTestFile(t, filepath.Join(sourceDir, "references", "guide.md"), "# Guide\n\nSafe guide content.\n")
	if _, err := runtime.Call(context.Background(), "skill_package", map[string]any{
		"action": "install", "source": sourceDir, "activate": true,
	}); err != nil {
		t.Fatalf("install skill: %v", err)
	}

	installedDir := filepath.Join(cfg.AgentDockHome, "skill-store", "installed", "demo-skill", "0.1.0")
	outside := filepath.Join(t.TempDir(), "outside-secret.txt")
	writeTestFile(t, outside, "must not be exposed")
	symlinkCreated := os.Symlink(outside, filepath.Join(installedDir, "outside-link.txt")) == nil
	insideSymlinkCreated := os.Symlink(filepath.Join(installedDir, "references", "guide.md"), filepath.Join(installedDir, "inside-link.txt")) == nil

	handler := runtimeAPIHandler(mcp.NewServer(runtime, cfg), cfg, auth.NewOAuthStore())

	listResponse := requestRuntimeAPI(t, handler, "/internal/runtime/skills")
	var listPayload struct {
		Skills []struct {
			Skill       string `json:"skill"`
			Name        string `json:"name"`
			Description string `json:"description"`
			FileCount   int    `json:"file_count"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(listResponse.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode Skill list: %v", err)
	}
	if len(listPayload.Skills) != 1 || listPayload.Skills[0].Skill != "demo-skill" || listPayload.Skills[0].Name != "demo-skill" || listPayload.Skills[0].Description != "Demo Skill" || listPayload.Skills[0].FileCount != 2 {
		t.Fatalf("unexpected Skill list metadata: %+v", listPayload.Skills)
	}

	detail := requestRuntimeAPI(t, handler, "/internal/runtime/skills/demo-skill")
	var detailPayload struct {
		FileCount int `json:"file_count"`
		Files     []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.Unmarshal(detail.Body.Bytes(), &detailPayload); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detailPayload.FileCount != 2 || len(detailPayload.Files) != 2 || detailPayload.Files[0].Path != "SKILL.md" || detailPayload.Files[1].Path != "references/guide.md" {
		t.Fatalf("unexpected detail files: %+v", detailPayload)
	}

	list := requestRuntimeAPI(t, handler, "/internal/runtime/skills/demo-skill/files")
	if !strings.Contains(list.Body.String(), `"count":2`) || strings.Contains(list.Body.String(), ".agentdock-install.json") || strings.Contains(list.Body.String(), "outside-link.txt") || strings.Contains(list.Body.String(), "inside-link.txt") {
		t.Fatalf("unsafe or incomplete file list: %s", list.Body.String())
	}

	file := requestRuntimeAPI(t, handler, "/internal/runtime/skills/demo-skill/files/references/guide.md")
	if !strings.Contains(file.Body.String(), "Safe guide content.") || !strings.Contains(file.Body.String(), `"truncated":false`) {
		t.Fatalf("unexpected file response: %s", file.Body.String())
	}

	privateMetadata := httptest.NewRecorder()
	handler.ServeHTTP(privateMetadata, httptest.NewRequest(http.MethodGet, "/internal/runtime/skills/demo-skill/files/.agentdock-install.json", nil))
	if privateMetadata.Code != http.StatusNotFound || !strings.Contains(privateMetadata.Body.String(), `"code":"SKILL_FILE_NOT_FOUND"`) {
		t.Fatalf("private metadata status=%d body=%s", privateMetadata.Code, privateMetadata.Body.String())
	}

	traversal := httptest.NewRecorder()
	handler.ServeHTTP(traversal, httptest.NewRequest(http.MethodGet, "/internal/runtime/skills/demo-skill/files/../secret", nil))
	if traversal.Code != http.StatusBadRequest || !strings.Contains(traversal.Body.String(), `"code":"INVALID_SKILL_FILE"`) {
		t.Fatalf("path traversal status=%d body=%s", traversal.Code, traversal.Body.String())
	}

	if symlinkCreated {
		link := httptest.NewRecorder()
		handler.ServeHTTP(link, httptest.NewRequest(http.MethodGet, "/internal/runtime/skills/demo-skill/files/outside-link.txt", nil))
		if link.Code != http.StatusBadRequest || !strings.Contains(link.Body.String(), `"code":"INVALID_SKILL_FILE"`) {
			t.Fatalf("outside symlink status=%d body=%s", link.Code, link.Body.String())
		}
	}
	if insideSymlinkCreated {
		link := httptest.NewRecorder()
		handler.ServeHTTP(link, httptest.NewRequest(http.MethodGet, "/internal/runtime/skills/demo-skill/files/inside-link.txt", nil))
		if link.Code != http.StatusBadRequest || !strings.Contains(link.Body.String(), `"code":"INVALID_SKILL_FILE"`) {
			t.Fatalf("inside symlink status=%d body=%s", link.Code, link.Body.String())
		}
	}
}

func requestRuntimeAPI(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET %s status=%d body=%s", path, recorder.Code, recorder.Body.String())
	}
	return recorder
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

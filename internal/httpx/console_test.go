package httpx

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/mcp"
	"github.com/uvwt/agentdock/internal/tools"
)

func TestConsolePageServesSettingsUI(t *testing.T) {
	root := t.TempDir()
	cfg := config.Config{
		AgentDockDefaultDir: root,
		AgentDockHome:       filepath.Join(root, ".agentdock"),
		Host:                "127.0.0.1",
		Port:                8765,
		LogLevel:            "error",
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	rt, err := tools.NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	server := mcp.NewServer(rt, cfg)
	mux := http.NewServeMux()
	registerConsole(mux, server, cfg, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/console", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"AgentDock Console", "MCP 接入口", "工作目錄", "認證與安全", "Cloudflare", "本機", "customUrlInput", "打開 ChatGPT", "/internal/runtime/tunnel"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

package media

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/workspace"
)

func newMediaTestService(t *testing.T) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	cfg := config.Config{AgentDockDefaultDir: root, AgentDockHome: filepath.Join(root, ".agentdock")}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	return New(cfg, ws, nil), root
}

func TestViewImageLoadsPathAsMCPImage(t *testing.T) {
	rt, root := newMediaTestService(t)
	imagePath := filepath.Join(root, "tiny.png")
	writeTinyPNG(t, imagePath)

	result, err := rt.ViewImage(context.Background(), map[string]any{"path": "tiny.png", "format": "png"})
	if err != nil {
		t.Fatal(err)
	}
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
	rt, root := newMediaTestService(t)
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

	result, err := rt.ViewImage(context.Background(), map[string]any{"url": server.URL + "/remote.png", "format": "png"})
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

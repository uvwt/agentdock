package media

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/workspace"
)

func TestFilePublishInputSchemaUsesDocumentedOpenAIFileShape(t *testing.T) {
	schema, ok := InputSchema(ToolFilePublish)
	if !ok {
		t.Fatal("file_publish input schema missing")
	}
	properties := schema["properties"].(map[string]any)
	file := properties["file"].(map[string]any)
	if file["type"] != "object" {
		t.Fatalf("file.type = %#v, want object", file["type"])
	}
	fileProperties := file["properties"].(map[string]any)
	for _, key := range []string{"download_url", "file_id", "file_name", "mime_type"} {
		if _, ok := fileProperties[key]; !ok {
			t.Fatalf("file.properties missing %q", key)
		}
	}
	required, ok := file["required"].([]string)
	if !ok {
		t.Fatalf("file.required = %#v", file["required"])
	}
	for _, key := range []string{"download_url", "file_id"} {
		if !slices.Contains(required, key) {
			t.Fatalf("file.required = %#v, missing %q", required, key)
		}
	}
}

func TestDownloadConnectorFileUsesDocumentedOpenAIFileObject(t *testing.T) {
	payload := []byte("connector file payload")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	home := t.TempDir()
	root := filepath.Join(t.TempDir(), "workspace")
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	service := New(config.Config{AgentDockHome: home}, ws, nil)
	input := connectorFileInput{
		DownloadURL: server.URL + "/download",
		FileID:      "file-test-123",
		FileName:    "../review.txt",
		MimeType:    "text/plain",
	}
	pathValue, cleanup, err := service.downloadConnectorFile(context.Background(), input, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(pathValue) != "review.txt" {
		t.Fatalf("downloaded basename = %q", filepath.Base(pathValue))
	}
	got, err := os.ReadFile(pathValue)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("downloaded payload = %q", got)
	}
	cleanup()
	if _, err := os.Stat(pathValue); !os.IsNotExist(err) {
		t.Fatalf("temp file still exists after cleanup: %v", err)
	}
}

func TestBlockedConnectorDownloadIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1",
		"10.0.0.1",
		"172.16.0.1",
		"192.168.1.1",
		"169.254.169.254",
		"100.64.0.1",
		"::1",
		"fc00::1",
		"fe80::1",
	}
	for _, raw := range blocked {
		if !blockedConnectorDownloadIP(net.ParseIP(raw)) {
			t.Errorf("blockedConnectorDownloadIP(%q) = false", raw)
		}
	}
	for _, raw := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if blockedConnectorDownloadIP(net.ParseIP(raw)) {
			t.Errorf("blockedConnectorDownloadIP(%q) = true", raw)
		}
	}
}

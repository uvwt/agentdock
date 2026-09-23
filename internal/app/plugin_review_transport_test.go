package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	pluginruntime "github.com/uvwt/agentdock/internal/plugin"
)

func TestPluginUpdateUsesOneReviewedArchiveSnapshot(t *testing.T) {
	rt, root := newPluginTestRuntime(t)
	current := writeAppPluginForTest(t, root, "1.0.0")
	if _, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "install", "source": current, "review_token": pluginReviewTokenForTest(t, rt, current),
	}); err != nil {
		t.Fatal(err)
	}

	reviewedArchive := portablePluginArchiveBytes(t, "demo.plugin", "2.0.0", "reviewed candidate")
	differentArchive := portablePluginArchiveBytes(t, "demo.plugin", "3.0.0", "different second fetch")
	var updatePhase atomic.Bool
	var updateRequests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		payload := reviewedArchive
		if updatePhase.Load() && updateRequests.Add(1) > 1 {
			payload = differentArchive
		}
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	oldTransport := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = oldTransport })
	source := server.URL + "/plugin.zip"

	validated, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "validate", "source": source, "source_type": "archive",
	})
	if err != nil {
		t.Fatal(err)
	}
	review, ok := validated["review"].(pluginruntime.Review)
	if !ok || !review.Valid || review.ReviewToken == "" {
		t.Fatalf("archive review = %#v", validated)
	}

	updatePhase.Store(true)
	updated, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "update", "source": source, "source_type": "archive",
		"review_token": review.ReviewToken, "confirmed_source_change": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := updateRequests.Load(); got != 1 {
		t.Fatalf("Plugin update fetched/staged source %d times, want exactly 1", got)
	}
	if updated["version"] != "2.0.0" {
		t.Fatalf("updated Plugin = %#v", updated)
	}
	inspected, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "inspect", "name": "demo.plugin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if inspected["version"] != "2.0.0" || inspected["package_digest"] != review.PackageDigest {
		t.Fatalf("installed candidate differs from reviewed snapshot: %#v review=%#v", inspected, review)
	}
}

func portablePluginArchiveBytes(t *testing.T, name, version, description string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	entry, err := writer.Create("plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(map[string]any{
		"$schema": testPluginSchema, "name": name, "version": version, "description": description,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(manifest); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

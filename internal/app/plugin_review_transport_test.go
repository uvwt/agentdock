package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	pluginruntime "github.com/uvwt/agentdock/internal/plugin"
)

func TestPluginUpdateReviewTokenBindsLocalArchiveContent(t *testing.T) {
	rt, root := newPluginTestRuntime(t)
	current := writeAppPluginForTest(t, root, "1.0.0")
	if _, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "install", "source": current, "review_token": pluginReviewTokenForTest(t, rt, current),
	}); err != nil {
		t.Fatal(err)
	}

	reviewedArchive := portablePluginArchiveBytes(t, "demo.plugin", "2.0.0", "reviewed candidate")
	differentArchive := portablePluginArchiveBytes(t, "demo.plugin", "3.0.0", "different candidate")
	source := filepath.Join(root, "candidate.zip")
	if err := os.WriteFile(source, reviewedArchive, 0o600); err != nil {
		t.Fatal(err)
	}

	validated, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "validate", "source": source,
	})
	if err != nil {
		t.Fatal(err)
	}
	review, ok := validated["review"].(pluginruntime.Review)
	if !ok || !review.Valid || review.ReviewToken == "" {
		t.Fatalf("archive review = %#v", validated)
	}

	if err := os.WriteFile(source, differentArchive, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "update", "source": source, "review_token": review.ReviewToken,
	}); err == nil {
		t.Fatal("update accepted archive content that changed after review")
	}

	if err := os.WriteFile(source, reviewedArchive, 0o600); err != nil {
		t.Fatal(err)
	}
	updated, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "update", "source": source, "review_token": review.ReviewToken,
	})
	if err != nil {
		t.Fatal(err)
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
		t.Fatalf("installed candidate differs from reviewed archive: %#v review=%#v", inspected, review)
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

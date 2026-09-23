package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPluginLocalUpdateDeactivatesRuntimeBeforeWaitingForVersionReaders(t *testing.T) {
	rt, root := newPluginTestRuntime(t)

	v1Root := filepath.Join(root, "local-v1")
	if err := os.MkdirAll(v1Root, 0o700); err != nil {
		t.Fatal(err)
	}
	sourceV1 := writeAppPluginForTest(t, v1Root, "local")
	if _, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "install", "source": sourceV1, "review_token": pluginReviewTokenForTest(t, rt, sourceV1),
	}); err != nil {
		t.Fatal(err)
	}

	v2Root := filepath.Join(root, "local-v2")
	if err := os.MkdirAll(v2Root, 0o700); err != nil {
		t.Fatal(err)
	}
	sourceV2 := writeAppPluginForTest(t, v2Root, "local")
	if err := os.WriteFile(
		filepath.Join(sourceV2, "skills", "plugin-skill", "references", "guide.md"),
		[]byte("local-v2-marker\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	updated, err := rt.Call(ctx, "plugin_manage", map[string]any{
		"action": "update", "source": sourceV2,
		"review_token": pluginReviewTokenForTest(t, rt, sourceV2),
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated["version"] != "local" || updated["changed"] != true {
		t.Fatalf("local Plugin update = %#v", updated)
	}
}

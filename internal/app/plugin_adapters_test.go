package app

import (
	"archive/zip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	pluginruntime "github.com/uvwt/agentdock/internal/plugin"
)

func TestPluginManageAutoLocalZIPAndCatalogSources(t *testing.T) {
	rt, root := newPluginTestRuntime(t)

	t.Run("auto-local-zip", func(t *testing.T) {
		archivePath := filepath.Join(root, "demo.zip")
		file, err := os.Create(archivePath)
		if err != nil {
			t.Fatal(err)
		}
		writer := zip.NewWriter(file)
		manifest, err := json.Marshal(map[string]any{
			"$schema": testPluginSchema, "name": "zip.plugin", "version": "1.0.0", "description": "ZIP Plugin",
		})
		if err != nil {
			t.Fatal(err)
		}
		entry, err := writer.Create("plugin.json")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(manifest); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}

		result, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
			"action": "validate", "source": "demo.zip",
		})
		if err != nil {
			t.Fatal(err)
		}
		review, ok := result["review"].(pluginruntime.Review)
		if !ok || !review.Valid || review.Source.Type != "archive" || review.Name != "zip.plugin" {
			t.Fatalf("ZIP validate = %#v", result)
		}
	})

	t.Run("read-only-catalog-and-item", func(t *testing.T) {
		catalogRoot := filepath.Join(root, "catalog")
		pluginRoot := filepath.Join(catalogRoot, "plugins", "demo")
		if err := os.MkdirAll(filepath.Join(catalogRoot, ".agents", "plugins"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(pluginRoot, ".codex-plugin"), 0o700); err != nil {
			t.Fatal(err)
		}
		writeAppPluginJSON(t, filepath.Join(catalogRoot, ".agents", "plugins", "marketplace.json"), map[string]any{
			"name": "test-catalog",
			"plugins": []any{
				map[string]any{"name": "catalog-plugin", "source": "./plugins/demo"},
			},
		})
		writeAppPluginJSON(t, filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"), map[string]any{
			"name": "catalog-plugin", "version": "1.0.0", "description": "Catalog Plugin",
		})

		listed, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
			"action": "catalog", "source": "catalog", "catalog": "openai",
		})
		if err != nil {
			t.Fatal(err)
		}
		catalog, ok := listed["catalog"].(pluginruntime.Catalog)
		if !ok || len(catalog.Entries) != 1 || catalog.Entries[0].ResolvedSource == nil {
			t.Fatalf("catalog = %#v", listed)
		}

		validated, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
			"action": "validate", "source": "catalog", "source_type": "catalog",
			"catalog": "openai", "catalog_item": "catalog-plugin",
		})
		if err != nil {
			t.Fatal(err)
		}
		review, ok := validated["review"].(pluginruntime.Review)
		if !ok || !review.Valid || review.Compatibility.Adapter != "openai" {
			t.Fatalf("catalog item validate = %#v", validated)
		}
	})
}

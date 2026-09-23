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

func TestPluginManagePortableZIPAndRejectsLegacySourceFeatures(t *testing.T) {
	rt, root := newPluginTestRuntime(t)

	t.Run("portable-local-zip", func(t *testing.T) {
		archivePath := filepath.Join(root, "demo.zip")
		file, err := os.Create(archivePath)
		if err != nil {
			t.Fatal(err)
		}
		writer := zip.NewWriter(file)
		manifest, err := json.Marshal(map[string]any{
			"$schema":     testPluginSchema,
			"name":        "zip.plugin",
			"version":     "1.0.0",
			"description": "ZIP Plugin",
			"provenance": map[string]any{
				"origin":   "https://github.com/example/plugins",
				"revision": "abc123",
				"subdir":   "plugins/zip",
				"format":   "example",
				"adapted":  true,
			},
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
		if !ok || !review.Valid || review.Name != "zip.plugin" || review.Compatibility.Format != "portable" {
			t.Fatalf("ZIP validate = %#v", result)
		}
		if review.Provenance == nil || review.Provenance.Origin != "https://github.com/example/plugins" || !review.Provenance.Adapted {
			t.Fatalf("ZIP provenance = %#v", review.Provenance)
		}
	})

	t.Run("catalog-action-removed", func(t *testing.T) {
		if _, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
			"action": "catalog", "source": ".",
		}); err == nil {
			t.Fatal("legacy catalog action was accepted")
		}
	})

	t.Run("legacy-source-parameter-removed", func(t *testing.T) {
		if _, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
			"action": "validate", "source": ".", "source_type": "git",
		}); err == nil {
			t.Fatal("legacy source_type parameter was accepted")
		}
	})

	t.Run("remote-source-rejected", func(t *testing.T) {
		if _, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
			"action": "validate", "source": "https://example.com/plugin.zip",
		}); err == nil {
			t.Fatal("remote Plugin source was accepted")
		}
	})
}

func TestPluginManageImportedCloudflareShape(t *testing.T) {
	rt, root := newPluginTestRuntime(t)
	source := filepath.Join(root, "cloudflare-import")
	if err := os.MkdirAll(filepath.Join(source, "skills"), 0o700); err != nil {
		t.Fatal(err)
	}

	writeAppPluginJSON(t, filepath.Join(source, "plugin.json"), map[string]any{
		"$schema":     testPluginSchema,
		"name":        "cloudflare",
		"version":     "0.1.2",
		"description": "Imported Cloudflare Plugin fixture.",
		"provenance": map[string]any{
			"origin":   "https://github.com/openai/plugins",
			"revision": "1dc195897af4161d039b80d8471ec0a10c9bbc89",
			"subdir":   "plugins/cloudflare",
			"format":   "openai",
			"adapted":  true,
		},
	})

	skills := []string{
		"agents-sdk",
		"building-ai-agent-on-cloudflare",
		"building-mcp-server-on-cloudflare",
		"cloudflare",
		"durable-objects",
		"sandbox-sdk",
		"web-perf",
		"workers-best-practices",
		"wrangler",
	}
	for _, name := range skills {
		skillRoot := filepath.Join(source, "skills", name)
		if err := os.MkdirAll(skillRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		document := "---\nname: " + name + "\ndescription: Imported Cloudflare Skill fixture.\n---\n\n# " + name + "\n"
		if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte(document), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeAppPluginJSON(t, filepath.Join(source, "mcp.json"), map[string]any{
		"$schema": testMCPSchema,
		"mcpServers": map[string]any{
			"cloudflare-api": map[string]any{
				"type": "streamable-http",
				"url":  "https://mcp.cloudflare.com/mcp",
			},
		},
	})

	validated, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "validate", "source": source,
	})
	if err != nil {
		t.Fatal(err)
	}
	review, ok := validated["review"].(pluginruntime.Review)
	if !ok || !review.Valid {
		t.Fatalf("Cloudflare import review = %#v", validated)
	}
	if len(review.Skills) != 9 || len(review.MCP) != 1 {
		t.Fatalf("Cloudflare components = skills:%d mcp:%d", len(review.Skills), len(review.MCP))
	}
	if review.Provenance == nil ||
		review.Provenance.Origin != "https://github.com/openai/plugins" ||
		review.Provenance.Subdir != "plugins/cloudflare" ||
		review.Provenance.Format != "openai" ||
		!review.Provenance.Adapted {
		t.Fatalf("Cloudflare provenance = %#v", review.Provenance)
	}

	installed, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "install", "source": source, "review_token": review.ReviewToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	if installed["name"] != "cloudflare" || installed["version"] != "0.1.2" {
		t.Fatalf("Cloudflare install = %#v", installed)
	}
	inspected, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "inspect", "name": "cloudflare",
	})
	if err != nil {
		t.Fatal(err)
	}
	if inspected["package_digest"] != review.PackageDigest {
		t.Fatalf("Cloudflare inspect = %#v review=%#v", inspected, review)
	}
}

package app

import (
	"archive/zip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
				"origin":      "https://github.com/example/plugins",
				"revision":    "abc123",
				"subdir":      "plugins/zip",
				"vendor_note": map[string]any{"preserved": true},
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
		if !ok || !review.Valid || review.Name != "zip.plugin" || review.Format != "portable" {
			t.Fatalf("ZIP validate = %#v", result)
		}
		if review.Provenance == nil || review.Provenance.Origin != "https://github.com/example/plugins" {
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

	t.Run("legacy-source-parameters-removed", func(t *testing.T) {
		legacy := map[string]any{
			"source_type":             "git",
			"source_adapter":          "openai",
			"source_version":          "1.0.0",
			"git_ref":                 "main",
			"git_commit":              "deadbeef",
			"subdir":                  "plugins/demo",
			"sha256":                  "deadbeef",
			"catalog":                 "openai",
			"catalog_item":            "demo",
			"confirmed_source_change": true,
		}
		for name, value := range legacy {
			t.Run(name, func(t *testing.T) {
				if _, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
					"action": "validate", "source": ".", name: value,
				}); err == nil {
					t.Fatalf("legacy %s parameter was accepted", name)
				}
			})
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

func TestPluginManageAutoConvertsOpenAICloudflareShape(t *testing.T) {
	rt, root := newPluginTestRuntime(t)
	source := filepath.Join(root, "cloudflare-openai")
	if err := os.MkdirAll(filepath.Join(source, ".codex-plugin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, "skills"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, "commands"), 0o700); err != nil {
		t.Fatal(err)
	}

	writeAppPluginJSON(t, filepath.Join(source, ".codex-plugin", "plugin.json"), map[string]any{
		"name":        "cloudflare",
		"version":     "0.1.2",
		"description": "OpenAI Cloudflare-shaped fixture.",
		"repository":  "https://github.com/openai/plugins",
		"skills":      "./skills/",
		"mcpServers":  "./.mcp.json",
		"commands":    map[string]any{"deploy": "./commands/deploy.md"},
		"interface":   map[string]any{"displayName": "Cloudflare"},
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
	if err := os.WriteFile(filepath.Join(source, "commands", "deploy.md"), []byte("# Deploy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeAppPluginJSON(t, filepath.Join(source, ".mcp.json"), map[string]any{
		"mcpServers": map[string]any{
			"cloudflare-api": map[string]any{
				"type": "http",
				"url":  "https://mcp.cloudflare.com/mcp",
				"note": "Official Cloudflare API MCP server.",
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
		t.Fatalf("Cloudflare OpenAI review = %#v", validated)
	}
	if review.Format != "openai" || len(review.Skills) != 9 || len(review.MCP) != 1 {
		t.Fatalf("Cloudflare normalized review = %#v", review)
	}
	if review.Provenance == nil ||
		review.Provenance.Origin != "https://github.com/openai/plugins" ||
		!strings.HasPrefix(review.Provenance.Revision, "sha256:") ||
		review.Format != "openai" {
		t.Fatalf("Cloudflare provenance = %#v", review.Provenance)
	}
	if len(review.Warnings) == 0 {
		t.Fatalf("Cloudflare conversion warnings = %#v", review.Warnings)
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
	plugin, ok := inspected["plugin"].(map[string]any)
	if !ok {
		t.Fatalf("Cloudflare inspect plugin = %#v", inspected["plugin"])
	}
	if plugin["format"] != "openai" {
		t.Fatalf("Cloudflare inspect format = %#v", plugin["format"])
	}
}

package plugin

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	skills "github.com/uvwt/agentdock/internal/skill"
)

func TestOpenAIPluginAutoConvertsWithoutAdapterParameter(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), ".agentdock"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeExternalJSONFile(t, filepath.Join(root, ".codex-plugin", "plugin.json"), map[string]any{
		"name":        "openai-auto",
		"version":     "1.2.3",
		"description": "OpenAI automatic conversion fixture.",
		"skills":      "./skills/",
		"mcpServers":  "./.mcp.json",
		"commands":    map[string]any{"demo": "./commands/demo.md"},
		"interface":   map[string]any{"displayName": "OpenAI Auto"},
	})
	writeExternalSkill(t, filepath.Join(root, "skills", "demo-skill"), "demo-skill")
	if err := os.MkdirAll(filepath.Join(root, "commands"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "commands", "demo.md"), []byte("# Demo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeExternalJSONFile(t, filepath.Join(root, ".mcp.json"), map[string]any{
		"mcpServers": map[string]any{
			"remote": map[string]any{
				"type": "http",
				"url":  "https://example.com/mcp",
				"note": "Descriptive metadata only.",
			},
		},
	})

	review := manager.Validate(root)
	if !review.Valid {
		t.Fatalf("OpenAI review invalid: %#v", review)
	}
	if review.Format != pluginFormatOpenAI {
		t.Fatalf("format = %q", review.Format)
	}
	if review.Provenance == nil {
		t.Fatalf("provenance = %#v", review.Provenance)
	}
	if !strings.HasPrefix(review.Provenance.Origin, "sha256:") {
		t.Fatalf("anonymous local import origin = %q", review.Provenance.Origin)
	}
	if len(review.Skills) != 1 || review.Skills[0].Name != "demo-skill" || len(review.MCP) != 1 {
		t.Fatalf("normalized components = skills:%#v mcp:%#v", review.Skills, review.MCP)
	}
	if !containsText(review.Warnings, "commands") || !containsText(review.Warnings, "metadata field note") {
		t.Fatalf("expected conversion warnings, got %#v", review.Warnings)
	}
	result, err := manager.InstallReviewedSource(context.Background(), root, true, review.ReviewToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.FinalizeActivation(result.Name); err != nil {
		t.Fatal(err)
	}
	installed, err := manager.Inspect("openai-auto")
	if err != nil {
		t.Fatal(err)
	}
	if installed.Format != pluginFormatOpenAI {
		t.Fatalf("installed format = %q", installed.Format)
	}
	if installed.Provenance == nil {
		t.Fatalf("installed provenance = %#v", installed.Provenance)
	}
	if !regularFileExists(filepath.Join(installed.Root, "plugin.json")) ||
		!regularFileExists(filepath.Join(installed.Root, "mcp.json")) {
		t.Fatalf("installed package is not canonical Portable layout: %s", installed.Root)
	}
	for _, preserved := range []string{".codex-plugin", ".mcp.json", "commands"} {
		if _, err := os.Lstat(filepath.Join(installed.Root, preserved)); err != nil {
			t.Fatalf("canonical OpenAI package lost preserved source artifact %s: %v", preserved, err)
		}
	}
}

func TestExternalPluginImportMetadataBecomesCanonicalProvenance(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), ".agentdock"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeExternalJSONFile(t, filepath.Join(root, ".codex-plugin", "plugin.json"), map[string]any{
		"name":       "openai-provenance",
		"version":    "1.0.0",
		"repository": "https://github.com/upstream/example",
	})
	writeExternalJSONFile(t, filepath.Join(root, pluginImportMetadataFile), map[string]any{
		"origin":   "https://github.com/example/fork",
		"ref":      "main",
		"revision": "0123456789abcdef",
		"subdir":   "plugins/openai-provenance",
	})

	review := manager.Validate(root)
	if !review.Valid {
		t.Fatalf("review invalid: %#v", review)
	}
	want := Provenance{
		Origin: "https://github.com/example/fork", Ref: "main",
		Revision: "0123456789abcdef", Subdir: "plugins/openai-provenance",
	}
	if review.Provenance == nil || *review.Provenance != want {
		t.Fatalf("review provenance = %#v, want %#v", review.Provenance, want)
	}
	if !regularFileExists(filepath.Join(root, pluginImportMetadataFile)) {
		t.Fatal("validate mutated the original import sidecar")
	}

	result, err := manager.InstallReviewedSource(context.Background(), root, true, review.ReviewToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.FinalizeActivation(result.Name); err != nil {
		t.Fatal(err)
	}
	installed, err := manager.Inspect(result.Name)
	if err != nil {
		t.Fatal(err)
	}
	if installed.Provenance == nil || *installed.Provenance != want {
		t.Fatalf("installed provenance = %#v, want %#v", installed.Provenance, want)
	}
	if _, err := os.Lstat(filepath.Join(installed.Root, pluginImportMetadataFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("installed Plugin retained import sidecar: %v", err)
	}
}

func TestPortablePluginImportMetadataBecomesProvenance(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), ".agentdock"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeExternalJSONFile(t, filepath.Join(root, "plugin.json"), map[string]any{
		"$schema": pluginSchemaURI,
		"name":    "portable-provenance",
		"version": "1.0.0",
	})
	writeExternalJSONFile(t, filepath.Join(root, pluginImportMetadataFile), map[string]any{
		"origin": "https://github.com/example/portable",
		"ref":    "v1",
	})

	review := manager.Validate(root)
	if !review.Valid {
		t.Fatalf("review invalid: %#v", review)
	}
	if review.Provenance == nil ||
		review.Provenance.Origin != "https://github.com/example/portable" ||
		review.Provenance.Ref != "v1" ||
		review.Format != pluginFormatPortable {
		t.Fatalf("portable provenance = %#v", review.Provenance)
	}
}

func TestClaudePluginImportMetadataKeepsVerifiedSourceIdentity(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), ".agentdock"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeExternalJSONFile(t, filepath.Join(root, ".claude-plugin", "plugin.json"), map[string]any{
		"name": "claude-provenance", "version": "1.0.0",
	})
	writeExternalJSONFile(t, filepath.Join(root, pluginImportMetadataFile), map[string]any{
		"origin":   "https://github.com/anthropics/claude-plugins-official",
		"ref":      "main",
		"revision": "abcdef0123456789",
		"subdir":   "external_plugins/context7",
	})

	review := manager.Validate(root)
	if !review.Valid {
		t.Fatalf("review invalid: %#v", review)
	}
	if review.Provenance == nil ||
		review.Provenance.Origin != "https://github.com/anthropics/claude-plugins-official" ||
		review.Provenance.Ref != "main" ||
		review.Provenance.Revision != "abcdef0123456789" ||
		review.Provenance.Subdir != "external_plugins/context7" {
		t.Fatalf("Claude provenance = %#v", review.Provenance)
	}
}

func TestPluginImportMetadataRejectsUntrustedFieldsAndUnsafeOrigin(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]any
		want     string
	}{
		{
			name:     "unknown field",
			metadata: map[string]any{"origin": "https://github.com/example/repo", "format": "claude"},
			want:     "unknown field",
		},
		{
			name:     "missing origin",
			metadata: map[string]any{"revision": "0123456789abcdef"},
			want:     "provenance.origin is required",
		},
		{
			name:     "credentialed origin",
			metadata: map[string]any{"origin": "https://token@github.com/example/repo"},
			want:     "must not contain userinfo",
		},
		{
			name:     "escaping subdir",
			metadata: map[string]any{"origin": "https://github.com/example/repo", "subdir": "../escape"},
			want:     "provenance.subdir",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, err := NewManager(filepath.Join(t.TempDir(), ".agentdock"))
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			writeExternalJSONFile(t, filepath.Join(root, ".claude-plugin", "plugin.json"), map[string]any{
				"name": "claude-invalid-provenance", "version": "1.0.0",
			})
			writeExternalJSONFile(t, filepath.Join(root, pluginImportMetadataFile), test.metadata)

			review := manager.Validate(root)
			if review.Valid || !containsText(review.Issues, test.want) {
				t.Fatalf("review = %#v, want issue containing %q", review, test.want)
			}
		})
	}
}

func TestPluginImportMetadataChangesReviewIdentity(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), ".agentdock"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeExternalJSONFile(t, filepath.Join(root, "plugin.json"), map[string]any{
		"$schema": pluginSchemaURI,
		"name":    "portable-review-identity",
		"version": "1.0.0",
	})
	writeExternalJSONFile(t, filepath.Join(root, pluginImportMetadataFile), map[string]any{
		"origin":   "https://github.com/example/portable",
		"ref":      "main",
		"revision": "rev-a",
	})
	first := manager.Validate(root)
	if !first.Valid {
		t.Fatalf("first review invalid: %#v", first)
	}
	writeExternalJSONFile(t, filepath.Join(root, pluginImportMetadataFile), map[string]any{
		"origin":   "https://github.com/example/portable",
		"ref":      "main",
		"revision": "rev-b",
	})
	second := manager.Validate(root)
	if !second.Valid {
		t.Fatalf("second review invalid: %#v", second)
	}
	if first.PackageDigest == second.PackageDigest || first.ReviewToken == second.ReviewToken {
		t.Fatalf("source identity change did not invalidate review: first=%#v second=%#v", first.Provenance, second.Provenance)
	}
}

func TestOpenAIPluginZIPWithWrapperDirectoryAutoConverts(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), ".agentdock"))
	if err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	writeExternalJSONFile(t, filepath.Join(source, ".codex-plugin", "plugin.json"), map[string]any{
		"name": "openai-zip", "version": "1.0.0", "skills": "./skills/",
	})
	writeExternalSkill(t, filepath.Join(source, "skills", "zip-skill"), "zip-skill")
	writeExternalJSONFile(t, filepath.Join(source, pluginImportMetadataFile), map[string]any{
		"origin":   "https://github.com/example/openai-plugins",
		"ref":      "main",
		"revision": "0123456789abcdef",
		"subdir":   "plugins/openai-zip",
	})

	archive := filepath.Join(t.TempDir(), "openai.zip")
	writeWrappedPluginZip(t, archive, "openai-plugin", source)

	review := manager.Validate(archive)
	if !review.Valid || review.Name != "openai-zip" || review.Format != pluginFormatOpenAI {
		t.Fatalf("wrapped OpenAI ZIP review = %#v", review)
	}
	if len(review.Skills) != 1 || review.Skills[0].Name != "zip-skill" {
		t.Fatalf("wrapped ZIP skills = %#v", review.Skills)
	}
	if review.Provenance == nil ||
		review.Provenance.Origin != "https://github.com/example/openai-plugins" ||
		review.Provenance.Ref != "main" ||
		review.Provenance.Revision != "0123456789abcdef" ||
		review.Provenance.Subdir != "plugins/openai-zip" {
		t.Fatalf("wrapped ZIP provenance = %#v", review.Provenance)
	}
	result, err := manager.InstallReviewedSource(context.Background(), archive, true, review.ReviewToken)
	if err != nil {
		t.Fatalf("install wrapped OpenAI ZIP: %v", err)
	}
	if err := manager.FinalizeActivation(result.Name); err != nil {
		t.Fatal(err)
	}
	installed, err := manager.Inspect("openai-zip")
	if err != nil {
		t.Fatal(err)
	}
	if installed.PackageDigest != review.PackageDigest || installed.Format != pluginFormatOpenAI {
		t.Fatalf("wrapped ZIP installed state = %#v review=%#v", installed.State, review)
	}
	if _, err := os.Lstat(filepath.Join(installed.Root, pluginImportMetadataFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("wrapped ZIP installed Plugin retained import sidecar: %v", err)
	}
}

func TestClaudePluginAutoConvertsRuntimePlaceholders(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), ".agentdock"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeExternalJSONFile(t, filepath.Join(root, ".claude-plugin", "plugin.json"), map[string]any{
		"name": "claude-auto", "version": "2.1.0", "description": "Claude automatic conversion fixture.",
	})
	writeExternalSkill(t, root, "root-skill")
	if err := os.WriteFile(filepath.Join(root, "server.js"), []byte("console.log('ok')\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeExternalJSONFile(t, filepath.Join(root, ".mcp.json"), map[string]any{
		"mcpServers": map[string]any{
			"local": map[string]any{
				"command": "node",
				"args":    []string{"${CLAUDE_PLUGIN_ROOT}/server.js"},
				"cwd":     "${CLAUDE_PLUGIN_DATA}",
				"env": map[string]string{
					"CONFIG": "${CLAUDE_PLUGIN_ROOT}/config.json",
					"TOKEN":  "${TOKEN}",
				},
			},
		},
	})

	review := manager.Validate(root)
	if !review.Valid || review.Format != pluginFormatClaude {
		t.Fatalf("Claude review = %#v", review)
	}
	if review.Provenance == nil {
		t.Fatalf("Claude provenance = %#v", review.Provenance)
	}
	if len(review.Skills) != 1 || review.Skills[0].Name != "root-skill" || len(review.MCP) != 1 {
		t.Fatalf("Claude components = skills:%#v mcp:%#v", review.Skills, review.MCP)
	}

	result, err := manager.InstallReviewedSource(context.Background(), root, true, review.ReviewToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.FinalizeActivation(result.Name); err != nil {
		t.Fatal(err)
	}
	installed, err := manager.Inspect("claude-auto")
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := LoadPackage(installed.Root)
	if err != nil {
		t.Fatal(err)
	}
	mcp := pkg.Components.MCP[0]
	if len(mcp.Args) != 1 || mcp.Args[0] != "${PLUGIN_ROOT}/server.js" ||
		mcp.CWD != "${PLUGIN_DATA}" || mcp.Environment["CONFIG"] != "${PLUGIN_ROOT}/config.json" ||
		mcp.EnvBindings["TOKEN"] != "TOKEN" {
		t.Fatalf("Claude placeholders were not normalized: %#v", mcp)
	}
	for _, preserved := range []string{".claude-plugin", ".mcp.json"} {
		if _, err := os.Lstat(filepath.Join(installed.Root, preserved)); err != nil {
			t.Fatalf("canonical Claude package lost preserved source artifact %s: %v", preserved, err)
		}
	}
}

func TestClaudePluginAutoConvertsSkillAllowedToolsArray(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), ".agentdock"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeExternalJSONFile(t, filepath.Join(root, ".claude-plugin", "plugin.json"), map[string]any{
		"name": "claude-allowed-tools", "version": "1.0.0", "description": "Claude allowed-tools fixture.",
	})
	skillRoot := filepath.Join(root, "skills", "access")
	if err := os.MkdirAll(skillRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	sourceSkill := "---\nname: access\ndescription: Manage access.\nuser-invocable: true\nallowed-tools:\n  - Read\n  - Write\n  - Bash(ls *)\n---\n\n# Access\n"
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte(sourceSkill), 0o600); err != nil {
		t.Fatal(err)
	}

	review := manager.Validate(root)
	if !review.Valid || review.Format != pluginFormatClaude {
		t.Fatalf("Claude allowed-tools review = %#v", review)
	}
	result, err := manager.InstallReviewedSource(context.Background(), root, true, review.ReviewToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.FinalizeActivation(result.Name); err != nil {
		t.Fatal(err)
	}
	installed, err := manager.Inspect("claude-allowed-tools")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := skills.LoadSkillDocument(filepath.Join(installed.Root, "skills", "access"))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Name != "access" {
		t.Fatalf("installed Skill identity = %#v", doc)
	}
	installedSkill, err := os.ReadFile(filepath.Join(installed.Root, "skills", "access", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(installedSkill) != sourceSkill {
		t.Fatalf("third-party Skill frontmatter was rewritten:\n%s", installedSkill)
	}
	original, err := os.ReadFile(filepath.Join(skillRoot, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(original) != sourceSkill {
		t.Fatalf("Claude source Skill was modified:\n%s", original)
	}
}

func TestAutoConversionRejectsAmbiguousVendorManifests(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".codex-plugin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude-plugin"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeExternalJSONFile(t, filepath.Join(root, ".codex-plugin", "plugin.json"), map[string]any{
		"name": "ambiguous-openai", "version": "1.0.0",
	})
	writeExternalJSONFile(t, filepath.Join(root, ".claude-plugin", "plugin.json"), map[string]any{
		"name": "ambiguous-claude", "version": "1.0.0",
	})

	_, err := detectPluginFormat(root)
	var pluginErr *Error
	if !errors.As(err, &pluginErr) || pluginErr.Code != "PLUGIN_FORMAT_AMBIGUOUS" {
		t.Fatalf("ambiguous format error = %#v", err)
	}
}

func TestAutoConversionPreservesUnknownMCPAuthWithoutActivation(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), ".agentdock"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeExternalJSONFile(t, filepath.Join(root, ".codex-plugin", "plugin.json"), map[string]any{
		"name": "auth-gap", "version": "1.0.0",
	})
	writeExternalJSONFile(t, filepath.Join(root, ".mcp.json"), map[string]any{
		"mcpServers": map[string]any{
			"remote": map[string]any{
				"type":           "http",
				"url":            "https://example.com/mcp",
				"oauth_resource": "https://example.com/mcp",
			},
		},
	})

	review := manager.Validate(root)
	if !review.Valid {
		t.Fatalf("unknown MCP auth should not invalidate package: %#v", review)
	}
	if len(review.MCP) != 0 || !containsText(review.Warnings, "oauth_resource") || !containsText(review.Warnings, "not activated") {
		t.Fatalf("unknown MCP auth was not isolated: %#v", review)
	}
	result, err := manager.InstallReviewedSource(context.Background(), root, true, review.ReviewToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.FinalizeActivation(result.Name); err != nil {
		t.Fatal(err)
	}
	installed, err := manager.Inspect(result.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(installed.Components.MCP) != 0 {
		t.Fatalf("unknown MCP auth became active: %#v", installed.Components.MCP)
	}
	if !containsText(installed.Warnings, "oauth_resource") || !containsText(installed.Warnings, "not activated") {
		t.Fatalf("installed state lost review warning: %#v", installed.Warnings)
	}
	if _, err := os.Stat(filepath.Join(installed.Root, ".mcp.json")); err != nil {
		t.Fatalf("source MCP config was not preserved: %v", err)
	}
}

func TestExternalConversionReviewTokenBindsSourceContent(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), ".agentdock"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeExternalJSONFile(t, filepath.Join(root, ".codex-plugin", "plugin.json"), map[string]any{
		"name": "review-bound", "version": "1.0.0", "description": "before",
	})

	first := manager.Validate(root)
	second := manager.Validate(root)
	if !first.Valid || !second.Valid || first.PackageDigest != second.PackageDigest || first.ReviewToken != second.ReviewToken {
		t.Fatalf("deterministic conversion failed: first=%#v second=%#v", first, second)
	}

	writeExternalJSONFile(t, filepath.Join(root, ".codex-plugin", "plugin.json"), map[string]any{
		"name": "review-bound", "version": "1.0.0", "description": "after",
	})
	_, err = manager.InstallReviewedSource(context.Background(), root, true, first.ReviewToken)
	var pluginErr *Error
	if !errors.As(err, &pluginErr) || pluginErr.Code != "PLUGIN_REVIEW_CHANGED" {
		t.Fatalf("changed external source accepted old review token: %v", err)
	}
}

func writeExternalSkill(t *testing.T, root, name string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: External Plugin fixture Skill.\n---\n\n# Fixture\n"
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeExternalJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	writeJSONFile(t, path, value)
}

func writeWrappedPluginZip(t *testing.T, archivePath, prefix, root string) {
	t.Helper()
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	meta, err := writer.Create("__MACOSX/._plugin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := meta.Write([]byte("macOS ZIP metadata")); err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(filepath.Join(prefix, relative))
		if entry.IsDir() {
			_, err := writer.Create(name + "/")
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = name
		out, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeErr := in.Close()
		return errors.Join(copyErr, closeErr)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func containsText(values []string, text string) bool {
	for _, value := range values {
		if strings.Contains(value, text) {
			return true
		}
	}
	return false
}

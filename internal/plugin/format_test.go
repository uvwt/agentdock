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
	if review.Compatibility.Format != pluginFormatOpenAI {
		t.Fatalf("compatibility = %#v", review.Compatibility)
	}
	if review.Provenance == nil || review.Provenance.Format != pluginFormatOpenAI || !review.Provenance.Adapted {
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
	if len(review.Unsupported) != 0 {
		t.Fatalf("unexpected unsupported components: %#v", review.Unsupported)
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
	if installed.Compatibility.Format != pluginFormatOpenAI {
		t.Fatalf("installed compatibility = %#v", installed.Compatibility)
	}
	if installed.Provenance == nil || installed.Provenance.Format != pluginFormatOpenAI {
		t.Fatalf("installed provenance = %#v", installed.Provenance)
	}
	if !regularFileExists(filepath.Join(installed.Root, "plugin.json")) ||
		!regularFileExists(filepath.Join(installed.Root, "mcp.json")) {
		t.Fatalf("installed package is not canonical Portable layout: %s", installed.Root)
	}
	for _, omitted := range []string{".codex-plugin", ".mcp.json", "commands"} {
		if _, err := os.Lstat(filepath.Join(installed.Root, omitted)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("canonical OpenAI package retained consumed artifact %s: %v", omitted, err)
		}
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

	archive := filepath.Join(t.TempDir(), "openai.zip")
	writeWrappedPluginZip(t, archive, "openai-plugin", source)

	review := manager.Validate(archive)
	if !review.Valid || review.Name != "openai-zip" || review.Compatibility.Format != pluginFormatOpenAI {
		t.Fatalf("wrapped OpenAI ZIP review = %#v", review)
	}
	if len(review.Skills) != 1 || review.Skills[0].Name != "zip-skill" {
		t.Fatalf("wrapped ZIP skills = %#v", review.Skills)
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
	if installed.PackageDigest != review.PackageDigest || installed.Compatibility.Format != pluginFormatOpenAI {
		t.Fatalf("wrapped ZIP installed state = %#v review=%#v", installed.State, review)
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
	if !review.Valid || review.Compatibility.Format != pluginFormatClaude {
		t.Fatalf("Claude review = %#v", review)
	}
	if review.Provenance == nil || review.Provenance.Format != pluginFormatClaude || !review.Provenance.Adapted {
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
	for _, consumed := range []string{".claude-plugin", ".mcp.json"} {
		if _, err := os.Lstat(filepath.Join(installed.Root, consumed)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("canonical Claude package retained consumed artifact %s: %v", consumed, err)
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
	if !review.Valid || review.Compatibility.Format != pluginFormatClaude {
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
	if doc.AllowedTools != "Read Write Bash(ls *)" {
		t.Fatalf("normalized allowed-tools = %q", doc.AllowedTools)
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

func TestAutoConversionKeepsUnknownMCPAuthAsUnsupported(t *testing.T) {
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
	if review.Valid {
		t.Fatalf("unknown MCP auth behavior was accepted: %#v", review)
	}
	if !containsText(review.Unsupported, "oauth_resource") {
		t.Fatalf("unsupported MCP auth field missing: %#v", review.Unsupported)
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

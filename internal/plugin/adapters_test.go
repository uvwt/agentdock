package plugin

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenAIAdapterNormalizesCodexPluginIntoP2Model(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentdock")
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeJSONTestFile(t, filepath.Join(root, ".codex-plugin", "plugin.json"), map[string]any{
		"name": "demo-plugin", "version": "1.2.3", "description": "Codex demo",
		"skills": "./skills/", "mcpServers": "./.mcp.json",
		"interface": map[string]any{"displayName": "Demo"},
	})
	writeAdapterSkill(t, filepath.Join(root, "skills", "demo-skill"), "demo-skill")
	writeJSONTestFile(t, filepath.Join(root, ".mcp.json"), map[string]any{
		"mcpServers": map[string]any{
			"remote": map[string]any{"type": "http", "url": "https://example.com/mcp"},
		},
	})

	review := manager.ValidateSource(context.Background(), SourceRequest{Type: "local", Ref: root, Adapter: "auto"})
	if !review.Valid {
		t.Fatalf("OpenAI review invalid: %#v", review)
	}
	if review.Compatibility.Adapter != "openai" || review.Compatibility.DetectedFormat != "openai-codex" {
		t.Fatalf("compatibility = %#v", review.Compatibility)
	}
	if review.Version != "1.2.3" || len(review.Skills) != 1 || len(review.MCP) != 1 {
		t.Fatalf("normalized review = %#v", review)
	}

	result, err := manager.InstallSource(context.Background(), SourceRequest{Type: "local", Ref: root}, true)
	if err != nil {
		t.Fatal(err)
	}
	installed, err := manager.Inspect(result.Name)
	if err != nil {
		t.Fatal(err)
	}
	if installed.Source.Adapter != "openai" || installed.Compatibility.Adapter != "openai" {
		t.Fatalf("installed provenance = %#v", installed.State)
	}
	pkg, err := LoadPackage(installed.Root)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Components.MCP[0].Transport != "streamable_http" {
		t.Fatalf("normalized MCP = %#v", pkg.Components.MCP)
	}
}

func TestOpenAIAdapterReportsUnsupportedMCPAuthInsteadOfDroppingIt(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), ".agentdock"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeJSONTestFile(t, filepath.Join(root, ".codex-plugin", "plugin.json"), map[string]any{
		"name": "figma-like", "version": "2.0.20", "description": "Figma-shaped fixture",
	})
	writeJSONTestFile(t, filepath.Join(root, ".mcp.json"), map[string]any{
		"mcpServers": map[string]any{
			"figma": map[string]any{
				"type": "http", "url": "https://mcp.example.com/mcp",
				"oauth_resource": "https://mcp.example.com/mcp",
			},
		},
	})
	review := manager.ValidateSource(context.Background(), SourceRequest{Type: "local", Ref: root})
	if review.Valid {
		t.Fatalf("unsupported OAuth field was silently accepted: %#v", review)
	}
	if !sliceContainsSubstring(review.Unsupported, "oauth_resource") {
		t.Fatalf("unsupported report = %#v", review.Unsupported)
	}
	if _, err := manager.InstallSource(context.Background(), SourceRequest{Type: "local", Ref: root}, true); err == nil {
		t.Fatal("install accepted Plugin with unsupported MCP OAuth behavior")
	}
}

func TestClaudeAdapterNormalizesManifestMCPAndPersistentPlaceholders(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), ".agentdock"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeJSONTestFile(t, filepath.Join(root, ".claude-plugin", "plugin.json"), map[string]any{
		"name": "claude-demo", "version": "3.1.0", "description": "Claude demo",
	})
	writeAdapterSkill(t, filepath.Join(root, "skills", "demo-skill"), "demo-skill")
	writeJSONTestFile(t, filepath.Join(root, ".mcp.json"), map[string]any{
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
	if err := os.WriteFile(filepath.Join(root, "server.js"), []byte("console.log('ok')\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	review := manager.ValidateSource(context.Background(), SourceRequest{Type: "local", Ref: root})
	if !review.Valid || review.Compatibility.Adapter != "claude" {
		t.Fatalf("Claude review = %#v", review)
	}
	if _, err := manager.InstallSource(context.Background(), SourceRequest{Type: "local", Ref: root}, true); err != nil {
		t.Fatal(err)
	}
	installed, err := manager.Inspect("claude-demo")
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
		t.Fatalf("Claude placeholder normalization = %#v", mcp)
	}
	if strings.Contains(mcp.Args[0]+mcp.CWD+mcp.Environment["CONFIG"], "CLAUDE_PLUGIN_") {
		t.Fatalf("Claude-specific runtime placeholders leaked into normalized package: %#v", mcp)
	}
}

func TestGitSourcePinsResolvedCommitWithoutRunningWorktreeHooks(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	repo := t.TempDir()
	runGitTest(t, repo, "init")
	runGitTest(t, repo, "config", "user.name", "AgentDock Test")
	runGitTest(t, repo, "config", "user.email", "agentdock@example.invalid")
	writePortableAdapterPlugin(t, repo, "git-demo", "1.0.0")
	runGitTest(t, repo, "add", ".")
	runGitTest(t, repo, "commit", "-m", "plugin")
	commit := strings.TrimSpace(runGitTest(t, repo, "rev-parse", "HEAD"))

	manager, err := NewManager(filepath.Join(t.TempDir(), ".agentdock"))
	if err != nil {
		t.Fatal(err)
	}
	review := manager.ValidateSource(context.Background(), SourceRequest{
		Type: "git", Ref: repo, GitCommit: commit, Adapter: "portable",
	})
	if !review.Valid || review.Source.Revision != commit {
		t.Fatalf("Git review = %#v", review)
	}
	if _, err := manager.InstallSource(context.Background(), SourceRequest{
		Type: "git", Ref: repo, GitCommit: commit, Adapter: "portable",
	}, true); err != nil {
		t.Fatal(err)
	}
	installed, err := manager.Inspect("git-demo")
	if err != nil {
		t.Fatal(err)
	}
	if installed.Source.Type != "git" || installed.Source.Revision != commit {
		t.Fatalf("Git source provenance = %#v", installed.Source)
	}
}

func TestExtractPluginZipRejectsTraversalAndSymlink(t *testing.T) {
	for _, test := range []struct {
		name string
		make func(*zip.Writer) error
	}{
		{
			name: "traversal",
			make: func(writer *zip.Writer) error {
				file, err := writer.Create("../escape")
				if err != nil {
					return err
				}
				_, err = file.Write([]byte("bad"))
				return err
			},
		},
		{
			name: "symlink",
			make: func(writer *zip.Writer) error {
				header := &zip.FileHeader{Name: "link"}
				header.SetMode(os.ModeSymlink | 0o777)
				file, err := writer.CreateHeader(header)
				if err != nil {
					return err
				}
				_, err = file.Write([]byte("target"))
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			archive := filepath.Join(t.TempDir(), "plugin.zip")
			file, err := os.Create(archive)
			if err != nil {
				t.Fatal(err)
			}
			writer := zip.NewWriter(file)
			if err := test.make(writer); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			if err := extractPluginZip(archive, t.TempDir()); err == nil {
				t.Fatalf("unsafe ZIP %s was accepted", test.name)
			}
		})
	}
}

func TestCatalogAdaptersReturnStableCatalogSourceDescriptors(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), ".agentdock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Run("openai", func(t *testing.T) {
		root := t.TempDir()
		pluginRoot := filepath.Join(root, "plugins", "demo")
		writeJSONTestFile(t, filepath.Join(root, ".agents", "plugins", "marketplace.json"), map[string]any{
			"name": "openai-curated",
			"plugins": []any{map[string]any{
				"name": "demo", "source": map[string]any{"source": "local", "path": "./plugins/demo"},
				"policy":   map[string]any{"installation": "AVAILABLE", "authentication": "ON_INSTALL"},
				"category": "Developer Tools",
			}},
		})
		writeJSONTestFile(t, filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"), map[string]any{
			"name": "demo", "version": "1.0.0", "description": "Demo",
		})
		catalog, err := manager.LoadCatalog(context.Background(), SourceRequest{Ref: root, Catalog: "openai"})
		if err != nil {
			t.Fatal(err)
		}
		if len(catalog.Entries) != 1 || catalog.Entries[0].Source.Type != "catalog" ||
			catalog.Entries[0].Source.Ref != root || catalog.Entries[0].Source.CatalogItem != "demo" {
			t.Fatalf("OpenAI catalog = %#v", catalog)
		}
		review := manager.ValidateSource(context.Background(), catalog.Entries[0].Source)
		if !review.Valid || review.Compatibility.Adapter != "openai" {
			t.Fatalf("OpenAI catalog Plugin review = %#v", review)
		}
	})

	t.Run("claude-strict-false-skill-bundle", func(t *testing.T) {
		root := t.TempDir()
		bundle := filepath.Join(root, "bundles", "skills-only")
		writeAdapterSkill(t, filepath.Join(bundle, "custom-skills", "bundle-skill"), "bundle-skill")
		strict := false
		_ = strict
		writeJSONTestFile(t, filepath.Join(root, ".claude-plugin", "marketplace.json"), map[string]any{
			"name":  "claude-community",
			"owner": map[string]any{"name": "Test"},
			"plugins": []any{map[string]any{
				"name": "skills-only", "source": "./bundles/skills-only", "strict": false,
				"version": "1.4.0", "description": "Skills only",
				"skills": []string{"./custom-skills"},
			}},
		})
		catalog, err := manager.LoadCatalog(context.Background(), SourceRequest{Ref: root, Catalog: "claude"})
		if err != nil {
			t.Fatal(err)
		}
		entry := catalog.Entries[0]
		if entry.Strict == nil || *entry.Strict || len(entry.Skills) != 1 {
			t.Fatalf("Claude catalog entry = %#v", entry)
		}
		review := manager.ValidateSource(context.Background(), entry.Source)
		if !review.Valid || review.Name != "skills-only" || review.Version != "1.4.0" ||
			review.Compatibility.DetectedFormat != "claude-marketplace-skill-bundle" || len(review.Skills) != 1 {
			t.Fatalf("Claude strict:false bundle review = %#v", review)
		}
	})
}

func TestPortableRootTakesPriorityOverCodexFallback(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), ".agentdock"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writePortableAdapterPlugin(t, root, "portable-first", "1.0.0")
	writeJSONTestFile(t, filepath.Join(root, ".codex-plugin", "plugin.json"), map[string]any{
		"name": "fallback", "version": "9.9.9", "description": "fallback",
	})

	review := manager.ValidateSource(context.Background(), SourceRequest{Type: "local", Ref: root, Adapter: "auto"})
	if !review.Valid {
		t.Fatalf("portable review invalid: %#v", review)
	}
	if review.Name != "portable-first" || review.Compatibility.Adapter != "portable" ||
		review.Compatibility.DetectedFormat != "portable-agent-plugin" {
		t.Fatalf("portable precedence = %#v", review)
	}
}

func TestLocalZIPArchiveSourceAutoDetectionAndPin(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), ".agentdock"))
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "demo-plugin.zip")
	writePortableZipArchive(t, archive, "zip-demo", "1.0.0")

	review := manager.ValidateSource(context.Background(), SourceRequest{Type: "auto", Ref: archive, Adapter: "portable"})
	if !review.Valid {
		t.Fatalf("local ZIP review invalid: %#v", review)
	}
	absolute, err := filepath.Abs(archive)
	if err != nil {
		t.Fatal(err)
	}
	if review.Source.Type != "archive" || review.Source.Ref != absolute ||
		!strings.HasPrefix(review.Source.Revision, "sha256:") {
		t.Fatalf("local ZIP source provenance = %#v", review.Source)
	}
	if _, err := manager.InstallSource(context.Background(), SourceRequest{
		Type: "archive", Ref: archive, Adapter: "portable", SHA256: review.Source.Revision,
	}, true); err != nil {
		t.Fatal(err)
	}
}

func TestSourceSubdirRejectsSymlinkParentEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "plugin")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := selectPluginSourceRoot(root, "linked/plugin"); err == nil {
		t.Fatal("source subdir accepted a symlink parent escape")
	}
}

func TestGitSourceSelectorChangeRequiresExplicitRebind(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	repo := t.TempDir()
	runGitTest(t, repo, "init")
	runGitTest(t, repo, "config", "user.name", "AgentDock Test")
	runGitTest(t, repo, "config", "user.email", "agentdock@example.invalid")
	writePortableAdapterPlugin(t, repo, "selector-demo", "1.0.0")
	runGitTest(t, repo, "add", ".")
	runGitTest(t, repo, "commit", "-m", "plugin")
	runGitTest(t, repo, "branch", "track-a")
	runGitTest(t, repo, "branch", "track-b")

	manager, err := NewManager(filepath.Join(t.TempDir(), ".agentdock"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.InstallSource(context.Background(), SourceRequest{
		Type: "git", Ref: repo, GitRef: "track-a", Adapter: "portable",
	}, true); err != nil {
		t.Fatal(err)
	}
	_, err = manager.UpdateSource(context.Background(), SourceRequest{
		Type: "git", Ref: repo, GitRef: "track-b", Adapter: "portable",
	}, false)
	var pluginErr *Error
	if !errors.As(err, &pluginErr) || pluginErr.Code != "PLUGIN_SOURCE_CHANGE_CONFIRMATION_REQUIRED" {
		t.Fatalf("Git selector change error = %#v", err)
	}
	result, err := manager.UpdateSource(context.Background(), SourceRequest{
		Type: "git", Ref: repo, GitRef: "track-b", Adapter: "portable",
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed {
		t.Fatalf("same commit/content selector rebind unexpectedly changed package: %#v", result)
	}
	installed, err := manager.Inspect("selector-demo")
	if err != nil {
		t.Fatal(err)
	}
	if installed.Source.Selector != "track-b" {
		t.Fatalf("persisted selector = %#v", installed.Source)
	}
}

func TestCatalogRejectsDuplicateNamesAndShowsResolvedSource(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), ".agentdock"))
	if err != nil {
		t.Fatal(err)
	}

	t.Run("duplicate", func(t *testing.T) {
		root := t.TempDir()
		writeJSONTestFile(t, filepath.Join(root, ".agents", "plugins", "marketplace.json"), map[string]any{
			"name": "duplicate-catalog",
			"plugins": []any{
				map[string]any{"name": "demo", "source": "./plugins/one"},
				map[string]any{"name": "demo", "source": "./plugins/two"},
			},
		})
		if _, err := manager.LoadCatalog(context.Background(), SourceRequest{Ref: root, Catalog: "openai"}); err == nil {
			t.Fatal("catalog accepted duplicate Plugin names")
		}
	})

	t.Run("resolved-source", func(t *testing.T) {
		root := t.TempDir()
		pluginRoot := filepath.Join(root, "plugins", "demo")
		writeJSONTestFile(t, filepath.Join(root, ".agents", "plugins", "marketplace.json"), map[string]any{
			"name": "resolved-catalog",
			"plugins": []any{
				map[string]any{"name": "demo", "source": map[string]any{"source": "local", "path": "./plugins/demo"}},
			},
		})
		writeJSONTestFile(t, filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"), map[string]any{
			"name": "demo", "version": "1.0.0", "description": "Demo",
		})
		catalog, err := manager.LoadCatalog(context.Background(), SourceRequest{Ref: root, Catalog: "openai"})
		if err != nil {
			t.Fatal(err)
		}
		if len(catalog.Entries) != 1 || catalog.Entries[0].ResolvedSource == nil {
			t.Fatalf("catalog resolved source missing: %#v", catalog)
		}
		resolved := catalog.Entries[0].ResolvedSource
		if resolved.Type != "local" || resolved.Ref != "./plugins/demo" || resolved.Adapter != "openai" {
			t.Fatalf("catalog resolved source = %#v", resolved)
		}
	})
}

func TestCatalogUnderlyingSourceChangeRequiresExplicitRebind(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), ".agentdock"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for _, fixture := range []struct {
		dir     string
		version string
	}{
		{dir: "one", version: "1.0.0"},
		{dir: "two", version: "2.0.0"},
	} {
		writeJSONTestFile(t, filepath.Join(root, "plugins", fixture.dir, ".codex-plugin", "plugin.json"), map[string]any{
			"name": "catalog-demo", "version": fixture.version, "description": fixture.dir,
		})
	}
	writeCatalog := func(path string) {
		writeJSONTestFile(t, filepath.Join(root, ".agents", "plugins", "marketplace.json"), map[string]any{
			"name": "moving-catalog",
			"plugins": []any{
				map[string]any{"name": "catalog-demo", "source": "./plugins/" + path},
			},
		})
	}
	writeCatalog("one")
	request := SourceRequest{Type: "catalog", Ref: root, Catalog: "openai", CatalogItem: "catalog-demo"}
	if _, err := manager.InstallSource(context.Background(), request, true); err != nil {
		t.Fatal(err)
	}

	writeCatalog("two")
	_, err = manager.UpdateSource(context.Background(), request, false)
	var pluginErr *Error
	if !errors.As(err, &pluginErr) || pluginErr.Code != "PLUGIN_SOURCE_CHANGE_CONFIRMATION_REQUIRED" {
		t.Fatalf("catalog source move error = %#v", err)
	}
	if _, err := manager.UpdateSource(context.Background(), request, true); err != nil {
		t.Fatal(err)
	}
	installed, err := manager.Inspect("catalog-demo")
	if err != nil {
		t.Fatal(err)
	}
	if installed.Version != "2.0.0" || installed.Source.ResolvedRef != "plugins/two" {
		t.Fatalf("catalog source rebind = %#v", installed.State)
	}
}

func TestP2SourceWithoutAdapterMatchesPortableP3Binding(t *testing.T) {
	legacy := Source{Type: "local", Ref: "/tmp/plugin"}
	portable := Source{Type: "local", Ref: "/tmp/plugin", Adapter: "portable"}
	if !samePluginSourceBinding(legacy, portable) {
		t.Fatal("P2 source without adapter should match portable P3 binding")
	}
	openAI := portable
	openAI.Adapter = "openai"
	if samePluginSourceBinding(legacy, openAI) {
		t.Fatal("P2 source without adapter must not silently rebind to OpenAI adapter")
	}
}

func TestAdapterDeduplicatesRepeatedSkillPath(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), ".agentdock"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeJSONTestFile(t, filepath.Join(root, ".claude-plugin", "plugin.json"), map[string]any{
		"name": "dedupe-plugin", "version": "1.0.0", "description": "Dedupe",
		"skills": []string{"./custom-skills", "./custom-skills"},
	})
	writeAdapterSkill(t, filepath.Join(root, "custom-skills", "only-skill"), "only-skill")
	review := manager.ValidateSource(context.Background(), SourceRequest{Type: "local", Ref: root, Adapter: "claude"})
	if !review.Valid {
		t.Fatalf("dedupe review invalid: %#v", review)
	}
	if len(review.Skills) != 1 || review.Skills[0].Name != "only-skill" {
		t.Fatalf("deduped skills = %#v", review.Skills)
	}
}

func TestArchiveDigestNormalization(t *testing.T) {
	sum := sha256.Sum256([]byte("archive"))
	hexValue := hex.EncodeToString(sum[:])
	if got := normalizePluginSHA256(hexValue); got != "sha256:"+hexValue {
		t.Fatalf("normalizePluginSHA256() = %q", got)
	}
	if got := normalizePluginSHA256("short"); !strings.HasPrefix(got, "invalid:") {
		t.Fatalf("invalid digest normalization = %q", got)
	}
}

func writePortableZipArchive(t *testing.T, archivePath, name, version string) {
	t.Helper()
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	writeEntry := func(path string, data []byte, mode os.FileMode) {
		t.Helper()
		header := &zip.FileHeader{Name: path}
		header.SetMode(mode)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := json.Marshal(map[string]any{
		"$schema": pluginSchemaURI, "name": name, "version": version, "description": "ZIP fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	writeEntry("plugin.json", manifest, 0o600)
	writeEntry("skills/fixture-skill/SKILL.md", []byte("---\nname: fixture-skill\ndescription: ZIP fixture skill.\n---\n\n# Fixture\n"), 0o600)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeAdapterSkill(t *testing.T, root, name string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: Adapter fixture skill.\n---\n\n# Fixture\n"
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writePortableAdapterPlugin(t *testing.T, root, name, version string) {
	t.Helper()
	writeJSONTestFile(t, filepath.Join(root, "plugin.json"), map[string]any{
		"$schema": pluginSchemaURI, "name": name, "version": version, "description": "Portable fixture",
	})
	writeAdapterSkill(t, filepath.Join(root, "skills", "fixture-skill"), "fixture-skill")
}

func writeJSONTestFile(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func runGitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
}

func sliceContainsSubstring(values []string, substring string) bool {
	for _, value := range values {
		if strings.Contains(value, substring) {
			return true
		}
	}
	return false
}

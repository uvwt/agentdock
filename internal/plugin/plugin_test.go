package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func writeTestPlugin(t *testing.T, root, name, version string, withMCP bool) {
	t.Helper()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"$schema":     pluginSchemaURI,
		"name":        name,
		"description": "Demo Plugin",
	}
	if version != "" {
		manifest["version"] = version
	}
	writeJSONFile(t, filepath.Join(root, "plugin.json"), manifest)

	skillRoot := filepath.Join(root, "skills", "demo-skill")
	if err := os.MkdirAll(skillRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	skill := "---\nname: demo-skill\ndescription: Demo plugin skill.\n---\n\n# Demo\n"
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte(skill), 0o600); err != nil {
		t.Fatal(err)
	}

	if !withMCP {
		return
	}
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	runner := filepath.Join(bin, "runner")
	if err := os.WriteFile(runner, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeJSONFile(t, filepath.Join(root, "mcp.json"), map[string]any{
		"$schema": mcpSchemaURI,
		"mcpServers": map[string]any{
			"local": map[string]any{
				"type":    "stdio",
				"command": "./bin/runner",
				"args":    []string{"--data", "${PLUGIN_DATA}/cache"},
				"env":     map[string]string{"CONFIG": "${PLUGIN_ROOT}/config.json", "MODE": "demo"},
				"cwd":     "${PLUGIN_ROOT}",
			},
			"remote": map[string]any{
				"type":    "streamable-http",
				"url":     "https://example.com/mcp",
				"headers": map[string]string{"X-Tenant": "public"},
			},
		},
	})
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadPackageParsesPortableAgentPlugin(t *testing.T) {
	root := t.TempDir()
	writeTestPlugin(t, root, "demo.plugin", "1.2.3", true)

	pkg, err := LoadPackage(root)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Manifest.Schema != pluginSchemaURI || pkg.Manifest.Name != "demo.plugin" || pkg.Manifest.Version != "1.2.3" {
		t.Fatalf("manifest = %+v", pkg.Manifest)
	}
	if len(pkg.Components.Skills) != 1 || pkg.Components.Skills[0].Name != "demo-skill" {
		t.Fatalf("skills = %+v", pkg.Components.Skills)
	}
	if len(pkg.Components.MCP) != 2 {
		t.Fatalf("mcp = %+v", pkg.Components.MCP)
	}
	local := pkg.Components.MCP[0]
	if local.Name != "local" || local.Transport != "stdio" || local.StorageKey != RuntimeMCPName("demo.plugin", "local") {
		t.Fatalf("local MCP = %+v", local)
	}
	if local.Environment["CONFIG"] != "${PLUGIN_ROOT}/config.json" {
		t.Fatalf("portable env was not preserved: %+v", local.Environment)
	}
	if pkg.PackageDigest == "" || len(pkg.Unsupported) != 0 {
		t.Fatalf("digest/unsupported = %q / %+v", pkg.PackageDigest, pkg.Unsupported)
	}
}

func TestLoadPackageDefaultsMissingVersionToLocal(t *testing.T) {
	root := t.TempDir()
	writeTestPlugin(t, root, "demo.plugin", "", false)
	pkg, err := LoadPackage(root)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Manifest.Version != VersionLocal {
		t.Fatalf("version = %q, want local", pkg.Manifest.Version)
	}
	if len(pkg.Warnings) == 0 || !strings.Contains(strings.Join(pkg.Warnings, " "), "version=local") {
		t.Fatalf("warnings = %+v", pkg.Warnings)
	}
}

func TestLoadPackageReportsUnsupportedWithoutPartialAcceptance(t *testing.T) {
	root := t.TempDir()
	writeTestPlugin(t, root, "demo.plugin", "1.0.0", false)
	writeJSONFile(t, filepath.Join(root, "plugin.json"), map[string]any{
		"$schema": pluginSchemaURI,
		"name":    "demo.plugin", "version": "1.0.0", "description": "Demo Plugin",
		"future_capability": map[string]any{"enabled": true},
		"extensions":        map[string]any{"example.vendor/future": map[string]any{"enabled": true}},
	})
	if err := os.MkdirAll(filepath.Join(root, "hooks"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "hooks", "hook.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeJSONFile(t, filepath.Join(root, "mcp.json"), map[string]any{
		"$schema": mcpSchemaURI,
		"mcpServers": map[string]any{
			"legacy": map[string]any{"type": "sse", "url": "https://example.com/sse"},
		},
	})
	pkg, err := LoadPackage(root)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(pkg.Unsupported, " | ")
	if !strings.Contains(got, "hooks") ||
		!strings.Contains(got, "unsupported sse transport") ||
		!strings.Contains(got, "unknown plugin.json field future_capability") ||
		!strings.Contains(got, "extension example.vendor/future") {
		t.Fatalf("unsupported = %+v", pkg.Unsupported)
	}
	manager, err := NewManager(filepath.Join(t.TempDir(), ".agentdock"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Install(context.Background(), root, true); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("install error = %v, want unsupported rejection", err)
	}
}

func TestLoadPackageRejectsUnsafePortableMCP(t *testing.T) {
	tests := []struct {
		name   string
		server map[string]any
		want   string
	}{
		{
			name:   "non-loopback HTTP",
			server: map[string]any{"type": "streamable-http", "url": "http://example.com/mcp"},
			want:   "must use https",
		},
		{
			name:   "credential header",
			server: map[string]any{"type": "streamable-http", "url": "https://example.com/mcp", "headers": map[string]string{"Authorization": "Bearer secret"}},
			want:   "credential header",
		},
		{
			name:   "command traversal",
			server: map[string]any{"type": "stdio", "command": "../outside"},
			want:   "begin with ./",
		},
		{
			name:   "reserved env",
			server: map[string]any{"type": "stdio", "command": "node", "env": map[string]string{"PLUGIN_DATA_DIR": "bad"}},
			want:   "reserved",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeTestPlugin(t, root, "demo.plugin", "1.0.0", false)
			writeJSONFile(t, filepath.Join(root, "mcp.json"), map[string]any{
				"$schema":    mcpSchemaURI,
				"mcpServers": map[string]any{"bad": test.server},
			})
			_, err := LoadPackage(root)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("LoadPackage() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestLoadPackageRejectsPackageSymlink(t *testing.T) {
	root := t.TempDir()
	writeTestPlugin(t, root, "demo.plugin", "1.0.0", false)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "assets-link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := LoadPackage(root); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("LoadPackage() error = %v, want symlink rejection", err)
	}
}

func TestManagerLifecyclePreservesCurrentOnFailedUpdateAndDataPolicy(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentdock")
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "plugin")
	writeTestPlugin(t, source, "demo.plugin", "1.0.0", true)

	installed, err := manager.Install(context.Background(), source, true)
	if err != nil {
		t.Fatal(err)
	}
	if !installed.Changed || !installed.Enabled {
		t.Fatalf("install = %+v", installed)
	}
	state, err := manager.Inspect("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	if state.Version != "1.0.0" || len(state.Components.MCP) != 2 {
		t.Fatalf("state = %+v", state)
	}
	for _, component := range state.Components.MCP {
		if len(component.Environment) != 0 || len(component.Headers) != 0 || len(component.Args) != 0 {
			t.Fatalf("state leaked package runtime values: %+v", component)
		}
	}

	dataDir, err := manager.EnsureDataDir("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "state.db"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	writeTestPlugin(t, source, "demo.plugin", "1.0.0", true)
	if err := os.WriteFile(filepath.Join(source, "extra.txt"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Update(context.Background(), source, false); err == nil || !strings.Contains(err.Error(), "different package digest") {
		t.Fatalf("same version drift error = %v", err)
	}
	current, err := manager.Inspect("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != "1.0.0" || current.PackageDigest != state.PackageDigest {
		t.Fatalf("failed update changed current state: %+v", current)
	}

	if _, err := manager.Remove(context.Background(), "demo.plugin", "keep"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "state.db")); err != nil {
		t.Fatalf("keep removed Plugin data: %v", err)
	}

	// Reinstall and purge must delete the version-independent data root.
	source2 := filepath.Join(t.TempDir(), "plugin")
	writeTestPlugin(t, source2, "demo.plugin", "2.0.0", false)
	if _, err := manager.Install(context.Background(), source2, false); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.EnsureDataDir("demo.plugin"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Remove(context.Background(), "demo.plugin", "purge"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dataDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("purge left Plugin data: %v", err)
	}
}

func TestManagerRejectsSymlinkedPluginPackageParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Developer-mode/symlink policy differs between Windows runners. The
		// os.Root implementation is covered by Windows cross-build and data-path tests.
		t.Skip("symlink creation is not reliably available on Windows CI")
	}
	home := filepath.Join(t.TempDir(), ".agentdock")
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	parent := filepath.Join(home, "plugins", "demo.plugin")
	if err := os.Symlink(outside, parent); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "plugin")
	writeTestPlugin(t, source, "demo.plugin", "1.0.0", false)
	if _, err := manager.Install(context.Background(), source, true); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("install error = %v, want symlink parent rejection", err)
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("install escaped into symlink target: entries=%v err=%v", entries, err)
	}
}

func TestStoreWriteWaitsForCrossInstanceBinding(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentdock")
	first, err := NewStore(home)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewStore(home)
	if err != nil {
		t.Fatal(err)
	}

	releaseRead, err := first.AcquireBinding(context.Background(), "demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	if _, err := second.AcquireWrite(ctx, "demo.plugin"); err == nil || !errors.Is(err, context.DeadlineExceeded) {
		releaseRead()
		t.Fatalf("AcquireWrite() error = %v, want deadline exceeded while reader is active", err)
	}
	releaseRead()

	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	releaseWrite, err := second.AcquireWrite(ctx2, "demo.plugin")
	if err != nil {
		t.Fatalf("AcquireWrite() after reader release: %v", err)
	}
	releaseWrite()
}

func TestLocalUpdateRestoreStateRestoresPreviousPackageContent(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentdock")
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "plugin")
	writeTestPlugin(t, source, "demo.plugin", "", false)

	if _, err := manager.Install(context.Background(), source, true); err != nil {
		t.Fatal(err)
	}
	previous, err := manager.Inspect("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	oldDoc, err := os.ReadFile(filepath.Join(previous.Root, "skills", "demo-skill", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}

	sourceDoc := filepath.Join(source, "skills", "demo-skill", "SKILL.md")
	if err := os.WriteFile(sourceDoc, append(oldDoc, []byte("\n# Local update\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Update(context.Background(), source, false)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Changed {
		t.Fatal("local update unexpectedly reported no-op")
	}
	current, err := manager.Inspect("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	if current.PackageDigest == previous.PackageDigest {
		t.Fatalf("local update digest did not change: %q", current.PackageDigest)
	}

	if err := manager.RestoreState(context.Background(), previous.State); err != nil {
		t.Fatal(err)
	}
	restored, err := manager.Inspect("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	if restored.PackageDigest != previous.PackageDigest {
		t.Fatalf("restored digest = %q, want %q", restored.PackageDigest, previous.PackageDigest)
	}
	restoredDoc, err := os.ReadFile(filepath.Join(restored.Root, "skills", "demo-skill", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(restoredDoc) != string(oldDoc) {
		t.Fatalf("restored local package content changed:\n%s", restoredDoc)
	}
	if manager.hasPendingUpdate("demo.plugin") {
		t.Fatal("rollback left a pending Plugin update")
	}
}

func TestFinalizeUpdateKeepsInFlightOldVersionUntilReaderRelease(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentdock")
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	sourceV1 := filepath.Join(t.TempDir(), "plugin-v1")
	writeTestPlugin(t, sourceV1, "demo.plugin", "1.0.0", false)
	if _, err := manager.Install(context.Background(), sourceV1, true); err != nil {
		t.Fatal(err)
	}
	oldRoot, err := manager.Store().PackagePath("demo.plugin", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	oldBinding, releaseOld, err := manager.Acquire(context.Background(), "demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	if oldBinding.Version != "1.0.0" {
		t.Fatalf("old binding version = %q", oldBinding.Version)
	}

	sourceV2 := filepath.Join(t.TempDir(), "plugin-v2")
	writeTestPlugin(t, sourceV2, "demo.plugin", "2.0.0", false)
	if _, err := manager.Update(context.Background(), sourceV2, true); err != nil {
		releaseOld()
		t.Fatal(err)
	}
	if _, err := os.Stat(oldRoot); err != nil {
		releaseOld()
		t.Fatalf("old version removed before runtime activation finalized: %v", err)
	}
	if !manager.hasPendingUpdate("demo.plugin") {
		releaseOld()
		t.Fatal("update did not retain a pending activation transaction")
	}

	manager.FinalizeUpdate("demo.plugin")
	if manager.hasPendingUpdate("demo.plugin") {
		releaseOld()
		t.Fatal("finalized update remained pending")
	}
	if _, err := os.Stat(oldRoot); err != nil {
		releaseOld()
		t.Fatalf("in-flight old version was removed after finalize: %v", err)
	}

	newBinding, releaseNew, err := manager.Acquire(context.Background(), "demo.plugin")
	if err != nil {
		releaseOld()
		t.Fatal(err)
	}
	if newBinding.Version != "2.0.0" {
		releaseNew()
		releaseOld()
		t.Fatalf("new binding version = %q, want 2.0.0", newBinding.Version)
	}
	releaseNew()
	releaseOld()
	if _, err := os.Stat(oldRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old version survived final reader release: %v", err)
	}
}

func TestManagerStartupCleansOnlyUnreferencedObsoleteVersions(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentdock")
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "plugin")
	writeTestPlugin(t, source, "demo.plugin", "1.0.0", false)
	if _, err := manager.Install(context.Background(), source, true); err != nil {
		t.Fatal(err)
	}

	obsolete, err := manager.Store().PackagePath("demo.plugin", "0.9.0")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(obsolete, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := NewManager(home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(obsolete); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("startup cleanup left unreferenced obsolete version: %v", err)
	}

	leased, err := manager.Store().PackagePath("demo.plugin", "0.8.0")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(leased, 0o700); err != nil {
		t.Fatal(err)
	}
	release, err := manager.Store().AcquireVersionRead("demo.plugin", "0.8.0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewManager(home); err != nil {
		release()
		t.Fatal(err)
	}
	if _, err := os.Stat(leased); err != nil {
		release()
		t.Fatalf("startup cleanup removed an actively referenced old version: %v", err)
	}
	release()
	if _, err := NewManager(home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(leased); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("startup cleanup left released obsolete version: %v", err)
	}
}

func TestLoadPackageNormalizesSecretEnvironmentBindingsWithoutPersistingValues(t *testing.T) {
	root := t.TempDir()
	writeTestPlugin(t, root, "demo.plugin", "1.0.0", false)
	writeJSONFile(t, filepath.Join(root, "mcp.json"), map[string]any{
		"$schema": mcpSchemaURI,
		"mcpServers": map[string]any{
			"local": map[string]any{
				"type": "stdio", "command": "node",
				"env": map[string]string{
					"TOKEN":  "${DEMO_TOKEN}",
					"CONFIG": "${PLUGIN_ROOT}/config.json",
				},
			},
			"remote": map[string]any{
				"type": "streamable-http", "url": "https://example.com/mcp",
				"headers": map[string]string{
					"Authorization": "${REMOTE_TOKEN}",
					"X-Tenant":      "public",
				},
			},
		},
	})
	pkg, err := LoadPackage(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Components.MCP) != 2 {
		t.Fatalf("MCP components = %+v", pkg.Components.MCP)
	}
	local := pkg.Components.MCP[0]
	if local.EnvBindings["TOKEN"] != "DEMO_TOKEN" || local.Environment["CONFIG"] != "${PLUGIN_ROOT}/config.json" {
		t.Fatalf("stdio env normalization = %+v bindings=%+v", local.Environment, local.EnvBindings)
	}
	if _, ok := local.Environment["TOKEN"]; ok {
		t.Fatalf("secret env reference remained a static value: %+v", local.Environment)
	}
	remote := pkg.Components.MCP[1]
	if remote.HeaderEnv["Authorization"] != "REMOTE_TOKEN" || remote.Headers["X-Tenant"] != "public" {
		t.Fatalf("HTTP header normalization = headers=%+v env=%+v", remote.Headers, remote.HeaderEnv)
	}
	if _, ok := remote.Headers["Authorization"]; ok {
		t.Fatalf("credential header remained static: %+v", remote.Headers)
	}
	required := strings.Join(append(append([]string{}, local.RequiredEnv...), remote.RequiredEnv...), ",")
	if !strings.Contains(required, "DEMO_TOKEN") || !strings.Contains(required, "REMOTE_TOKEN") {
		t.Fatalf("required env names = %q", required)
	}

	manager, err := NewManager(filepath.Join(t.TempDir(), ".agentdock"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Install(context.Background(), root, true); err != nil {
		t.Fatal(err)
	}
	state, err := manager.Store().Load("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("Bearer")) || bytes.Contains(raw, []byte("${DEMO_TOKEN}")) || bytes.Contains(raw, []byte("${REMOTE_TOKEN}")) {
		t.Fatalf("Plugin state leaked runtime secret expressions: %s", raw)
	}
}

func TestLoadPackageRejectsSensitiveLiteralEnvAndCredentialQuery(t *testing.T) {
	tests := []struct {
		name   string
		server map[string]any
		want   string
	}{
		{
			name: "sensitive literal env",
			server: map[string]any{
				"type": "stdio", "command": "node",
				"env": map[string]string{"API_KEY": "literal-secret"},
			},
			want: "must use a ${ENV_NAME} binding",
		},
		{
			name: "credential query",
			server: map[string]any{
				"type": "streamable-http", "url": "https://example.com/mcp?token=literal-secret",
			},
			want: "query parameters",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeTestPlugin(t, root, "demo.plugin", "1.0.0", false)
			writeJSONFile(t, filepath.Join(root, "mcp.json"), map[string]any{
				"$schema":    mcpSchemaURI,
				"mcpServers": map[string]any{"bad": test.server},
			})
			_, err := LoadPackage(root)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("LoadPackage() error = %v, want %q", err, test.want)
			}
		})
	}
}

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

func hasPendingPluginActivationForTest(manager *Manager, name string) bool {
	_, ok := manager.pendingActivation(name)
	return ok
}

func simulatePluginActivationCrashForTest(t *testing.T, manager *Manager, name string) {
	t.Helper()
	pending, ok := manager.pendingActivation(name)
	if !ok {
		t.Fatalf("Plugin %s has no pending activation", name)
	}
	pending.release()
	manager.pendingMu.Lock()
	delete(manager.pending, name)
	manager.pendingMu.Unlock()
}

func installLocalPluginForTest(manager *Manager, ctx context.Context, source string, enabled bool) (ChangeResult, error) {
	return installPluginSourceForTest(manager, ctx, legacyLocalSourceRequest(source), enabled)
}

func installPluginSourceForTest(manager *Manager, ctx context.Context, request SourceRequest, enabled bool) (ChangeResult, error) {
	review := manager.ValidateSource(ctx, request)
	result, err := manager.InstallReviewedSource(ctx, request, enabled, review.ReviewToken)
	if err != nil || !result.Changed {
		return result, err
	}
	if err := manager.FinalizeActivation(result.Name); err != nil {
		return ChangeResult{}, err
	}
	return result, nil
}

func updateLocalPluginForTest(manager *Manager, ctx context.Context, source string, confirmSourceChange bool) (ChangeResult, error) {
	return updatePluginSourceForTest(manager, ctx, legacyLocalSourceRequest(source), confirmSourceChange)
}

func updatePluginSourceForTest(manager *Manager, ctx context.Context, request SourceRequest, confirmSourceChange bool) (ChangeResult, error) {
	review := manager.ValidateSource(ctx, request)
	return manager.UpdateReviewedSource(ctx, request, confirmSourceChange, review.ReviewToken, nil)
}

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
	if _, err := installLocalPluginForTest(manager, context.Background(), root, true); err == nil || !strings.Contains(err.Error(), "unsupported") {
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

	installed, err := installLocalPluginForTest(manager, context.Background(), source, true)
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
	if _, err := updateLocalPluginForTest(manager, context.Background(), source, false); err == nil || !strings.Contains(err.Error(), "different package digest") {
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
	if _, err := installLocalPluginForTest(manager, context.Background(), source2, false); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.EnsureDataDir("demo.plugin"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RemoveWithLifecycle(context.Background(), "demo.plugin", "purge", RemoveLifecycle{
		Purge: func(PurgeOwnership) error { return nil },
	}); err != nil {
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
	if _, err := installLocalPluginForTest(manager, context.Background(), source, true); err == nil || !strings.Contains(err.Error(), "symlink") {
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

func TestLocalUpdateAbortActivationRestoresPreviousPackageContent(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentdock")
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "plugin")
	writeTestPlugin(t, source, "demo.plugin", "", false)

	if _, err := installLocalPluginForTest(manager, context.Background(), source, true); err != nil {
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
	updated, err := updateLocalPluginForTest(manager, context.Background(), source, false)
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
	if current.PackageDigest != previous.PackageDigest {
		t.Fatalf("pending local update became formal state: got %q want %q", current.PackageDigest, previous.PackageDigest)
	}
	candidate, releaseCandidate, err := manager.AcquireActivationCandidate("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	if candidate.PackageDigest == previous.PackageDigest {
		releaseCandidate()
		t.Fatalf("activation candidate digest did not change: %q", candidate.PackageDigest)
	}
	releaseCandidate()

	if err := manager.AbortActivation(context.Background(), previous.Name); err != nil {
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
	if hasPendingPluginActivationForTest(manager, "demo.plugin") {
		t.Fatal("rollback left a pending Plugin update")
	}
}

func TestFinalizeActivationKeepsInFlightOldVersionUntilReaderRelease(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentdock")
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	sourceV1 := filepath.Join(t.TempDir(), "plugin-v1")
	writeTestPlugin(t, sourceV1, "demo.plugin", "1.0.0", false)
	if _, err := installLocalPluginForTest(manager, context.Background(), sourceV1, true); err != nil {
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
	if _, err := updateLocalPluginForTest(manager, context.Background(), sourceV2, true); err != nil {
		releaseOld()
		t.Fatal(err)
	}
	if _, err := os.Stat(oldRoot); err != nil {
		releaseOld()
		t.Fatalf("old version removed before runtime activation finalized: %v", err)
	}
	if !hasPendingPluginActivationForTest(manager, "demo.plugin") {
		releaseOld()
		t.Fatal("update did not retain a pending activation transaction")
	}

	if err := manager.FinalizeActivation("demo.plugin"); err != nil {
		t.Fatal(err)
	}
	if hasPendingPluginActivationForTest(manager, "demo.plugin") {
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
	if _, err := installLocalPluginForTest(manager, context.Background(), source, true); err != nil {
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
	if _, err := installLocalPluginForTest(manager, context.Background(), root, true); err != nil {
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

func TestInstallCandidateStaysHiddenUntilActivationFinalize(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentdock")
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "plugin")
	writeTestPlugin(t, source, "demo.plugin", "1.0.0", true)
	review := manager.ValidateSource(context.Background(), legacyLocalSourceRequest(source))
	result, err := manager.InstallReviewedSource(context.Background(), legacyLocalSourceRequest(source), true, review.ReviewToken)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || !hasPendingPluginActivationForTest(manager, "demo.plugin") {
		t.Fatalf("install did not retain pending activation: %#v", result)
	}
	if _, err := manager.Inspect("demo.plugin"); err == nil {
		t.Fatal("pending install published formal state before runtime activation")
	}
	listed, err := manager.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("pending install became visible in List: %#v", listed)
	}
	candidate, release, err := manager.AcquireActivationCandidate("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Version != "1.0.0" {
		t.Fatalf("activation candidate = %#v", candidate.State)
	}
	release()

	second, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Inspect("demo.plugin"); err == nil {
		t.Fatal("second Manager observed a live pending install as installed")
	}
	if err := manager.FinalizeActivation("demo.plugin"); err != nil {
		t.Fatal(err)
	}
	installed, err := manager.Inspect("demo.plugin")
	if err != nil || installed.Version != "1.0.0" {
		t.Fatalf("finalized install = %#v err=%v", installed, err)
	}
}

func TestManagerRestartRollsBackUnfinalizedInstallJournal(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentdock")
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "plugin")
	writeTestPlugin(t, source, "demo.plugin", "1.0.0", false)
	review := manager.ValidateSource(context.Background(), legacyLocalSourceRequest(source))
	if _, err := manager.InstallReviewedSource(context.Background(), legacyLocalSourceRequest(source), true, review.ReviewToken); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Store().LoadActivationTransaction("demo.plugin"); err != nil {
		t.Fatalf("install transaction was not persisted: %v", err)
	}

	simulatePluginActivationCrashForTest(t, manager, "demo.plugin")
	restarted, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Inspect("demo.plugin"); err == nil {
		t.Fatal("restart recovery published an unfinalized install")
	}
	if _, err := restarted.Store().LoadActivationTransaction("demo.plugin"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("restart recovery left install journal: %v", err)
	}
	packagePath, err := restarted.Store().PackagePath("demo.plugin", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(packagePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("restart recovery left install candidate package: %v", err)
	}
}

func TestManagerRestartFinishesInstallPublishedBeforeJournalCommit(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentdock")
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "plugin")
	writeTestPlugin(t, source, "demo.plugin", "1.0.0", false)
	review := manager.ValidateSource(context.Background(), legacyLocalSourceRequest(source))
	if _, err := manager.InstallReviewedSource(context.Background(), legacyLocalSourceRequest(source), true, review.ReviewToken); err != nil {
		t.Fatal(err)
	}
	transaction, err := manager.Store().LoadActivationTransaction("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	if transaction.Phase != "pending" {
		t.Fatalf("activation phase = %q", transaction.Phase)
	}

	// 模拟 FinalizeActivation 已持久化正式 state，但还没来得及把 journal
	// 标成 committed 就崩溃。正式 state 是 publish commit point，重启只能向前。
	if err := manager.Store().Save(transaction.Candidate); err != nil {
		t.Fatal(err)
	}
	simulatePluginActivationCrashForTest(t, manager, "demo.plugin")

	restarted, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	installed, err := restarted.Inspect("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	if installed.Version != transaction.Candidate.Version || installed.PackageDigest != transaction.Candidate.PackageDigest {
		t.Fatalf("published install was rolled back during recovery: %#v", installed.State)
	}
	if _, err := restarted.Store().LoadActivationTransaction("demo.plugin"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("published install recovery left activation journal: %v", err)
	}
}

func TestManagerRestartRollsBackUnfinalizedUpdateJournal(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentdock")
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	sourceV1 := filepath.Join(t.TempDir(), "plugin-v1")
	writeTestPlugin(t, sourceV1, "demo.plugin", "1.0.0", false)
	if _, err := installLocalPluginForTest(manager, context.Background(), sourceV1, true); err != nil {
		t.Fatal(err)
	}
	previous, err := manager.Inspect("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}

	sourceV2 := filepath.Join(t.TempDir(), "plugin-v2")
	writeTestPlugin(t, sourceV2, "demo.plugin", "2.0.0", false)
	if _, err := updateLocalPluginForTest(manager, context.Background(), sourceV2, true); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Store().LoadActivationTransaction("demo.plugin"); err != nil {
		t.Fatalf("update transaction was not persisted: %v", err)
	}
	if current, err := manager.Inspect("demo.plugin"); err != nil || current.Version != "1.0.0" {
		t.Fatalf("formal state exposed candidate before activation finalize = %#v err=%v", current, err)
	}

	// Simulate process termination after candidate package+journal are durable:
	// the OS releases the writer lease, but no in-process rollback runs.
	simulatePluginActivationCrashForTest(t, manager, "demo.plugin")
	restarted, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := restarted.Inspect("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	if restored.Version != previous.Version || restored.PackageDigest != previous.PackageDigest {
		t.Fatalf("restart did not restore previous Plugin: %#v", restored.State)
	}
	v2Path, err := restarted.Store().PackagePath("demo.plugin", "2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(v2Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("restart recovery left unfinalized candidate package: %v", err)
	}
	if _, err := restarted.Store().LoadActivationTransaction("demo.plugin"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("restart recovery left transaction journal: %v", err)
	}
}

func TestManagerRestartRestoresLocalPackageBackup(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentdock")
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "plugin")
	writeTestPlugin(t, source, "demo.plugin", "", false)
	if _, err := installLocalPluginForTest(manager, context.Background(), source, true); err != nil {
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

	mutated := "---\nname: demo-skill\ndescription: Mutated local plugin skill.\n---\n\n# Mutated\n"
	if err := os.WriteFile(filepath.Join(source, "skills", "demo-skill", "SKILL.md"), []byte(mutated), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := updateLocalPluginForTest(manager, context.Background(), source, true); err != nil {
		t.Fatal(err)
	}
	backup, err := manager.Store().UpdateBackupPath("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("pending local activation did not preserve Previous backup: %v", err)
	}
	transaction, err := manager.Store().LoadActivationTransaction("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := manager.Store().UpdateCandidatePath("demo.plugin", transaction.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(candidate); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("promoted local candidate still exists in temporary activation path: %v", err)
	}
	formal, err := manager.Inspect("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	if formal.PackageDigest != previous.PackageDigest || formal.Version != previous.Version {
		t.Fatalf("pending local activation published candidate formal state: %#v", formal.State)
	}
	simulatePluginActivationCrashForTest(t, manager, "demo.plugin")

	restarted, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := restarted.Inspect("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	restoredDoc, err := os.ReadFile(filepath.Join(restored.Root, "skills", "demo-skill", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(restoredDoc) != string(oldDoc) || restored.PackageDigest != previous.PackageDigest {
		t.Fatalf("local restart recovery did not restore reviewed previous package")
	}
	if _, err := os.Stat(backup); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("local restart recovery left backup: %v", err)
	}
	if _, err := os.Stat(candidate); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("local restart recovery left pending candidate: %v", err)
	}
}

func TestManagerRestartRecoversJournalBeforeCandidatePackageCommit(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentdock")
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "plugin-v1")
	writeTestPlugin(t, source, "demo.plugin", "1.0.0", false)
	if _, err := installLocalPluginForTest(manager, context.Background(), source, true); err != nil {
		t.Fatal(err)
	}
	previous, err := manager.Inspect("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	candidate := previous.State
	candidate.Version = "2.0.0"
	candidate.PackageDigest = "sha256:" + strings.Repeat("1", 64)
	candidate.InstalledAt = time.Now().UTC()
	transaction := ActivationTransaction{
		SchemaVersion: ActivationTransactionSchemaVersion,
		Name:          previous.Name,
		OwnerID:       strings.Repeat("a", 32),
		Phase:         "pending",
		Kind:          "update",
		Previous:      &previous.State,
		Candidate:     candidate,
		CreatedAt:     time.Now().UTC(),
	}
	if err := manager.Store().SaveActivationTransaction(transaction); err != nil {
		t.Fatal(err)
	}

	restarted, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := restarted.Inspect("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	if restored.Version != previous.Version || restored.PackageDigest != previous.PackageDigest {
		t.Fatalf("pre-commit journal recovery changed current Plugin: %#v", restored.State)
	}
	if _, err := restarted.Store().LoadActivationTransaction("demo.plugin"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pre-commit recovery left transaction journal: %v", err)
	}
}

func TestManagerRestartFinishesCommittedUpdateJournal(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentdock")
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	sourceV1 := filepath.Join(t.TempDir(), "plugin-v1")
	writeTestPlugin(t, sourceV1, "demo.plugin", "1.0.0", false)
	if _, err := installLocalPluginForTest(manager, context.Background(), sourceV1, true); err != nil {
		t.Fatal(err)
	}
	oldPath, err := manager.Store().PackagePath("demo.plugin", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	sourceV2 := filepath.Join(t.TempDir(), "plugin-v2")
	writeTestPlugin(t, sourceV2, "demo.plugin", "2.0.0", false)
	if _, err := updateLocalPluginForTest(manager, context.Background(), sourceV2, true); err != nil {
		t.Fatal(err)
	}
	transaction, err := manager.Store().LoadActivationTransaction("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	transaction.Phase = "committed"
	if err := manager.Store().SaveActivationTransaction(transaction); err != nil {
		t.Fatal(err)
	}
	simulatePluginActivationCrashForTest(t, manager, "demo.plugin")

	restarted, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	current, err := restarted.Inspect("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != "2.0.0" || current.PackageDigest != transaction.Candidate.PackageDigest {
		t.Fatalf("committed journal recovery did not finish forward: %#v", current.State)
	}
	if _, err := restarted.Store().LoadActivationTransaction("demo.plugin"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("committed recovery left transaction journal: %v", err)
	}
	if _, err := os.Stat(oldPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("committed recovery left obsolete previous version: %v", err)
	}
}

func TestUpdateRollbackPreservesPreexistingCandidateWithActiveReader(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentdock")
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	sourceV1 := filepath.Join(t.TempDir(), "plugin-v1")
	sourceV2 := filepath.Join(t.TempDir(), "plugin-v2")
	sourceV3 := filepath.Join(t.TempDir(), "plugin-v3")
	writeTestPlugin(t, sourceV1, "demo.plugin", "1.0.0", false)
	writeTestPlugin(t, sourceV2, "demo.plugin", "2.0.0", false)
	writeTestPlugin(t, sourceV3, "demo.plugin", "3.0.0", false)
	if _, err := installLocalPluginForTest(manager, context.Background(), sourceV1, true); err != nil {
		t.Fatal(err)
	}
	if _, err := updateLocalPluginForTest(manager, context.Background(), sourceV2, true); err != nil {
		t.Fatal(err)
	}
	if err := manager.FinalizeActivation("demo.plugin"); err != nil {
		t.Fatal(err)
	}

	oldV2, releaseV2, err := manager.Acquire(context.Background(), "demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	if oldV2.Version != "2.0.0" {
		releaseV2()
		t.Fatalf("reader version = %q", oldV2.Version)
	}
	if _, err := updateLocalPluginForTest(manager, context.Background(), sourceV3, true); err != nil {
		releaseV2()
		t.Fatal(err)
	}
	if err := manager.FinalizeActivation("demo.plugin"); err != nil {
		releaseV2()
		t.Fatal(err)
	}
	if _, err := os.Stat(oldV2.Root); err != nil {
		releaseV2()
		t.Fatalf("active v2 reader package removed after v3 finalize: %v", err)
	}

	previousV3, err := manager.Inspect("demo.plugin")
	if err != nil {
		releaseV2()
		t.Fatal(err)
	}
	if _, err := updateLocalPluginForTest(manager, context.Background(), sourceV2, true); err != nil {
		releaseV2()
		t.Fatal(err)
	}
	if err := manager.AbortActivation(context.Background(), previousV3.Name); err != nil {
		releaseV2()
		t.Fatal(err)
	}
	if _, err := os.Stat(oldV2.Root); err != nil {
		releaseV2()
		t.Fatalf("rollback removed preexisting candidate with active reader: %v", err)
	}

	releaseV2()
	if _, err := os.Stat(oldV2.Root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("released obsolete v2 package was not cleaned: %v", err)
	}
}

func TestActivationTransactionHidesCandidateAndRejectsConflictingOperations(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentdock")
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	sourceV1 := filepath.Join(t.TempDir(), "plugin-v1")
	sourceV2 := filepath.Join(t.TempDir(), "plugin-v2")
	writeTestPlugin(t, sourceV1, "demo.plugin", "1.0.0", false)
	writeTestPlugin(t, sourceV2, "demo.plugin", "2.0.0", false)
	if _, err := installLocalPluginForTest(manager, context.Background(), sourceV1, true); err != nil {
		t.Fatal(err)
	}
	if _, err := updateLocalPluginForTest(manager, context.Background(), sourceV2, true); err != nil {
		t.Fatal(err)
	}

	current, err := manager.Inspect("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != "1.0.0" {
		t.Fatalf("formal state exposed pending candidate version %q", current.Version)
	}
	assertActivationBlocked := func(stage string, err error) {
		t.Helper()
		var pluginErr *Error
		if !errors.As(err, &pluginErr) || pluginErr.Code != "PLUGIN_ACTIVATION_IN_PROGRESS" {
			t.Fatalf("%s error = %#v, want PLUGIN_ACTIVATION_IN_PROGRESS", stage, err)
		}
	}
	_, _, err = manager.Acquire(context.Background(), "demo.plugin")
	assertActivationBlocked("acquire", err)
	_, err = manager.SetEnabled(context.Background(), "demo.plugin", false)
	assertActivationBlocked("disable", err)
	_, err = manager.Remove(context.Background(), "demo.plugin", "keep")
	assertActivationBlocked("remove", err)

	current, err = manager.Inspect("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	if !current.Enabled || current.Version != "1.0.0" {
		t.Fatalf("conflicting operation changed formal state: %#v", current.State)
	}

	if err := manager.FinalizeActivation("demo.plugin"); err != nil {
		t.Fatal(err)
	}
	current, err = manager.Inspect("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != "2.0.0" || !current.Enabled {
		t.Fatalf("finalized state = %#v", current.State)
	}
	if _, err := manager.SetEnabled(context.Background(), "demo.plugin", false); err != nil {
		t.Fatal(err)
	}
	current, err = manager.Inspect("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	if current.Enabled {
		t.Fatal("disable after finalize was lost")
	}
}

func TestSecondManagerDoesNotRecoverLiveActivationOwner(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentdock")
	first, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	sourceV1 := filepath.Join(t.TempDir(), "plugin-v1")
	sourceV2 := filepath.Join(t.TempDir(), "plugin-v2")
	writeTestPlugin(t, sourceV1, "demo.plugin", "1.0.0", false)
	writeTestPlugin(t, sourceV2, "demo.plugin", "2.0.0", false)
	if _, err := installLocalPluginForTest(first, context.Background(), sourceV1, true); err != nil {
		t.Fatal(err)
	}
	if _, err := updateLocalPluginForTest(first, context.Background(), sourceV2, true); err != nil {
		t.Fatal(err)
	}

	second, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	current, err := second.Inspect("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != "1.0.0" {
		t.Fatalf("second Manager changed formal state during live activation: %#v", current.State)
	}
	if _, err := second.Store().LoadActivationTransaction("demo.plugin"); err != nil {
		t.Fatalf("second Manager removed live activation journal: %v", err)
	}
	acquireCtx, cancelAcquire := context.WithTimeout(context.Background(), 120*time.Millisecond)
	_, _, acquireErr := second.Acquire(acquireCtx, "demo.plugin")
	cancelAcquire()
	if acquireErr == nil || !errors.Is(acquireErr, context.DeadlineExceeded) {
		t.Fatalf("second Manager Acquire during live activation = %#v, want deadline exceeded", acquireErr)
	}
	var ownerErr *Error
	if err := second.FinalizeActivation("demo.plugin"); !errors.As(err, &ownerErr) || ownerErr.Code != "PLUGIN_UPDATE_NOT_OWNER" {
		t.Fatalf("non-owner FinalizeActivation error = %#v", err)
	}

	if err := first.FinalizeActivation("demo.plugin"); err != nil {
		t.Fatalf("live owner FinalizeActivation failed after second Manager startup: %v", err)
	}
	current, err = second.Inspect("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != "2.0.0" {
		t.Fatalf("finalized state not visible to second Manager: %#v", current.State)
	}
	acquired, release, err := second.Acquire(context.Background(), "demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	if acquired.Version != "2.0.0" {
		release()
		t.Fatalf("post-finalize Acquire version = %q, want 2.0.0", acquired.Version)
	}
	release()
}

func TestFinalizeActivationCASRejectsMutatedFormalState(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentdock")
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	sourceV1 := filepath.Join(t.TempDir(), "plugin-v1")
	sourceV2 := filepath.Join(t.TempDir(), "plugin-v2")
	writeTestPlugin(t, sourceV1, "demo.plugin", "1.0.0", false)
	writeTestPlugin(t, sourceV2, "demo.plugin", "2.0.0", false)
	if _, err := installLocalPluginForTest(manager, context.Background(), sourceV1, true); err != nil {
		t.Fatal(err)
	}
	if _, err := updateLocalPluginForTest(manager, context.Background(), sourceV2, true); err != nil {
		t.Fatal(err)
	}

	mutated, err := manager.Store().Load("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	mutated.Enabled = false
	if err := manager.Store().Save(mutated); err != nil {
		t.Fatal(err)
	}
	var pluginErr *Error
	if err := manager.FinalizeActivation("demo.plugin"); !errors.As(err, &pluginErr) || pluginErr.Code != "PLUGIN_UPDATE_CAS_FAILED" {
		t.Fatalf("FinalizeActivation CAS error = %#v", err)
	}
	_, ok := manager.pendingActivation("demo.plugin")
	if !ok {
		t.Fatal("CAS failure lost activation owner")
	}
	if err := manager.AbortActivation(context.Background(), "demo.plugin"); err != nil {
		t.Fatal(err)
	}
	restored, err := manager.Inspect("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	if restored.Version != "1.0.0" || !restored.Enabled {
		t.Fatalf("CAS rollback did not restore previous state: %#v", restored.State)
	}
}

func TestPluginPurgeFailureKeepsTombstoneForIdempotentRetry(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentdock")
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "plugin")
	writeTestPlugin(t, source, "demo.plugin", "1.0.0", false)
	if _, err := installLocalPluginForTest(manager, context.Background(), source, true); err != nil {
		t.Fatal(err)
	}
	dataDir, err := manager.EnsureDataDir("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dataDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataDir, []byte("not-a-directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = manager.RemoveWithLifecycle(context.Background(), "demo.plugin", "purge", RemoveLifecycle{
		Purge: func(PurgeOwnership) error { return nil },
	})
	var pluginErr *Error
	if !errors.As(err, &pluginErr) || pluginErr.Code != "PLUGIN_PURGE_FAILED" {
		t.Fatalf("first purge error = %#v", err)
	}
	if _, err := manager.Inspect("demo.plugin"); err == nil {
		t.Fatal("failed purge exposed Plugin as installed after tombstone commit")
	}
	record, err := manager.Store().LoadRemovalRecord("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	if record.Phase != removalPhasePurging || record.RemovedState == nil || record.RemovedState.Version != "1.0.0" {
		t.Fatalf("purge tombstone = %#v", record)
	}

	if err := os.Remove(dataDir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	result, err := manager.RemoveWithLifecycle(context.Background(), "demo.plugin", "purge", RemoveLifecycle{
		Purge: func(PurgeOwnership) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed {
		t.Fatalf("retry purge should only finish committed cleanup: %#v", result)
	}
	if _, err := manager.Store().LoadRemovalRecord("demo.plugin"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retry purge left tombstone: %v", err)
	}
}

func TestPluginPurgeCommitsTombstoneBeforeIrreversibleCleanup(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentdock")
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "plugin")
	writeTestPlugin(t, source, "demo.plugin", "1.0.0", false)
	if _, err := installLocalPluginForTest(manager, context.Background(), source, true); err != nil {
		t.Fatal(err)
	}
	installed, err := manager.Inspect("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	dataDir, err := manager.EnsureDataDir("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	dataFile := filepath.Join(dataDir, "state.db")
	if err := os.WriteFile(dataFile, []byte("durable-user-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	statePath, err := manager.Store().StatePath("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}

	_, err = manager.RemoveWithLifecycle(context.Background(), "demo.plugin", "purge", RemoveLifecycle{
		BeforeDelete: func(State) error {
			if err := os.Remove(statePath); err != nil {
				return err
			}
			if err := os.Mkdir(statePath, 0o700); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(statePath, "block-delete"), []byte("x"), 0o600)
		},
		Purge: func(PurgeOwnership) error { return nil },
	})
	var pluginErr *Error
	if !errors.As(err, &pluginErr) || pluginErr.Code != "PLUGIN_REMOVE_FAILED" {
		t.Fatalf("state delete failure = %#v", err)
	}
	if _, err := os.Stat(dataFile); err != nil {
		t.Fatalf("purge deleted Plugin data before formal remove completed: %v", err)
	}
	record, err := manager.Store().LoadRemovalRecord("demo.plugin")
	if err != nil || record.Phase != removalPhasePurging {
		t.Fatalf("committed purge tombstone = %#v err=%v", record, err)
	}

	// Recreate a stale formal state to model the real failure mode where state
	// deletion did not happen at all. The tombstone must still hide it everywhere.
	if err := os.RemoveAll(statePath); err != nil {
		t.Fatal(err)
	}
	if err := manager.Store().Save(installed.State); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Inspect("demo.plugin"); err == nil {
		t.Fatal("purging tombstone did not hide stale formal state from Inspect")
	}
	listed, err := manager.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("purging tombstone did not hide stale formal state from List: %#v", listed)
	}
	if _, err := installLocalPluginForTest(manager, context.Background(), source, true); err == nil {
		t.Fatal("install was allowed to overwrite a committed purge")
	} else if !errors.As(err, &pluginErr) || pluginErr.Code != "PLUGIN_REMOVAL_IN_PROGRESS" {
		t.Fatalf("install during committed purge error = %#v", err)
	}

	if _, err := manager.RemoveWithLifecycle(context.Background(), "demo.plugin", "purge", RemoveLifecycle{
		Purge: func(PurgeOwnership) error { return nil },
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dataDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retry purge left Plugin data: %v", err)
	}
	if _, err := manager.Store().LoadRemovalRecord("demo.plugin"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retry purge left tombstone: %v", err)
	}
}

func TestPluginKeepThenMissingPackagePurgeUsesRemovalOwnership(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentdock")
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "plugin")
	writeTestPlugin(t, source, "demo.plugin", "1.0.0", true)
	if _, err := installLocalPluginForTest(manager, context.Background(), source, true); err != nil {
		t.Fatal(err)
	}
	installed, err := manager.Inspect("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	dataDir, err := manager.EnsureDataDir("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "state.db"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Remove(context.Background(), "demo.plugin", "keep"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Inspect("demo.plugin"); err == nil {
		t.Fatal("keep removal left Plugin formally installed")
	}

	var gotOwnership PurgeOwnership
	result, err := manager.RemoveWithLifecycle(context.Background(), "demo.plugin", "purge", RemoveLifecycle{
		Purge: func(ownership PurgeOwnership) error {
			gotOwnership = ownership
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed {
		t.Fatalf("missing-package purge should be idempotent cleanup only: %#v", result)
	}
	if len(gotOwnership.MCPStorageKeys) != len(installed.MCPStorageKeys) {
		t.Fatalf("purge ownership = %#v, want keys %#v", gotOwnership, installed.MCPStorageKeys)
	}
	if _, err := os.Stat(dataDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing-package purge left Plugin data: %v", err)
	}
	if _, err := manager.Store().LoadRemovalOwnership("demo.plugin"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("purge left removal ownership record: %v", err)
	}
}

func TestPluginPurgeLifecycleLockBlocksSameNameReinstall(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentdock")
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	sourceV1 := filepath.Join(t.TempDir(), "plugin-v1")
	sourceV2 := filepath.Join(t.TempDir(), "plugin-v2")
	writeTestPlugin(t, sourceV1, "demo.plugin", "1.0.0", false)
	writeTestPlugin(t, sourceV2, "demo.plugin", "2.0.0", false)
	if _, err := installLocalPluginForTest(manager, context.Background(), sourceV1, true); err != nil {
		t.Fatal(err)
	}

	purgeEntered := make(chan struct{})
	releasePurge := make(chan struct{})
	removeDone := make(chan error, 1)
	go func() {
		_, err := manager.RemoveWithLifecycle(context.Background(), "demo.plugin", "purge", RemoveLifecycle{
			Purge: func(PurgeOwnership) error {
				close(purgeEntered)
				<-releasePurge
				return nil
			},
		})
		removeDone <- err
	}()
	<-purgeEntered

	installDone := make(chan error, 1)
	go func() {
		_, err := installLocalPluginForTest(manager, context.Background(), sourceV2, true)
		installDone <- err
	}()
	select {
	case err := <-installDone:
		close(releasePurge)
		t.Fatalf("same-name reinstall completed while old purge held lifecycle lock: %v", err)
	case <-time.After(120 * time.Millisecond):
	}
	close(releasePurge)
	if err := <-removeDone; err != nil {
		t.Fatal(err)
	}
	if err := <-installDone; err != nil {
		t.Fatal(err)
	}
	current, err := manager.Inspect("demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != "2.0.0" {
		t.Fatalf("reinstall after purge = %#v", current.State)
	}
}

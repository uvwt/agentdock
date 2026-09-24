package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pluginruntime "github.com/uvwt/agentdock/internal/plugin"
)

func TestStdioMCPUsesWritableRuntimeSnapshot(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentdock")
	manager, err := pluginruntime.NewManager(home)
	if err != nil {
		t.Fatal(err)
	}

	source := filepath.Join(t.TempDir(), "runtime-plugin")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	writePluginServiceJSON(t, filepath.Join(source, "plugin.json"), map[string]any{
		"$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json",
		"name":    "runtime.snapshot",
		"version": "1.0.0",
	})
	writePluginServiceJSON(t, filepath.Join(source, "mcp.json"), map[string]any{
		"$schema": "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json",
		"mcpServers": map[string]any{
			"local": map[string]any{
				"type":    "stdio",
				"command": "./runner",
				"args":    []string{"${PLUGIN_ROOT}/server.js", "${PLUGIN_DATA}/state"},
				"cwd":     "${PLUGIN_ROOT}",
				"env":     map[string]string{"CONFIG": "${PLUGIN_ROOT}/config.json"},
			},
		},
	})
	for path, content := range map[string]string{
		"runner":      "#!/bin/sh\nexit 0\n",
		"server.js":   "console.log('runtime');\n",
		"config.json": "{}\n",
	} {
		mode := os.FileMode(0o600)
		if path == "runner" {
			mode = 0o700
		}
		if err := os.WriteFile(filepath.Join(source, path), []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}

	review := manager.Validate(source)
	if !review.Valid {
		t.Fatalf("Plugin review invalid: %#v", review)
	}
	result, err := manager.InstallReviewedSource(context.Background(), source, true, review.ReviewToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.FinalizeActivation(result.Name); err != nil {
		t.Fatal(err)
	}
	installed, release, err := manager.Acquire(context.Background(), result.Name)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	service := &Service{manager: manager}
	configs, leases, err := service.ownedMCPConfigs("", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	released := false
	defer func() {
		if !released {
			releasePluginLeases(leases)
		}
	}()
	if len(configs) != 1 {
		t.Fatalf("MCP configs = %#v", configs)
	}
	cfg := configs[0]
	runtimeRoot := cfg.PluginRuntimeRoot
	if runtimeRoot == installed.Root {
		t.Fatalf("stdio MCP still runs from immutable package root %q", installed.Root)
	}
	runtimePrefix := filepath.Join(home, "run", "plugins", result.Name, result.Version) + string(filepath.Separator)
	if !strings.HasPrefix(runtimeRoot, runtimePrefix) {
		t.Fatalf("runtime root %q is outside %q", runtimeRoot, runtimePrefix)
	}
	if !strings.HasPrefix(filepath.Base(runtimeRoot), "generation-") {
		t.Fatalf("runtime generation %q does not use generation-* naming", runtimeRoot)
	}
	if cfg.Command != filepath.Join(runtimeRoot, "runner") || cfg.Cwd != runtimeRoot {
		t.Fatalf("stdio command/cwd = %q / %q, runtime root = %q", cfg.Command, cfg.Cwd, runtimeRoot)
	}
	dataDir, err := manager.Store().DataPath(result.Name)
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := []string{filepath.Join(runtimeRoot, "server.js"), filepath.Join(dataDir, "state")}
	if len(cfg.Args) != len(wantArgs) ||
		filepath.Clean(filepath.FromSlash(cfg.Args[0])) != wantArgs[0] ||
		filepath.Clean(filepath.FromSlash(cfg.Args[1])) != wantArgs[1] {
		t.Fatalf("stdio args = %#v, want %#v", cfg.Args, wantArgs)
	}
	if filepath.Clean(filepath.FromSlash(cfg.StaticEnv["CONFIG"])) != filepath.Join(runtimeRoot, "config.json") {
		t.Fatalf("CONFIG = %q", cfg.StaticEnv["CONFIG"])
	}

	generated := filepath.Join(runtimeRoot, "node_modules", "generated.txt")
	if err := os.MkdirAll(filepath.Dir(generated), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(generated, []byte("runtime output\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(installed.Root, "node_modules")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("runtime write reached immutable package: %v", err)
	}
	reloaded, err := pluginruntime.LoadPackage(installed.Root)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.PackageDigest != installed.PackageDigest {
		t.Fatalf("immutable package digest changed: %q != %q", reloaded.PackageDigest, installed.PackageDigest)
	}

	releasePluginLeases(leases)
	released = true
	if _, err := os.Stat(runtimeRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("runtime snapshot still exists after MCP lease release: %v", err)
	}
}

func writePluginServiceJSON(t *testing.T, path string, value any) {
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

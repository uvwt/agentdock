package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
	pluginruntime "github.com/uvwt/agentdock/internal/plugin"
	toolplugin "github.com/uvwt/agentdock/internal/tool/plugin"
)

const (
	testPluginSchema = "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"
	testMCPSchema    = "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json"
)

func writeAppPluginForTest(t *testing.T, root, version string) string {
	t.Helper()
	source := filepath.Join(root, "demo-plugin")
	if err := os.MkdirAll(filepath.Join(source, "skills", "plugin-skill", "references"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeAppPluginJSON(t, filepath.Join(source, "plugin.json"), map[string]any{
		"$schema":     testPluginSchema,
		"name":        "demo.plugin",
		"version":     version,
		"description": "Plugin integration test.",
	})
	skill := "---\nname: plugin-skill\ndescription: Plugin Skill integration test.\n---\n\n# Plugin Skill\n"
	if err := os.WriteFile(filepath.Join(source, "skills", "plugin-skill", "SKILL.md"), []byte(skill), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "skills", "plugin-skill", "references", "guide.md"), []byte("plugin-resource-marker\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeAppPluginJSON(t, filepath.Join(source, "mcp.json"), map[string]any{
		"$schema": testMCPSchema,
		"mcpServers": map[string]any{
			"remote": map[string]any{
				"type":    "streamable-http",
				"url":     "https://example.com/mcp",
				"headers": map[string]string{"X-Tenant": "public"},
			},
		},
	})
	return source
}

func writeAppPluginJSON(t *testing.T, path string, value any) {
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

func newPluginTestRuntime(t *testing.T) (*Runtime, string) {
	t.Helper()
	root := t.TempDir()
	cfg := config.Config{
		AgentDockDefaultDir: root,
		AgentDockHome:       filepath.Join(t.TempDir(), ".agentdock"),
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	return runtime, root
}

func TestPluginComponentsEnterExistingRuntimeAndSkillExecUsesPluginData(t *testing.T) {
	rt, root := newPluginTestRuntime(t)
	source := writeAppPluginForTest(t, root, "1.0.0")

	validated, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "validate", "source": source,
	})
	if err != nil {
		t.Fatal(err)
	}
	review, ok := validated["review"].(pluginruntime.Review)
	if !ok || !review.Valid || review.Name != "demo.plugin" {
		t.Fatalf("validate = %#v", validated)
	}

	installed, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "install", "source": source, "confirmed": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if installed["name"] != "demo.plugin" || installed["changed"] != true {
		t.Fatalf("install = %#v", installed)
	}

	contextResult, err := rt.AgentDockContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	skill := findContextMap(t, contextResult["skills"], func(item map[string]any) bool {
		return item["name"] == "plugin-skill" && item["source_type"] == "plugin"
	})
	if skill["plugin_name"] != "demo.plugin" {
		t.Fatalf("Plugin Skill provenance = %#v", skill)
	}
	skillRef, _ := skill["skill_ref"].(string)
	if skillRef != "skill://plugin/demo.plugin/plugin-skill" {
		t.Fatalf("skill_ref = %q", skillRef)
	}

	runtimeSkills, err := rt.RuntimeSkills()
	if err != nil {
		t.Fatal(err)
	}
	runtimeSkill := findContextMap(t, runtimeSkills["skills"], func(item map[string]any) bool {
		return item["skill_ref"] == skillRef
	})
	if runtimeSkill["source_type"] != "plugin" || runtimeSkill["plugin_name"] != "demo.plugin" {
		t.Fatalf("Runtime Plugin Skill provenance = %#v", runtimeSkill)
	}
	runtimeDetail, err := rt.RuntimeSkill(skillRef)
	if err != nil {
		t.Fatal(err)
	}
	if runtimeDetail["skill_ref"] != skillRef || runtimeDetail["plugin_name"] != "demo.plugin" {
		t.Fatalf("Runtime Plugin Skill detail = %#v", runtimeDetail)
	}
	runtimeFiles, err := rt.RuntimeSkillFiles(skillRef)
	if err != nil {
		t.Fatal(err)
	}
	if runtimeFiles["skill_ref"] != skillRef {
		t.Fatalf("Runtime Plugin Skill files = %#v", runtimeFiles)
	}
	runtimeFile, err := rt.RuntimeSkillFile(skillRef, "references/guide.md")
	if err != nil {
		t.Fatal(err)
	}
	if runtimeFile["skill_ref"] != skillRef || !strings.Contains(fmt.Sprint(runtimeFile["file"]), "plugin-resource-marker") {
		t.Fatalf("Runtime Plugin Skill file = %#v", runtimeFile)
	}

	resource, err := rt.Call(context.Background(), "read_file", map[string]any{
		"path": skillRef + "/references/guide.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fmt.Sprint(resource["content"]), "plugin-resource-marker") {
		t.Fatalf("Plugin Skill resource = %#v", resource)
	}

	command := "test -n \"$PLUGIN_DATA_DIR\" && test -z \"$SKILL_DATA_DIR\" && printf %s \"$PLUGIN_DATA_DIR\""
	if goruntime.GOOS == "windows" {
		command = "if (-not $env:PLUGIN_DATA_DIR -or $env:SKILL_DATA_DIR) { exit 1 }; [Console]::Write($env:PLUGIN_DATA_DIR)"
	}
	executed, err := rt.Call(context.Background(), "exec_command", map[string]any{
		"cmd": command, "skill_ref": skillRef, "execution_mode": "sync",
	})
	if err != nil {
		t.Fatal(err)
	}
	wantData, err := config.PluginDataDir(rt.cfg, "demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	if executed["stdout"] != wantData {
		t.Fatalf("PLUGIN_DATA_DIR = %#v, want %q", executed["stdout"], wantData)
	}
	if _, err := os.Stat(wantData); err != nil {
		t.Fatalf("Plugin data dir not created: %v", err)
	}

	_, err = rt.Call(context.Background(), "exec_command", map[string]any{
		"cmd": commandNoopForTest(), "skill_ref": skillRef,
		"env": map[string]any{"PLUGIN_DATA_DIR": "override"},
	})
	assertToolErrorCode(t, err, "INVALID_ENV_NAME")

	mcp := findContextMap(t, contextResult["dynamic_mcp"], func(item map[string]any) bool {
		return item["source_type"] == "plugin"
	})
	if mcp["plugin_name"] != "demo.plugin" || mcp["name"] != pluginruntime.RuntimeMCPName("demo.plugin", "remote") {
		t.Fatalf("Plugin MCP provenance = %#v", mcp)
	}
}

func TestPluginLifecycleKeepsStandaloneMCPAndOwnsMCPEnvironment(t *testing.T) {
	rt, root := newPluginTestRuntime(t)
	source := writeAppPluginForTest(t, root, "1.0.0")

	if _, err := rt.Call(context.Background(), "mcp_manage", map[string]any{
		"action": "add", "name": "standalone", "description": "Standalone MCP",
		"transport": "streamable_http", "url": "http://127.0.0.1:1/mcp",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "install", "source": source, "confirmed": true,
	}); err != nil {
		t.Fatal(err)
	}

	runtimeName := pluginruntime.RuntimeMCPName("demo.plugin", "remote")
	listed, err := rt.Call(context.Background(), "mcp_manage", map[string]any{"action": "list"})
	if err != nil {
		t.Fatal(err)
	}
	findContextMap(t, listed["servers"], func(item map[string]any) bool {
		return item["name"] == "standalone" && item["source_type"] == "standalone"
	})
	pluginServer := findContextMap(t, listed["servers"], func(item map[string]any) bool {
		return item["name"] == runtimeName
	})
	if pluginServer["plugin_name"] != "demo.plugin" || pluginServer["source_type"] != "plugin" {
		t.Fatalf("Plugin MCP summary = %#v", pluginServer)
	}

	if _, err := rt.Call(context.Background(), "mcp_manage", map[string]any{
		"action": "disable", "name": runtimeName,
	}); err == nil {
		t.Fatal("mcp_manage disabled a Plugin-owned MCP")
	} else {
		assertToolErrorCode(t, err, "MCP_OWNED_BY_PLUGIN")
	}

	const secret = "runtime-secret-must-not-be-returned"
	setResult, err := rt.Call(context.Background(), "mcp_manage", map[string]any{
		"action": "env_set", "name": runtimeName, "key": "DEMO_TOKEN", "value": secret,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprint(setResult), secret) {
		t.Fatalf("Plugin MCP env_set returned secret: %#v", setResult)
	}
	envPath := filepath.Join(rt.cfg.AgentDockHome, "env", "mcp", runtimeName+".env")
	if _, err := os.Stat(envPath); err != nil {
		t.Fatalf("Plugin MCP env file missing: %v", err)
	}
	if _, err := rt.Call(context.Background(), "mcp_manage", map[string]any{
		"action": "env_set", "name": runtimeName, "key": "PLUGIN_DATA_DIR", "value": "override",
	}); err == nil {
		t.Fatal("Plugin MCP reserved runtime env was configurable")
	} else {
		assertToolErrorCode(t, err, "VALIDATION_ERROR")
	}

	if _, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "disable", "name": "demo.plugin",
	}); err != nil {
		t.Fatal(err)
	}
	contextResult, err := rt.AgentDockContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if contextHasMap(contextResult["skills"], func(item map[string]any) bool { return item["source_type"] == "plugin" }) {
		t.Fatalf("disabled Plugin Skill remained in context: %#v", contextResult["skills"])
	}
	if contextHasMap(contextResult["dynamic_mcp"], func(item map[string]any) bool { return item["source_type"] == "plugin" }) {
		t.Fatalf("disabled Plugin MCP remained in context: %#v", contextResult["dynamic_mcp"])
	}
	if !contextHasMap(contextResult["dynamic_mcp"], func(item map[string]any) bool { return item["name"] == "standalone" }) {
		t.Fatalf("disabling Plugin removed standalone MCP: %#v", contextResult["dynamic_mcp"])
	}

	if _, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "remove", "name": "demo.plugin", "data_policy": "keep",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(envPath); err != nil {
		t.Fatalf("keep removed Plugin MCP env: %v", err)
	}

	if _, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "install", "source": source, "confirmed": true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Call(context.Background(), "mcp_manage", map[string]any{
		"action": "env_set", "name": runtimeName, "key": "DEMO_TOKEN", "value": secret,
	}); err != nil {
		t.Fatal(err)
	}
	dataDir, err := config.PluginDataDir(rt.cfg, "demo.plugin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "remove", "name": "demo.plugin", "data_policy": "purge",
	}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{envPath, dataDir} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("purge left %s: %v", path, err)
		}
	}
	finalContext, err := rt.AgentDockContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !contextHasMap(finalContext["dynamic_mcp"], func(item map[string]any) bool { return item["name"] == "standalone" }) {
		t.Fatalf("Plugin remove affected standalone MCP: %#v", finalContext["dynamic_mcp"])
	}
}

func findContextMap(t *testing.T, value any, match func(map[string]any) bool) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatalf("normalize context list: %v; value=%#v", err, value)
	}
	for _, item := range items {
		if match(item) {
			return item
		}
	}
	t.Fatalf("matching item not found in %#v", value)
	return nil
}

func contextHasMap(value any, match func(map[string]any) bool) bool {
	data, err := json.Marshal(value)
	if err != nil {
		return false
	}
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		return false
	}
	for _, item := range items {
		if match(item) {
			return true
		}
	}
	return false
}

func TestPluginUpdateRuntimeActivationFailureRestoresPreviousPackageAndStandaloneMCP(t *testing.T) {
	rt, root := newPluginTestRuntime(t)

	v1Root := filepath.Join(root, "v1")
	if err := os.MkdirAll(v1Root, 0o700); err != nil {
		t.Fatal(err)
	}
	sourceV1 := writeAppPluginForTest(t, v1Root, "1.0.0")
	if err := os.Remove(filepath.Join(sourceV1, "mcp.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "install", "source": sourceV1, "confirmed": true,
	}); err != nil {
		t.Fatal(err)
	}
	before, err := rt.plugins.Manage(context.Background(), pluginruntimeRequestInspect("demo.plugin"))
	if err != nil {
		t.Fatal(err)
	}
	beforeDigest, _ := before["package_digest"].(string)

	collision := pluginruntime.RuntimeMCPName("demo.plugin", "remote")
	if _, err := rt.Call(context.Background(), "mcp_manage", map[string]any{
		"action": "add", "name": collision, "description": "Standalone collision",
		"transport": "streamable_http", "url": "http://127.0.0.1:1/mcp",
	}); err != nil {
		t.Fatal(err)
	}

	v2Root := filepath.Join(root, "v2")
	if err := os.MkdirAll(v2Root, 0o700); err != nil {
		t.Fatal(err)
	}
	sourceV2 := writeAppPluginForTest(t, v2Root, "2.0.0")
	_, err = rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "update", "source": sourceV2, "confirmed": true, "confirmed_source_change": true,
	})
	assertToolErrorCode(t, err, "PLUGIN_RUNTIME_ACTIVATION_FAILED")

	after, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "inspect", "name": "demo.plugin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if after["version"] != "1.0.0" || after["package_digest"] != beforeDigest {
		t.Fatalf("failed update did not restore v1: %#v", after)
	}
	v2Package := filepath.Join(rt.cfg.AgentDockHome, "plugins", "demo.plugin", "2.0.0")
	if _, statErr := os.Stat(v2Package); !os.IsNotExist(statErr) {
		t.Fatalf("failed update left v2 package at %s: %v", v2Package, statErr)
	}
	listed, err := rt.Call(context.Background(), "mcp_manage", map[string]any{"action": "list"})
	if err != nil {
		t.Fatal(err)
	}
	standalone := findContextMap(t, listed["servers"], func(item map[string]any) bool {
		return item["name"] == collision
	})
	if standalone["source_type"] != "standalone" {
		t.Fatalf("standalone MCP was replaced by Plugin ownership: %#v", standalone)
	}
}

func pluginruntimeRequestInspect(name string) toolplugin.ManageRequest {
	return toolplugin.ManageRequest{Action: "inspect", Name: name}
}

func TestPluginPurgeRemovesEnvironmentFromMCPRemovedByUpdate(t *testing.T) {
	rt, root := newPluginTestRuntime(t)
	v1Root := filepath.Join(root, "purge-v1")
	if err := os.MkdirAll(v1Root, 0o700); err != nil {
		t.Fatal(err)
	}
	sourceV1 := writeAppPluginForTest(t, v1Root, "1.0.0")
	if _, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "install", "source": sourceV1, "confirmed": true,
	}); err != nil {
		t.Fatal(err)
	}
	runtimeName := pluginruntime.RuntimeMCPName("demo.plugin", "remote")
	if _, err := rt.Call(context.Background(), "mcp_manage", map[string]any{
		"action": "env_set", "name": runtimeName, "key": "DEMO_TOKEN", "value": "historical-secret",
	}); err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(rt.cfg.AgentDockHome, "env", "mcp", runtimeName+".env")
	if _, err := os.Stat(envPath); err != nil {
		t.Fatal(err)
	}

	v2Root := filepath.Join(root, "purge-v2")
	if err := os.MkdirAll(v2Root, 0o700); err != nil {
		t.Fatal(err)
	}
	sourceV2 := writeAppPluginForTest(t, v2Root, "2.0.0")
	if err := os.Remove(filepath.Join(sourceV2, "mcp.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "update", "source": sourceV2, "confirmed": true, "confirmed_source_change": true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(envPath); err != nil {
		t.Fatalf("update unexpectedly removed historical MCP env before explicit purge: %v", err)
	}

	if _, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "remove", "name": "demo.plugin", "data_policy": "purge",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(envPath); !os.IsNotExist(err) {
		t.Fatalf("purge left environment for MCP removed by a prior Plugin update: %v", err)
	}
}

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func pluginReviewTokenForTest(t *testing.T, rt *Runtime, source string) string {
	t.Helper()
	validated, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "validate", "source": source,
	})
	if err != nil {
		t.Fatal(err)
	}
	review, ok := validated["review"].(pluginruntime.Review)
	if !ok || !review.Valid || review.ReviewToken == "" {
		t.Fatalf("Plugin validate did not return a usable review token: %#v", validated)
	}
	if _, exists := validated["review_token"]; exists {
		t.Fatalf("Plugin validate still duplicates review_token at top level: %#v", validated)
	}
	if _, exists := validated["package_digest"]; exists {
		t.Fatalf("Plugin validate still duplicates package_digest at top level: %#v", validated)
	}
	return review.ReviewToken
}

func TestPluginRemoteOptionalHeaderAllowsAnonymousRuntime(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if got := request.URL.Query().Get("client"); got != "claude-code-plugin" {
			t.Errorf("client query = %q", got)
		}
		if got := request.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want omitted header", got)
		}
		if request.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		var rpc struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(request.Body).Decode(&rpc); err != nil {
			t.Errorf("decode upstream request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch rpc.Method {
		case "server/discover":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      rpc.ID,
				"error":   map[string]any{"code": -32601, "message": "Method not found"},
			})
		case "initialize":
			writeDynamicMCPRPCResult(t, w, rpc.ID, map[string]any{
				"protocolVersion": "2025-11-25",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "plugin-upstream", "version": "1.0.0"},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			writeDynamicMCPRPCResult(t, w, rpc.ID, map[string]any{
				"tools": []map[string]any{{
					"name":        "ping",
					"description": "Ping",
					"inputSchema": map[string]any{"type": "object"},
				}},
			})
		default:
			t.Errorf("unexpected upstream method %q", rpc.Method)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()

	rt, root := newPluginTestRuntime(t)
	source := writeAppPluginForTest(t, root, "1.0.0")
	writeAppPluginJSON(t, filepath.Join(source, "mcp.json"), map[string]any{
		"$schema": testMCPSchema,
		"mcpServers": map[string]any{
			"remote": map[string]any{
				"type": "streamable-http",
				"url":  upstream.URL + "?client=claude-code-plugin",
				"headers": map[string]string{
					"Authorization": "${CONTEXT7_API_KEY:-}",
				},
			},
		},
	})
	if _, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "install", "source": source, "review_token": pluginReviewTokenForTest(t, rt, source),
	}); err != nil {
		t.Fatal(err)
	}

	runtimeName := pluginruntime.RuntimeMCPName("demo.plugin", "remote")
	search, err := rt.Call(context.Background(), "mcp_tool_search", map[string]any{
		"server": runtimeName, "query": "ping", "limit": 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if search["count"] != 1 {
		t.Fatalf("mcp_tool_search = %#v", search)
	}
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
		"action": "install", "source": source, "review_token": pluginReviewTokenForTest(t, rt, source),
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
	plugin := findContextMap(t, contextResult["plugins"], func(item map[string]any) bool {
		return item["name"] == "demo.plugin"
	})
	if plugin["version"] != "1.0.0" || plugin["enabled"] != true ||
		plugin["description"] != "Plugin integration test." ||
		plugin["skills_count"] != float64(1) || plugin["mcp_count"] != float64(1) ||
		plugin["format"] != "portable" {
		t.Fatalf("Plugin context summary = %#v", plugin)
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

	command := "test -n \"$PLUGIN_DATA_DIR\" && test -n \"$SKILL_DATA_DIR\" && printf '%s|%s' \"$PLUGIN_DATA_DIR\" \"$SKILL_DATA_DIR\""
	if goruntime.GOOS == "windows" {
		command = "if (-not $env:PLUGIN_DATA_DIR -or -not $env:SKILL_DATA_DIR) { exit 1 }; [Console]::Write($env:PLUGIN_DATA_DIR + '|' + $env:SKILL_DATA_DIR)"
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
	wantSkillData, err := config.PluginSkillDataDir(rt.cfg, "demo.plugin", "plugin-skill")
	if err != nil {
		t.Fatal(err)
	}
	wantOutput := wantData + "|" + wantSkillData
	if executed["stdout"] != wantOutput {
		t.Fatalf("Plugin Skill data env = %#v, want %q", executed["stdout"], wantOutput)
	}
	if _, err := os.Stat(wantData); err != nil {
		t.Fatalf("Plugin data dir not created: %v", err)
	}
	if _, err := os.Stat(wantSkillData); err != nil {
		t.Fatalf("Plugin Skill data dir not created: %v", err)
	}

	_, err = rt.Call(context.Background(), "exec_command", map[string]any{
		"cmd": commandNoopForTest(), "skill_ref": skillRef,
		"env": map[string]any{"PLUGIN_DATA_DIR": "override"},
	})
	assertToolErrorCode(t, err, "INVALID_ENV_NAME")

	_, err = rt.Call(context.Background(), "exec_command", map[string]any{
		"cmd": commandNoopForTest(), "skill_ref": skillRef,
		"env": map[string]any{"SKILL_DATA_DIR": "override"},
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
		"action": "install", "source": source, "review_token": pluginReviewTokenForTest(t, rt, source),
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

	skillRef := "skill://plugin/demo.plugin/plugin-skill"
	skillEnvResult, err := rt.Call(context.Background(), "skill_manage", map[string]any{
		"action": "env_set", "skill_ref": skillRef, "key": "SKILL_TOKEN", "value": secret,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprint(skillEnvResult), secret) {
		t.Fatalf("Plugin Skill env_set returned secret: %#v", skillEnvResult)
	}
	skillEnvList, err := rt.Call(context.Background(), "skill_manage", map[string]any{
		"action": "env_list", "skill_ref": skillRef,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprint(skillEnvList), secret) {
		t.Fatalf("Plugin Skill env_list returned secret: %#v", skillEnvList)
	}
	for _, reserved := range []string{"SKILL_DATA_DIR", "PLUGIN_DATA_DIR"} {
		if _, err := rt.Call(context.Background(), "skill_manage", map[string]any{
			"action": "env_set", "skill_ref": skillRef, "key": reserved, "value": "override",
		}); err == nil {
			t.Fatalf("Plugin Skill reserved env %s was configurable", reserved)
		} else {
			assertToolErrorCode(t, err, "VALIDATION_ERROR")
		}
	}
	pluginSkillEnvPath := filepath.Join(rt.cfg.AgentDockHome, "env", "skill", "plugin", "demo.plugin", "plugin-skill.env")
	if _, err := os.Stat(pluginSkillEnvPath); err != nil {
		t.Fatalf("Plugin Skill env file missing: %v", err)
	}
	if _, err := rt.Call(context.Background(), "exec_command", map[string]any{
		"cmd": commandNoopForTest(), "skill_ref": skillRef, "execution_mode": "sync",
	}); err != nil {
		t.Fatal(err)
	}
	pluginSkillDataDir, err := config.PluginSkillDataDir(rt.cfg, "demo.plugin", "plugin-skill")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pluginSkillDataDir); err != nil {
		t.Fatalf("Plugin Skill data dir missing: %v", err)
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
	disabledPlugin := findContextMap(t, contextResult["plugins"], func(item map[string]any) bool {
		return item["name"] == "demo.plugin"
	})
	if disabledPlugin["enabled"] != false {
		t.Fatalf("disabled Plugin disappeared or stayed enabled in Plugin index: %#v", disabledPlugin)
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
	for _, path := range []string{pluginSkillEnvPath, pluginSkillDataDir} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("keep removed Plugin Skill state %s: %v", path, err)
		}
	}

	if _, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "install", "source": source, "review_token": pluginReviewTokenForTest(t, rt, source),
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
	for _, path := range []string{envPath, dataDir, pluginSkillEnvPath, pluginSkillDataDir} {
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
	if contextHasMap(finalContext["plugins"], func(item map[string]any) bool { return item["name"] == "demo.plugin" }) {
		t.Fatalf("removed Plugin remained in Plugin context index: %#v", finalContext["plugins"])
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

func TestPluginInstallRuntimeActivationFailureNeverPublishesPluginOrSkill(t *testing.T) {
	rt, root := newPluginTestRuntime(t)
	collision := pluginruntime.RuntimeMCPName("demo.plugin", "remote")
	if _, err := rt.Call(context.Background(), "mcp_manage", map[string]any{
		"action": "add", "name": collision, "description": "Standalone collision",
		"transport": "streamable_http", "url": "http://127.0.0.1:1/mcp",
	}); err != nil {
		t.Fatal(err)
	}

	source := writeAppPluginForTest(t, root, "1.0.0")
	_, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "install", "source": source, "review_token": pluginReviewTokenForTest(t, rt, source),
	})
	assertToolErrorCode(t, err, "PLUGIN_RUNTIME_ACTIVATION_FAILED")

	if _, inspectErr := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "inspect", "name": "demo.plugin",
	}); inspectErr == nil {
		t.Fatal("failed install became visible as an installed Plugin")
	}
	contextResult, err := rt.AgentDockContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if contextHasMap(contextResult["skills"], func(item map[string]any) bool {
		return item["plugin_name"] == "demo.plugin"
	}) {
		t.Fatalf("failed install exposed Plugin Skill through agentdock_context: %#v", contextResult["skills"])
	}
	packagePath := filepath.Join(rt.cfg.AgentDockHome, "plugins", "demo.plugin", "1.0.0")
	if _, statErr := os.Stat(packagePath); !os.IsNotExist(statErr) {
		t.Fatalf("failed install left candidate package at %s: %v", packagePath, statErr)
	}
	listed, err := rt.Call(context.Background(), "mcp_manage", map[string]any{"action": "list"})
	if err != nil {
		t.Fatal(err)
	}
	standalone := findContextMap(t, listed["servers"], func(item map[string]any) bool {
		return item["name"] == collision
	})
	if standalone["source_type"] != "standalone" {
		t.Fatalf("failed Plugin install replaced standalone MCP ownership: %#v", standalone)
	}
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
		"action": "install", "source": sourceV1, "review_token": pluginReviewTokenForTest(t, rt, sourceV1),
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
		"action": "update", "source": sourceV2, "review_token": pluginReviewTokenForTest(t, rt, sourceV2),
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
		"action": "install", "source": sourceV1, "review_token": pluginReviewTokenForTest(t, rt, sourceV1),
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
		"action": "update", "source": sourceV2, "review_token": pluginReviewTokenForTest(t, rt, sourceV2),
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

func TestPluginInstallReviewTokenRejectsChangedCandidate(t *testing.T) {
	rt, root := newPluginTestRuntime(t)
	source := writeAppPluginForTest(t, root, "1.0.0")
	token := pluginReviewTokenForTest(t, rt, source)

	manifestPath := filepath.Join(source, "plugin.json")
	writeAppPluginJSON(t, manifestPath, map[string]any{
		"$schema": testPluginSchema, "name": "demo.plugin", "version": "1.0.0",
		"description": "mutated after security review",
	})

	_, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "install", "source": source, "review_token": token,
	})
	assertToolErrorCode(t, err, "PLUGIN_REVIEW_CHANGED")
	if _, inspectErr := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "inspect", "name": "demo.plugin",
	}); inspectErr == nil {
		t.Fatal("candidate changed after review but was installed")
	}

	newToken := pluginReviewTokenForTest(t, rt, source)
	installed, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "install", "source": source, "review_token": newToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	if installed["changed"] != true {
		t.Fatalf("re-reviewed candidate was not installed: %#v", installed)
	}
}

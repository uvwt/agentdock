package mcp

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/uvwt/agentdock/internal/app"
	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/observability"
)

func TestToolDescriptorsExposeSafetyAnnotations(t *testing.T) {
	descriptors := toolDescriptorsForConfig(t, []string{"read_file", "skill_manage", "task_manage", "file_publish"}, config.Config{})
	byName := map[string]map[string]any{}
	for _, descriptor := range descriptors {
		name, _ := descriptor["name"].(string)
		byName[name] = descriptor
	}

	assertToolAnnotation(t, byName["read_file"], true, false, false)
	assertToolAnnotation(t, byName["skill_manage"], false, true, true)
	assertToolAnnotation(t, byName["task_manage"], false, false, false)
	assertToolAnnotation(t, byName["file_publish"], false, false, true)

	for _, def := range app.ToolDefinitions() {
		if def.Annotations == nil {
			t.Fatalf("%s has no safety annotations", def.Name)
		}
	}
}

func assertToolAnnotation(t *testing.T, descriptor map[string]any, readOnly, destructive, openWorld bool) {
	t.Helper()
	annotations, ok := descriptor["annotations"].(map[string]any)
	if !ok {
		t.Fatalf("descriptor has no annotations: %#v", descriptor)
	}
	if got, _ := annotations["readOnlyHint"].(bool); got != readOnly {
		t.Fatalf("readOnlyHint = %v, want %v", got, readOnly)
	}
	assertBoolPointer := func(key string, want bool) {
		value, ok := annotations[key].(*bool)
		if !ok || value == nil || *value != want {
			t.Fatalf("%s = %#v, want %v", key, annotations[key], want)
		}
	}
	assertBoolPointer("destructiveHint", destructive)
	assertBoolPointer("openWorldHint", openWorld)
}

func TestOpenAIReviewedToolDescriptorsMatchBehavior(t *testing.T) {
	browser := toolDescriptorsForConfig(t, []string{"browser_snapshot"}, config.Config{BrowserEnabled: true})[0]
	assertToolAnnotation(t, browser, false, false, true)

	plugin := toolDescriptorsForConfig(t, []string{"plugin_manage"}, config.Config{})[0]
	description, _ := plugin["description"].(string)
	for _, forbidden := range []string{"Git", "catalog"} {
		if strings.Contains(description, forbidden) {
			t.Fatalf("plugin_manage description still claims unsupported %s behavior: %q", forbidden, description)
		}
	}
	for _, required := range []string{"local Plugin directories", "ZIP archives", "Remote sources must be fetched locally"} {
		if !strings.Contains(description, required) {
			t.Fatalf("plugin_manage description missing %q: %q", required, description)
		}
	}
}

func TestFilePublishDescriptorExposesFileRewritePath(t *testing.T) {
	descriptors := toolDescriptorsForConfig(t, []string{"file_publish"}, config.Config{})
	byName := map[string]map[string]any{}
	for _, descriptor := range descriptors {
		name, _ := descriptor["name"].(string)
		byName[name] = descriptor
	}
	args, ok := byName["file_publish"]["file_arg_rewrite_paths"].([]string)
	if !ok || len(args) != 1 || args[0] != "file" {
		t.Fatalf("file_publish file_arg_rewrite_paths = %#v", byName["file_publish"]["file_arg_rewrite_paths"])
	}
	meta, ok := byName["file_publish"]["_meta"].(map[string]any)
	if !ok || meta["file_arg_rewrite_paths"] == nil || meta["openai/fileParams"] == nil {
		t.Fatalf("file_publish _meta missing: %#v", meta)
	}
}

func TestOpenAIFileMetadataMatchesDeclaredSchemas(t *testing.T) {
	for _, def := range app.ToolDefinitions() {
		meta := toolMetadata(def, config.MCPAppsModeFull)
		inputProps, _ := def.InputSchema["properties"].(map[string]any)
		for _, path := range def.FileArgRewritePaths {
			property, ok := inputProps[path].(map[string]any)
			if !ok {
				t.Fatalf("%s file input path %q missing from input schema", def.Name, path)
			}
			if property["type"] != "object" {
				t.Fatalf("%s file input %q must be an object: %#v", def.Name, path, property)
			}
			properties, ok := property["properties"].(map[string]any)
			if !ok {
				t.Fatalf("%s file input %q properties = %#v", def.Name, path, property["properties"])
			}
			for _, key := range []string{"download_url", "file_id", "file_name", "mime_type"} {
				if _, ok := properties[key]; !ok {
					t.Fatalf("%s file input %q missing documented property %q", def.Name, path, key)
				}
			}
			required, ok := property["required"].([]string)
			if !ok || !slices.Contains(required, "download_url") || !slices.Contains(required, "file_id") {
				t.Fatalf("%s file input %q required = %#v", def.Name, path, property["required"])
			}
		}
		if len(def.FileArgRewritePaths) > 0 {
			paths, ok := meta["openai/fileParams"].([]string)
			if !ok || strings.Join(paths, ",") != strings.Join(def.FileArgRewritePaths, ",") {
				t.Fatalf("%s openai/fileParams = %#v, want %#v", def.Name, meta["openai/fileParams"], def.FileArgRewritePaths)
			}
		}

		outputProps, _ := def.OutputSchema["properties"].(map[string]any)
		for _, path := range def.FileResultRewritePaths {
			if _, ok := outputProps[path]; !ok {
				t.Fatalf("%s file output path %q missing from output schema", def.Name, path)
			}
		}
		if len(def.FileResultRewritePaths) > 0 {
			paths, ok := meta["openai/fileResultPaths"].([]string)
			if !ok || strings.Join(paths, ",") != strings.Join(def.FileResultRewritePaths, ",") {
				t.Fatalf("%s openai/fileResultPaths = %#v, want %#v", def.Name, meta["openai/fileResultPaths"], def.FileResultRewritePaths)
			}
		}
	}
}

func TestToolDescriptorsUseConfigAwareTaskManageSchema(t *testing.T) {
	withoutNexus := toolDescriptorsForConfig(t, []string{"task_manage"}, config.Config{})[0]
	withoutProps := withoutNexus["inputSchema"].(map[string]any)["properties"].(map[string]any)
	if _, ok := withoutProps["template_id"]; ok {
		t.Fatal("task_manage descriptor should hide Nexus-only fields without Nexus")
	}
	withoutOutputProps := withoutNexus["outputSchema"].(map[string]any)["properties"].(map[string]any)
	if _, ok := withoutOutputProps["guidance_context"]; ok {
		t.Fatal("task_manage descriptor should hide Nexus-only output fields without Nexus")
	}

	withNexus := toolDescriptorsForConfig(t, []string{"task_manage"}, config.Config{NexusEndpoint: "http://127.0.0.1:18777"})[0]
	withProps := withNexus["inputSchema"].(map[string]any)["properties"].(map[string]any)
	if _, ok := withProps["template_id"]; !ok {
		t.Fatal("task_manage descriptor should expose Nexus fields with Nexus")
	}
	withOutputProps := withNexus["outputSchema"].(map[string]any)["properties"].(map[string]any)
	if _, ok := withOutputProps["guidance_context"]; !ok {
		t.Fatal("task_manage descriptor should expose Nexus output fields with Nexus")
	}
}

func TestToolEnvelopeMCPImageStripsInternalBase64FromStructuredContent(t *testing.T) {
	response := toolEnvelope("view_image", map[string]any{
		"ok":                   true,
		"source":               map[string]any{"type": "artifact", "artifact_id": "artifact-1"},
		"_mcp_image_base64":    "abc123",
		"_mcp_image_mime_type": "image/png",
	}, nil)
	content := response["content"].([]map[string]any)
	if content[0]["type"] != "image" || content[0]["data"] != "abc123" || content[0]["mimeType"] != "image/png" {
		t.Fatalf("content = %#v", content)
	}
	structured := response["structuredContent"].(map[string]any)
	if _, ok := structured["_mcp_image_base64"]; ok {
		t.Fatalf("structuredContent leaked internal base64: %#v", structured)
	}
	if _, ok := structured["_mcp_image_mime_type"]; ok {
		t.Fatalf("structuredContent leaked internal mime type: %#v", structured)
	}
}

func TestToolEnvelopePassesThroughDynamicMCPContent(t *testing.T) {
	response := toolEnvelope("mcp_tool_call", map[string]any{
		"ok":   true,
		"name": "figma:get_screenshot",
		"result": map[string]any{
			"content": []any{
				map[string]any{"type": "text", "text": "done"},
				map[string]any{"type": "image", "data": "abc123", "mimeType": "image/png"},
			},
			"structuredContent": map[string]any{"node_id": "1:2"},
		},
	}, nil)
	content, ok := response["content"].([]any)
	if !ok || len(content) != 2 {
		t.Fatalf("dynamic MCP content = %#v", response["content"])
	}
	image, _ := content[1].(map[string]any)
	if image["type"] != "image" || image["data"] != "abc123" || image["mimeType"] != "image/png" {
		t.Fatalf("dynamic MCP image content = %#v", image)
	}
	structured, _ := response["structuredContent"].(map[string]any)
	if structured["name"] != "figma:get_screenshot" {
		t.Fatalf("structuredContent = %#v", structured)
	}
}

func TestOfficialSDKServerListsAndCallsAgentDockTools(t *testing.T) {
	root := t.TempDir()
	cfg := config.Config{AgentDockDefaultDir: root, AgentDockHome: filepath.Join(root, ".agentdock")}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	runtime, err := app.NewRuntime(cfg)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	defer runtime.Close()

	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
	server := NewServer(runtime, cfg)
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.sdk.Run(t.Context(), serverTransport) }()

	client := mcpsdk.NewClient(
		&mcpsdk.Implementation{Name: "agentdock-test", Version: "1.0.0"},
		&mcpsdk.ClientOptions{Capabilities: &mcpsdk.ClientCapabilities{}},
	)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	foundAgentDockContext := false
	foundFilePublishMetadata := false
	for tool, err := range session.Tools(t.Context(), nil) {
		if err != nil {
			t.Fatalf("Tools() error = %v", err)
		}
		switch tool.Name {
		case "agentdock_context":
			foundAgentDockContext = true
		case "file_publish":
			paths, _ := tool.Meta["openai/fileParams"].([]any)
			foundFilePublishMetadata = len(paths) == 1 && paths[0] == "file"
		}
	}
	if !foundAgentDockContext || !foundFilePublishMetadata {
		t.Fatalf("tool discovery incomplete: agentdock_context=%v file_publish_meta=%v", foundAgentDockContext, foundFilePublishMetadata)
	}

	result, err := session.CallTool(t.Context(), &mcpsdk.CallToolParams{Name: "agentdock_context", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	structured, ok := result.StructuredContent.(map[string]any)
	runtimeInfo, runtimeOK := structured["runtime"].(map[string]any)
	if !ok || !runtimeOK || runtimeInfo["os"] == "" || runtimeInfo["path_model"] != config.PathModel || result.IsError {
		t.Fatalf("CallTool() result = %#v", result)
	}

	analytics := runtime.RuntimeAnalytics()
	recent, ok := analytics["recent_calls"].([]observability.ExecutionRecord)
	if !ok || len(recent) == 0 {
		t.Fatalf("runtime analytics recent_calls = %#v", analytics["recent_calls"])
	}
	if recent[0].Tool != "agentdock_context" || recent[0].Source != observability.SourceMCP || !recent[0].Success {
		t.Fatalf("MCP tool analytics = %#v", recent[0])
	}

	if err := session.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("Server.Run() error = %v", err)
	}
}

func TestStreamableHTTPHandlerAllowsAuthenticatedPublicHost(t *testing.T) {
	for name, cfg := range map[string]config.Config{
		"static token": {
			AuthToken:      "configured-token",
			OAuthServerURL: "https://dockmini.example",
		},
		"OAuth": {
			OAuthEnabled:   true,
			OAuthServerURL: "https://dockmini.example",
		},
	} {
		t.Run(name, func(t *testing.T) {
			response := serveStreamableHTTPRequest(t, cfg)
			if response.Code == http.StatusForbidden {
				t.Fatalf("authenticated public Host was rejected: status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestStreamableHTTPHandlerKeepsLocalhostProtection(t *testing.T) {
	for name, cfg := range map[string]config.Config{
		"no public URL": {
			AuthToken: "configured-token",
		},
		"public URL without authentication": {
			OAuthServerURL: "https://dockmini.example",
		},
	} {
		t.Run(name, func(t *testing.T) {
			response := serveStreamableHTTPRequest(t, cfg)
			if response.Code != http.StatusForbidden {
				t.Fatalf("untrusted public Host status=%d, want %d; body=%s", response.Code, http.StatusForbidden, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), "invalid Host header") {
				t.Fatalf("unexpected rejection body: %s", response.Body.String())
			}
		})
	}
}

func serveStreamableHTTPRequest(t *testing.T, cfg config.Config) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "http://dockmini.example/mcp", nil)
	request.Host = "dockmini.example"
	localAddress := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 18766}
	request = request.WithContext(context.WithValue(request.Context(), http.LocalAddrContextKey, localAddress))
	response := httptest.NewRecorder()
	NewServer(nil, cfg).HTTPHandler().ServeHTTP(response, request)
	return response
}

func toolDescriptorsForConfig(t *testing.T, names []string, cfg config.Config) []map[string]any {
	t.Helper()
	root := t.TempDir()
	cfg.AgentDockDefaultDir = root
	cfg.AgentDockHome = filepath.Join(root, ".agentdock")
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("normalize runtime config: %v", err)
	}
	runtime, err := app.NewRuntime(cfg)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	t.Cleanup(func() {
		if err := runtime.Close(); err != nil {
			t.Errorf("close runtime: %v", err)
		}
	})
	definitions := make([]ToolDefinition, 0, len(names))
	for _, name := range names {
		definition, ok := runtime.ToolDefinition(name)
		if !ok {
			t.Fatalf("tool %s is not available for test config", name)
		}
		definitions = append(definitions, definition)
	}
	return toolDescriptors(definitions, config.MCPAppsModeFull)
}

func TestStreamableHTTPDownstreamFallbackIsolatesInboundProtocolVersion(t *testing.T) {
	var mu sync.Mutex
	var observed []string
	recordMethodHeader := func(method, header string) {
		mu.Lock()
		defer mu.Unlock()
		observed = append(observed, method+":"+header)
	}

	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var rpc struct {
			ID     any            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&rpc); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		headerVersion := r.Header.Get("Mcp-Protocol-Version")
		recordMethodHeader(rpc.Method, headerVersion)

		switch rpc.Method {
		case "server/discover":
			// 模拟仅支持 2025-11-25 的严格服务端（如 NocoBase）：拒绝 2026-07-28 探测。
			http.Error(w, "unsupported protocol version", http.StatusBadRequest)
			return
		case "initialize":
			// 回退 initialize 握手阶段不应携带入站 2026-07-28 协议头。
			if headerVersion != "" {
				http.Error(w, "unexpected Mcp-Protocol-Version on initialize: "+headerVersion, http.StatusBadRequest)
				return
			}
			if got, _ := rpc.Params["protocolVersion"].(string); got != "2025-11-25" {
				http.Error(w, "unexpected protocolVersion param: "+got, http.StatusBadRequest)
				return
			}
			w.Header().Set("Mcp-Session-Id", "downstream-session-1")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      rpc.ID,
				"result": map[string]any{
					"protocolVersion": "2025-11-25",
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo":      map[string]any{"name": "nocobase-mock", "version": "1.0.0"},
				},
			})
		case "notifications/initialized":
			if headerVersion != "2025-11-25" {
				http.Error(w, "unsupported Mcp-Protocol-Version: "+headerVersion, http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			if headerVersion != "2025-11-25" {
				http.Error(w, "unsupported Mcp-Protocol-Version: "+headerVersion, http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      rpc.ID,
				"result": map[string]any{
					"tools": []map[string]any{{
						"name":        "nocobase_ping",
						"description": "Ping NocoBase mock",
						"inputSchema": map[string]any{
							"type":       "object",
							"properties": map[string]any{"text": map[string]any{"type": "string"}},
						},
					}},
				},
			})
		case "tools/call":
			if headerVersion != "2025-11-25" {
				http.Error(w, "unsupported Mcp-Protocol-Version: "+headerVersion, http.StatusBadRequest)
				return
			}
			args, _ := rpc.Params["arguments"].(map[string]any)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      rpc.ID,
				"result": map[string]any{
					"content": []map[string]any{{"type": "text", "text": "pong:" + strings.TrimSpace(args["text"].(string))}},
				},
			})
		default:
			http.Error(w, "unexpected method: "+rpc.Method, http.StatusNotFound)
		}
	}))
	defer downstream.Close()

	root := t.TempDir()
	cfg := config.Config{
		AgentDockDefaultDir: root,
		AgentDockHome:       filepath.Join(root, ".agentdock"),
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	runtime, err := app.NewRuntime(cfg)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	defer func() { _ = runtime.Close() }()

	if _, err := runtime.Call(t.Context(), "mcp_manage", map[string]any{
		"action":      "add",
		"name":        "nocobase",
		"description": "Legacy Streamable HTTP mock",
		"transport":   "streamable_http",
		"url":         downstream.URL,
	}); err != nil {
		t.Fatalf("mcp_manage add error = %v", err)
	}

	agentdockHTTP := httptest.NewServer(NewServer(runtime, cfg).HTTPHandler())
	defer agentdockHTTP.Close()

	inboundClient := mcpsdk.NewClient(
		&mcpsdk.Implementation{Name: "inbound-2026-client", Version: "1.0.0"},
		&mcpsdk.ClientOptions{Capabilities: &mcpsdk.ClientCapabilities{}},
	)
	inboundSession, err := inboundClient.Connect(
		t.Context(),
		&mcpsdk.StreamableClientTransport{Endpoint: agentdockHTTP.URL, DisableStandaloneSSE: true},
		nil,
	)
	if err != nil {
		t.Fatalf("inbound Connect() error = %v", err)
	}
	defer func() { _ = inboundSession.Close() }()

	if ir := inboundSession.InitializeResult(); ir == nil || ir.ProtocolVersion != "2026-07-28" {
		t.Fatalf("inbound ProtocolVersion = %+v, want 2026-07-28", ir)
	}

	searchRes, err := inboundSession.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name:      "mcp_tool_search",
		Arguments: map[string]any{"server": "nocobase", "query": "ping"},
	})
	if err != nil {
		t.Fatalf("inbound mcp_tool_search error = %v", err)
	}
	if searchRes.IsError {
		t.Fatalf("inbound mcp_tool_search returned error result: %#v", searchRes)
	}
	searchJSON, _ := json.Marshal(searchRes.StructuredContent)
	if !strings.Contains(string(searchJSON), "nocobase:nocobase_ping") {
		t.Fatalf("mcp_tool_search missing nocobase:nocobase_ping: %s", searchJSON)
	}

	callRes, err := inboundSession.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name: "mcp_tool_call",
		Arguments: map[string]any{
			"name":      "nocobase:nocobase_ping",
			"arguments": map[string]any{"text": "ok"},
		},
	})
	if err != nil {
		t.Fatalf("inbound mcp_tool_call error = %v", err)
	}
	if callRes.IsError {
		t.Fatalf("inbound mcp_tool_call returned error result: %#v", callRes)
	}

	mu.Lock()
	gotObserved := append([]string(nil), observed...)
	mu.Unlock()
	want := []string{
		"server/discover:2026-07-28",
		"initialize:",
		"notifications/initialized:2025-11-25",
		"tools/list:2025-11-25",
		"tools/call:2025-11-25",
	}
	if !slices.Equal(gotObserved, want) {
		t.Fatalf("downstream method/header sequence = %v, want %v", gotObserved, want)
	}
}

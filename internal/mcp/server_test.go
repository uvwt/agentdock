package mcp

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/uvwt/agentdock/internal/app"
	"github.com/uvwt/agentdock/internal/config"
)

func TestToolDescriptorsExposeSafetyAnnotations(t *testing.T) {
	descriptors := toolDescriptorsForConfig(t, []string{"read_file", "skill_package", "task_manage", "file_publish"}, config.Config{})
	byName := map[string]map[string]any{}
	for _, descriptor := range descriptors {
		name, _ := descriptor["name"].(string)
		byName[name] = descriptor
	}

	assertToolAnnotation(t, byName["read_file"], true, false, false)
	assertToolAnnotation(t, byName["skill_package"], false, true, true)
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
		meta := toolMetadata(def)
		inputProps, _ := def.InputSchema["properties"].(map[string]any)
		for _, path := range def.FileArgRewritePaths {
			property, ok := inputProps[path].(map[string]any)
			if !ok {
				t.Fatalf("%s file input path %q missing from input schema", def.Name, path)
			}
			if property["type"] != "string" || property["format"] != "binary" {
				t.Fatalf("%s file input %q must be string/binary: %#v", def.Name, path, property)
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
	return toolDescriptors(definitions)
}

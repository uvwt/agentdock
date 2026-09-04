package mcp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	protocol "github.com/uvwt/agentdock-protocol"
	"github.com/uvwt/agentdock/internal/app"
	"github.com/uvwt/agentdock/internal/config"
)

type mcpAppTestHarness struct {
	runtime    *app.Runtime
	server     *Server
	session    *mcpsdk.ClientSession
	serverDone chan error
}

func assertToolUIResource(t *testing.T, tool *mcpsdk.Tool, uri string, allowedMetaKeys ...string) {
	t.Helper()
	if tool == nil {
		t.Fatal("UI tool is nil")
	}
	ui, ok := tool.Meta["ui"].(map[string]any)
	if !ok || ui["resourceUri"] != uri {
		t.Fatalf("%s standard ui metadata = %#v", tool.Name, tool.Meta["ui"])
	}
	allowed := map[string]bool{"ui": true}
	for _, key := range allowedMetaKeys {
		allowed[key] = true
	}
	for key := range tool.Meta {
		if !allowed[key] {
			t.Fatalf("%s has unexpected tool metadata %q: %#v", tool.Name, key, tool.Meta)
		}
	}
}

func assertResourceUIMeta(t *testing.T, meta mcpsdk.Meta, domain string) {
	t.Helper()
	ui, ok := meta["ui"].(map[string]any)
	if !ok || ui["prefersBorder"] != true {
		t.Fatalf("standard resource ui metadata = %#v", meta["ui"])
	}
	csp, ok := ui["csp"].(map[string]any)
	if !ok || csp["connectDomains"] == nil || csp["resourceDomains"] == nil {
		t.Fatalf("standard resource csp = %#v", ui["csp"])
	}
	if domain == "" {
		if _, exists := ui["domain"]; exists {
			t.Fatalf("unexpected resource ui.domain = %#v", ui["domain"])
		}
	} else if ui["domain"] != domain {
		t.Fatalf("resource domain metadata = %#v", meta)
	}
	if len(meta) != 1 {
		t.Fatalf("resource should expose only standard ui metadata: %#v", meta)
	}
}

func newMCPAppTestHarness(t *testing.T, cfg config.Config) *mcpAppTestHarness {
	return newMCPAppTestHarnessWithApps(t, cfg, true)
}

func newMCPAppTestHarnessWithApps(t *testing.T, cfg config.Config, enabled bool) *mcpAppTestHarness {
	t.Helper()
	cfg.MCPAppsEnabled = enabled
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	runtime, err := app.NewRuntime(cfg)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
	server := NewServer(runtime, cfg)
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.sdk.Run(t.Context(), serverTransport) }()

	client := mcpsdk.NewClient(
		&mcpsdk.Implementation{Name: "agentdock-mcp-apps-test", Version: "1.0.0"},
		&mcpsdk.ClientOptions{Capabilities: &mcpsdk.ClientCapabilities{}},
	)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		_ = runtime.Close()
		t.Fatalf("Connect() error = %v", err)
	}

	harness := &mcpAppTestHarness{runtime: runtime, server: server, session: session, serverDone: serverDone}
	t.Cleanup(func() {
		if err := session.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
		if err := <-serverDone; err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("Server.Run() error = %v", err)
		}
		if err := runtime.Close(); err != nil {
			t.Errorf("Runtime.Close() error = %v", err)
		}
	})
	return harness
}

func TestUIResourcesMatchServedResourceRegistry(t *testing.T) {
	server := &Server{cfg: config.Config{NexusEndpoint: "https://nexus.example.test", ACPEnabled: true, MCPAppsEnabled: true}}
	definitions := server.appResourceDefinitions()
	resources := server.UIResources()
	if len(definitions) != 8 || len(resources) != len(definitions) {
		t.Fatalf("resource registry=%d bridge capabilities=%d, want 8", len(definitions), len(resources))
	}
	byURI := make(map[string]protocol.UIResourceCapability, len(resources))
	for _, resource := range resources {
		if _, duplicate := byURI[resource.URI]; duplicate {
			t.Fatalf("duplicate bridge UI resource capability: %s", resource.URI)
		}
		byURI[resource.URI] = resource
	}
	for _, definition := range definitions {
		contract, known := protocol.UIResourceContract(definition.URI)
		if !known {
			t.Fatalf("served resource %s is missing from shared protocol registry", definition.URI)
		}
		resource, ok := byURI[definition.URI]
		if !ok {
			t.Fatalf("served resource %s was not advertised to Nexus", definition.URI)
		}
		if resource.Contract != contract || resource.MIMEType != protocol.MCPAppMIMEType {
			t.Fatalf("resource capability %s = %#v", definition.URI, resource)
		}
	}
}

func TestMCPAppsCanBeDisabledWithoutRemovingTools(t *testing.T) {
	root := t.TempDir()
	harness := newMCPAppTestHarnessWithApps(t, config.Config{
		AgentDockDefaultDir: root,
		AgentDockHome:       filepath.Join(root, ".agentdock"),
	}, false)

	tools := map[string]*mcpsdk.Tool{}
	for tool, err := range harness.session.Tools(t.Context(), nil) {
		if err != nil {
			t.Fatalf("Tools() error = %v", err)
		}
		tools[tool.Name] = tool
	}
	if tools["agentdock_context"] == nil || tools["file_edit"] == nil || tools["task_manage"] == nil {
		t.Fatalf("core tools disappeared when MCP Apps UI was disabled: %#v", tools)
	}
	for _, name := range []string{"agentdock_context", "file_edit", "task_manage", "mcp_tool_call", "file_publish"} {
		if ui := tools[name].Meta["ui"]; ui != nil {
			t.Fatalf("%s still exposes Apps UI metadata while disabled: %#v", name, ui)
		}
	}
	if tools["file_publish"].Meta["file_arg_rewrite_paths"] == nil {
		t.Fatalf("file_publish lost non-UI metadata while MCP Apps UI was disabled: %#v", tools["file_publish"].Meta)
	}

	resources := 0
	for _, err := range harness.session.Resources(t.Context(), nil) {
		if err != nil {
			t.Fatalf("Resources() error = %v", err)
		}
		resources++
	}
	if resources != 0 {
		t.Fatalf("resources/list count = %d, want 0 while MCP Apps UI is disabled", resources)
	}
	if got := harness.server.UIResources(); len(got) != 0 {
		t.Fatalf("UIResources() = %#v, want empty while disabled", got)
	}
	if _, err := harness.server.ReadAppResource(protocol.ContextUIResourceURI); err == nil {
		t.Fatal("ReadAppResource() served an MCP App while disabled")
	}
}

func TestMCPAppsBindResourcesDirectlyToBusinessTools(t *testing.T) {
	root := t.TempDir()
	const widgetDomain = "https://dockmini.example.test"
	harness := newMCPAppTestHarness(t, config.Config{
		AgentDockDefaultDir: root,
		AgentDockHome:       filepath.Join(root, ".agentdock"),
		OAuthServerURL:      widgetDomain + "/",
	})

	initialize := harness.session.InitializeResult()
	if initialize == nil || initialize.Capabilities == nil || initialize.Capabilities.Resources == nil {
		t.Fatalf("resources capability not advertised: %#v", initialize)
	}

	tools := map[string]*mcpsdk.Tool{}
	for tool, err := range harness.session.Tools(t.Context(), nil) {
		if err != nil {
			t.Fatalf("Tools() error = %v", err)
		}
		tools[tool.Name] = tool
	}
	if len(tools) != 16 {
		t.Fatalf("tools/list count = %d, want 16", len(tools))
	}
	contextTool := tools["agentdock_context"]
	if contextTool == nil {
		t.Fatal("tools/list did not expose agentdock_context")
	}
	assertToolUIResource(t, contextTool, protocol.ContextUIResourceURI)
	fileEditTool := tools["file_edit"]
	if fileEditTool == nil {
		t.Fatal("tools/list did not expose file_edit")
	}
	assertToolUIResource(t, fileEditTool, protocol.FileChangeUIResourceURI)
	taskManageTool := tools["task_manage"]
	if taskManageTool == nil {
		t.Fatal("tools/list did not expose task_manage")
	}
	assertToolUIResource(t, taskManageTool, protocol.TaskProgressUIResourceURI)
	if taskManageTool.Annotations == nil || taskManageTool.Annotations.ReadOnlyHint {
		t.Fatalf("task_manage annotations = %#v", taskManageTool.Annotations)
	}
	for _, name := range []string{"exec_command", "session_observe", "session_act"} {
		tool := tools[name]
		if tool == nil {
			t.Fatalf("tools/list did not expose %s", name)
		}
		if tool.Meta["ui"] != nil {
			t.Fatalf("%s should not bind an Apps UI because command tools are high-frequency: %#v", name, tool.Meta)
		}
	}
	assertToolUIResource(t, tools["mcp_tool_call"], protocol.DynamicMCPUIResourceURI)
	for _, name := range []string{"mcp_manage", "mcp_tool_search", "mcp_tool_inspect"} {
		tool := tools[name]
		if tool == nil {
			t.Fatalf("tools/list did not expose %s", name)
		}
		if tool.Meta["ui"] != nil {
			t.Fatalf("%s should not bind an Apps UI: %#v", name, tool.Meta)
		}
	}
	assertToolUIResource(t, tools["file_publish"], protocol.ArtifactUIResourceURI, "file_arg_rewrite_paths", "openai/fileParams")

	resources := map[string]*mcpsdk.Resource{}
	for resource, err := range harness.session.Resources(t.Context(), nil) {
		if err != nil {
			t.Fatalf("Resources() error = %v", err)
		}
		resources[resource.URI] = resource
	}
	if len(resources) != 5 {
		t.Fatalf("resources/list count = %d, want 5", len(resources))
	}
	for _, uri := range []string{
		protocol.ContextUIResourceURI,
		protocol.TaskProgressUIResourceURI,
		protocol.FileChangeUIResourceURI,
		protocol.DynamicMCPUIResourceURI,
		protocol.ArtifactUIResourceURI,
	} {
		resource := resources[uri]
		if resource == nil || resource.MIMEType != protocol.MCPAppMIMEType {
			t.Fatalf("resource %s = %#v", uri, resource)
		}
		read, err := harness.session.ReadResource(t.Context(), &mcpsdk.ReadResourceParams{URI: uri})
		if err != nil {
			t.Fatalf("ReadResource(%s) error = %v", uri, err)
		}
		if len(read.Contents) != 1 || read.Contents[0].MIMEType != protocol.MCPAppMIMEType || !strings.Contains(read.Contents[0].Text, `window.addEventListener("message"`) || !strings.Contains(read.Contents[0].Text, "connect-src 'none'") {
			t.Fatalf("ReadResource(%s) = %#v", uri, read.Contents)
		}
		html := read.Contents[0].Text
		for _, marker := range []string{
			`window.parent.postMessage({jsonrpc:"2.0",id,method`,
			`rpcRequest("ui/initialize"`,
			`protocolVersion:"2026-01-26"`,
			`rpcNotify("ui/notifications/initialized"`,
			`rpcNotify("ui/notifications/size-changed",{height})`,
			`html.style.height="max-content"`,
			`html.getBoundingClientRect().height`,
			`new ResizeObserver(reportSize)`,
			`startAutoResize()`,
			`message.method==="ui/notifications/tool-result"`,
			`message.params&&message.params.structuredContent`,
		} {
			if !strings.Contains(html, marker) {
				t.Fatalf("resource %s missing standard MCP Apps bridge marker %q", uri, marker)
			}
		}
		for _, legacy := range []string{"window.openai", "toolOutput", "openai/widget", "openai/outputTemplate", "ui/resourceUri"} {
			if strings.Contains(html, legacy) {
				t.Fatalf("resource %s contains legacy bridge marker %q", uri, legacy)
			}
		}
		assertResourceUIMeta(t, resource.Meta, widgetDomain)
		assertResourceUIMeta(t, read.Contents[0].Meta, widgetDomain)
	}
	contextRead, err := harness.session.ReadResource(t.Context(), &mcpsdk.ReadResourceParams{URI: protocol.ContextUIResourceURI})
	if err != nil {
		t.Fatalf("ReadResource(agent context) error = %v", err)
	}
	if len(contextRead.Contents) != 1 {
		t.Fatalf("agent context resource = %#v", contextRead.Contents)
	}
	contextHTML := contextRead.Contents[0].Text
	for _, marker := range []string{`expectedView="agentdock_context"`, "renderAgentContext", "renderNodeAgentContext", "renderFleetAgentContext", "appendNodeAgentContext", "contextItems", "appendContextOverview", "appendContextSection", "contextPill", `contextItems(data.skills)`, `const commonSkillIndex=isObject(data.common_skills)?data.common_skills:{}`, `appendContextSection(groups,"Common Skills · lower priority",commonSkills,10)`, `contextItems(data.dynamic_mcp)`, `Array.isArray(data.nodes)`, `const fleetOverview=el("div","context-overview fleet-overview")`, `appendContextOverview(fleetOverview,nodes.length,"Devices")`, `appendContextOverview(fleetOverview,online,"Online")`, `const tabs=el("div","context-node-tabs")`, `fragment.append(tabs,fleetOverview,content)`, `.fleet-overview{grid-template-columns:repeat(2,minmax(0,1fr));margin-top:11px}`, `.context-node-tabs{display:flex;justify-content:safe center`, `.context-node-tab{flex:0 0 auto;border:1px solid #e1e1e1`, `contextPill(workflowCount,"Workflow")`, `contextPill(recallEnabled?"ON":"OFF","Recall")`, `button.addEventListener("click",()=>selectNode(index))`, `appendNodeAgentContext(content,{`, `common_skills:node.context.common_skills`, `workflow_templates:shared.workflow_templates`, `node.error?"不可用"`, `.context-node-tab.active{border-color:#111;color:#111}`, `.context-node-content{padding-top:11px}`, ".context-overview{display:grid;grid-template-columns:repeat(4", ".context-overview-card{min-width:0;padding:1px 12px;border-right:1px solid #ececec", ".context-overview-value{font-size:18px;font-weight:760;line-height:1;color:#111", ".context-section+.context-section{border-top:1px solid #eee", ".context-list{display:grid;grid-template-columns:repeat(2", ".context-item{min-width:0;padding:7px 0;border-bottom:1px solid #f0f0f0", ".context-section-title{font-size:11.5px;font-weight:750;letter-spacing:.01em;color:#111", ".context-name{font-size:11.5px;font-weight:650;color:#111", ".context-desc{margin-top:1px;min-width:0;color:#777", ".compact-context"} {
		if !strings.Contains(contextHTML, marker) {
			t.Fatalf("agent context resource missing structured summary marker %q", marker)
		}
	}
	for _, rawMarker := range []string{`el("pre","context-text",text)`, "contextSectionLines", "contextNamedItems", `String(data.context||"")`, ".context-overview-card{padding:10px 11px", ".context-item{min-width:0;padding:8px 9px;border-radius:9px;background:#f8f8f8"} {
		if strings.Contains(contextHTML, rawMarker) {
			t.Fatalf("agent context resource still renders raw context marker %q", rawMarker)
		}
	}

	fileChangeRead, err := harness.session.ReadResource(t.Context(), &mcpsdk.ReadResourceParams{URI: protocol.FileChangeUIResourceURI})
	if err != nil {
		t.Fatalf("ReadResource(file change) error = %v", err)
	}
	if len(fileChangeRead.Contents) != 1 || !strings.Contains(fileChangeRead.Contents[0].Text, `expectedView="file_change"`) || !strings.Contains(fileChangeRead.Contents[0].Text, "renderFileChange") || strings.Contains(fileChangeRead.Contents[0].Text, "innerHTML") {
		t.Fatalf("file change resource = %#v", fileChangeRead.Contents)
	}
	fileChangeHTML := fileChangeRead.Contents[0].Text
	for _, marker := range []string{`.waiting-empty{text-align:center}`, `<main><div id="content" class="empty waiting-empty">Waiting for tool output…</div></main>`, `root.classList.remove("empty","waiting-empty")`} {
		if !strings.Contains(fileChangeHTML, marker) {
			t.Fatalf("file change resource missing scoped waiting-state marker %q", marker)
		}
	}
	for _, forbidden := range []string{`.empty{text-align:center`, `main{text-align:center`} {
		if strings.Contains(fileChangeHTML, forbidden) {
			t.Fatalf("file change resource contains global waiting-state alignment marker %q", forbidden)
		}
	}
	for _, marker := range []string{"@media(max-width:560px){:root{font-size:12px}", ".compact-title{font-size:12px;font-weight:700}", ".compact-summary{font-size:11px", ".compact-path{font-size:10.5px}", ".summary{font-size:11px", ".message-text{font-size:11px", ".context-name{font-size:10.5px;color:#111}", ".context-desc{font-size:9.5px;color:#777}", "@media(max-width:400px){:root{font-size:11.5px}"} {
		if !strings.Contains(fileChangeHTML, marker) {
			t.Fatalf("file change resource missing mobile typography marker %q", marker)
		}
	}
	for _, marker := range []string{"body{margin:0;padding:0;background:transparent", ".compact-toggle{width:100%;min-height:68px;border:0", ".compact-toggle.single-line{min-height:44px", ".compact-row", ".compact-path", ".compact-file-row", ".compact-file-stats", ".compact-file-stat.add{color:#52745e}", ".compact-file-stat.del{color:#8a5a55}", `end.append(el("span","brand","AgentDock"))`, ".detail-panel{border-top:1px solid #ececec", ".detail-panel[hidden]", `toggle.setAttribute("aria-expanded","false")`, `toggle.classList.add("single-line")`, `toggle.addEventListener("click"`, `compactRows.push(el("span","compact-path",pathText))`, `const detailMeta=el("div","meta")`, `detailMeta.append(el("span","compact-file-stat add",insertions))`, `detailMeta.append(el("span","compact-file-stat del",deletions))`, `stats.append(el("span","compact-file-stat add",insertions))`, `stats.append(el("span","compact-file-stat del",deletions))`, `compactRows.push(fileRow)`, `compactShell({action:data.action||"change",title:fileName},compactRows,fragment)`, ".diff-add{color:#315b45;background:#f5fbf7", ".diff-del{color:#7a3e39;background:#fff7f6", `line.startsWith("--- ")`, `line.startsWith("+++ ")`, `line.startsWith("@@")`, `@media(max-width:560px){`, `.brand,.compact-action{display:none}`, `@media(max-width:400px){`, `.compact-file-state{display:none}`} {
		if !strings.Contains(fileChangeHTML, marker) {
			t.Fatalf("file change resource missing simplified UI marker %q", marker)
		}
	}
	for _, nestedFrame := range []string{".compact-toggle{width:100%;min-height:68px;border:1px", ".detail-panel{border:1px"} {
		if strings.Contains(fileChangeHTML, nestedFrame) {
			t.Fatalf("file change resource still contains nested app frame marker %q", nestedFrame)
		}
	}
	taskRead, err := harness.session.ReadResource(t.Context(), &mcpsdk.ReadResourceParams{URI: protocol.TaskProgressUIResourceURI})
	if err != nil {
		t.Fatalf("ReadResource(task progress) error = %v", err)
	}
	for _, marker := range []string{".compact-progress", ".compact-progress-row", ".compact-summary", ".progress-fill", ".progress-node.completed", ".progress-node.in-progress", ".progress-node.blocked", `node.title=(step.title||step.id||"Step")+" · "+status`, `fill.style.width="calc((100% - 14px) * "+ratio+")"`, "taskProgress(steps)", `compactRows.push(el("span","compact-summary",task.summary))`, `progressRow.append(progress)`, `compactRows.push(progressRow)`, ".steps::before", ".step-marker", `status==="completed"?"✓"`} {
		if len(taskRead.Contents) != 1 || !strings.Contains(taskRead.Contents[0].Text, marker) {
			t.Fatalf("task resource missing compact or detailed progress marker %q", marker)
		}
	}
	for _, tc := range []struct {
		uri      string
		view     string
		renderer string
	}{
		{protocol.DynamicMCPUIResourceURI, "dynamic_mcp", "renderDynamicMCP"},
		{protocol.ArtifactUIResourceURI, "artifact", "renderArtifact"},
	} {
		read, err := harness.session.ReadResource(t.Context(), &mcpsdk.ReadResourceParams{URI: tc.uri})
		if err != nil || len(read.Contents) != 1 {
			t.Fatalf("ReadResource(%s)=%#v err=%v", tc.uri, read, err)
		}
		html := read.Contents[0].Text
		for _, marker := range []string{`expectedView="` + tc.view + `"`, tc.renderer, `compactShell({action:`, `function resultList(fragment,items,emptyText)`, `.compact-result-footer{display:grid;grid-template-columns:minmax(0,1fr);row-gap:2px`, `function compactResultFooter(preview,meta)`} {
			if !strings.Contains(html, marker) {
				t.Fatalf("resource %s missing UI marker %q", tc.uri, marker)
			}
		}
		for _, forbidden := range []string{"result-kicker", "result-heading", "result-head-meta", "resultHead("} {
			if strings.Contains(html, forbidden) {
				t.Fatalf("resource %s contains a separate gray domain header marker %q", tc.uri, forbidden)
			}
		}
		if tc.uri == protocol.ArtifactUIResourceURI {
			for _, marker := range []string{`.detail-link{color:#6f9fc9;text-decoration:none}`, `href:data.url`, `link.target="_blank"`, `link.rel="noopener noreferrer"`} {
				if !strings.Contains(html, marker) {
					t.Fatalf("artifact resource missing clickable signed URL marker %q", marker)
				}
			}
		}
	}
	if resources[protocol.RecallUIResourceURI] != nil || resources[protocol.WorkflowUIResourceURI] != nil {
		t.Fatal("Nexus-only UI resources should not be listed when Nexus is disabled")
	}
	if resources[protocol.ACPStatusUIResourceURI] != nil {
		t.Fatal("ACP UI resource should not be listed when ACP is disabled")
	}

	contextResult, err := harness.session.CallTool(t.Context(), &mcpsdk.CallToolParams{Name: "agentdock_context", Arguments: map[string]any{}})
	if err != nil || contextResult.IsError {
		t.Fatalf("agentdock_context result=%#v err=%v", contextResult, err)
	}
	contextStructured, ok := contextResult.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("agentdock_context structuredContent = %#v", contextResult.StructuredContent)
	}
	for _, field := range []string{"skills", "dynamic_mcp", "workflow_templates", "rules"} {
		if contextStructured[field] == nil {
			t.Fatalf("agentdock_context structuredContent missing %s: %#v", field, contextStructured)
		}
	}
	if contextStructured["context"] != nil {
		t.Fatalf("agentdock_context structuredContent still contains legacy Markdown context: %#v", contextStructured)
	}

	filePath := filepath.Join(root, "note.txt")
	if err := os.WriteFile(filePath, []byte("alpha\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fileEditResult, err := harness.session.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name: "file_edit",
		Arguments: map[string]any{
			"action": "replace", "path": "note.txt", "old": "alpha", "new": "beta", "dry_run": true,
		},
	})
	if err != nil || fileEditResult.IsError {
		t.Fatalf("file_edit result=%#v err=%v", fileEditResult, err)
	}
	fileEditStructured, ok := fileEditResult.StructuredContent.(map[string]any)
	if !ok || fileEditStructured["action"] != "replace" || fileEditStructured["dry_run"] != true || fileEditStructured["view"] != nil {
		t.Fatalf("file_edit structuredContent = %#v", fileEditResult.StructuredContent)
	}
	diffPreview, _ := fileEditStructured["diff_preview"].(string)
	if !strings.Contains(diffPreview, "beta") {
		t.Fatalf("file_edit diff_preview = %q", diffPreview)
	}
	fileBytes, err := os.ReadFile(filePath)
	if err != nil || string(fileBytes) != "alpha\n" {
		t.Fatalf("dry-run changed file: content=%q err=%v", fileBytes, err)
	}
	errorResult, err := harness.session.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name: "file_edit",
		Arguments: map[string]any{
			"action": "replace", "path": "note.txt", "old": "missing", "new": "beta", "expected_matches": 1,
		},
	})
	if err != nil || !errorResult.IsError {
		t.Fatalf("file_edit validation error result=%#v err=%v", errorResult, err)
	}
	errorStructured, ok := errorResult.StructuredContent.(map[string]any)
	if !ok || errorStructured["code"] != "MATCH_COUNT_MISMATCH" {
		t.Fatalf("file_edit validation structuredContent = %#v", errorResult.StructuredContent)
	}

	createdTask, err := harness.session.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name: "task_manage",
		Arguments: map[string]any{
			"action": "create", "title": "Widget task", "goal": "verify direct task UI",
			"completion_conditions": []string{"done"},
			"steps":                 []map[string]any{{"id": "verify", "title": "Verify"}},
		},
	})
	if err != nil || createdTask.IsError {
		t.Fatalf("task_manage create result=%#v err=%v", createdTask, err)
	}
	createdTaskStructured, ok := createdTask.StructuredContent.(map[string]any)
	if !ok || createdTaskStructured["action"] != "create" || createdTaskStructured["view"] != nil || createdTaskStructured["task_summary"] == nil {
		t.Fatalf("task_manage create structuredContent = %#v", createdTask.StructuredContent)
	}
	listedTasks, err := harness.session.CallTool(t.Context(), &mcpsdk.CallToolParams{Name: "task_manage", Arguments: map[string]any{"action": "list"}})
	if err != nil || listedTasks.IsError {
		t.Fatalf("task_manage list result=%#v err=%v", listedTasks, err)
	}
	listedStructured, ok := listedTasks.StructuredContent.(map[string]any)
	if !ok || listedStructured["action"] != "list" || listedStructured["tasks"] == nil {
		t.Fatalf("task_manage list structuredContent = %#v", listedTasks.StructuredContent)
	}

}

func TestMCPAppsExposeNexusViewsWhenNexusEnabled(t *testing.T) {
	root := t.TempDir()
	harness := newMCPAppTestHarness(t, config.Config{
		AgentDockDefaultDir: root,
		AgentDockHome:       filepath.Join(root, ".agentdock"),
		NexusEndpoint:       "https://nexus.example.test",
	})

	tools := map[string]*mcpsdk.Tool{}
	for tool, err := range harness.session.Tools(t.Context(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		tools[tool.Name] = tool
	}
	assertToolUIResource(t, tools["recall_write"], protocol.RecallUIResourceURI)
	for _, name := range []string{"recall_search", "recall_read", "recall_maintain"} {
		tool := tools[name]
		if tool == nil {
			t.Fatalf("tools/list did not expose %s", name)
		}
		if tool.Meta["ui"] != nil {
			t.Fatalf("%s should not bind an Apps UI: %#v", name, tool.Meta)
		}
	}
	workflowTool := tools["workflow_template_manage"]
	if workflowTool == nil {
		t.Fatal("workflow_template_manage was not exposed")
	}
	if workflowTool.Meta["ui"] != nil {
		t.Fatalf("workflow_template_manage should bind UI only on action=match results: %#v", workflowTool.Meta)
	}
	workflowDef, ok := toolDefinition("workflow_template_manage")
	if !ok {
		t.Fatal("workflow_template_manage definition missing")
	}
	matchMeta := toolResultMetadata(workflowDef, map[string]any{"action": "match"}, true)
	matchUI, ok := matchMeta["ui"].(map[string]any)
	if !ok || matchUI["resourceUri"] != protocol.WorkflowUIResourceURI {
		t.Fatalf("workflow match result UI metadata = %#v", matchMeta)
	}
	if meta := toolResultMetadata(workflowDef, map[string]any{"action": "list"}, true); len(meta) != 0 {
		t.Fatalf("workflow list should not bind result UI: %#v", meta)
	}
	if tool := tools["recall_bootstrap"]; tool != nil {
		t.Fatalf("recall_bootstrap should be absorbed by agentdock_context, got %#v", tool)
	}

	resources := map[string]*mcpsdk.Resource{}
	for resource, err := range harness.session.Resources(t.Context(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		resources[resource.URI] = resource
	}
	if len(resources) != 7 {
		t.Fatalf("resources/list count = %d, want 7", len(resources))
	}
	for _, tc := range []struct {
		uri      string
		view     string
		renderer string
	}{
		{protocol.RecallUIResourceURI, "recall", "renderRecall"},
		{protocol.WorkflowUIResourceURI, "workflow", "renderWorkflow"},
	} {
		resource := resources[tc.uri]
		if resource == nil || resource.MIMEType != protocol.MCPAppMIMEType {
			t.Fatalf("resource %s = %#v", tc.uri, resource)
		}
		read, err := harness.session.ReadResource(t.Context(), &mcpsdk.ReadResourceParams{URI: tc.uri})
		if err != nil || len(read.Contents) != 1 {
			t.Fatalf("ReadResource(%s)=%#v err=%v", tc.uri, read, err)
		}
		html := read.Contents[0].Text
		for _, marker := range []string{`expectedView="` + tc.view + `"`, tc.renderer, `function resultList(fragment,items,emptyText)`} {
			if !strings.Contains(html, marker) {
				t.Fatalf("resource %s missing UI marker %q", tc.uri, marker)
			}
		}
		for _, forbidden := range []string{"result-kicker", "result-heading", "result-head-meta", "resultHead("} {
			if strings.Contains(html, forbidden) {
				t.Fatalf("resource %s contains a separate gray domain header marker %q", tc.uri, forbidden)
			}
		}
	}
}

func TestReadAppResourceForNexusBridge(t *testing.T) {
	root := t.TempDir()
	harness := newMCPAppTestHarness(t, config.Config{
		AgentDockDefaultDir: root,
		AgentDockHome:       filepath.Join(root, ".agentdock"),
		OAuthServerURL:      "https://dockmini.example.test/",
	})

	result, err := harness.server.ReadAppResource(protocol.FileChangeUIResourceURI)
	if err != nil {
		t.Fatalf("ReadAppResource() error = %v", err)
	}
	contents, ok := result["contents"].([]any)
	if !ok || len(contents) != 1 {
		t.Fatalf("ReadAppResource() contents = %#v", result["contents"])
	}
	content, ok := contents[0].(map[string]any)
	if !ok || content["uri"] != protocol.FileChangeUIResourceURI || content["mimeType"] != protocol.MCPAppMIMEType {
		t.Fatalf("ReadAppResource() content = %#v", contents[0])
	}
	text, _ := content["text"].(string)
	if !strings.Contains(text, `expectedView="file_change"`) {
		t.Fatal("ReadAppResource() missing file change UI marker")
	}
	meta, ok := content["_meta"].(mcpsdk.Meta)
	if !ok {
		t.Fatalf("ReadAppResource() meta = %#v", content["_meta"])
	}
	assertResourceUIMeta(t, meta, "https://dockmini.example.test")

	if _, err := harness.server.ReadAppResource("ui://agentdock/not-found"); err == nil {
		t.Fatal("ReadAppResource() accepted an unknown AgentDock resource")
	}
}

func TestMCPAppsExposeACPViewOnlyWhenACPEnabled(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	harness := newMCPAppTestHarness(t, config.Config{
		AgentDockDefaultDir: root,
		AgentDockHome:       filepath.Join(root, ".agentdock"),
		ACPEnabled:          true,
		ACPAgentName:        "helper",
		ACPCommand:          executable,
	})

	tools := map[string]*mcpsdk.Tool{}
	for tool, err := range harness.session.Tools(t.Context(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		tools[tool.Name] = tool
	}
	if len(tools) != 19 {
		t.Fatalf("tools/list count = %d, want 19", len(tools))
	}
	assertToolUIResource(t, tools["acp_session"], protocol.ACPStatusUIResourceURI)
	for _, name := range []string{"acp_prompt", "acp_interaction"} {
		if tool := tools[name]; tool == nil {
			t.Fatalf("%s was not exposed", name)
		} else if tool.Meta["ui"] != nil {
			t.Fatalf("%s should not bind a static ACP widget: %#v", name, tool.Meta)
		}
	}

	read, err := harness.session.ReadResource(t.Context(), &mcpsdk.ReadResourceParams{URI: protocol.ACPStatusUIResourceURI})
	if err != nil {
		t.Fatalf("ReadResource() error = %v", err)
	}
	if len(read.Contents) != 1 || !strings.Contains(read.Contents[0].Text, "acp_status") {
		t.Fatalf("ACP resource contents = %#v", read.Contents)
	}
	for _, marker := range []string{"message-role", `message.role!=="user"&&message.role!=="assistant"`, "No user or assistant messages in this AgentDock process.", `session.agent||(isObject(state.agent)`, `const latest=[...state.messages].reverse().find`, `compactRows.push(el("span","compact-summary",latest.content))`, `const sessionMeta=[session.status||state.status,session.agent||"",session.cwd||""]`, `compactShell({action:state.action||"status",title:identity}`} {
		if !strings.Contains(read.Contents[0].Text, marker) {
			t.Fatalf("ACP resource missing conversation marker %q", marker)
		}
	}
	assertResourceUIMeta(t, read.Contents[0].Meta, "")

	sessionList, err := harness.session.CallTool(t.Context(), &mcpsdk.CallToolParams{Name: "acp_session", Arguments: map[string]any{"action": "list"}})
	if err != nil || sessionList.IsError {
		t.Fatalf("acp_session list result=%#v err=%v", sessionList, err)
	}
	sessionStructured, ok := sessionList.StructuredContent.(map[string]any)
	if !ok || sessionStructured["action"] != "list" || sessionStructured["sessions"] == nil || sessionStructured["view"] != nil {
		t.Fatalf("acp_session list structuredContent = %#v", sessionList.StructuredContent)
	}

}

func TestAppWidgetDomainRequiresHTTPSOrigin(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "https origin", raw: "https://dockmini.example.test/", want: "https://dockmini.example.test"},
		{name: "https port", raw: "https://dockmini.example.test:8443", want: "https://dockmini.example.test:8443"},
		{name: "empty", raw: "", want: ""},
		{name: "http", raw: "http://127.0.0.1:8765", want: ""},
		{name: "path", raw: "https://dockmini.example.test/mcp", want: ""},
		{name: "query", raw: "https://dockmini.example.test?x=1", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := appWidgetDomain(test.raw); got != test.want {
				t.Fatalf("appWidgetDomain(%q) = %q, want %q", test.raw, got, test.want)
			}
		})
	}
}

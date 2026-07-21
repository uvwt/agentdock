package tools

import (
	"context"

	"github.com/uvwt/agentdock/internal/config"
)

type ToolHandler func(context.Context, *Runtime, map[string]any) (Result, error)

// ToolSpec 是工具公开入口的单一事实源：运行时分发、MCP 描述、
// 配置开关都从这里派生，避免多处手写列表漂移。
type ToolSpec struct {
	Name                   string
	Title                  string
	Description            string
	FileArgRewritePaths    []string
	FileResultRewritePaths []string
	InputSchema            func() map[string]any
	OutputSchema           func() map[string]any
	Availability           func(config.Config) bool
	Handler                ToolHandler
}

type ToolDefinition struct {
	Name                   string
	Title                  string
	Description            string
	FileArgRewritePaths    []string
	FileResultRewritePaths []string
	InputSchema            map[string]any
	OutputSchema           map[string]any
}

// ToolDefinitions 只导出 MCP 层需要的描述和 schema，不暴露 handler。
func ToolDefinitions() []ToolDefinition {
	defs := make([]ToolDefinition, 0, len(allToolSpecs()))
	for _, spec := range allToolSpecs() {
		defs = append(defs, spec.definition())
	}
	return defs
}

func (s ToolSpec) definition() ToolDefinition {
	return ToolDefinition{
		Name:                   s.Name,
		Title:                  s.Title,
		Description:            s.Description,
		FileArgRewritePaths:    append([]string(nil), s.FileArgRewritePaths...),
		FileResultRewritePaths: append([]string(nil), s.FileResultRewritePaths...),
		InputSchema:            s.InputSchema(),
		OutputSchema:           s.OutputSchema(),
	}
}

func (r *Runtime) availableToolSpecs() []ToolSpec {
	out := make([]ToolSpec, 0, len(allToolSpecs()))
	for _, spec := range allToolSpecs() {
		if !spec.available(r.cfg) {
			continue
		}
		out = append(out, spec)
	}
	return out
}

func (s ToolSpec) available(cfg config.Config) bool {
	if s.Availability == nil {
		return true
	}
	return s.Availability(cfg)
}

func toolSpecByName(name string) (ToolSpec, bool) {
	for _, spec := range allToolSpecs() {
		if spec.Name == name {
			return spec, true
		}
	}
	return ToolSpec{}, false
}

func requiresNexus(cfg config.Config) bool   { return cfg.NexusEndpoint != "" }
func requiresBrowser(cfg config.Config) bool { return cfg.BrowserEnabled }

func toolHandler(fn func(*Runtime, map[string]any) (Result, error)) ToolHandler {
	return func(_ context.Context, r *Runtime, args map[string]any) (Result, error) { return fn(r, args) }
}

func ctxToolHandler(fn func(*Runtime, context.Context, map[string]any) (Result, error)) ToolHandler {
	return func(ctx context.Context, r *Runtime, args map[string]any) (Result, error) { return fn(r, ctx, args) }
}

func allToolSpecs() []ToolSpec {
	// 顺序保持和旧 ToolNames 一致，避免 tools/list 与 server_info 的展示顺序无谓变化。
	return bindToolSchemas([]ToolSpec{
		{Name: "server_info", Title: "Server info", Description: "Return server, host path model, auth, and exposed-tool metadata.", Handler: func(_ context.Context, r *Runtime, _ map[string]any) (Result, error) { return r.serverInfo(), nil }},
		{Name: "agentdock_context", Title: "AgentDock context", Description: "Return AgentDock bootstrap context including available capabilities, integrations, rules, and high-priority context for clients that cannot inject system prompt context.", Handler: ctxToolHandler((*Runtime).agentDockContextTool)},
		{Name: "read_file", Title: "Read file", Description: fileToolDescription("Read a UTF-8 text file slice. Supports normal Host paths and skill://<name>/<path> resources from the active Skill version."), Handler: ctxToolHandler((*Runtime).readFile)},
		{Name: "list_dir", Title: "List directory", Description: fileToolDescription("List directory entries. Relative paths resolve from ~/AgentDock; absolute and ~/ paths use Host rules."), Handler: ctxToolHandler((*Runtime).listDir)},
		{Name: "list_files", Title: "List files", Description: fileToolDescription("List files using glob and ignore filters. Relative paths resolve from ~/AgentDock by default."), Handler: ctxToolHandler((*Runtime).listFiles)},
		{Name: "search_text", Title: "Search text", Description: fileToolDescription("Search UTF-8 files for text or regex matches. Relative paths search ~/AgentDock by default; absolute paths are allowed."), Handler: ctxToolHandler((*Runtime).searchText)},
		{Name: "file_edit", Title: "Edit file", Description: fileEditToolDescription("Edit files through one action-based entrypoint: replace, patch, add, atomic_write, delete, or move. Prefer atomic_write for full-file rewrites (temp+fsync+rename) so interrupted long translations never leave a 0-byte target. Relative paths resolve from ~/AgentDock; absolute and ~/ paths use Host rules."), Handler: ctxToolHandler((*Runtime).fileEdit)},
		{Name: "exec_command", Title: "Run command", Description: execCommandDescription(), Handler: ctxToolHandler((*Runtime).execCommand)},
		{Name: "session_observe", Title: "Observe command sessions", Description: "List or inspect command sessions through a read-only session tool.", Handler: toolHandler((*Runtime).sessionObserve)},
		{Name: "session_act", Title: "Act on command sessions", Description: "Write to or stop command sessions through a mutating session tool.", Handler: toolHandler((*Runtime).sessionAct)},
		{Name: "task_manage", Title: "Manage recoverable tasks", Description: "Persist substantial AgentDock tasks and update live step progress with checkpoint.", Handler: ctxToolHandler((*Runtime).taskManage)},
		{Name: "goal_manage", Title: "Manage Goal Mode goals", Description: "Create and drive durable Goal Mode goals with capsules, reasoning leases, structured commit_turn decisions, approvals, evidence, and cross-conversation resume. Prefer goal_manage over task_manage for long-running verifiable outcomes.", Handler: ctxToolHandler((*Runtime).goalManage)},
		{Name: "workflow_template_manage", Title: "Manage workflow templates", Description: "List, get, get multiple, save, validate, publish, retire, or match AgentDock workflow templates. get_many requires the model to compose the returned templates before task creation.", Availability: requiresNexus, Handler: ctxToolHandler((*Runtime).workflowTemplateManage)},
		{Name: "skill_package", Title: "Manage Skill packages", Description: "Validate, install, activate, or roll back AgentDock Skill packages and manage each Skill's isolated environment without returning secret values.", Handler: ctxToolHandler((*Runtime).skillPackage)},
		{Name: "mcp_manage", Title: "Manage dynamic MCP servers", Description: "Register, inspect, enable, disable, refresh, remove, or manage the isolated environment of dynamic MCP servers. Dynamic MCP tools remain separate from AgentDock built-in tools.", Handler: ctxToolHandler((*Runtime).mcpManage)},
		{Name: "mcp_tool_search", Title: "Search dynamic MCP tools", Description: "Search lightweight tool summaries from enabled dynamic MCP servers. Use a server name from agentdock_context when possible.", Handler: ctxToolHandler((*Runtime).mcpToolSearch)},
		{Name: "mcp_tool_inspect", Title: "Inspect a dynamic MCP tool", Description: "Read the complete schema for one dynamic MCP tool identified as <server>:<tool>.", Handler: ctxToolHandler((*Runtime).mcpToolInspect)},
		{Name: "mcp_tool_call", Title: "Call a dynamic MCP tool", Description: "Call one previously discovered dynamic MCP tool identified as <server>:<tool>. Arguments are validated against the discovered tool schema before forwarding.", Handler: ctxToolHandler((*Runtime).mcpToolCall)},
		{Name: "git_read", Title: "Read Git repository state", Description: "Read Git repository information through one action-based entrypoint: repos, status, diff, log, show, blame, or github_repo_access.", Handler: ctxToolHandler((*Runtime).gitRead)},
		{Name: "git_write", Title: "Write Git repository state", Description: "Run mutating Git operations through one action-based entrypoint: clone, commit, fetch, pull, or push.", Handler: ctxToolHandler((*Runtime).gitWrite)},
		{Name: "view_image", Title: "View image", Description: "Load an image by AgentDock artifact_id, Host path, or HTTP(S) URL and return it as standard MCP image content.", Handler: ctxToolHandler((*Runtime).viewImage)},
		{Name: "recall_bootstrap", Title: "Bootstrap NexusDock Recall context", Description: "Load high-priority NexusDock Recall context at the start of substantial AgentDock, project, deployment, debugging, or preference-sensitive tasks. max_bytes controls pack budget only; compact index/excerpt output is default, and full body requires include_body or targeted recall_read.", Availability: requiresNexus, Handler: ctxToolHandler((*Runtime).recallBootstrap)},
		{Name: "recall_search", Title: "Search NexusDock Recall", Description: "Search NexusDock Recall memories, cards, and notes. Use kind=all, markdown, card, or note; when kind=note, use note_scope=questions or github-learning. Backend handles internal routing such as prefix and scope.", Availability: requiresNexus, Handler: ctxToolHandler((*Runtime).recallSearch)},
		{Name: "recall_read", Title: "Read NexusDock Recall entry", Description: "Read one Markdown, card, or note entry from the configured NexusDock Recall store by path.", Availability: requiresNexus, Handler: ctxToolHandler((*Runtime).recallRead)},
		{Name: "recall_write", Title: "Write NexusDock Recall entry", Description: "Plan, create, replace, append, patch, update facts, diff, or delete NexusDock Recall content. The model must choose target=card/note/markdown and action explicitly.", Availability: requiresNexus, Handler: ctxToolHandler((*Runtime).recallWrite)},
		{Name: "recall_maintain", Title: "Maintain NexusDock Recall", Description: "Run NexusDock Recall maintenance actions such as sync_status, list, lint, embedding_status, reindex, or reindex_cards.", Availability: requiresNexus, Handler: ctxToolHandler((*Runtime).recallMaintain)},
		{Name: "private_note_manage", Title: "Manage private notes", Description: "Explicit low-frequency NexusDock private note vault entrypoint. Do not use by default: use only when the user explicitly requests private note access or the content clearly contains sensitive secrets, credentials, or personal information. Search is metadata-only; plaintext is returned only by explicit read, and Git backups contain age ciphertext only. Actions: search, read, write, delete, status, or maintain.", Handler: ctxToolHandler((*Runtime).privateNoteManage), Availability: requiresNexus},
		{Name: "browser_session", Title: "Browser session", Description: "Start, close, or clean up a browser automation session; supports persistent profiles, storage_state_path, injected cookies/localStorage, and page metadata. On macOS, prefer browser=chrome for system Chrome; do not suggest the Playwright browser install command when bundled Chromium is missing.", Availability: requiresBrowser, Handler: ctxToolHandler((*Runtime).browserSession)},
		{Name: "browser_act", Title: "Browser actions", Description: "Run browser automation actions on an optional page_id, including URL/text/response waits, and return page metadata plus the final screenshot; supports close_after and storage state path save. If bundled Chromium is missing, retry with browser=chrome rather than suggesting the Playwright browser install command.", Availability: requiresBrowser, Handler: func(ctx context.Context, r *Runtime, args map[string]any) (Result, error) {
			return r.browserRunnerCall(ctx, "action", args)
		}},
		{Name: "browser_snapshot", Title: "Browser snapshot", Description: "Capture an optional page_id and return page_id/pages, page text, viewport, visible interactive elements, browser errors, screenshot, and optional storage_state_path save. If bundled Chromium is missing, retry with browser=chrome rather than suggesting the Playwright browser install command.", Availability: requiresBrowser, Handler: func(ctx context.Context, r *Runtime, args map[string]any) (Result, error) {
			return r.browserRunnerCall(ctx, "snapshot", args)
		}},
		{Name: "file_publish", Title: "Publish signed file", Description: "Publish a local file or directory as an immutable Artifact snapshot under ~/.agentdock/public-artifacts. Returns artifact_id and, when a reachable base URL is available, a temporary signed download URL. Directories are packaged as tar.gz.", FileArgRewritePaths: []string{"file"}, Handler: ctxToolHandler((*Runtime).filePublish)},
	})
}

func bindToolSchemas(specs []ToolSpec) []ToolSpec {
	for i := range specs {
		name := specs[i].Name
		if specs[i].InputSchema == nil {
			specs[i].InputSchema = func() map[string]any { return InputSchema(name) }
		}
		if specs[i].OutputSchema == nil {
			specs[i].OutputSchema = func() map[string]any { return OutputSchema(name) }
		}
	}
	return specs
}

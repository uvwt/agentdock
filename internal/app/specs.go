package app

import (
	"context"

	mcpcontract "github.com/uvwt/agentdock-protocol/mcpcontract"
	"github.com/uvwt/agentdock/internal/config"
	toolcommand "github.com/uvwt/agentdock/internal/tool/command"
	toolfile "github.com/uvwt/agentdock/internal/tool/file"
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
	Annotations            *ToolAnnotations
	Availability           func(config.Config) bool
	Handler                ToolHandler
}

type ToolAnnotations struct {
	Title           string
	ReadOnlyHint    bool
	DestructiveHint *bool
	IdempotentHint  bool
	OpenWorldHint   *bool
}

type ToolDefinition struct {
	Name                   string
	Title                  string
	Description            string
	UIBinding              *UIBinding
	FileArgRewritePaths    []string
	FileResultRewritePaths []string
	InputSchema            map[string]any
	OutputSchema           map[string]any
	Annotations            *ToolAnnotations
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
		UIBinding:              toolUIBinding(s.Name),
		FileArgRewritePaths:    append([]string(nil), s.FileArgRewritePaths...),
		FileResultRewritePaths: append([]string(nil), s.FileResultRewritePaths...),
		InputSchema:            s.InputSchema(),
		OutputSchema:           s.OutputSchema(),
		Annotations:            cloneToolAnnotations(s.Annotations),
	}
}

func cloneToolAnnotations(value *ToolAnnotations) *ToolAnnotations {
	if value == nil {
		return nil
	}
	cloned := *value
	if value.DestructiveHint != nil {
		flag := *value.DestructiveHint
		cloned.DestructiveHint = &flag
	}
	if value.OpenWorldHint != nil {
		flag := *value.OpenWorldHint
		cloned.OpenWorldHint = &flag
	}
	return &cloned
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
func requiresACP(cfg config.Config) bool     { return cfg.ACPEnabled }

func readOnlyToolAnnotations(openWorld bool) *ToolAnnotations {
	return &ToolAnnotations{ReadOnlyHint: true, DestructiveHint: boolPointer(false), OpenWorldHint: boolPointer(openWorld)}
}

func mutatingToolAnnotations(destructive, openWorld bool) *ToolAnnotations {
	return &ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPointer(destructive), OpenWorldHint: boolPointer(openWorld)}
}

func boolPointer(value bool) *bool { return &value }

func canonicalToolAnnotations(value mcpcontract.Annotations) *ToolAnnotations {
	annotations := &ToolAnnotations{
		ReadOnlyHint:    value.ReadOnlyHint,
		DestructiveHint: value.DestructiveHint,
		OpenWorldHint:   value.OpenWorldHint,
	}
	if value.IdempotentHint != nil {
		annotations.IdempotentHint = *value.IdempotentHint
	}
	return annotations
}

func toolHandler(fn func(*Runtime, map[string]any) (Result, error)) ToolHandler {
	return func(_ context.Context, r *Runtime, args map[string]any) (Result, error) { return fn(r, args) }
}

func ctxToolHandler(fn func(*Runtime, context.Context, map[string]any) (Result, error)) ToolHandler {
	return func(ctx context.Context, r *Runtime, args map[string]any) (Result, error) { return fn(r, ctx, args) }
}

func allToolSpecs() []ToolSpec {
	return bindToolSchemas([]ToolSpec{
		{Name: "agentdock_context", Title: "AgentDock context", Description: "Return structured AgentDock bootstrap context including available capabilities, integrations, rules, and high-priority context.", Handler: ctxToolHandler((*Runtime).agentDockContextTool)},
		{Name: "read_file", Title: "Read file", Description: toolfile.ToolDescription("Read a UTF-8 text file slice. Supports normal Host paths and skill://<name>/<path> resources from the active Skill version."), Annotations: readOnlyToolAnnotations(false), Handler: ctxToolHandler((*Runtime).readFile)},
		{Name: "list_dir", Title: "List directory", Description: toolfile.ToolDescription("List directory entries with explicit depth, glob filters, and entry-type filtering. Glob patterns are relative to path: * stays within one path segment and ** crosses directories. Relative paths resolve from ~/AgentDock; absolute and ~/ paths use Host rules."), Annotations: readOnlyToolAnnotations(false), Handler: ctxToolHandler((*Runtime).listDir)},
		{Name: "search_text", Title: "Search text", Description: toolfile.ToolDescription("Search UTF-8 files for text or regex matches. Relative paths search ~/AgentDock by default; absolute paths are allowed."), Annotations: readOnlyToolAnnotations(false), Handler: ctxToolHandler((*Runtime).searchText)},
		{Name: "file_edit", Title: "Edit file", Description: toolfile.EditDescription("Edit files through one action-based entrypoint: replace, patch, add, delete, or move. Relative paths resolve from ~/AgentDock; absolute and ~/ paths use Host rules."), Annotations: mutatingToolAnnotations(true, false), Handler: ctxToolHandler((*Runtime).fileEdit)},
		{Name: "exec_command", Title: "Run command", Description: toolcommand.Description(), Annotations: mutatingToolAnnotations(true, true), Handler: ctxToolHandler((*Runtime).execCommand)},
		{Name: "session_observe", Title: "Observe command sessions", Description: "List or inspect command sessions through a read-only session tool.", Annotations: readOnlyToolAnnotations(false), Handler: toolHandler((*Runtime).sessionObserve)},
		{Name: "session_act", Title: "Act on command sessions", Description: "Write to or stop command sessions through a mutating session tool.", Annotations: mutatingToolAnnotations(true, true), Handler: toolHandler((*Runtime).sessionAct)},
		{Name: "task_manage", Title: "Manage recoverable tasks", Description: "Persist substantial AgentDock tasks and update live step progress with checkpoint.", Annotations: mutatingToolAnnotations(false, false), Handler: ctxToolHandler((*Runtime).taskManage)},
		{Name: "evolve", Title: "Evolve AgentDock knowledge", Description: "Propose reusable knowledge, pre-bind Task learning checks, supersede, or retract. Bind must happen before execution and declares on_success/on_failure semantics; AgentDock resolves later Task outcomes and owns lifecycle policy while Recall only persists the result.", Annotations: mutatingToolAnnotations(true, false), Availability: requiresNexus, Handler: ctxToolHandler((*Runtime).evolve)},
		{Name: "acp_session", Title: "Manage ACP sessions", Description: "Inspect or authenticate the configured ACP agent and create, load, resume, fork, configure, list, inspect, close, or delete persistent ACP sessions through one action-based entrypoint. Session workspaces may use any host-accessible directory and optional methods are capability-gated.", Annotations: mutatingToolAnnotations(true, true), Availability: requiresACP, Handler: func(ctx context.Context, r *Runtime, args map[string]any) (Result, error) {
			return r.acp.Session(ctx, args)
		}},
		{Name: "acp_prompt", Title: "Run ACP prompts", Description: "Start asynchronous ACP prompt turns, poll ordered session events, steer a running turn, or cancel a turn. start returns immediately with a run_id; use action=events for bounded long-poll observation.", Annotations: mutatingToolAnnotations(true, true), Availability: requiresACP, Handler: func(ctx context.Context, r *Runtime, args map[string]any) (Result, error) {
			return r.acp.Prompt(ctx, args)
		}},
		{Name: "acp_interaction", Title: "Handle ACP interactions", Description: "List, inspect, respond to, or cancel pending ACP permission interactions. Only options offered by the agent and permitted by the local AgentDock policy may be selected.", Annotations: mutatingToolAnnotations(true, true), Availability: requiresACP, Handler: func(ctx context.Context, r *Runtime, args map[string]any) (Result, error) {
			return r.acp.Interaction(ctx, args)
		}},
		{Name: "workflow_template_manage", Title: "Manage workflow templates", Description: "List, get, get multiple, publish, retire, or match AgentDock workflow templates. publish validates and activates a complete immutable template version; get_many requires the model to compose the returned templates before task creation.", Availability: requiresNexus, Handler: ctxToolHandler((*Runtime).workflowTemplateManage)},
		{Name: "skill_package", Title: "Manage Skill packages", Description: "Validate, install, activate, or roll back AgentDock Skill packages and manage each Skill's isolated environment without returning secret values.", Annotations: mutatingToolAnnotations(true, true), Handler: ctxToolHandler((*Runtime).skillPackage)},
		{Name: "mcp_manage", Title: "Manage dynamic MCP servers", Description: "Register, inspect, enable, disable, refresh, remove, or manage the isolated environment of dynamic MCP servers. Dynamic MCP tools remain separate from AgentDock built-in tools.", Annotations: mutatingToolAnnotations(true, true), Handler: func(ctx context.Context, r *Runtime, args map[string]any) (Result, error) {
			return r.dynamicMCP.Manage(ctx, args)
		}},
		{Name: "mcp_tool_search", Title: "Search dynamic MCP tools", Description: "Search lightweight tool summaries from enabled dynamic MCP servers. Use a server name from agentdock_context when possible.", Annotations: readOnlyToolAnnotations(true), Handler: func(ctx context.Context, r *Runtime, args map[string]any) (Result, error) {
			return r.dynamicMCP.Search(ctx, args)
		}},
		{Name: "mcp_tool_inspect", Title: "Inspect a dynamic MCP tool", Description: "Read the complete schema for one dynamic MCP tool identified as <server>:<tool>.", Annotations: readOnlyToolAnnotations(true), Handler: func(ctx context.Context, r *Runtime, args map[string]any) (Result, error) {
			return r.dynamicMCP.Inspect(ctx, args)
		}},
		{Name: "mcp_tool_call", Title: "Call a dynamic MCP tool", Description: "Call one previously discovered dynamic MCP tool identified as <server>:<tool>. Arguments are validated against the discovered tool schema before forwarding.", Annotations: mutatingToolAnnotations(true, true), Handler: func(ctx context.Context, r *Runtime, args map[string]any) (Result, error) {
			return r.dynamicMCP.Call(ctx, args)
		}},
		{Name: "view_image", Title: "View image", Description: "Load an image by AgentDock artifact_id, Host path, or HTTP(S) URL and return it as standard MCP image content.", Annotations: readOnlyToolAnnotations(true), Handler: func(ctx context.Context, r *Runtime, args map[string]any) (Result, error) {
			return r.media.ViewImage(ctx, args)
		}},
		{Name: "recall_bootstrap", Title: "Bootstrap NexusDock Recall context", Description: "Load high-priority NexusDock Recall context at the start of substantial AgentDock, project, deployment, debugging, or preference-sensitive tasks. max_bytes controls pack budget only; compact index/excerpt output is default, and full body requires include_body or targeted recall_read.", Availability: requiresNexus, Handler: ctxToolHandler((*Runtime).recallBootstrap)},
		{Name: "recall_search", Title: "Search NexusDock Recall", Description: "Search NexusDock Recall Markdown documents and cards with lexical retrieval and optional semantic enhancement when embeddings are available. Use kind=all, markdown, or card; backend routing stays internal.", Availability: requiresNexus, Handler: ctxToolHandler((*Runtime).recallSearch)},
		{Name: "recall_read", Title: "Read NexusDock Recall entry", Description: "Read one Markdown document or card from the configured NexusDock Recall store by path.", Availability: requiresNexus, Handler: ctxToolHandler((*Runtime).recallRead)},
		{Name: "recall_write", Title: "Write NexusDock Recall entry", Description: "Plan, create, replace, append, patch, update facts, diff, or delete NexusDock Recall content. The model must choose target=card/markdown and action explicitly.", Availability: requiresNexus, Handler: ctxToolHandler((*Runtime).recallWrite)},
		{Name: "recall_maintain", Title: "Maintain NexusDock Recall", Description: "Run NexusDock Recall maintenance actions such as list, lint, embedding_status, reindex, or reindex_cards.", Availability: requiresNexus, Handler: ctxToolHandler((*Runtime).recallMaintain)},
		{Name: "private_note_manage", Title: "Manage private notes", Description: "Explicit low-frequency NexusDock private note vault entrypoint. Do not use by default: use only when the user explicitly requests private note access or the content clearly contains sensitive secrets, credentials, or personal information. Search is metadata-only; plaintext is returned only by explicit read, and Git backups contain age ciphertext only. Actions: search, read, write, delete, status, or maintain.", Handler: ctxToolHandler((*Runtime).privateNoteManage), Availability: requiresNexus},
		{Name: "browser_session", Title: "Browser session", Description: "Start an AgentDock-owned Chromium-family browser or attach to an existing CDP browser with a dedicated AgentDock target, then close or clean up the session. External browsers remain running when the session closes.", Annotations: mutatingToolAnnotations(true, true), Availability: requiresBrowser, Handler: ctxToolHandler((*Runtime).browserSession)},
		{Name: "browser_act", Title: "Browser actions", Description: "Run strictly validated CSS/CDP browser actions against an AgentDock-managed browser target and return the final typed page snapshot plus screenshot Artifact.", Annotations: mutatingToolAnnotations(true, true), Availability: requiresBrowser, Handler: ctxToolHandler((*Runtime).browserAct)},
		{Name: "browser_snapshot", Title: "Browser snapshot", Description: "Capture the active or requested CDP target with page text, viewport, page size, focus, visible interactive elements, diagnostics, and a PNG screenshot Artifact.", Annotations: readOnlyToolAnnotations(true), Availability: requiresBrowser, Handler: ctxToolHandler((*Runtime).browserSnapshot)},
		{Name: "file_publish", Title: "Publish signed file", Description: "Publish a local file or directory as an immutable Artifact snapshot under ~/.agentdock/public-artifacts. Returns artifact_id and, when a reachable base URL is available, a temporary signed download URL. Directories are packaged as tar.gz.", Annotations: mutatingToolAnnotations(false, true), FileArgRewritePaths: []string{"file"}, Handler: func(ctx context.Context, r *Runtime, args map[string]any) (Result, error) {
			return r.media.FilePublish(ctx, args)
		}},
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
		if annotations, ok := mcpcontract.AnnotationContract(name); ok {
			specs[i].Annotations = canonicalToolAnnotations(annotations)
		}
	}
	return specs
}

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

func requiresRecall(cfg config.Config) bool  { return cfg.NexusEndpoint != "" }
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
		{Name: "agentdock_context", Title: "AgentDock context", Description: "Return AgentDock bootstrap context including capabilities, skills, workflows, rules, and high-priority context for clients that cannot inject system prompt context.", Handler: ctxToolHandler((*Runtime).agentDockContextTool)},
		{Name: "read_file", Title: "Read file", Description: "Read a UTF-8 text file slice. Relative paths resolve from ~/AgentDock; absolute and ~/ paths use Host rules.", Handler: toolHandler((*Runtime).readFile)},
		{Name: "list_dir", Title: "List directory", Description: "List directory entries. Relative paths resolve from ~/AgentDock; absolute and ~/ paths use Host rules.", Handler: ctxToolHandler((*Runtime).listDir)},
		{Name: "list_files", Title: "List files", Description: "List files using glob and ignore filters. Relative paths resolve from ~/AgentDock by default.", Handler: toolHandler((*Runtime).listFiles)},
		{Name: "search_text", Title: "Search text", Description: "Search UTF-8 files for text or regex matches. Relative paths search ~/AgentDock by default; absolute paths are allowed.", Handler: ctxToolHandler((*Runtime).searchText)},
		{Name: "file_edit", Title: "Edit file", Description: "Edit files through one action-based entrypoint: replace, patch, add, delete, or move. Relative paths resolve from ~/AgentDock; absolute and ~/ paths use Host rules.", Handler: ctxToolHandler((*Runtime).fileEdit)},
		{Name: "exec_command", Title: "Run command", Description: "Run a bounded command. Relative workdir values resolve from ~/AgentDock; actual access follows the Host path model.", Handler: ctxToolHandler((*Runtime).execCommand)},
		{Name: "session_observe", Title: "Observe command sessions", Description: "List or inspect command sessions through a read-only session tool.", Handler: toolHandler((*Runtime).sessionObserve)},
		{Name: "session_act", Title: "Act on command sessions", Description: "Write to or stop command sessions through a mutating session tool.", Handler: toolHandler((*Runtime).sessionAct)},
		{Name: "task_manage", Title: "Manage recoverable tasks", Description: "Persist and resume substantial AgentDock tasks. Use workflow_template_manage action=match for template discovery.", Handler: ctxToolHandler((*Runtime).taskManage)},
		{Name: "workflow_template_manage", Title: "Manage workflow templates", Description: "List, get, save, validate, publish, retire, or match AgentDock workflow templates.", Handler: ctxToolHandler((*Runtime).workflowTemplateManage)},
		{Name: "skill_read", Title: "Read AgentDock Skills", Description: "Read-only Skill discovery through the local Skill Runtime. Actions: list or inspect.", Handler: ctxToolHandler((*Runtime).skillRead)},
		{Name: "skill_package", Title: "Manage Skill packages", Description: "Validate, install, or roll back AgentDock Skill packages through the local Skill Runtime.", Handler: ctxToolHandler((*Runtime).skillPackage)},
		{Name: "skill_run", Title: "Run Skill operation", Description: "Run a single AgentDock Skill operation. The action field is omitted by default; when present it must be run.", Handler: ctxToolHandler((*Runtime).skillRun)},
		{Name: "skill_env_manage", Title: "Manage Skill environment", Description: "Manage redacted Skill environment variables through the local Skill Env Registry.", Handler: ctxToolHandler((*Runtime).skillEnvManage)},
		{Name: "git_read", Title: "Read Git repository state", Description: "Read Git repository information through one action-based entrypoint: repos, status, diff, log, show, blame, or github_repo_access.", Handler: ctxToolHandler((*Runtime).gitRead)},
		{Name: "git_write", Title: "Write Git repository state", Description: "Run mutating Git operations through one action-based entrypoint: clone, commit, fetch, pull, or push.", Handler: ctxToolHandler((*Runtime).gitWrite)},
		{Name: "view_image", Title: "View image", Description: "Publish or inline a local image. Defaults to a temporary signed public URL; Base64/MCP image/data URL require return_mode.", Handler: ctxToolHandler((*Runtime).viewImage)},
		{Name: "recall_bootstrap", Title: "Bootstrap NexusDock Recall context", Description: "Load high-priority NexusDock Recall context at the start of substantial AgentDock, project, deployment, debugging, or preference-sensitive tasks. max_bytes controls pack budget only; compact index/excerpt output is default, and full body requires include_body or targeted recall_read.", Availability: requiresRecall, Handler: ctxToolHandler((*Runtime).recallBootstrap)},
		{Name: "recall_search", Title: "Search NexusDock Recall", Description: "Search NexusDock Recall memories, cards, and notes. Use kind=all, markdown, card, or note; when kind=note, use note_scope=questions or github-learning. Backend handles internal routing such as prefix and scope.", Availability: requiresRecall, Handler: ctxToolHandler((*Runtime).recallSearch)},
		{Name: "recall_read", Title: "Read NexusDock Recall entry", Description: "Read one Markdown, card, or note entry from the configured NexusDock Recall store by path.", Availability: requiresRecall, Handler: ctxToolHandler((*Runtime).recallRead)},
		{Name: "recall_write", Title: "Write NexusDock Recall entry", Description: "Plan, create, replace, append, patch, update facts, diff, or delete NexusDock Recall content. The model must choose target=card/note/markdown and action explicitly.", Availability: requiresRecall, Handler: ctxToolHandler((*Runtime).recallWrite)},
		{Name: "recall_maintain", Title: "Maintain NexusDock Recall", Description: "Run NexusDock Recall maintenance actions such as sync_status, list, lint, embedding_status, reindex, or reindex_cards.", Availability: requiresRecall, Handler: ctxToolHandler((*Runtime).recallMaintain)},
		{Name: "private_note_manage", Title: "Manage private notes", Description: "Explicit low-frequency private note vault entrypoint. Do not use by default: use only when the user asks for private/local-only/non-synced notes, explicitly requests private note access, or the content clearly contains sensitive secrets or personal credentials. Actions: search, read, write, status, or maintain.", Handler: ctxToolHandler((*Runtime).privateNoteManage)},
		{Name: "browser_session", Title: "Browser session", Description: "Start, close, or clean up a browser automation session; supports persistent profiles and injected cookies/localStorage. On macOS, prefer browser=chrome for system Chrome; do not suggest the Playwright browser install command when bundled Chromium is missing.", Availability: requiresBrowser, Handler: ctxToolHandler((*Runtime).browserSession)},
		{Name: "browser_act", Title: "Browser actions", Description: "Run browser automation actions and capture a structured screenshot snapshot; supports close_after, inline image, and storage state save. If bundled Chromium is missing, retry with browser=chrome rather than suggesting the Playwright browser install command.", Availability: requiresBrowser, Handler: func(ctx context.Context, r *Runtime, args map[string]any) (Result, error) {
			return r.browserRunnerCall(ctx, "action", args)
		}},
		{Name: "browser_snapshot", Title: "Browser snapshot", Description: "Capture page URL, title, text, screenshot, image, viewport, visible interactive elements, and browser errors. If bundled Chromium is missing, retry with browser=chrome rather than suggesting the Playwright browser install command.", Availability: requiresBrowser, Handler: func(ctx context.Context, r *Runtime, args map[string]any) (Result, error) {
			return r.browserRunnerCall(ctx, "snapshot", args)
		}},
		{Name: "file_publish", Title: "Publish signed file", Description: "Publish a local file or directory as an immutable snapshot under ~/.agentdock/public-artifacts and return a temporary signed download URL. Directories are packaged as tar.gz.", FileArgRewritePaths: []string{"file"}, Handler: ctxToolHandler((*Runtime).filePublish)},
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

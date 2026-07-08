package tools

import (
	"context"
	"strings"

	"github.com/uvwt/agentdock/internal/config"
)

type ToolHandler func(context.Context, *Runtime, map[string]any) (Result, error)

// ToolSpec 是工具公开入口的单一事实源：运行时分发、MCP 描述、
// profile 暴露和配置开关都从这里派生，避免多处手写列表漂移。
type ToolSpec struct {
	Name                   string
	Title                  string
	Description            string
	ReadOnly               bool
	Destructive            bool
	OpenWorld              bool
	FileArgRewritePaths    []string
	FileResultRewritePaths []string
	Profiles               []string
	Availability           func(config.Config) bool
	Handler                ToolHandler
}

type ToolDefinition struct {
	Name                   string
	Title                  string
	Description            string
	ReadOnly               bool
	Destructive            bool
	OpenWorld              bool
	FileArgRewritePaths    []string
	FileResultRewritePaths []string
}

// ToolDefinitions 只导出 MCP 层需要的描述信息，不暴露 handler。
// schema 仍留在 mcp 包，后续迁移 workspace_edit/git_read 时再继续收敛。
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
		ReadOnly:               s.ReadOnly,
		Destructive:            s.Destructive,
		OpenWorld:              s.OpenWorld,
		FileArgRewritePaths:    append([]string(nil), s.FileArgRewritePaths...),
		FileResultRewritePaths: append([]string(nil), s.FileResultRewritePaths...),
	}
}

func (r *Runtime) availableToolSpecs() []ToolSpec {
	out := make([]ToolSpec, 0, len(allToolSpecs()))
	for _, spec := range allToolSpecs() {
		if !spec.available(r.cfg) || !spec.allowedInProfile(r.cfg.ToolProfile) {
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

func (s ToolSpec) allowedInProfile(profile string) bool {
	if profile != config.ProfileReadOnly {
		return true
	}
	for _, candidate := range s.Profiles {
		if candidate == config.ProfileReadOnly {
			return true
		}
	}
	return false
}

func toolSpecByName(name string) (ToolSpec, bool) {
	for _, spec := range allToolSpecs() {
		if spec.Name == name {
			return spec, true
		}
	}
	return ToolSpec{}, false
}

func readOnlyProfiles() []string { return []string{config.ProfileUnified, config.ProfileReadOnly} }
func unifiedProfiles() []string  { return []string{config.ProfileUnified} }

func requiresRecall(cfg config.Config) bool  { return cfg.RecallEndpoint != "" }
func requiresBrowser(cfg config.Config) bool { return cfg.BrowserEnabled }
func requiresDesktop(cfg config.Config) bool { return cfg.DesktopEnabled }
func requiresNexus(cfg config.Config) bool   { return strings.TrimSpace(cfg.NexusEndpoint) != "" }
func requiresArtifactFetch(cfg config.Config) bool {
	return requiresNexus(cfg) && cfg.ArtifactFetchEnabled
}
func requiresViewImage(cfg config.Config) bool { return cfg.EnableViewImage }

func toolHandler(fn func(*Runtime, map[string]any) (Result, error)) ToolHandler {
	return func(_ context.Context, r *Runtime, args map[string]any) (Result, error) { return fn(r, args) }
}

func ctxToolHandler(fn func(*Runtime, context.Context, map[string]any) (Result, error)) ToolHandler {
	return func(ctx context.Context, r *Runtime, args map[string]any) (Result, error) { return fn(r, ctx, args) }
}

func allToolSpecs() []ToolSpec {
	// 顺序保持和旧 ToolNames 一致，避免 tools/list 与 server_info 的展示顺序无谓变化。
	return []ToolSpec{
		{Name: "server_info", Title: "Server info", Description: "Return server, workspace, auth, profile, sandbox, and exposed-tool metadata.", ReadOnly: true, Profiles: readOnlyProfiles(), Handler: func(_ context.Context, r *Runtime, _ map[string]any) (Result, error) { return r.serverInfo(), nil }},
		{Name: "read_file", Title: "Read file", Description: "Read a UTF-8 text file slice inside the configured workspace.", ReadOnly: true, Profiles: readOnlyProfiles(), Handler: toolHandler((*Runtime).readFile)},
		{Name: "list_dir", Title: "List directory", Description: "List directory entries inside the configured workspace.", ReadOnly: true, Profiles: readOnlyProfiles(), Handler: toolHandler((*Runtime).listDir)},
		{Name: "list_files", Title: "List files", Description: "List workspace files using glob and ignore filters.", ReadOnly: true, Profiles: readOnlyProfiles(), Handler: toolHandler((*Runtime).listFiles)},
		{Name: "search_text", Title: "Search text", Description: "Search UTF-8 workspace files for text or regex matches.", ReadOnly: true, Profiles: readOnlyProfiles(), Handler: toolHandler((*Runtime).searchText)},
		{Name: "workspace_edit", Title: "Edit workspace", Description: "Edit workspace files through one action-based entrypoint: replace, patch, add, delete, or move.", Profiles: unifiedProfiles(), Handler: ctxToolHandler((*Runtime).workspaceEdit)},
		{Name: "exec_command", Title: "Run workspace command", Description: "Run a bounded command in the workspace with sandbox and approval controls.", OpenWorld: true, Profiles: unifiedProfiles(), Handler: ctxToolHandler((*Runtime).execCommand)},
		{Name: "session_control", Title: "Control command sessions", Description: "List, inspect, write to, or stop command sessions through one unified session action tool.", Profiles: readOnlyProfiles(), Handler: toolHandler((*Runtime).sessionControl)},
		{Name: "check_github_repo_access", Title: "Check GitHub repo access", Description: "Check stored GitHub credential authentication and repository visibility without exposing secrets.", ReadOnly: true, OpenWorld: true, Profiles: readOnlyProfiles(), Handler: toolHandler((*Runtime).checkGitHubRepoAccess)},
		{Name: "task_manage", Title: "Manage recoverable tasks", Description: "Persist and resume substantial AgentDock tasks; template_match is the only model-facing workflow-template match entrypoint.", Profiles: unifiedProfiles(), Handler: toolHandler((*Runtime).taskManage)},
		{Name: "workflow_template_manage", Title: "Manage workflow templates", Description: "List, get, save, validate, publish, or retire AgentDock workflow templates. Use task_manage action=template_match for matching.", Profiles: unifiedProfiles(), Handler: toolHandler((*Runtime).workflowTemplateManage)},
		{Name: "skill_manage", Title: "Manage AgentDock Skills", Description: "List, inspect, validate, install, run, or roll back AgentDock Skills through the local Skill Runtime.", Destructive: true, OpenWorld: true, Profiles: unifiedProfiles(), Handler: ctxToolHandler((*Runtime).skillManage)},
		{Name: "env_manage", Title: "Manage Skill environment", Description: "Manage redacted Skill environment variables through the local Nexus Env Registry.", Destructive: true, OpenWorld: true, Profiles: unifiedProfiles(), Handler: ctxToolHandler((*Runtime).envManage)},
		{Name: "git_read", Title: "Read Git repository state", Description: "Read Git repository information through one action-based entrypoint: repos, status, diff, log, show, or blame.", ReadOnly: true, Profiles: readOnlyProfiles(), Handler: ctxToolHandler((*Runtime).gitRead)},
		{Name: "git_write", Title: "Write Git repository state", Description: "Run mutating Git operations through one action-based entrypoint: clone, commit, fetch, pull, or push.", OpenWorld: true, Profiles: unifiedProfiles(), Handler: ctxToolHandler((*Runtime).gitWrite)},
		{Name: "view_image", Title: "View image", Description: "Return a workspace image as MCP image content.", ReadOnly: true, Profiles: readOnlyProfiles(), Availability: requiresViewImage, Handler: toolHandler((*Runtime).viewImage)},
		{Name: "recall_bootstrap", Title: "Bootstrap RecallDock context", Description: "Load high-priority RecallDock context at the start of substantial AgentDock, project, deployment, debugging, or preference-sensitive tasks. max_bytes controls pack budget only; compact index/excerpt output is default, and full body requires include_body or targeted recall_read.", ReadOnly: true, OpenWorld: true, Profiles: readOnlyProfiles(), Availability: requiresRecall, Handler: ctxToolHandler((*Runtime).recallBootstrap)},
		{Name: "recall_search", Title: "Search RecallDock", Description: "Search RecallDock memories, cards, and notes. Use kind=all, markdown, card, or note; when kind=note, use note_scope=questions or github-learning. Backend handles internal routing such as prefix and scope.", ReadOnly: true, OpenWorld: true, Profiles: readOnlyProfiles(), Availability: requiresRecall, Handler: ctxToolHandler((*Runtime).recallSearch)},
		{Name: "recall_read", Title: "Read RecallDock entry", Description: "Read one Markdown, card, or note entry from the configured RecallDock store by path.", ReadOnly: true, OpenWorld: true, Profiles: readOnlyProfiles(), Availability: requiresRecall, Handler: ctxToolHandler((*Runtime).recallRead)},
		{Name: "recall_write", Title: "Write RecallDock entry", Description: "Plan, write, patch, update facts, or delete RecallDock content. The model must choose target and action; target=auto action=plan is the safe classifier path.", Destructive: true, OpenWorld: true, Profiles: unifiedProfiles(), Availability: requiresRecall, Handler: ctxToolHandler((*Runtime).recallWrite)},
		{Name: "recall_maintain", Title: "Maintain RecallDock", Description: "Run RecallDock maintenance actions such as sync_status, list, lint, embedding_status, reindex, or reindex_cards.", Destructive: true, OpenWorld: true, Profiles: unifiedProfiles(), Availability: requiresRecall, Handler: ctxToolHandler((*Runtime).recallMaintain)},
		{Name: "private_notes_search", Title: "Search private notes", Description: "Search the user private notes store. Returns titles, paths, metadata, and code-redacted snippets only; use private_notes_read for full plaintext.", ReadOnly: true, OpenWorld: true, Profiles: readOnlyProfiles(), Handler: ctxToolHandler((*Runtime).privateNotesSearch)},
		{Name: "private_notes_read", Title: "Read private note", Description: "Read one plaintext private note from private-notes/notes. This explicit private-note access returns full plaintext by default.", ReadOnly: true, OpenWorld: true, Profiles: readOnlyProfiles(), Handler: ctxToolHandler((*Runtime).privateNotesRead)},
		{Name: "private_notes_write", Title: "Write private note", Description: "Write a plaintext private note under private-notes/notes and always generate a mandatory age encrypted .md.age backup under private-notes/encrypted. Do not use recall_write for private material.", OpenWorld: true, Profiles: unifiedProfiles(), Handler: ctxToolHandler((*Runtime).privateNotesWrite)},
		{Name: "private_notes_maintain", Title: "Maintain private notes", Description: "Initialize age encryption, list, check, sync, or migrate encrypted backups for the private-notes store. Age .md.age backups are mandatory for every plaintext note.", OpenWorld: true, Profiles: readOnlyProfiles(), Handler: ctxToolHandler((*Runtime).privateNotesMaintain)},
		{Name: "browser_session", Title: "Browser session", Description: "Start, close, or clean up a browser automation session; supports persistent profiles and injected cookies/localStorage.", OpenWorld: true, Profiles: unifiedProfiles(), Availability: requiresBrowser, Handler: ctxToolHandler((*Runtime).browserSession)},
		{Name: "browser_act", Title: "Browser actions", Description: "Run browser automation actions and capture a structured screenshot snapshot; supports close_after, inline image, and storage state save.", OpenWorld: true, Profiles: unifiedProfiles(), Availability: requiresBrowser, Handler: func(ctx context.Context, r *Runtime, args map[string]any) (Result, error) {
			return r.browserRunnerCall(ctx, "action", args)
		}},
		{Name: "browser_snapshot", Title: "Browser snapshot", Description: "Capture page URL, title, text, screenshot, image, viewport, visible interactive elements, and browser errors.", ReadOnly: true, OpenWorld: true, Profiles: readOnlyProfiles(), Availability: requiresBrowser, Handler: func(ctx context.Context, r *Runtime, args map[string]any) (Result, error) {
			return r.browserRunnerCall(ctx, "snapshot", args)
		}},
		{Name: "desktop_observe", Title: "Observe desktop", Description: "Unified macOS desktop observation tool for preflight, app list, app state, windows, screen snapshots, and app snapshots.", ReadOnly: true, Profiles: readOnlyProfiles(), Availability: requiresDesktop, Handler: ctxToolHandler((*Runtime).desktopObserve)},
		{Name: "desktop_act", Title: "Act on desktop", Description: "Unified macOS desktop action tool for focus, move, click, double-click, scroll, drag, type, set value, accessibility actions, hotkeys, and waits.", Destructive: true, Profiles: unifiedProfiles(), Availability: requiresDesktop, Handler: ctxToolHandler((*Runtime).desktopAct)},
		{Name: "desktop_clipboard", Title: "Desktop clipboard", Description: "Read or set the macOS clipboard text through one unified clipboard tool.", Destructive: true, Profiles: unifiedProfiles(), Availability: requiresDesktop, Handler: ctxToolHandler((*Runtime).desktopClipboard)},
		{Name: "artifact_send", Title: "Send encrypted artifact", Description: "Encrypt and send a top-level file parameter or local file/directory through AgentDock Nexus to one or more registered devices. The target only writes to its controlled inbox or configured logical target and never executes the file.", OpenWorld: true, FileArgRewritePaths: []string{"file"}, Profiles: unifiedProfiles(), Availability: requiresNexus, Handler: ctxToolHandler((*Runtime).artifactSend)},
		{Name: "artifact_fetch_create", Title: "Create artifact fetch", Description: "Create an asynchronous high-risk request for a registered device to list or encrypt an absolute-path file or directory under immutable deny rules.", Destructive: true, OpenWorld: true, Profiles: unifiedProfiles(), Availability: requiresArtifactFetch, Handler: ctxToolHandler((*Runtime).artifactFetchCreate)},
		{Name: "artifact_fetch_status", Title: "Artifact fetch status", Description: "Return status or a bounded directory listing for a local artifact fetch request.", ReadOnly: true, OpenWorld: true, Profiles: unifiedProfiles(), Availability: requiresArtifactFetch, Handler: ctxToolHandler((*Runtime).artifactFetchStatus)},
		{Name: "artifact_fetch_download", Title: "Download artifact fetch", Description: "Download and decrypt a ready artifact fetch, return a file resource, or confirm that the GPT sandbox mounted it so ciphertext can be deleted.", Destructive: true, OpenWorld: true, FileResultRewritePaths: []string{"file_path"}, Profiles: unifiedProfiles(), Availability: requiresArtifactFetch, Handler: ctxToolHandler((*Runtime).artifactFetchDownload)},
	}
}

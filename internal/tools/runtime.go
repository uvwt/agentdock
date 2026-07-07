package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/sandbox"
	"github.com/uvwt/agentdock/internal/taskstate"
	"github.com/uvwt/agentdock/internal/workspace"
)

type Result map[string]any

type Runtime struct {
	cfg      config.Config
	ws       *workspace.Workspace
	sessions *SessionStore
	skills   *skillRuntimeManager
	tasks    *taskstate.Store
}

func NewRuntime(cfg config.Config) (*Runtime, error) {
	ws, err := workspace.New(cfg.Workspace, cfg.PathPolicy == config.PathPolicyHost)
	if err != nil {
		return nil, err
	}
	skills, err := newSkillRuntimeManager(cfg)
	if err != nil {
		return nil, err
	}
	taskRoot := cfg.AgentDockDir
	if !filepath.IsAbs(taskRoot) {
		taskRoot = filepath.Join(cfg.Workspace, taskRoot)
	}
	tasks, err := taskstate.NewWithOptions(filepath.Join(taskRoot, "tasks"), taskstate.StoreOptions{
		TaskVectorSearch:    cfg.TaskVectorSearch,
		EmbeddingEndpoint:   cfg.TaskEmbeddingEndpoint,
		EmbeddingToken:      cfg.TaskEmbeddingToken,
		EmbeddingModel:      cfg.TaskEmbeddingModel,
		TaskVectorTimeoutMS: cfg.TaskVectorTimeoutMS,
		TaskVectorMinScore:  cfg.TaskVectorMinScore,
	})
	if err != nil {
		return nil, err
	}
	return &Runtime{cfg: cfg, ws: ws, sessions: NewSessionStore(), skills: skills, tasks: tasks}, nil
}

func (r *Runtime) Config() config.Config           { return r.cfg }
func (r *Runtime) Workspace() *workspace.Workspace { return r.ws }

func (r *Runtime) ToolNames() []string {
	all := []string{"server_info", "tool_descriptors", "get_default_cwd", "set_default_cwd", "read_file", "list_dir", "list_files", "search_text", "apply_patch", "edit_file", "exec_command", "session_control", "configure_github_token", "check_github_repo_access", "github_create_repo", "task_manage", "workflow_template_manage", "skill_manage", "env_manage", "workspace_repos", "git_status", "git_diff", "git_log", "git_inspect", "git_remote", "git_clone", "git_commit", "request_permissions", "view_image"}
	if r.cfg.RecallEndpoint != "" {
		all = append(all, "recall_bootstrap", "recall_search", "recall_read", "recall_write", "recall_maintain")
	}
	all = append(all, "private_notes_search", "private_notes_read", "private_notes_write", "private_notes_maintain")
	if r.cfg.BrowserEnabled {
		all = append(all, "browser_session", "browser_act", "browser_snapshot", "browser_profile")
	}
	if r.cfg.DesktopEnabled {
		all = append(all, "desktop_observe", "desktop_act", "desktop_clipboard")
	}
	if strings.TrimSpace(r.cfg.NexusEndpoint) != "" {
		all = append(all, "artifact_send")
		if r.cfg.ArtifactFetchEnabled {
			all = append(all, "artifact_fetch_create", "artifact_fetch_status", "artifact_fetch_download")
		}
	}
	if !r.cfg.EnableViewImage {
		all = removeTool(all, "view_image")
	}
	if r.cfg.ToolProfile != config.ProfileReadOnly {
		return all
	}
	readOnly := []string{"server_info", "tool_descriptors", "get_default_cwd", "set_default_cwd", "read_file", "list_dir", "list_files", "search_text", "session_control", "check_github_repo_access", "workspace_repos", "git_status", "git_diff", "git_log", "git_inspect", "request_permissions", "view_image"}
	if r.cfg.RecallEndpoint != "" {
		readOnly = append(readOnly, "recall_bootstrap", "recall_search", "recall_read")
	}
	readOnly = append(readOnly, "private_notes_search", "private_notes_read", "private_notes_maintain")
	if r.cfg.BrowserEnabled {
		readOnly = append(readOnly, "browser_snapshot")
	}
	if r.cfg.DesktopEnabled {
		readOnly = append(readOnly, "desktop_observe")
	}
	if !r.cfg.EnableViewImage {
		readOnly = removeTool(readOnly, "view_image")
	}
	return readOnly
}

func removeTool(names []string, target string) []string {
	out := names[:0]
	for _, name := range names {
		if name != target {
			out = append(out, name)
		}
	}
	return out
}

func (r *Runtime) Call(ctx context.Context, name string, args map[string]any) (Result, error) {
	if args == nil {
		args = map[string]any{}
	}
	if !r.available(name) {
		return nil, toolErrorDetails("UNKNOWN_TOOL", "tool is not available", "validation", map[string]any{"tool": name})
	}
	switch name {
	case "server_info":
		return r.serverInfo(), nil
	case "tool_descriptors":
		return r.toolDescriptors(), nil
	case "get_default_cwd":
		return Result{"ok": true, "path": r.ws.DefaultDisplay()}, nil
	case "set_default_cwd":
		p, err := r.ws.SetDefaultCWD(stringArg(args, "path", "."))
		return Result{"ok": err == nil, "path": p}, err
	case "read_file":
		return r.readFile(args)
	case "list_dir":
		return r.listDir(args)
	case "list_files":
		return r.listFiles(args)
	case "search_text":
		return r.searchText(args)
	case "apply_patch":
		return r.applyPatch(ctx, args)
	case "edit_file":
		return r.editFile(args)
	case "exec_command":
		return r.execCommand(ctx, args)
	case "session_control":
		return r.sessionControl(args)
	case "write_stdin":
		return r.writeStdin(args)
	case "session_status":
		return r.sessionStatus(args)
	case "list_sessions":
		return r.listSessions()
	case "kill_session":
		return r.killSession(args)
	case "kill_all_sessions":
		return r.killAllSessions(args)
	case "configure_github_token":
		return r.configureGitHubToken(args)
	case "check_github_repo_access":
		return r.checkGitHubRepoAccess(args)
	case "github_create_repo":
		return r.githubCreateRepo(args)
	case "task_manage":
		return r.taskManage(args)
	case "workflow_template_manage":
		return r.workflowTemplateManage(args)
	case "skill_manage":
		return r.skillManage(ctx, args)
	case "env_manage":
		return r.envManage(ctx, args)
	case "artifact_send":
		return r.artifactSend(ctx, args)
	case "artifact_fetch_create":
		return r.artifactFetchCreate(ctx, args)
	case "artifact_fetch_status":
		return r.artifactFetchStatus(ctx, args)
	case "artifact_fetch_download":
		return r.artifactFetchDownload(ctx, args)
	case "recall_bootstrap":
		return r.recallBootstrap(ctx, args)
	case "recall_search":
		return r.recallSearch(ctx, args)
	case "recall_read":
		return r.recallRead(ctx, args)
	case "recall_write":
		return r.recallWrite(ctx, args)
	case "recall_maintain":
		return r.recallMaintain(ctx, args)
	case "private_notes_search":
		return r.privateNotesSearch(ctx, args)
	case "private_notes_read":
		return r.privateNotesRead(ctx, args)
	case "private_notes_write":
		return r.privateNotesWrite(ctx, args)
	case "private_notes_maintain":
		return r.privateNotesMaintain(ctx, args)
	case "browser_session":
		return r.browserSession(ctx, args)
	case "browser_profile":
		return r.browserProfile(ctx, args)
	case "browser_act", "browser_action":
		return r.browserRunnerCall(ctx, "action", args)
	case "browser_session_start":
		return r.browserRunnerCall(ctx, "session_start", args)
	case "browser_snapshot":
		return r.browserRunnerCall(ctx, "snapshot", args)
	case "browser_session_close":
		return r.browserRunnerCall(ctx, "session_close", args)
	case "browser_session_cleanup":
		return r.browserRunnerCall(ctx, "session_cleanup", args)
	case "desktop_observe":
		return r.desktopObserve(ctx, args)
	case "desktop_act":
		return r.desktopAct(ctx, args)
	case "desktop_clipboard":
		return r.desktopClipboard(ctx, args)
	case "desktop_preflight":
		return r.desktopPreflight(ctx, args)
	case "desktop_list_apps":
		return r.desktopListApps(ctx, args)
	case "desktop_get_app_state":
		return r.desktopGetAppState(ctx, args)
	case "desktop_window_list":
		return r.desktopWindowList(ctx, args)
	case "desktop_snapshot":
		return r.desktopSnapshot(ctx, args)
	case "desktop_snapshot_app":
		return r.desktopSnapshotApp(ctx, args)
	case "desktop_clipboard_set":
		return r.desktopClipboardSet(ctx, args)
	case "desktop_clipboard_get":
		return r.desktopClipboardGet(ctx, args)
	case "desktop_focus_app":
		return r.desktopFocusApp(ctx, args)
	case "desktop_move":
		return r.desktopMove(ctx, args)
	case "desktop_click":
		return r.desktopClick(ctx, args)
	case "desktop_double_click":
		return r.desktopDoubleClick(ctx, args)
	case "desktop_scroll":
		return r.desktopScroll(ctx, args)
	case "desktop_drag":
		return r.desktopDrag(ctx, args)
	case "desktop_type":
		return r.desktopType(ctx, args)
	case "desktop_set_value":
		return r.desktopSetValue(ctx, args)
	case "desktop_perform_secondary_action":
		return r.desktopPerformSecondaryAction(ctx, args)
	case "desktop_hotkey":
		return r.desktopHotkey(ctx, args)
	case "desktop_wait":
		return r.desktopWait(ctx, args)
	case "workspace_repos":
		return r.workspaceRepos(ctx, args)
	case "git_repo_status":
		return r.gitRepoStatus(ctx, args)
	case "git_status":
		return r.gitRepoStatus(ctx, args)
	case "git_diff":
		return r.gitDiff(ctx, args)
	case "git_log":
		return r.gitLog(ctx, args)
	case "git_show":
		return r.gitShow(ctx, args)
	case "git_blame":
		return r.gitBlame(ctx, args)
	case "git_inspect":
		return r.gitInspect(ctx, args)
	case "git_remote":
		return r.gitRemote(ctx, args)
	case "git_fetch":
		return r.gitFetch(ctx, args)
	case "git_pull":
		return r.gitPull(ctx, args)
	case "git_push":
		return r.gitPush(ctx, args)
	case "git_clone":
		return r.gitClone(ctx, args)
	case "git_commit":
		return r.gitCommit(ctx, args)
	case "request_permissions":
		return r.requestPermissions(args), nil
	case "view_image":
		return r.viewImage(args)
	default:
		return nil, toolErrorDetails("UNKNOWN_TOOL", "unknown tool", "validation", map[string]any{"tool": name})
	}
}

func (r *Runtime) available(name string) bool {
	for _, candidate := range r.ToolNames() {
		if candidate == name {
			return true
		}
	}
	return false
}

func (r *Runtime) serverInfo() Result {
	names := r.ToolNames()

	// server_info 是排障入口：这里按主题分组保留字段，避免新增运行能力时
	// 把自检输出重新堆成一行难以审查的 map。
	return Result{
		"ok":               true,
		"server":           config.ServerName,
		"title":            "AgentDock",
		"version":          config.Version,
		"protocol_version": config.ProtocolVersion,

		"os":         runtime.GOOS,
		"arch":       runtime.GOARCH,
		"go_version": runtime.Version(),

		"workspace":      r.ws.Root(),
		"default_cwd":    r.ws.DefaultDisplay(),
		"mode":           r.cfg.Mode,
		"path_policy":    r.cfg.PathPolicy,
		"tool_profile":   r.cfg.ToolProfile,
		"sandbox_mode":   r.cfg.SandboxMode,
		"agent_dock_dir": r.cfg.AgentDockDir,

		"recall_enabled":               r.cfg.RecallEndpoint != "",
		"recall_endpoint":              r.cfg.RecallEndpoint,
		"recall_bootstrap_recommended": r.cfg.RecallEndpoint != "",
		"recall_bootstrap_tool":        "recall_bootstrap",
		"recall_bootstrap_args":        map[string]any{},

		"task_state_dir": r.tasks.Root(),
		"workflow_dir":   r.tasks.WorkflowRoot(),

		"browser_enabled":      r.cfg.BrowserEnabled,
		"browser_runner_dir":   r.cfg.BrowserRunnerDir,
		"browser_artifact_dir": r.cfg.BrowserArtifactDir,
		"desktop_enabled":      r.cfg.DesktopEnabled,
		"desktop_artifact_dir": r.cfg.DesktopArtifactDir,

		"auth_enabled":  r.authEnabled(),
		"endpoint_path": "/mcp",
		"tools":         names,
		"tool_count":    len(names),
		"sandbox":       sandbox.StatusForWorkspace(r.ws.Root()),
	}
}

func (r *Runtime) authEnabled() bool {
	return strings.TrimSpace(r.cfg.AuthToken) != "" || strings.TrimSpace(r.cfg.OAuthClientID) != "" || strings.TrimSpace(r.cfg.OAuthServerURL) != ""
}

func (r *Runtime) toolDescriptors() Result {
	descriptors := make([]map[string]any, 0)
	for _, name := range r.ToolNames() {
		descriptors = append(descriptors, map[string]any{"name": name})
	}
	return Result{"ok": true, "tools": descriptors, "count": len(descriptors)}
}

func (r *Runtime) readFile(args map[string]any) (Result, error) {
	p, err := r.ws.ResolveExisting(stringArg(args, "path", "."))
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(p.Abs)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, toolError("IS_DIRECTORY", "cannot read directory", "validation")
	}
	data, err := os.ReadFile(p.Abs)
	if err != nil {
		return nil, err
	}
	if looksBinary(data) {
		return nil, toolError("BINARY_FILE", "binary file read blocked for text tool", "validation")
	}
	if !utf8.Valid(data) {
		return nil, toolError("UNSUPPORTED_ENCODING", "file is not valid utf-8", "validation")
	}
	content, meta := sliceText(string(data), intArg(args, "start_line", 1), intArg(args, "end_line", 0), intArg(args, "max_bytes", 262144))
	result := Result{"ok": true, "path": p.Display, "content": content, "encoding": "utf-8", "size_bytes": len(data), "truncated": meta.Truncated, "start_line": meta.Start, "end_line": meta.End, "total_lines": meta.Total}
	if meta.NextStartLine > 0 {
		result["next_start_line"] = meta.NextStartLine
	}
	if meta.TruncatedReason != "" {
		result["truncated_reason"] = meta.TruncatedReason
	}
	return result, nil
}

func (r *Runtime) listDir(args map[string]any) (Result, error) {
	p, err := r.ws.ResolveExisting(stringArg(args, "path", "."))
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(p.Abs)
	if err != nil {
		return nil, err
	}
	includeHidden := boolArg(args, "include_hidden", false)
	recursive := boolArg(args, "recursive", false)
	maxDepth := intArg(args, "max_depth", 1)
	maxEntries := intArg(args, "max_entries", 200)
	includeIgnored := boolArg(args, "include_ignored", false)
	if recursive {
		return r.listDirRecursive(p, includeHidden, includeIgnored, maxDepth, maxEntries)
	}
	ignore := loadIgnoreMatcher(r.ws.Root())
	items := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		if !includeHidden && workspace.Hidden(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		abs := filepath.Join(p.Abs, entry.Name())
		rel, _ := r.ws.Relative(abs)
		if !includeIgnored && (shouldSkipDir(entry.Name()) || ignore.Ignored(rel, info.IsDir())) {
			continue
		}
		kind := "file"
		if info.IsDir() {
			kind = "directory"
		}
		items = append(items, map[string]any{"name": entry.Name(), "path": rel, "type": kind, "size_bytes": info.Size(), "modified": info.ModTime().UTC().Format(time.RFC3339Nano), "is_hidden": workspace.Hidden(entry.Name())})
		if maxEntries > 0 && len(items) >= maxEntries {
			break
		}
	}
	sort.Slice(items, func(i, j int) bool { return fmt.Sprint(items[i]["path"]) < fmt.Sprint(items[j]["path"]) })
	return Result{"ok": true, "path": p.Display, "entries": items, "truncated": maxEntries > 0 && len(items) >= maxEntries}, nil
}

func (r *Runtime) listDirRecursive(root workspace.Path, includeHidden, includeIgnored bool, maxDepth, maxEntries int) (Result, error) {
	items := make([]map[string]any, 0)
	ignore := loadIgnoreMatcher(r.ws.Root())
	rootDepth := len(strings.Split(filepath.Clean(root.Abs), string(os.PathSeparator)))
	err := filepath.WalkDir(root.Abs, func(abs string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if abs == root.Abs {
			return nil
		}
		depth := len(strings.Split(filepath.Clean(abs), string(os.PathSeparator))) - rootDepth
		if maxDepth > 0 && depth > maxDepth {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !includeHidden && workspace.Hidden(entry.Name()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := r.ws.Relative(abs)
		if !includeIgnored && (shouldSkipDir(entry.Name()) || ignore.Ignored(rel, entry.IsDir())) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		kind := "file"
		if info.IsDir() {
			kind = "directory"
		}
		items = append(items, map[string]any{"name": entry.Name(), "path": rel, "type": kind, "size_bytes": info.Size(), "modified": info.ModTime().UTC().Format(time.RFC3339Nano), "is_hidden": workspace.Hidden(entry.Name())})
		if maxEntries > 0 && len(items) >= maxEntries {
			return filepath.SkipAll
		}
		return nil
	})
	sort.Slice(items, func(i, j int) bool { return fmt.Sprint(items[i]["path"]) < fmt.Sprint(items[j]["path"]) })
	return Result{"ok": err == nil, "path": root.Display, "entries": items, "recursive": true, "max_depth": maxDepth, "truncated": maxEntries > 0 && len(items) >= maxEntries}, err
}

func (r *Runtime) listFiles(args map[string]any) (Result, error) {
	p, err := r.ws.ResolveExisting(stringArg(args, "path", "."))
	if err != nil {
		return nil, err
	}
	patterns := stringSliceArg(args, "patterns")
	if len(patterns) == 0 {
		patterns = []string{"**/*"}
	}
	if glob := stringArg(args, "glob", ""); glob != "" {
		patterns = []string{glob}
	}
	excludePatterns := stringSliceArg(args, "exclude_patterns")
	maxResults := intArg(args, "max_results", 500)
	includeHidden := boolArg(args, "include_hidden", false)
	includeIgnored := boolArg(args, "include_ignored", false)
	ignore := loadIgnoreMatcher(r.ws.Root())
	files := make([]map[string]any, 0)
	err = filepath.WalkDir(p.Abs, func(abs string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		rel, relErr := r.ws.Relative(abs)
		if relErr == nil && !includeIgnored && ignore.Ignored(rel, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if !includeIgnored && abs != p.Abs && shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			if !includeHidden && abs != p.Abs && workspace.Hidden(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !includeHidden && workspace.Hidden(d.Name()) {
			return nil
		}
		rel, err := r.ws.Relative(abs)
		if err != nil || !matchesAny(rel, patterns) || matchesAny(rel, excludePatterns) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		files = append(files, map[string]any{"path": rel, "type": "file", "size_bytes": info.Size(), "modified": info.ModTime().UTC().Format(time.RFC3339Nano)})
		if maxResults > 0 && len(files) >= maxResults {
			return filepath.SkipAll
		}
		return nil
	})
	return Result{"ok": err == nil, "path": p.Display, "files": files, "truncated": maxResults > 0 && len(files) >= maxResults}, err
}

func (r *Runtime) viewImage(args map[string]any) (Result, error) {
	p, err := r.ws.ResolveExisting(stringArg(args, "path", "."))
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p.Abs)
	if err != nil {
		return nil, err
	}
	info, err := identifyImage(data)
	if err != nil {
		return nil, toolError("BINARY_FILE", "file is not a supported image", "validation")
	}
	maxBytes := intArg(args, "max_bytes", 750000)
	maxWidth := intArg(args, "max_width", 1280)
	maxHeight := intArg(args, "max_height", 1280)
	format := stringArg(args, "format", "jpeg")
	quality := intArg(args, "quality", 72)
	crop := cropArg(args)
	autoResize := boolArg(args, "auto_resize", true)

	original := map[string]any{"bytes": len(data), "width": info.Width, "height": info.Height, "mime_type": info.MIME}
	prepared := data
	preparedInfo := info
	warnings := []string{}
	resized := false
	if crop != nil || autoResize || strings.TrimSpace(format) != "" || quality != 72 {
		var ok bool
		var prepOriginal map[string]any
		prepared, preparedInfo, prepOriginal, warnings, ok = prepareImageBytes(data, crop, maxBytes, maxWidth, maxHeight, format, quality)
		if prepOriginal != nil {
			original = prepOriginal
		}
		if !ok {
			return nil, toolErrorDetails("IMAGE_TOO_LARGE", "image exceeds max_bytes after processing", "validation", map[string]any{"bytes": len(prepared), "max_bytes": maxBytes, "auto_resize": autoResize, "warnings": warnings})
		}
		resized = preparedInfo.Width != info.Width || preparedInfo.Height != info.Height || len(prepared) != len(data)
	}
	if len(prepared) > maxBytes {
		return nil, toolErrorDetails("IMAGE_TOO_LARGE", "image exceeds max_bytes", "validation", map[string]any{"bytes": len(prepared), "max_bytes": maxBytes, "auto_resize": autoResize, "warnings": warnings})
	}
	output := stringArg(args, "output", "mcp_image")
	encoded := base64.StdEncoding.EncodeToString(prepared)
	result := Result{"ok": true, "path": p.Display, "mime_type": preparedInfo.MIME, "size_bytes": len(prepared), "width": preparedInfo.Width, "height": preparedInfo.Height, "original": original, "resized": resized, "warnings": warnings, "data_base64": encoded, "output": output, "image_attached": output == "mcp_image"}
	if output == "data_url" {
		result["data_url"] = "data:" + preparedInfo.MIME + ";base64," + encoded
	}
	return result, nil
}

func (r *Runtime) requestPermissions(args map[string]any) Result {
	if r.cfg.DangerouslySkipAllPermissions {
		return Result{"ok": true, "status": "granted", "grant_id": "dangerously-skip-all-permissions", "requested": args}
	}
	return Result{"ok": false, "status": "required", "requested": args}
}

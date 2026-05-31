package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/sandbox"
	"github.com/uvwt/agentdock/internal/workspace"
)

type Result map[string]any

type Runtime struct {
	cfg      config.Config
	ws       *workspace.Workspace
	sessions *SessionStore
}

func NewRuntime(cfg config.Config) (*Runtime, error) {
	ws, err := workspace.New(cfg.Workspace, cfg.PathPolicy == config.PathPolicyHost)
	if err != nil {
		return nil, err
	}
	return &Runtime{cfg: cfg, ws: ws, sessions: NewSessionStore()}, nil
}

func (r *Runtime) Config() config.Config           { return r.cfg }
func (r *Runtime) Workspace() *workspace.Workspace { return r.ws }

func (r *Runtime) ToolNames() []string {
	all := []string{"server_info", "tool_descriptors", "get_default_cwd", "set_default_cwd", "read_file", "list_dir", "list_files", "search_text", "apply_patch", "exec_command", "write_stdin", "session_status", "list_sessions", "kill_session", "kill_all_sessions", "configure_github_token", "check_github_repo_access", "github_create_repo", "plugin_list", "plugin_describe", "plugin_call", "workspace_repos", "git_repo_status", "git_status", "git_diff", "git_log", "git_show", "git_blame", "git_fetch", "git_pull", "git_push", "git_clone", "git_commit", "request_permissions", "view_image"}
	if r.cfg.MemoryEndpoint != "" {
		all = append(all, "memory_bootstrap", "memory_list", "memory_read", "memory_search", "memory_pack", "memory_append_note", "memory_write", "memory_delete", "memory_sync_status")
	}
	if r.cfg.BrowserEnabled {
		all = append(all, "browser_session_start", "browser_action", "browser_snapshot", "browser_session_close")
	}
	if r.cfg.DesktopEnabled {
		all = append(all, "desktop_preflight", "desktop_window_list", "desktop_snapshot", "desktop_clipboard_set", "desktop_clipboard_get", "desktop_focus_app", "desktop_move", "desktop_click", "desktop_double_click", "desktop_scroll", "desktop_drag", "desktop_type", "desktop_hotkey", "desktop_wait")
	}
	if !r.cfg.EnableViewImage {
		all = removeTool(all, "view_image")
	}
	if r.cfg.ToolProfile != config.ProfileReadOnly {
		return all
	}
	readOnly := []string{"server_info", "tool_descriptors", "get_default_cwd", "set_default_cwd", "read_file", "list_dir", "list_files", "search_text", "session_status", "list_sessions", "check_github_repo_access", "plugin_list", "plugin_describe", "workspace_repos", "git_repo_status", "git_status", "git_diff", "git_log", "git_show", "git_blame", "request_permissions", "view_image"}
	if r.cfg.MemoryEndpoint != "" {
		readOnly = append(readOnly, "memory_bootstrap", "memory_list", "memory_read", "memory_search", "memory_pack", "memory_sync_status")
	}
	if r.cfg.BrowserEnabled {
		readOnly = append(readOnly, "browser_snapshot")
	}
	if r.cfg.DesktopEnabled {
		readOnly = append(readOnly, "desktop_preflight", "desktop_window_list", "desktop_snapshot", "desktop_clipboard_get", "desktop_wait")
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
	case "exec_command":
		return r.execCommand(ctx, args)
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
	case "plugin_list":
		return r.pluginList(args)
	case "plugin_describe":
		return r.pluginDescribe(args)
	case "plugin_call":
		return r.pluginCall(ctx, args)
	case "memory_bootstrap":
		return r.memoryBootstrap(ctx, args)
	case "memory_list":
		return r.memoryList(ctx, args)
	case "memory_read":
		return r.memoryRead(ctx, args)
	case "memory_search":
		return r.memorySearch(ctx, args)
	case "memory_pack":
		return r.memoryPack(ctx, args)
	case "memory_append_note":
		return r.memoryAppendNote(ctx, args)
	case "memory_write":
		return r.memoryWrite(ctx, args)
	case "memory_delete":
		return r.memoryDelete(ctx, args)
	case "memory_sync_status":
		return r.memorySyncStatus(ctx, args)
	case "browser_session_start":
		return r.browserSessionStart(ctx, args)
	case "browser_action":
		return r.browserAction(ctx, args)
	case "browser_snapshot":
		return r.browserSnapshot(ctx, args)
	case "browser_session_close":
		return r.browserSessionClose(ctx, args)
	case "desktop_preflight":
		return r.desktopPreflight(ctx, args)
	case "desktop_window_list":
		return r.desktopWindowList(ctx, args)
	case "desktop_snapshot":
		return r.desktopSnapshot(ctx, args)
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
	case "desktop_hotkey":
		return r.desktopHotkey(ctx, args)
	case "desktop_wait":
		return r.desktopWait(ctx, args)
	case "workspace_repos":
		return r.workspaceRepos(ctx, args)
	case "git_repo_status":
		return r.gitRepoStatus(ctx, args)
	case "git_status":
		return r.gitStatus(ctx, args)
	case "git_diff":
		return r.gitDiff(ctx, args)
	case "git_log":
		return r.gitLog(ctx, args)
	case "git_show":
		return r.gitShow(ctx, args)
	case "git_blame":
		return r.gitBlame(ctx, args)
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
	return Result{"ok": true, "server": config.ServerName, "title": "AgentDock", "version": config.Version, "protocol_version": config.ProtocolVersion, "os": runtime.GOOS, "arch": runtime.GOARCH, "go_version": runtime.Version(), "workspace": r.ws.Root(), "default_cwd": r.ws.DefaultDisplay(), "mode": r.cfg.Mode, "path_policy": r.cfg.PathPolicy, "tool_profile": r.cfg.ToolProfile, "sandbox_mode": r.cfg.SandboxMode, "agent_dock_dir": r.cfg.AgentDockDir, "plugin_dir": r.cfg.PluginDir, "memory_enabled": r.cfg.MemoryEndpoint != "", "memory_endpoint": r.cfg.MemoryEndpoint, "memory_bootstrap_recommended": r.cfg.MemoryEndpoint != "", "memory_bootstrap_tool": "memory_bootstrap", "memory_bootstrap_args": map[string]any{"project": "agentdock", "max_bytes": 50000}, "browser_enabled": r.cfg.BrowserEnabled, "browser_runner_dir": r.cfg.BrowserRunnerDir, "browser_artifact_dir": r.cfg.BrowserArtifactDir, "desktop_enabled": r.cfg.DesktopEnabled, "desktop_artifact_dir": r.cfg.DesktopArtifactDir, "auth_enabled": r.cfg.AuthToken != "", "endpoint_path": "/mcp", "tools": names, "tool_count": len(names), "sandbox": sandbox.StatusForWorkspace(r.ws.Root())}
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
	return Result{"ok": true, "path": p.Display, "content": content, "encoding": "utf-8", "size_bytes": len(data), "truncated": meta.Truncated, "start_line": meta.Start, "end_line": meta.End, "total_lines": meta.Total}, nil
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

func (r *Runtime) searchText(args map[string]any) (Result, error) {
	query := stringArg(args, "query", "")
	if query == "" {
		return nil, toolError("INVALID_ARGUMENT", "query is required", "validation")
	}
	p, err := r.ws.ResolveExisting(stringArg(args, "path", "."))
	if err != nil {
		return nil, err
	}
	caseSensitive := boolArg(args, "case_sensitive", false)
	useRegex := boolArg(args, "regex", false)
	includeIgnored := boolArg(args, "include_ignored", false)
	includeGlobs := stringSliceArg(args, "include_globs")
	if glob := stringArg(args, "glob", ""); glob != "" {
		includeGlobs = append(includeGlobs, glob)
	}
	excludeGlobs := stringSliceArg(args, "exclude_globs")
	maxResults := intArg(args, "max_results", 100)
	contextLines := intArg(args, "context_lines", 0)
	needle := query
	if !caseSensitive {
		needle = strings.ToLower(needle)
	}
	var re *regexp.Regexp
	if useRegex {
		pattern := query
		if !caseSensitive {
			pattern = "(?i)" + pattern
		}
		re, err = regexp.Compile(pattern)
		if err != nil {
			return nil, err
		}
	}
	matches := make([]map[string]any, 0)
	ignore := loadIgnoreMatcher(r.ws.Root())
	_ = filepath.WalkDir(p.Abs, func(abs string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		rel, _ := r.ws.Relative(abs)
		if !includeIgnored && ignore.Ignored(rel, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if !includeIgnored && abs != p.Abs && shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if len(includeGlobs) > 0 && !matchesAny(rel, includeGlobs) {
			return nil
		}
		if matchesAny(rel, excludeGlobs) {
			return nil
		}
		data, err := os.ReadFile(abs)
		if err != nil || looksBinary(data) {
			return nil
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			probe := line
			if !caseSensitive {
				probe = strings.ToLower(probe)
			}
			ok := strings.Contains(probe, needle)
			if re != nil {
				ok = re.MatchString(line)
			}
			if !ok {
				continue
			}
			before, after := contextAround(lines, i, contextLines)
			matches = append(matches, map[string]any{"path": rel, "line": i + 1, "preview": truncateString(line, 500), "before": before, "after": after})
			if maxResults > 0 && len(matches) >= maxResults {
				return filepath.SkipAll
			}
		}
		return nil
	})
	return Result{"ok": true, "query": query, "matches": matches, "total_matches": len(matches), "truncated": maxResults > 0 && len(matches) >= maxResults}, nil
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
	maxBytes := intArg(args, "max_bytes", 5242880)
	maxWidth := intArg(args, "max_width", 2000)
	maxHeight := intArg(args, "max_height", 2000)
	autoResize := boolArg(args, "auto_resize", true)
	original := map[string]any{"bytes": len(data), "width": info.Width, "height": info.Height, "mime_type": info.MIME}
	resized := false
	warnings := []string{}
	if autoResize && shouldResizeImage(len(data), info.Width, info.Height, maxBytes, maxWidth, maxHeight) {
		if resizedData, resizedInfo, ok := resizeImageBytes(data, maxBytes, maxWidth, maxHeight); ok {
			data = resizedData
			info = resizedInfo
			resized = true
		} else {
			warnings = append(warnings, "auto_resize requested but image resize failed")
		}
	}
	if len(data) > maxBytes {
		return nil, toolErrorDetails("IMAGE_TOO_LARGE", "image exceeds max_bytes", "validation", map[string]any{"bytes": len(data), "max_bytes": maxBytes, "resize_attempted": autoResize, "warnings": warnings})
	}
	output := stringArg(args, "output", "mcp_image")
	encoded := base64.StdEncoding.EncodeToString(data)
	result := Result{"ok": true, "path": p.Display, "mime_type": info.MIME, "size_bytes": len(data), "width": info.Width, "height": info.Height, "original": original, "resized": resized, "warnings": warnings, "data_base64": encoded, "output": output}
	if output == "data_url" {
		result["data_url"] = "data:" + info.MIME + ";base64," + encoded
	}
	return result, nil
}

func (r *Runtime) requestPermissions(args map[string]any) Result {
	if r.cfg.DangerouslySkipAllPermissions {
		return Result{"ok": true, "status": "granted", "grant_id": "dangerously-skip-all-permissions", "requested": args}
	}
	return Result{"ok": false, "status": "required", "requested": args}
}

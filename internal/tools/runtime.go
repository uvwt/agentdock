package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/taskstate"
	"github.com/uvwt/agentdock/internal/workspace"
)

const (
	maxTextFileReadBytes = 32 << 20
	maxTextOutputBytes   = 4 << 20
)

type Result map[string]any

type Runtime struct {
	cfg            config.Config
	ws             *workspace.Workspace
	sessions       *SessionStore
	skills         *skillManager
	tasks          *taskstate.Store
	privateNotesMu sync.RWMutex
}

func NewRuntime(cfg config.Config) (*Runtime, error) {
	ws, err := workspace.New(cfg.AgentDockDefaultDir)
	if err != nil {
		return nil, err
	}
	skills, err := newSkillManager(cfg)
	if err != nil {
		return nil, err
	}
	tasks, err := taskstate.New(filepath.Join(cfg.AgentDockHome, "tasks"))
	if err != nil {
		return nil, err
	}
	return &Runtime{cfg: cfg, ws: ws, sessions: NewSessionStore(), skills: skills, tasks: tasks}, nil
}

func (r *Runtime) Config() config.Config           { return r.cfg }
func (r *Runtime) Workspace() *workspace.Workspace { return r.ws }

func (r *Runtime) ToolNames() []string {
	specs := r.availableToolSpecs()
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		names = append(names, spec.Name)
	}
	return names
}

func (r *Runtime) Call(ctx context.Context, name string, args map[string]any) (Result, error) {
	if args == nil {
		args = map[string]any{}
	}
	spec, ok := toolSpecByName(name)
	if !ok || !spec.available(r.cfg) {
		return nil, toolErrorDetails("UNKNOWN_TOOL", "tool is not available", "validation", map[string]any{"tool": name})
	}
	if spec.Handler == nil {
		return nil, toolErrorDetails("UNKNOWN_TOOL", "tool has no handler", "validation", map[string]any{"tool": name})
	}
	return spec.Handler(ctx, r, args)
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

		"agentdock_home":        r.cfg.AgentDockHome,
		"agentdock_default_dir": r.cfg.AgentDockDefaultDir,
		"default_cwd":           r.ws.DefaultDisplay(),
		"path_model":            config.PathModel,

		"recall_enabled":               r.cfg.NexusEndpoint != "",
		"nexus_endpoint":               r.cfg.NexusEndpoint,
		"recall_bootstrap_recommended": r.cfg.NexusEndpoint != "",
		"recall_bootstrap_tool":        "recall_bootstrap",
		"recall_bootstrap_args":        map[string]any{},

		"task_state_dir": r.tasks.Root(),

		"browser_enabled": r.cfg.BrowserEnabled,

		"auth_enabled":  r.authEnabled(),
		"endpoint_path": "/mcp",
		"tools":         names,
		"tool_count":    len(names),
	}
}

func (r *Runtime) authEnabled() bool {
	return r.cfg.AuthRequired()
}

func (r *Runtime) readFile(args map[string]any) (Result, error) {
	rawPath := stringArg(args, "path", ".")
	absPath := ""
	displayPath := ""
	if strings.HasPrefix(rawPath, "skill://") {
		var err error
		absPath, displayPath, err = r.resolveSkillResource(rawPath)
		if err != nil {
			return nil, err
		}
	} else {
		p, err := r.ws.ResolveExisting(rawPath)
		if err != nil {
			return nil, err
		}
		absPath = p.Abs
		displayPath = p.Display
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, toolError("IS_DIRECTORY", "cannot read directory", "validation")
	}
	if info.Size() > maxTextFileReadBytes {
		return nil, toolErrorDetails(
			"FILE_TOO_LARGE",
			"text file exceeds the read_file input limit",
			"validation",
			map[string]any{"path": displayPath, "size_bytes": info.Size(), "max_size_bytes": maxTextFileReadBytes},
		)
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	if looksBinary(data) {
		return nil, toolError("BINARY_FILE", "binary file read blocked for text tool", "validation")
	}
	if !utf8.Valid(data) {
		return nil, toolError("UNSUPPORTED_ENCODING", "file is not valid utf-8", "validation")
	}
	maxBytes := boundedInt(intArg(args, "max_bytes", 262144), 262144, 1, maxTextOutputBytes)
	content, meta := sliceText(string(data), intArg(args, "start_line", 1), intArg(args, "end_line", 0), maxBytes)
	result := Result{"ok": true, "path": displayPath, "content": content, "encoding": "utf-8", "size_bytes": len(data), "truncated": meta.Truncated, "start_line": meta.Start, "end_line": meta.End, "total_lines": meta.Total}
	if meta.NextStartLine > 0 {
		result["next_start_line"] = meta.NextStartLine
	}
	if meta.TruncatedReason != "" {
		result["truncated_reason"] = meta.TruncatedReason
	}
	return result, nil
}

func (r *Runtime) listDir(ctx context.Context, args map[string]any) (Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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
	maxDepth := boundedInt(intArg(args, "max_depth", 1), 1, 1, 20)
	maxEntries := boundedInt(intArg(args, "max_entries", 200), 200, 1, 2000)
	includeIgnored := boolArg(args, "include_ignored", false)
	if recursive {
		return r.listDirRecursive(ctx, p, includeHidden, includeIgnored, maxDepth, maxEntries)
	}
	ignore := loadIgnoreMatcher(r.ws.Root())
	items := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !includeHidden && workspace.Hidden(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("inspect directory entry %s: %w", entry.Name(), err)
		}
		abs := filepath.Join(p.Abs, entry.Name())
		rel, err := r.ws.Relative(abs)
		if err != nil {
			return nil, fmt.Errorf("resolve directory entry %s: %w", abs, err)
		}
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

func (r *Runtime) listDirRecursive(ctx context.Context, root workspace.Path, includeHidden, includeIgnored bool, maxDepth, maxEntries int) (Result, error) {
	items := make([]map[string]any, 0)
	ignore := loadIgnoreMatcher(r.ws.Root())
	rootDepth := len(strings.Split(filepath.Clean(root.Abs), string(os.PathSeparator)))
	err := filepath.WalkDir(root.Abs, func(abs string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
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
		rel, err := r.ws.Relative(abs)
		if err != nil {
			return fmt.Errorf("resolve directory entry %s: %w", abs, err)
		}
		if !includeIgnored && (shouldSkipDir(entry.Name()) || ignore.Ignored(rel, entry.IsDir())) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect directory entry %s: %w", abs, err)
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

func (r *Runtime) listFiles(ctx context.Context, args map[string]any) (Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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
	maxResults := boundedInt(intArg(args, "max_results", 500), 500, 1, 5000)
	includeHidden := boolArg(args, "include_hidden", false)
	includeIgnored := boolArg(args, "include_ignored", false)
	ignore := loadIgnoreMatcher(r.ws.Root())
	files := make([]map[string]any, 0)
	err = filepath.WalkDir(p.Abs, func(abs string, d os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
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
		if err != nil {
			return fmt.Errorf("resolve listed file %s: %w", abs, err)
		}
		if !matchesAny(rel, patterns) || matchesAny(rel, excludePatterns) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("inspect listed file %s: %w", abs, err)
		}
		files = append(files, map[string]any{"path": rel, "type": "file", "size_bytes": info.Size(), "modified": info.ModTime().UTC().Format(time.RFC3339Nano)})
		if maxResults > 0 && len(files) >= maxResults {
			return filepath.SkipAll
		}
		return nil
	})
	return Result{"ok": err == nil, "path": p.Display, "files": files, "truncated": maxResults > 0 && len(files) >= maxResults}, err
}

func (r *Runtime) viewImage(ctx context.Context, args map[string]any) (Result, error) {
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
	returnMode, err := imageReturnMode(args, "return_mode")
	if err != nil {
		return nil, err
	}
	maxBytes := intArg(args, "max_bytes", 750000)
	maxWidth := intArg(args, "max_width", 1280)
	maxHeight := intArg(args, "max_height", 1280)
	format := stringArg(args, "format", "jpeg")
	quality := intArg(args, "quality", 72)
	crop := cropArg(args)
	autoResize := boolArg(args, "auto_resize", true)

	original := map[string]any{"size_bytes": len(data), "width": info.Width, "height": info.Height, "mime_type": info.MIME}
	prepared := data
	preparedInfo := info
	warnings := []string{}
	resized := false
	if crop != nil || autoResize || strings.TrimSpace(format) != "" || quality != 72 {
		var ok bool
		var prepOriginal map[string]any
		prepared, preparedInfo, prepOriginal, warnings, ok = prepareImageBytes(data, crop, maxBytes, maxWidth, maxHeight, format, quality)
		if prepOriginal != nil {
			if bytes, ok := prepOriginal["bytes"]; ok {
				prepOriginal["size_bytes"] = bytes
				delete(prepOriginal, "bytes")
			}
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
	image := imageMetadata(p.Display, preparedInfo, len(prepared))
	if needsPublicURL(returnMode) {
		published, err := r.publishImageBytes(ctx, prepared, p.Display, preparedInfo, intArg(args, "retention_seconds", 0))
		if err != nil {
			return nil, err
		}
		image = published
	}
	result := Result{"ok": true, "path": p.Display, "return_mode": returnMode, "image": image, "original": original, "resized": resized, "warnings": warnings}
	if err := attachInlineImage(result, prepared, preparedInfo.MIME, returnMode, args); err != nil {
		return nil, err
	}
	return result, nil
}

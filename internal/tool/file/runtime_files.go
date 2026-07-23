package file

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uvwt/agentdock/internal/workspace"
)

func (svc *Service) ReadFile(ctx context.Context, args map[string]any) (Result, error) {
	selection, err := selectFileRuntime(args)
	if err != nil {
		return nil, err
	}
	if selection.isWSL() {
		return svc.readFileWSL(ctx, args, selection)
	}

	rawPath := stringArg(args, "path", ".")
	absPath := ""
	displayPath := ""
	if strings.HasPrefix(rawPath, "skill://") {
		var err error
		absPath, displayPath, err = svc.resolveSkillResource(rawPath)
		if err != nil {
			return nil, err
		}
	} else {
		p, err := svc.ws.ResolveExisting(rawPath)
		if err != nil {
			return nil, err
		}
		absPath = p.Abs
		displayPath = p.Display
	}
	read, err := readBoundedFile(absPath, int64(maxTextFileReadBytes))
	if err != nil {
		return nil, err
	}
	if read.Info.IsDir() {
		return nil, toolError("IS_DIRECTORY", "cannot read directory", "validation")
	}
	if read.TooLarge {
		return nil, toolErrorDetails(
			"FILE_TOO_LARGE",
			"text file exceeds the read_file input limit",
			"validation",
			map[string]any{"path": displayPath, "size_bytes": read.Size, "max_size_bytes": maxTextFileReadBytes},
		)
	}
	data := read.Data
	if looksBinary(data) {
		return nil, toolError("BINARY_FILE", "binary file read blocked for text tool", "validation")
	}
	if !utf8.Valid(data) {
		return nil, toolError("ENCODING_UNSUPPORTED", "file is not valid utf-8", "validation")
	}
	maxBytes := boundedInt(intArg(args, "max_bytes", 262144), 262144, 1, maxTextOutputBytes)
	content, meta := sliceText(string(data), intArg(args, "start_line", 1), intArg(args, "end_line", 0), maxBytes)
	result := Result{"path": displayPath, "content": content, "encoding": "utf-8", "size_bytes": len(data), "truncated": meta.Truncated, "start_line": meta.Start, "end_line": meta.End, "total_lines": meta.Total}
	if meta.NextStartLine > 0 {
		result["next_start_line"] = meta.NextStartLine
	}
	if meta.TruncatedReason != "" {
		result["truncated_reason"] = meta.TruncatedReason
	}
	return addFileRuntimeResult(result, selection), nil
}

func (svc *Service) ListDir(ctx context.Context, args map[string]any) (Result, error) {
	selection, err := selectFileRuntime(args)
	if err != nil {
		return nil, err
	}
	if selection.isWSL() {
		return svc.listDirWSL(ctx, args, selection)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p, err := svc.ws.ResolveExisting(stringArg(args, "path", "."))
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
		result, err := svc.listDirRecursive(ctx, p, includeHidden, includeIgnored, maxDepth, maxEntries)
		return addFileRuntimeResult(result, selection), err
	}
	ignore := loadIgnoreMatcher(svc.ws.Root())
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
		rel, err := svc.ws.Relative(abs)
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
	return addFileRuntimeResult(Result{"path": p.Display, "entries": items, "truncated": maxEntries > 0 && len(items) >= maxEntries}, selection), nil
}

func (svc *Service) listDirRecursive(ctx context.Context, root workspace.Path, includeHidden, includeIgnored bool, maxDepth, maxEntries int) (Result, error) {
	items := make([]map[string]any, 0)
	ignore := loadIgnoreMatcher(svc.ws.Root())
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
		rel, err := svc.ws.Relative(abs)
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
	return Result{"path": root.Display, "entries": items, "recursive": true, "max_depth": maxDepth, "truncated": maxEntries > 0 && len(items) >= maxEntries}, err
}

func (svc *Service) ListFiles(ctx context.Context, args map[string]any) (Result, error) {
	selection, err := selectFileRuntime(args)
	if err != nil {
		return nil, err
	}
	if selection.isWSL() {
		return svc.listFilesWSL(ctx, args, selection)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p, err := svc.ws.ResolveExisting(stringArg(args, "path", "."))
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
	ignore := loadIgnoreMatcher(svc.ws.Root())
	files := make([]map[string]any, 0)
	err = filepath.WalkDir(p.Abs, func(abs string, d os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := svc.ws.Relative(abs)
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
		rel, err := svc.ws.Relative(abs)
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
	return addFileRuntimeResult(Result{"path": p.Display, "files": files, "truncated": maxResults > 0 && len(files) >= maxResults}, selection), err
}

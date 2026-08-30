package file

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uvwt/agentdock/internal/workspace"
)

func (svc *Service) ReadFile(ctx context.Context, request ReadRequest) (Result, error) {
	selection, err := selectFileRuntime(request.RuntimeOptions)
	if err != nil {
		return nil, err
	}
	if request.Path == "" {
		return nil, toolError("INVALID_ARGUMENT", "path is required", "validation")
	}
	if selection.isWSL() {
		return svc.readFileWSL(ctx, request, selection)
	}

	rawPath := request.Path
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
	maxBytes := boundedInt(intValue(request.MaxBytes, 262144), 262144, 1, maxTextOutputBytes)
	content, meta := sliceText(string(data), intValue(request.StartLine, 1), intValue(request.EndLine, 0), maxBytes)
	result := Result{"path": displayPath, "content": content, "encoding": "utf-8", "size_bytes": len(data), "truncated": meta.Truncated, "start_line": meta.Start, "end_line": meta.End, "total_lines": meta.Total}
	if meta.NextStartLine > 0 {
		result["next_start_line"] = meta.NextStartLine
	}
	if meta.TruncatedReason != "" {
		result["truncated_reason"] = meta.TruncatedReason
	}
	return addFileRuntimeResult(result, selection), nil
}

type listDirOptions struct {
	MaxDepth        int
	MaxEntries      int
	Patterns        []string
	ExcludePatterns []string
	EntryType       string
	IncludeHidden   bool
	IncludeIgnored  bool
}

func parseListDirOptions(request ListRequest) (listDirOptions, error) {
	patterns := append([]string(nil), request.Patterns...)
	if len(patterns) == 0 {
		patterns = []string{"**/*"}
	}
	entryType := request.EntryType
	if entryType == "" {
		entryType = "any"
	}
	switch entryType {
	case "any", "file", "directory":
	default:
		return listDirOptions{}, toolError("INVALID_ARGUMENT", "entry_type must be any, file, or directory", "validation")
	}
	return listDirOptions{
		MaxDepth:        boundedInt(intValue(request.MaxDepth, 1), 1, 1, 20),
		MaxEntries:      boundedInt(intValue(request.MaxEntries, 200), 200, 1, 5000),
		Patterns:        patterns,
		ExcludePatterns: append([]string(nil), request.ExcludePatterns...),
		EntryType:       entryType,
		IncludeHidden:   request.IncludeHidden,
		IncludeIgnored:  request.IncludeIgnored,
	}, nil
}

func (svc *Service) ListDir(ctx context.Context, request ListRequest) (Result, error) {
	selection, err := selectFileRuntime(request.RuntimeOptions)
	if err != nil {
		return nil, err
	}
	opts, err := parseListDirOptions(request)
	if err != nil {
		return nil, err
	}
	if selection.isWSL() {
		return svc.listDirWSL(ctx, request, selection, opts)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	path := request.Path
	if path == "" {
		path = "."
	}
	root, err := svc.ws.ResolveExisting(path)
	if err != nil {
		return nil, err
	}
	rootInfo, err := os.Stat(root.Abs)
	if err != nil {
		return nil, err
	}
	if !rootInfo.IsDir() {
		return nil, toolError("NOT_A_DIRECTORY", "list_dir path is not a directory", "validation")
	}

	ignore := loadIgnoreMatcher(svc.ws.Root())

	items := make([]map[string]any, 0, min(opts.MaxEntries, 200))
	skippedPaths := make([]string, 0)
	truncated := false
	walkErr := filepath.WalkDir(root.Abs, func(abs string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			if abs == root.Abs {
				return walkErr
			}
			if errors.Is(walkErr, os.ErrPermission) {
				if rel, relErr := relativePathFromRoot(root.Abs, abs); relErr == nil {
					skippedPaths = append(skippedPaths, rel)
				}
				if entry != nil && entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			return walkErr
		}
		if abs == root.Abs {
			return nil
		}

		rel, err := relativePathFromRoot(root.Abs, abs)
		if err != nil {
			return fmt.Errorf("resolve directory entry %s: %w", abs, err)
		}
		depth := strings.Count(rel, "/") + 1
		if depth > opts.MaxDepth {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		isDir := entry.IsDir()
		isHidden := hiddenRelativePath(rel)
		if !opts.IncludeHidden && isHidden {
			if isDir {
				return filepath.SkipDir
			}
			return nil
		}
		if !opts.IncludeIgnored {
			ignoreRel, relErr := svc.ws.Relative(abs)
			if relErr != nil {
				return fmt.Errorf("resolve ignored directory entry %s: %w", abs, relErr)
			}
			if shouldSkipDir(entry.Name()) || ignore.Ignored(ignoreRel, isDir) {
				if isDir {
					return filepath.SkipDir
				}
				return nil
			}
		}

		kind := "file"
		if isDir {
			kind = "directory"
		}
		includeEntry := (opts.EntryType == "any" || opts.EntryType == kind) && matchesAny(rel, opts.Patterns) && !matchesAny(rel, opts.ExcludePatterns)
		if includeEntry {
			// 只有真正看到第 max_entries+1 个匹配项时才标记截断，避免“刚好达到上限”被误报。
			if len(items) >= opts.MaxEntries {
				truncated = true
				return filepath.SkipAll
			}
			info, err := entry.Info()
			if err != nil {
				if errors.Is(err, os.ErrPermission) {
					skippedPaths = append(skippedPaths, rel)
					if isDir {
						return filepath.SkipDir
					}
					return nil
				}
				return fmt.Errorf("inspect directory entry %s: %w", abs, err)
			}
			items = append(items, map[string]any{
				"name":       entry.Name(),
				"path":       rel,
				"type":       kind,
				"size_bytes": info.Size(),
				"modified":   info.ModTime().UTC().Format(time.RFC3339Nano),
				"is_hidden":  isHidden,
			})
		}

		if isDir && depth >= opts.MaxDepth {
			return filepath.SkipDir
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	sort.Slice(items, func(i, j int) bool { return fmt.Sprint(items[i]["path"]) < fmt.Sprint(items[j]["path"]) })
	result := Result{
		"path":          root.Display,
		"entries":       items,
		"truncated":     truncated,
		"partial":       len(skippedPaths) > 0,
		"skipped_paths": skippedPaths,
	}
	return addFileRuntimeResult(result, selection), nil
}

func hiddenRelativePath(rel string) bool {
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if workspace.Hidden(part) {
			return true
		}
	}
	return false
}

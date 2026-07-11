package tools

import (
	"context"
	"os"
	"strings"

	"github.com/uvwt/agentdock/internal/atomicfile"
)

func (r *Runtime) fileEdit(ctx context.Context, args map[string]any) (Result, error) {
	action := strings.ToLower(stringArg(args, "action", ""))
	if action == "" {
		return nil, toolErrorDetails("MISSING_ACTION", "file_edit requires action", "validation", map[string]any{"allowed": []string{"replace", "patch", "add", "delete", "move"}})
	}
	switch action {
	case "patch":
		result, err := r.applyPatch(ctx, args)
		if result != nil {
			result["action"] = "patch"
		}
		return result, err
	case "replace":
		result, err := r.editFile(args)
		if result != nil {
			result["action"] = "replace"
		}
		return result, err
	case "add":
		return r.fileEditAdd(args)
	case "delete":
		return r.fileEditDelete(args)
	case "move":
		return r.fileEditMove(args)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported file_edit action", "validation", map[string]any{"action": action, "allowed": []string{"replace", "patch", "add", "delete", "move"}})
	}
}

func (r *Runtime) fileEditAdd(args map[string]any) (Result, error) {
	path := stringArg(args, "path", "")
	if path == "" {
		return nil, toolError("INVALID_ARGUMENT", "path is required", "validation")
	}
	content := stringArg(args, "content", "")
	dryRun := boolArg(args, "dry_run", false)
	overwrite := boolArg(args, "overwrite", false)
	maxDiffBytes := intArg(args, "max_diff_bytes", 65536)

	p, err := r.ws.ResolveForWrite(path)
	if err != nil {
		return nil, err
	}
	if p.Exists && !overwrite {
		return nil, toolErrorDetails("FILE_EXISTS", "file already exists; set overwrite=true to replace it", "validation", map[string]any{"path": p.Display})
	}
	oldContent := ""
	mode := os.FileMode(0o644)
	if p.Exists {
		info, err := os.Stat(p.Abs)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			return nil, toolErrorDetails("IS_DIRECTORY", "cannot overwrite a directory with text content", "validation", map[string]any{"path": p.Display})
		}
		data, err := os.ReadFile(p.Abs)
		if err != nil {
			return nil, err
		}
		oldContent = string(data)
		mode = info.Mode().Perm()
	}
	diffPreview, diffTruncated, stats, err := unifiedDiffPreview(p.Display, oldContent, content, maxDiffBytes)
	if err != nil {
		return nil, err
	}
	changed := oldContent != content
	result := Result{"ok": true, "action": "add", "path": p.Display, "dry_run": dryRun, "changed": changed, "diff_preview": diffPreview, "truncated": diffTruncated, "files_changed": stats.FilesChanged, "insertions": stats.Insertions, "deletions": stats.Deletions, "summary": editSummary(p.Display, changed)}
	if dryRun || !changed {
		return result, nil
	}
	if err := atomicfile.Write(p.Abs, []byte(content), mode); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *Runtime) fileEditDelete(args map[string]any) (Result, error) {
	path := stringArg(args, "path", "")
	if path == "" {
		return nil, toolError("INVALID_ARGUMENT", "path is required", "validation")
	}
	dryRun := boolArg(args, "dry_run", false)
	recursive := boolArg(args, "recursive", false)
	p, err := r.ws.ResolveExisting(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(p.Abs)
	if err != nil {
		return nil, err
	}
	if info.IsDir() && !recursive {
		return nil, toolErrorDetails("IS_DIRECTORY", "directory deletion requires recursive=true", "validation", map[string]any{"path": p.Display})
	}
	result := Result{"ok": true, "action": "delete", "path": p.Display, "dry_run": dryRun, "changed": true, "recursive": recursive, "summary": "deleted " + p.Display}
	if dryRun {
		return result, nil
	}
	if recursive {
		return result, os.RemoveAll(p.Abs)
	}
	return result, os.Remove(p.Abs)
}

func (r *Runtime) fileEditMove(args map[string]any) (Result, error) {
	path := stringArg(args, "path", "")
	newPath := stringArg(args, "new_path", "")
	if path == "" || newPath == "" {
		return nil, toolError("INVALID_ARGUMENT", "path and new_path are required", "validation")
	}
	dryRun := boolArg(args, "dry_run", false)
	overwrite := boolArg(args, "overwrite", false)
	src, err := r.ws.ResolveExisting(path)
	if err != nil {
		return nil, err
	}
	dest, err := r.ws.ResolveForWrite(newPath)
	if err != nil {
		return nil, err
	}
	if dest.Exists && !overwrite {
		return nil, toolErrorDetails("FILE_EXISTS", "destination already exists; set overwrite=true to replace it", "validation", map[string]any{"path": dest.Display})
	}
	if dest.Exists {
		info, err := os.Stat(dest.Abs)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			return nil, toolErrorDetails("IS_DIRECTORY", "cannot overwrite destination directory", "validation", map[string]any{"path": dest.Display})
		}
	}
	changed := src.Abs != dest.Abs
	result := Result{"ok": true, "action": "move", "path": src.Display, "new_path": dest.Display, "dry_run": dryRun, "changed": changed, "summary": "moved " + src.Display + " to " + dest.Display}
	if dryRun || !changed {
		return result, nil
	}
	if dest.Exists && overwrite {
		if err := os.Remove(dest.Abs); err != nil {
			return nil, err
		}
	}
	if err := os.Rename(src.Abs, dest.Abs); err != nil {
		return nil, err
	}
	return result, nil
}

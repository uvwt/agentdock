package tools

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/uvwt/agentdock/internal/atomicfile"
)

func (r *Runtime) fileEdit(ctx context.Context, args map[string]any) (Result, error) {
	selection, err := selectFileRuntime(args)
	if err != nil {
		return nil, err
	}
	if selection.isWSL() {
		return r.fileEditWSL(ctx, args, selection)
	}

	action := strings.ToLower(stringArg(args, "action", ""))
	if action == "" {
		return nil, toolErrorDetails("MISSING_ACTION", "file_edit requires action", "validation", map[string]any{"allowed": []string{"replace", "patch", "add", "delete", "move"}})
	}
	var result Result
	switch action {
	case "patch":
		result, err = r.applyPatch(ctx, args)
	case "replace":
		result, err = r.editFile(args)
	case "add":
		result, err = r.fileEditAdd(args)
	case "delete":
		result, err = r.fileEditDelete(args)
	case "move":
		result, err = r.fileEditMove(args)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported file_edit action", "validation", map[string]any{"action": action, "allowed": []string{"replace", "patch", "add", "delete", "move"}})
	}
	if result != nil {
		result["action"] = action
	}
	return addFileRuntimeResult(result, selection), err
}

func (r *Runtime) fileEditAdd(args map[string]any) (Result, error) {
	path := stringArg(args, "path", "")
	if path == "" {
		return nil, toolError("INVALID_ARGUMENT", "path is required", "validation")
	}
	content := stringArg(args, "content", "")
	dryRun := boolArg(args, "dry_run", false)
	overwrite := boolArg(args, "overwrite", false)

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
		if info.Size() > maxTextFileReadBytes {
			return nil, toolErrorDetails(
				"FILE_TOO_LARGE",
				"text file exceeds the file_edit input limit",
				"validation",
				map[string]any{"path": p.Display, "size_bytes": info.Size(), "max_size_bytes": maxTextFileReadBytes},
			)
		}
		data, err := os.ReadFile(p.Abs)
		if err != nil {
			return nil, err
		}
		if looksBinary(data) {
			return nil, toolErrorDetails("BINARY_FILE", "binary file edit blocked for text tool", "validation", map[string]any{"path": p.Display})
		}
		if !utf8.Valid(data) {
			return nil, toolErrorDetails("ENCODING_UNSUPPORTED", "file is not valid utf-8", "validation", map[string]any{"path": p.Display})
		}
		oldContent = string(data)
		mode = info.Mode().Perm()
	}
	result, err := prepareTextAddition(p.Display, oldContent, content, p.Exists, args)
	if err != nil {
		return nil, err
	}
	changed, _ := result["changed"].(bool)
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
	srcInfo, err := os.Stat(src.Abs)
	if err != nil {
		return nil, err
	}
	if srcInfo.IsDir() && pathIsDescendant(src.Abs, dest.Abs) {
		return nil, toolErrorDetails(
			"INVALID_MOVE_DESTINATION",
			"cannot move a directory into its own descendant",
			"validation",
			map[string]any{"path": src.Display, "new_path": dest.Display},
		)
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
	if err := movePathWithRollback(src.Abs, dest.Abs, dest.Exists && overwrite, os.Rename); err != nil {
		return nil, err
	}
	return result, nil
}

func pathIsDescendant(parent, candidate string) bool {
	rel, err := filepath.Rel(parent, candidate)
	if err != nil || rel == "." {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func movePathWithRollback(src, dest string, replace bool, rename func(string, string) error) error {
	if !replace {
		return rename(src, dest)
	}

	backupDir, err := os.MkdirTemp(filepath.Dir(dest), ".agentdock-move-backup-*")
	if err != nil {
		return fmt.Errorf("create move backup directory: %w", err)
	}
	backupPath := filepath.Join(backupDir, "payload")
	cleanupBackupDir := func() error {
		if err := os.RemoveAll(backupDir); err != nil {
			return fmt.Errorf("remove move backup directory: %w", err)
		}
		return nil
	}

	if err := rename(dest, backupPath); err != nil {
		cleanupErr := cleanupBackupDir()
		return errors.Join(fmt.Errorf("backup move destination: %w", err), cleanupErr)
	}
	if err := rename(src, dest); err != nil {
		if rollbackErr := rename(backupPath, dest); rollbackErr != nil {
			return errors.Join(
				fmt.Errorf("move source to destination: %w", err),
				fmt.Errorf("restore move destination from %s: %w", backupPath, rollbackErr),
			)
		}
		cleanupErr := cleanupBackupDir()
		return errors.Join(fmt.Errorf("move source to destination: %w", err), cleanupErr)
	}
	if err := cleanupBackupDir(); err != nil {
		slog.Warn("remove committed move backup failed", "path", backupDir, "error", err)
	}
	return nil
}

package file

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func (svc *Service) Edit(ctx context.Context, args map[string]any) (Result, error) {
	selection, err := selectFileRuntime(args)
	if err != nil {
		return nil, err
	}
	if selection.isWSL() {
		return svc.fileEditWSL(ctx, args, selection)
	}

	action := strings.ToLower(stringArg(args, "action", ""))
	if action == "" {
		return nil, toolErrorDetails("MISSING_ACTION", "file_edit requires action", "validation", map[string]any{"allowed": []string{"replace", "patch", "add", "delete", "move"}})
	}
	var result Result
	switch action {
	case "patch":
		result, err = svc.applyPatch(ctx, args)
	case "replace":
		result, err = svc.editFile(args)
	case "add":
		result, err = svc.fileEditAdd(args)
	case "delete":
		result, err = svc.fileEditDelete(args)
	case "move":
		result, err = svc.fileEditMove(args)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported file_edit action", "validation", map[string]any{"action": action, "allowed": []string{"replace", "patch", "add", "delete", "move"}})
	}
	if result != nil {
		result["action"] = action
	}
	return addFileRuntimeResult(result, selection), err
}

func (svc *Service) fileEditAdd(args map[string]any) (Result, error) {
	path := stringArg(args, "path", "")
	if path == "" {
		return nil, toolError("INVALID_ARGUMENT", "path is required", "validation")
	}
	content := stringArg(args, "content", "")
	dryRun := boolArg(args, "dry_run", false)
	overwrite := boolArg(args, "overwrite", false)

	p, err := svc.ws.ResolveForWrite(path)
	if err != nil {
		return nil, err
	}
	if p.Exists && !overwrite {
		return nil, toolErrorDetails("FILE_EXISTS", "file already exists; set overwrite=true to replace it", "validation", map[string]any{"path": p.Display})
	}
	oldContent := ""
	var original []byte
	mode := os.FileMode(0o644)
	if p.Exists {
		read, err := readBoundedFile(p.Abs, int64(maxTextFileReadBytes))
		if err != nil {
			return nil, err
		}
		if read.Info.IsDir() {
			return nil, toolErrorDetails("IS_DIRECTORY", "cannot overwrite a directory with text content", "validation", map[string]any{"path": p.Display})
		}
		if read.TooLarge {
			return nil, toolErrorDetails(
				"FILE_TOO_LARGE",
				"text file exceeds the file_edit input limit",
				"validation",
				map[string]any{"path": p.Display, "size_bytes": read.Size, "max_size_bytes": maxTextFileReadBytes},
			)
		}
		info := read.Info
		data := read.Data
		if looksBinary(data) {
			return nil, toolErrorDetails("BINARY_FILE", "binary file edit blocked for text tool", "validation", map[string]any{"path": p.Display})
		}
		if !utf8.Valid(data) {
			return nil, toolErrorDetails("ENCODING_UNSUPPORTED", "file is not valid utf-8", "validation", map[string]any{"path": p.Display})
		}
		oldContent = string(data)
		original = append([]byte(nil), data...)
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
	updated := content
	staged := map[string]stagedPatchFile{
		p.Abs: {
			Abs:            p.Abs,
			Display:        p.Display,
			Content:        &updated,
			Mode:           mode,
			Original:       original,
			OriginalExists: p.Exists,
		},
	}
	if err := commitStagedPatch(staged); err != nil {
		return nil, err
	}
	return result, nil
}

func (svc *Service) fileEditDelete(args map[string]any) (Result, error) {
	path := stringArg(args, "path", "")
	if path == "" {
		return nil, toolError("INVALID_ARGUMENT", "path is required", "validation")
	}
	dryRun := boolArg(args, "dry_run", false)
	recursive := boolArg(args, "recursive", false)
	p, err := svc.ws.ResolveExisting(path)
	if err != nil {
		return nil, err
	}
	snapshot, err := captureFileSnapshot(p.Abs)
	if err != nil {
		return nil, err
	}
	if snapshot.Info.IsDir() && !recursive {
		return nil, toolErrorDetails("IS_DIRECTORY", "directory deletion requires recursive=true", "validation", map[string]any{"path": p.Display})
	}
	result := Result{"action": "delete", "path": p.Display, "dry_run": dryRun, "changed": true, "recursive": recursive, "summary": "deleted " + p.Display}
	if dryRun {
		return result, nil
	}
	return result, deletePathSafely(p.Abs, snapshot, recursive, os.Rename, renameNoReplace)
}

func deletePathSafely(path string, expected fileSnapshot, recursive bool, rename, restoreNoReplace func(string, string) error) error {
	backupDir, err := os.MkdirTemp(filepath.Dir(path), ".agentdock-delete-backup-*")
	if err != nil {
		return fmt.Errorf("create delete backup directory: %w", err)
	}
	backupPath := filepath.Join(backupDir, "payload")
	cleanup := func() error {
		if err := os.RemoveAll(backupDir); err != nil {
			return fmt.Errorf("remove delete backup directory: %w", err)
		}
		return nil
	}
	if err := rename(path, backupPath); err != nil {
		return errors.Join(fmt.Errorf("stage delete target: %w", err), cleanup())
	}
	moved, err := captureFileSnapshot(backupPath)
	if err != nil {
		return fmt.Errorf("inspect staged delete target at %s: %w", backupPath, err)
	}
	if !sameFileSnapshot(expected, moved) {
		restoreErr := restoreNoReplace(backupPath, path)
		if restoreErr == nil {
			return errors.Join(
				toolErrorDetails("FILE_CHANGED", "delete target changed before commit", "runtime", map[string]any{"path": path}),
				cleanup(),
			)
		}
		return errors.Join(
			toolErrorDetails("FILE_CHANGED", "delete target changed before commit", "runtime", map[string]any{"path": path, "backup_path": backupPath}),
			fmt.Errorf("restore changed delete target: %w", restoreErr),
		)
	}
	if recursive || moved.Info.IsDir() {
		err = os.RemoveAll(backupPath)
	} else {
		err = os.Remove(backupPath)
	}
	if err != nil {
		return fmt.Errorf("remove staged delete target at %s: %w", backupPath, err)
	}
	return cleanup()
}

func (svc *Service) fileEditMove(args map[string]any) (Result, error) {
	path := stringArg(args, "path", "")
	newPath := stringArg(args, "new_path", "")
	if path == "" || newPath == "" {
		return nil, toolError("INVALID_ARGUMENT", "path and new_path are required", "validation")
	}
	dryRun := boolArg(args, "dry_run", false)
	overwrite := boolArg(args, "overwrite", false)
	src, err := svc.ws.ResolveExisting(path)
	if err != nil {
		return nil, err
	}
	dest, err := svc.ws.ResolveForWrite(newPath)
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
	result := Result{"action": "move", "path": src.Display, "new_path": dest.Display, "dry_run": dryRun, "changed": changed, "summary": "moved " + src.Display + " to " + dest.Display}
	if dryRun || !changed {
		return result, nil
	}
	if err := movePathWithRollback(src.Abs, dest.Abs, dest.Exists && overwrite, os.Rename, renameNoReplace); err != nil {
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

func verifyMovedSource(src, dest, backupPath string, expected fileSnapshot, installNoReplace func(string, string) error, cleanup func() error) error {
	moved, err := captureFileSnapshot(dest)
	if err == nil && sameFileSnapshot(expected, moved) {
		return nil
	}
	conflict := toolErrorDetails(
		"FILE_CHANGED",
		"move source changed before commit",
		"runtime",
		map[string]any{"path": src, "new_path": dest, "backup_path": backupPath},
	)
	if err != nil {
		conflict.Details["reason"] = err.Error()
	}
	if restoreErr := installNoReplace(dest, src); restoreErr != nil {
		return errors.Join(conflict, fmt.Errorf("restore changed move source: %w", restoreErr))
	}
	if backupPath != "" {
		if restoreErr := installNoReplace(backupPath, dest); restoreErr != nil {
			return errors.Join(conflict, fmt.Errorf("restore move destination: %w", restoreErr))
		}
	}
	if cleanup != nil {
		return errors.Join(conflict, cleanup())
	}
	return conflict
}

func movePathWithRollback(src, dest string, replace bool, rename, installNoReplace func(string, string) error) error {
	expectedSource, err := captureFileSnapshot(src)
	if err != nil {
		return fmt.Errorf("inspect move source: %w", err)
	}
	if !replace {
		if err := installNoReplace(src, dest); err != nil {
			return err
		}
		return verifyMovedSource(src, dest, "", expectedSource, installNoReplace, nil)
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
	if err := installNoReplace(src, dest); err != nil {
		if rollbackErr := installNoReplace(backupPath, dest); rollbackErr != nil {
			return errors.Join(
				fmt.Errorf("move source to destination: %w", err),
				fmt.Errorf("restore move destination from %s: %w", backupPath, rollbackErr),
			)
		}
		cleanupErr := cleanupBackupDir()
		return errors.Join(fmt.Errorf("move source to destination: %w", err), cleanupErr)
	}
	if err := verifyMovedSource(src, dest, backupPath, expectedSource, installNoReplace, cleanupBackupDir); err != nil {
		return err
	}
	if err := cleanupBackupDir(); err != nil {
		slog.Warn("remove committed move backup failed", "path", backupDir, "error", err)
	}
	return nil
}

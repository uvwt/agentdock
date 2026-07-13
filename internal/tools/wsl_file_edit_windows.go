//go:build windows

package tools

import (
	"context"
	pathpkg "path"
	"sort"
	"strings"
)

func (r *Runtime) fileEditWSL(ctx context.Context, args map[string]any, selection fileRuntimeSelection) (Result, error) {
	action := strings.ToLower(strings.TrimSpace(stringArg(args, "action", "")))
	if action == "" {
		return nil, toolErrorDetails("MISSING_ACTION", "file_edit requires action", "validation", map[string]any{"allowed": []string{"replace", "patch", "add", "delete", "move"}})
	}
	switch action {
	case "replace":
		return r.fileEditReplaceWSL(ctx, args, selection)
	case "add":
		return r.fileEditAddWSL(ctx, args, selection)
	case "delete":
		return r.fileEditDeleteWSL(ctx, args, selection)
	case "move":
		return r.fileEditMoveWSL(ctx, args, selection)
	case "patch":
		return r.fileEditPatchWSL(ctx, args, selection)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported file_edit action", "validation", map[string]any{"action": action, "allowed": []string{"replace", "patch", "add", "delete", "move"}})
	}
}

func (r *Runtime) fileEditReplaceWSL(ctx context.Context, args map[string]any, selection fileRuntimeSelection) (Result, error) {
	path, err := resolveWSLFilePath(stringArg(args, "path", ""))
	if err != nil {
		return nil, err
	}
	loaded, err := r.callWSLFileHelper(ctx, selection, map[string]any{"action": "read", "path": path, "reject_symlink": true})
	if err != nil {
		return nil, err
	}
	content, _ := loaded["content"].(string)
	result, updated, err := prepareTextReplacement(path, content, args)
	if err != nil {
		return nil, err
	}
	result["action"] = "replace"
	if !boolArg(args, "dry_run", false) && updated != content {
		if _, err := r.callWSLFileHelper(ctx, selection, map[string]any{"action": "write_atomic", "path": path, "content": updated, "must_exist": true, "overwrite": true}); err != nil {
			return nil, err
		}
	}
	return addFileRuntimeResult(result, selection), nil
}

func (r *Runtime) fileEditAddWSL(ctx context.Context, args map[string]any, selection fileRuntimeSelection) (Result, error) {
	path, err := resolveWSLFilePath(stringArg(args, "path", ""))
	if err != nil {
		return nil, err
	}
	overwrite := boolArg(args, "overwrite", false)
	loaded, err := r.callWSLFileHelper(ctx, selection, map[string]any{"action": "read", "path": path, "reject_symlink": true, "allow_missing": true})
	if err != nil {
		return nil, err
	}
	exists, _ := loaded["exists"].(bool)
	if exists && !overwrite {
		return nil, toolErrorDetails("FILE_EXISTS", "file already exists; set overwrite=true to replace it", "validation", map[string]any{"path": path})
	}
	oldContent, _ := loaded["content"].(string)
	content := stringArg(args, "content", "")
	result, err := prepareTextAddition(path, oldContent, content, exists, args)
	if err != nil {
		return nil, err
	}
	changed, _ := result["changed"].(bool)
	if !boolArg(args, "dry_run", false) && changed {
		if _, err := r.callWSLFileHelper(ctx, selection, map[string]any{"action": "write_atomic", "path": path, "content": content, "overwrite": overwrite}); err != nil {
			return nil, err
		}
	}
	return addFileRuntimeResult(result, selection), nil
}

func (r *Runtime) fileEditDeleteWSL(ctx context.Context, args map[string]any, selection fileRuntimeSelection) (Result, error) {
	path, err := resolveWSLFilePath(stringArg(args, "path", ""))
	if err != nil {
		return nil, err
	}
	if boolArg(args, "recursive", false) {
		return nil, toolError("INVALID_ARGUMENT", "runtime=wsl file_edit only deletes regular UTF-8 files; recursive directory deletion is not supported", "validation")
	}
	if _, err := r.callWSLFileHelper(ctx, selection, map[string]any{"action": "read", "path": path, "reject_symlink": true}); err != nil {
		return nil, err
	}
	dryRun := boolArg(args, "dry_run", false)
	result := Result{"ok": true, "action": "delete", "path": path, "dry_run": dryRun, "changed": true, "summary": "deleted " + path}
	if !dryRun {
		if _, err := r.callWSLFileHelper(ctx, selection, map[string]any{"action": "delete", "path": path}); err != nil {
			return nil, err
		}
	}
	return addFileRuntimeResult(result, selection), nil
}

func (r *Runtime) fileEditMoveWSL(ctx context.Context, args map[string]any, selection fileRuntimeSelection) (Result, error) {
	path, err := resolveWSLFilePath(stringArg(args, "path", ""))
	if err != nil {
		return nil, err
	}
	newPath, err := resolveWSLFilePath(stringArg(args, "new_path", ""))
	if err != nil {
		return nil, err
	}
	if _, err := r.callWSLFileHelper(ctx, selection, map[string]any{"action": "read", "path": path, "reject_symlink": true}); err != nil {
		return nil, err
	}
	overwrite := boolArg(args, "overwrite", false)
	destination, err := r.callWSLFileHelper(ctx, selection, map[string]any{"action": "read", "path": newPath, "reject_symlink": true, "allow_missing": true})
	if err != nil {
		return nil, err
	}
	if exists, _ := destination["exists"].(bool); exists && !overwrite {
		return nil, toolErrorDetails("FILE_EXISTS", "destination already exists; set overwrite=true to replace it", "validation", map[string]any{"path": newPath})
	}
	changed := path != newPath
	dryRun := boolArg(args, "dry_run", false)
	result := Result{"ok": true, "action": "move", "path": path, "new_path": newPath, "dry_run": dryRun, "changed": changed, "summary": "moved " + path + " to " + newPath}
	if !dryRun && changed {
		if _, err := r.callWSLFileHelper(ctx, selection, map[string]any{"action": "move", "path": path, "new_path": newPath, "overwrite": overwrite}); err != nil {
			return nil, err
		}
	}
	return addFileRuntimeResult(result, selection), nil
}

type wslPatchStage struct {
	OldContent    string
	NewContent    *string
	Mode          int
	OwnerUID      int
	OwnerGID      int
	PreserveOwner bool
	Existed       bool
}

func resolveWSLPatchPath(basePath, rawPath string) (string, error) {
	clean := pathpkg.Clean(strings.TrimSpace(rawPath))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", toolErrorDetails("PATCH_FAILED", "WSL patch paths must stay relative to workdir", "validation", map[string]any{"path": rawPath})
	}
	resolved := pathpkg.Join(basePath, clean)
	if resolved != basePath && !strings.HasPrefix(resolved, strings.TrimSuffix(basePath, "/")+"/") {
		return "", toolErrorDetails("PATCH_FAILED", "WSL patch path escaped workdir", "validation", map[string]any{"path": rawPath})
	}
	return resolved, nil
}

func (r *Runtime) loadWSLPatchStage(ctx context.Context, selection fileRuntimeSelection, path string, allowMissing bool) (*wslPatchStage, error) {
	loaded, err := r.callWSLFileHelper(ctx, selection, map[string]any{"action": "read", "path": path, "reject_symlink": true, "allow_missing": allowMissing})
	if err != nil {
		return nil, err
	}
	exists, _ := loaded["exists"].(bool)
	content, _ := loaded["content"].(string)
	mode := resultInt(loaded, "mode")
	if mode == 0 {
		mode = 0o644
	}
	copyContent := content
	return &wslPatchStage{
		OldContent: content, NewContent: &copyContent, Mode: mode,
		OwnerUID: resultInt(loaded, "uid"), OwnerGID: resultInt(loaded, "gid"), PreserveOwner: exists, Existed: exists,
	}, nil
}

func (r *Runtime) fileEditPatchWSL(ctx context.Context, args map[string]any, selection fileRuntimeSelection) (Result, error) {
	patch := stringArg(args, "patch", "")
	if patch == "" {
		return nil, toolError("INVALID_ARGUMENT", "patch is required", "validation")
	}
	if !strings.HasPrefix(strings.TrimSpace(patch), "*** Begin Patch") {
		return nil, toolError(
			"WSL_PATCH_FORMAT_REQUIRED",
			"runtime=wsl file_edit patch requires the structured *** Begin Patch envelope so every target can be safety-checked",
			"validation",
		)
	}
	workdir, err := resolveWSLFilePath(stringArg(args, "workdir", ""))
	if err != nil {
		return nil, err
	}
	operations, err := parseEnvelopePatch(patch)
	if err != nil {
		return nil, err
	}
	if err := validateWSLPatchOperations(operations); err != nil {
		return nil, err
	}
	staged := map[string]*wslPatchStage{}
	affected := make([]map[string]any, 0, len(operations))
	summaries := make([]string, 0, len(operations))

	for _, operation := range operations {
		sourcePath, err := resolveWSLPatchPath(workdir, operation.Path)
		if err != nil {
			return nil, err
		}
		switch operation.Kind {
		case "add":
			stage, err := r.loadWSLPatchStage(ctx, selection, sourcePath, true)
			if err != nil {
				return nil, err
			}
			if stage.Existed {
				return nil, toolErrorDetails("PATCH_FAILED", "cannot add file that already exists", "validation", map[string]any{"path": sourcePath})
			}
			content := operation.AddContent
			stage.NewContent = &content
			staged[sourcePath] = stage
			affected = append(affected, map[string]any{"path": sourcePath, "operation": "add"})
			summaries = append(summaries, "A "+sourcePath)
		case "delete":
			stage, err := r.loadWSLPatchStage(ctx, selection, sourcePath, false)
			if err != nil {
				return nil, err
			}
			stage.NewContent = nil
			staged[sourcePath] = stage
			affected = append(affected, map[string]any{"path": sourcePath, "operation": "delete"})
			summaries = append(summaries, "D "+sourcePath)
		case "update":
			stage := staged[sourcePath]
			if stage == nil {
				stage, err = r.loadWSLPatchStage(ctx, selection, sourcePath, false)
				if err != nil {
					return nil, err
				}
			}
			if stage.NewContent == nil {
				return nil, toolErrorDetails("PATCH_FAILED", "cannot update a deleted file", "validation", map[string]any{"path": sourcePath})
			}
			updated, err := applyUpdateHunks(*stage.NewContent, operation.Hunks, sourcePath)
			if err != nil {
				return nil, err
			}
			if operation.MoveTo == "" {
				stage.NewContent = &updated
				staged[sourcePath] = stage
				affected = append(affected, map[string]any{"path": sourcePath, "operation": "update"})
				summaries = append(summaries, "M "+sourcePath)
				continue
			}
			destinationPath, err := resolveWSLPatchPath(workdir, operation.MoveTo)
			if err != nil {
				return nil, err
			}
			destination, err := r.loadWSLPatchStage(ctx, selection, destinationPath, true)
			if err != nil {
				return nil, err
			}
			if destination.Existed && destinationPath != sourcePath {
				return nil, toolErrorDetails("PATCH_FAILED", "cannot move over an existing file", "validation", map[string]any{"path": destinationPath})
			}
			stage.NewContent = nil
			staged[sourcePath] = stage
			destination.OldContent = ""
			destination.NewContent = &updated
			destination.Mode = stage.Mode
			destination.OwnerUID = stage.OwnerUID
			destination.OwnerGID = stage.OwnerGID
			destination.PreserveOwner = stage.PreserveOwner
			staged[destinationPath] = destination
			affected = append(affected, map[string]any{"path": sourcePath, "operation": "move", "move_to": destinationPath})
			summaries = append(summaries, "R "+sourcePath+" -> "+destinationPath)
		}
	}
	if len(staged) == 0 {
		return nil, toolError("PATCH_FAILED", "no files were modified", "validation")
	}

	maxDiffBytes := boundedInt(intArg(args, "max_diff_bytes", 65536), 65536, 1, maxTextOutputBytes)
	paths := make([]string, 0, len(staged))
	for path := range staged {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var diffBuilder strings.Builder
	totalStats := diffStats{}
	for _, path := range paths {
		stage := staged[path]
		newContent := ""
		if stage.NewContent != nil {
			newContent = *stage.NewContent
		}
		diff, _, stats, err := unifiedDiffPreview(path, stage.OldContent, newContent, 0)
		if err != nil {
			return nil, err
		}
		diffBuilder.WriteString(diff)
		if diff != "" && !strings.HasSuffix(diff, "\n") {
			diffBuilder.WriteByte('\n')
		}
		if !stage.Existed && stage.NewContent != nil && stats.FilesChanged == 0 {
			stats.FilesChanged = 1
		}
		if stats.FilesChanged > 0 {
			totalStats.FilesChanged++
		}
		totalStats.Insertions += stats.Insertions
		totalStats.Deletions += stats.Deletions
	}
	preview := diffBuilder.String()
	previewResult := truncateString(preview, maxDiffBytes)
	truncated := len(previewResult) < len(preview)
	dryRun := boolArg(args, "dry_run", false)
	if !dryRun {
		// 先完整写入所有新增/更新目标，再删除旧路径。失败时最多留下重复文件，不会丢失原内容。
		for _, path := range paths {
			stage := staged[path]
			if stage.NewContent == nil {
				continue
			}
			writeRequest := map[string]any{
				"action": "write_atomic", "path": path, "content": *stage.NewContent,
				"must_exist": stage.Existed, "overwrite": stage.Existed, "mode": stage.Mode,
			}
			if !stage.Existed && stage.PreserveOwner {
				writeRequest["owner_uid"] = stage.OwnerUID
				writeRequest["owner_gid"] = stage.OwnerGID
			}
			if _, err := r.callWSLFileHelper(ctx, selection, writeRequest); err != nil {
				return nil, err
			}
		}
		for _, path := range paths {
			stage := staged[path]
			if stage.NewContent != nil {
				continue
			}
			if _, err := r.callWSLFileHelper(ctx, selection, map[string]any{"action": "delete", "path": path}); err != nil {
				return nil, err
			}
		}
	}
	result := Result{
		"ok": true, "action": "patch", "dry_run": dryRun, "workdir": workdir,
		"affected_files": affected, "summary": strings.Join(summaries, "\n"),
		"diff_preview": previewResult, "truncated": truncated,
		"files_changed": totalStats.FilesChanged, "insertions": totalStats.Insertions, "deletions": totalStats.Deletions,
	}
	return addFileRuntimeResult(result, selection), nil
}

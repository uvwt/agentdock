package tools

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	workspacepkg "github.com/uvwt/agentdock/internal/workspace"
)

type patchOperation struct {
	Kind       string
	Path       string
	AddContent string
	Hunks      [][]string
	MoveTo     string
}

type stagedPatchFile struct {
	Abs            string
	Display        string
	Content        *string
	Mode           os.FileMode
	Original       []byte
	OriginalExists bool
}

type preparedPatchFile struct {
	file       stagedPatchFile
	tempPath   string
	backupPath string
	installed  bool
}

func patchPathInBase(basePath, rawPath string) (string, error) {
	cleanRaw, err := workspacepkg.Clean(rawPath)
	if err != nil {
		return "", err
	}
	cleanBase, err := workspacepkg.Clean(basePath)
	if err != nil {
		return "", err
	}
	if cleanBase == "." {
		return cleanRaw, nil
	}
	return filepath.ToSlash(filepath.Join(filepath.FromSlash(cleanBase), filepath.FromSlash(cleanRaw))), nil
}

func (r *Runtime) applyEnvelopePatch(patch string, dryRun bool, basePath string) (Result, error) {
	operations, err := parseEnvelopePatch(patch)
	if err != nil {
		return nil, err
	}
	staged := map[string]stagedPatchFile{}
	affected := make([]map[string]any, 0)
	summaries := make([]string, 0)

	for _, op := range operations {
		switch op.Kind {
		case "add":
			targetPath, err := patchPathInBase(basePath, op.Path)
			if err != nil {
				return nil, err
			}
			target, err := r.ws.ResolveForWrite(targetPath)
			if err != nil {
				return nil, err
			}
			if target.Exists {
				return nil, toolError("PATCH_FAILED", "cannot add file that already exists", "validation")
			}
			content := op.AddContent
			staged[target.Abs] = stagedPatchFile{Abs: target.Abs, Display: target.Display, Content: &content, Mode: 0o644}
			affected = append(affected, map[string]any{"path": target.Display, "operation": "add"})
			summaries = append(summaries, "A "+target.Display)
		case "delete":
			targetPath, err := patchPathInBase(basePath, op.Path)
			if err != nil {
				return nil, err
			}
			target, err := r.ws.ResolveExisting(targetPath)
			if err != nil {
				return nil, err
			}
			info, original, err := readPatchFile(target.Abs)
			if err != nil {
				return nil, err
			}
			staged[target.Abs] = stagedPatchFile{Abs: target.Abs, Display: target.Display, Mode: info.Mode().Perm(), Original: original, OriginalExists: true}
			affected = append(affected, map[string]any{"path": target.Display, "operation": "delete"})
			summaries = append(summaries, "D "+target.Display)
		case "update":
			sourcePath, err := patchPathInBase(basePath, op.Path)
			if err != nil {
				return nil, err
			}
			source, err := r.ws.ResolveExisting(sourcePath)
			if err != nil {
				return nil, err
			}
			current, exists := staged[source.Abs]
			if exists && current.Content == nil {
				return nil, toolError("PATCH_FAILED", "cannot update a deleted file", "validation")
			}
			if !exists {
				info, original, err := readPatchFile(source.Abs)
				if err != nil {
					return nil, err
				}
				content := string(original)
				current = stagedPatchFile{Abs: source.Abs, Display: source.Display, Content: &content, Mode: info.Mode().Perm(), Original: original, OriginalExists: true}
			}
			updated, err := applyUpdateHunks(*current.Content, op.Hunks, source.Display)
			if err != nil {
				return nil, err
			}
			if op.MoveTo == "" {
				current.Content = &updated
				staged[source.Abs] = current
				affected = append(affected, map[string]any{"path": source.Display, "operation": "update"})
				summaries = append(summaries, "M "+source.Display)
				continue
			}

			destPath, err := patchPathInBase(basePath, op.MoveTo)
			if err != nil {
				return nil, err
			}
			dest, err := r.ws.ResolveForWrite(destPath)
			if err != nil {
				return nil, err
			}
			if dest.Abs == source.Abs {
				current.Content = &updated
				staged[source.Abs] = current
				affected = append(affected, map[string]any{"path": source.Display, "operation": "update"})
				summaries = append(summaries, "M "+source.Display)
				continue
			}
			if dest.Exists {
				return nil, toolError("PATCH_FAILED", "cannot move over an existing file", "validation")
			}
			staged[source.Abs] = stagedPatchFile{Abs: source.Abs, Display: source.Display, Mode: current.Mode, Original: current.Original, OriginalExists: true}
			staged[dest.Abs] = stagedPatchFile{Abs: dest.Abs, Display: dest.Display, Content: &updated, Mode: current.Mode}
			affected = append(affected, map[string]any{"path": source.Display, "operation": "move", "move_to": dest.Display})
			summaries = append(summaries, "R "+source.Display+" -> "+dest.Display)
		}
	}
	if len(affected) == 0 {
		return nil, toolError("PATCH_FAILED", "no files were modified", "validation")
	}
	diffPreview, diffTruncated, stats, err := stagedDiffPreview(staged, 65536)
	if err != nil {
		return nil, err
	}
	if !dryRun {
		if err := commitStagedPatch(staged); err != nil {
			return nil, err
		}
	}
	return Result{"ok": true, "dry_run": dryRun, "workdir": basePath, "affected_files": affected, "summary": strings.Join(summaries, "\n"), "diff_preview": diffPreview, "truncated": diffTruncated, "files_changed": stats.FilesChanged, "insertions": stats.Insertions, "deletions": stats.Deletions}, nil
}

func readPatchFile(path string) (os.FileInfo, []byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, err
	}
	if info.IsDir() {
		return nil, nil, toolError("PATCH_FAILED", "cannot patch a directory", "validation")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	return info, content, nil
}

func parseEnvelopePatch(patch string) ([]patchOperation, error) {
	lines := strings.Split(strings.ReplaceAll(patch, "\r\n", "\n"), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "*** Begin Patch" || strings.TrimSpace(lines[len(lines)-1]) != "*** End Patch" {
		return nil, toolError("PATCH_FAILED", "patch must use begin/end envelope", "validation")
	}
	operations := make([]patchOperation, 0)
	for i := 1; i < len(lines)-1; {
		line := lines[i]
		if line == "" {
			i++
			continue
		}
		if strings.HasPrefix(line, "*** Add File: ") {
			path := strings.TrimSpace(strings.TrimPrefix(line, "*** Add File: "))
			i++
			content := make([]string, 0)
			for i < len(lines)-1 && !strings.HasPrefix(lines[i], "*** ") {
				if !strings.HasPrefix(lines[i], "+") {
					return nil, toolError("PATCH_FAILED", "add file lines must start with '+'", "validation")
				}
				content = append(content, strings.TrimPrefix(lines[i], "+"))
				i++
			}
			operations = append(operations, patchOperation{Kind: "add", Path: path, AddContent: strings.Join(content, "\n") + "\n"})
			continue
		}
		if strings.HasPrefix(line, "*** Delete File: ") {
			operations = append(operations, patchOperation{Kind: "delete", Path: strings.TrimSpace(strings.TrimPrefix(line, "*** Delete File: "))})
			i++
			continue
		}
		if strings.HasPrefix(line, "*** Update File: ") {
			path := strings.TrimSpace(strings.TrimPrefix(line, "*** Update File: "))
			i++
			moveTo := ""
			if i < len(lines)-1 && strings.HasPrefix(lines[i], "*** Move to: ") {
				moveTo = strings.TrimSpace(strings.TrimPrefix(lines[i], "*** Move to: "))
				i++
			}
			hunks := make([][]string, 0)
			current := make([]string, 0)
			for i < len(lines)-1 && !strings.HasPrefix(lines[i], "*** ") {
				if strings.HasPrefix(lines[i], "@@") {
					if len(current) > 0 {
						hunks = append(hunks, current)
					}
					current = make([]string, 0)
				} else {
					current = append(current, lines[i])
				}
				i++
			}
			if len(current) > 0 {
				hunks = append(hunks, current)
			}
			operations = append(operations, patchOperation{Kind: "update", Path: path, Hunks: hunks, MoveTo: moveTo})
			continue
		}
		return nil, toolErrorDetails("PATCH_FAILED", "unrecognized patch line", "validation", map[string]any{"line": line})
	}
	return operations, nil
}

func applyUpdateHunks(content string, hunks [][]string, path string) (string, error) {
	if len(hunks) == 0 {
		return content, nil
	}
	hasBOM := strings.HasPrefix(content, "\ufeff")
	if hasBOM {
		content = strings.TrimPrefix(content, "\ufeff")
	}
	lineEnding := "\n"
	if strings.Contains(content, "\r\n") {
		lineEnding = "\r\n"
	}
	lines := strings.Split(strings.TrimSuffix(strings.ReplaceAll(content, "\r\n", "\n"), "\n"), "\n")
	if content == "" {
		lines = []string{}
	}
	trailing := strings.HasSuffix(content, "\n")
	for hunkIndex, hunk := range hunks {
		oldLines, newLines, err := parseUpdateHunk(hunk)
		if err != nil {
			return "", err
		}
		idxs := findAllSubsequences(lines, oldLines)
		if len(idxs) == 0 {
			return "", toolErrorDetails("PATCH_FAILED", "patch context did not match", "validation", map[string]any{"path": path, "diagnostic": map[string]any{"code": "CONTEXT_NOT_FOUND", "path": path, "hunk_index": hunkIndex, "message": "patch context did not match", "nearby_context": patchNearbyContext(lines, oldLines)}})
		}
		if len(idxs) > 1 {
			return "", toolErrorDetails("PATCH_FAILED", "patch context matched multiple locations", "validation", map[string]any{"path": path, "matches": len(idxs), "diagnostic": map[string]any{"code": "AMBIGUOUS_CONTEXT", "path": path, "hunk_index": hunkIndex, "message": "patch context matched multiple locations", "nearby_context": patchContextsForMatches(lines, idxs)}})
		}
		idx := idxs[0]
		updated := make([]string, 0, len(lines)-len(oldLines)+len(newLines))
		updated = append(updated, lines[:idx]...)
		updated = append(updated, newLines...)
		updated = append(updated, lines[idx+len(oldLines):]...)
		lines = updated
	}
	result := strings.Join(lines, lineEnding)
	if trailing || len(lines) > 0 {
		result += lineEnding
	}
	if hasBOM {
		result = "\ufeff" + result
	}
	return result, nil
}

func parseUpdateHunk(hunk []string) ([]string, []string, error) {
	oldLines := make([]string, 0)
	newLines := make([]string, 0)
	for _, raw := range hunk {
		if raw == "*** End of File" {
			continue
		}
		if raw == "" {
			return nil, nil, toolError("PATCH_FAILED", "invalid empty patch line", "validation")
		}
		marker := raw[0]
		value := raw[1:]
		switch marker {
		case ' ':
			oldLines = append(oldLines, value)
			newLines = append(newLines, value)
		case '-':
			oldLines = append(oldLines, value)
		case '+':
			newLines = append(newLines, value)
		default:
			return nil, nil, toolError("PATCH_FAILED", "update lines must start with space, '-' or '+'", "validation")
		}
	}
	return oldLines, newLines, nil
}

func findAllSubsequences(lines, needle []string) []int {
	if len(needle) == 0 {
		return []int{0}
	}
	limit := len(lines) - len(needle) + 1
	matches := make([]int, 0)
	for i := 0; i < limit; i++ {
		ok := true
		for j := range needle {
			if lines[i+j] != needle[j] {
				ok = false
				break
			}
		}
		if ok {
			matches = append(matches, i)
		}
	}
	return matches
}

func commitStagedPatch(staged map[string]stagedPatchFile) error {
	return commitStagedPatchWithFileOps(staged, os.Rename, os.Link)
}

func commitStagedPatchWithRename(staged map[string]stagedPatchFile, rename func(string, string) error) error {
	return commitStagedPatchWithFileOps(staged, rename, os.Link)
}

func commitStagedPatchWithFileOps(staged map[string]stagedPatchFile, rename, link func(string, string) error) error {
	paths := make([]string, 0, len(staged))
	for path := range staged {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	prepared := make([]preparedPatchFile, 0, len(paths))
	createdDirs := make([]string, 0)
	for _, path := range paths {
		file := staged[path]
		if err := verifyPatchOriginal(file); err != nil {
			cleanupPreparedPatch(prepared, createdDirs)
			return err
		}
		if file.Content == nil {
			prepared = append(prepared, preparedPatchFile{file: file})
			continue
		}
		dirs, err := ensurePatchParent(filepath.Dir(file.Abs))
		if err != nil {
			cleanupPreparedPatch(prepared, createdDirs)
			return err
		}
		createdDirs = append(createdDirs, dirs...)
		tempPath, err := writePatchTemp(file)
		if err != nil {
			cleanupPreparedPatch(prepared, createdDirs)
			return err
		}
		prepared = append(prepared, preparedPatchFile{file: file, tempPath: tempPath})
	}

	for i := range prepared {
		item := &prepared[i]
		if item.file.OriginalExists {
			backupPath, err := reservePatchPath(filepath.Dir(item.file.Abs), ".agentdock-patch-backup-*")
			if err != nil {
				return rollbackPatch(prepared, createdDirs, rename, fmt.Errorf("reserve patch backup for %s: %w", item.file.Display, err))
			}
			if err := rename(item.file.Abs, backupPath); err != nil {
				return rollbackPatch(prepared, createdDirs, rename, fmt.Errorf("backup patch target %s: %w", item.file.Display, err))
			}
			item.backupPath = backupPath
			if err := verifyPatchBackup(*item); err != nil {
				return rollbackPatch(prepared, createdDirs, rename, err)
			}
		}
	}
	for i := range prepared {
		item := &prepared[i]
		if item.file.Content == nil {
			continue
		}
		if !item.file.OriginalExists {
			if err := link(item.tempPath, item.file.Abs); err != nil {
				return rollbackPatch(prepared, createdDirs, rename, toolErrorDetails("PATCH_CONFLICT", "patch target was created concurrently or cannot be installed without replacing an existing file", "runtime", map[string]any{"path": item.file.Display, "reason": err.Error()}))
			}
			item.installed = true
			if err := os.Remove(item.tempPath); err != nil {
				return rollbackPatch(prepared, createdDirs, rename, fmt.Errorf("remove linked patch temp for %s: %w", item.file.Display, err))
			}
			item.tempPath = ""
			continue
		}
		if err := rename(item.tempPath, item.file.Abs); err != nil {
			return rollbackPatch(prepared, createdDirs, rename, fmt.Errorf("install patched file %s: %w", item.file.Display, err))
		}
		item.tempPath = ""
		item.installed = true
	}

	for _, item := range prepared {
		if item.backupPath != "" {
			if err := os.Remove(item.backupPath); err != nil {
				slog.Warn("remove committed patch backup failed", "path", item.backupPath, "error", err)
			}
		}
	}
	return nil
}

func verifyPatchOriginal(file stagedPatchFile) error {
	if !file.OriginalExists {
		if _, err := os.Lstat(file.Abs); err == nil {
			return toolErrorDetails("PATCH_CONFLICT", "patch target was created concurrently", "runtime", map[string]any{"path": file.Display})
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	info, content, err := readPatchFile(file.Abs)
	if err != nil {
		return toolErrorDetails("PATCH_CONFLICT", "patch target changed before commit", "runtime", map[string]any{"path": file.Display, "reason": err.Error()})
	}
	if info.Mode().Perm() != file.Mode.Perm() || !bytes.Equal(content, file.Original) {
		return toolErrorDetails("PATCH_CONFLICT", "patch target changed before commit", "runtime", map[string]any{"path": file.Display})
	}
	return nil
}

func verifyPatchBackup(item preparedPatchFile) error {
	info, content, err := readPatchFile(item.backupPath)
	if err != nil {
		return toolErrorDetails("PATCH_CONFLICT", "patch target changed while commit was starting", "runtime", map[string]any{"path": item.file.Display, "reason": err.Error()})
	}
	if info.Mode().Perm() != item.file.Mode.Perm() || !bytes.Equal(content, item.file.Original) {
		return toolErrorDetails("PATCH_CONFLICT", "patch target changed while commit was starting", "runtime", map[string]any{"path": item.file.Display})
	}
	return nil
}

func ensurePatchParent(parent string) ([]string, error) {
	missing := make([]string, 0)
	cursor := parent
	for {
		info, err := os.Stat(cursor)
		if err == nil {
			if !info.IsDir() {
				return nil, fmt.Errorf("patch parent is not a directory: %s", cursor)
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		missing = append(missing, cursor)
		next := filepath.Dir(cursor)
		if next == cursor {
			return nil, fmt.Errorf("patch parent directory not found: %s", parent)
		}
		cursor = next
	}
	created := make([]string, 0, len(missing))
	for i := len(missing) - 1; i >= 0; i-- {
		if err := os.Mkdir(missing[i], 0o755); err != nil {
			removeEmptyPatchDirs(created)
			return nil, err
		}
		created = append(created, missing[i])
	}
	return created, nil
}

func writePatchTemp(file stagedPatchFile) (path string, returnErr error) {
	temp, err := os.CreateTemp(filepath.Dir(file.Abs), ".agentdock-patch-write-*")
	if err != nil {
		return "", err
	}
	path = temp.Name()
	defer func() {
		if returnErr != nil {
			_ = temp.Close()
			_ = os.Remove(path)
		}
	}()
	if err := temp.Chmod(file.Mode.Perm()); err != nil {
		return "", err
	}
	if _, err := temp.WriteString(*file.Content); err != nil {
		return "", err
	}
	if err := temp.Sync(); err != nil {
		return "", err
	}
	if err := temp.Close(); err != nil {
		return "", err
	}
	return path, nil
}

func reservePatchPath(dir, pattern string) (string, error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return path, nil
}

func rollbackPatch(prepared []preparedPatchFile, createdDirs []string, rename func(string, string) error, cause error) error {
	errs := []error{cause}
	for i := len(prepared) - 1; i >= 0; i-- {
		item := &prepared[i]
		if item.installed {
			if err := os.Remove(item.file.Abs); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, fmt.Errorf("remove partially installed %s: %w", item.file.Display, err))
			}
		}
		if item.backupPath != "" {
			if err := rename(item.backupPath, item.file.Abs); err != nil {
				errs = append(errs, fmt.Errorf("restore patch backup %s from %s: %w", item.file.Display, item.backupPath, err))
			}
		}
		if item.tempPath != "" {
			if err := os.Remove(item.tempPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, fmt.Errorf("remove patch temp %s: %w", item.tempPath, err))
			}
		}
	}
	removeEmptyPatchDirs(createdDirs)
	return errors.Join(errs...)
}

func cleanupPreparedPatch(prepared []preparedPatchFile, createdDirs []string) {
	for _, item := range prepared {
		if item.tempPath != "" {
			_ = os.Remove(item.tempPath)
		}
	}
	removeEmptyPatchDirs(createdDirs)
}

func removeEmptyPatchDirs(created []string) {
	for i := len(created) - 1; i >= 0; i-- {
		_ = os.Remove(created[i])
	}
}

func stagedDiffPreview(staged map[string]stagedPatchFile, maxBytes int) (string, bool, diffStats, error) {
	paths := make([]string, 0, len(staged))
	for path := range staged {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var builder strings.Builder
	total := diffStats{}
	for _, path := range paths {
		file := staged[path]
		oldContent := string(file.Original)
		newContent := ""
		if file.Content != nil {
			newContent = *file.Content
		}
		diff, _, stats, err := unifiedDiffPreview(file.Display, oldContent, newContent, 0)
		if err != nil {
			return "", false, diffStats{}, err
		}
		builder.WriteString(diff)
		if diff != "" && !strings.HasSuffix(diff, "\n") {
			builder.WriteString("\n")
		}
		if stats.FilesChanged > 0 {
			total.FilesChanged++
		}
		total.Insertions += stats.Insertions
		total.Deletions += stats.Deletions
	}
	text := builder.String()
	truncated := truncateString(text, maxBytes)
	return truncated, maxBytes > 0 && len([]byte(text)) > maxBytes, total, nil
}

func patchNearbyContext(lines, oldLines []string) []map[string]any {
	if len(lines) == 0 {
		return nil
	}
	needle := ""
	for _, line := range oldLines {
		if strings.TrimSpace(line) != "" {
			needle = strings.TrimSpace(line)
			break
		}
	}
	if needle == "" {
		return []map[string]any{{"line": 1, "context_start_line": 1, "context": firstLines(lines, 20)}}
	}
	for i, line := range lines {
		if strings.Contains(line, needle) {
			return []map[string]any{lineContext(lines, i)}
		}
	}
	return []map[string]any{{"line": 1, "context_start_line": 1, "context": firstLines(lines, 20)}}
}

func patchContextsForMatches(lines []string, indexes []int) []map[string]any {
	out := make([]map[string]any, 0)
	for _, idx := range indexes {
		out = append(out, lineContext(lines, idx))
		if len(out) >= 5 {
			break
		}
	}
	return out
}

func lineContext(lines []string, idx int) map[string]any {
	start := idx - 10
	if start < 0 {
		start = 0
	}
	end := idx + 11
	if end > len(lines) {
		end = len(lines)
	}
	return map[string]any{"line": idx + 1, "context_start_line": start + 1, "context": append([]string(nil), lines[start:end]...)}
}

func firstLines(lines []string, limit int) []string {
	if len(lines) > limit {
		lines = lines[:limit]
	}
	return append([]string(nil), lines...)
}

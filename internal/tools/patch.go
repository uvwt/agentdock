package tools

import (
	"os"
	"path/filepath"
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
	staged := map[string]*string{}
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
			staged[target.Display] = &content
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
			info, err := os.Stat(target.Abs)
			if err != nil {
				return nil, err
			}
			if info.IsDir() {
				return nil, toolError("PATCH_FAILED", "cannot delete a directory", "validation")
			}
			staged[target.Display] = nil
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
			info, err := os.Stat(source.Abs)
			if err != nil {
				return nil, err
			}
			if info.IsDir() {
				return nil, toolError("PATCH_FAILED", "cannot update a directory", "validation")
			}
			currentPtr, exists := staged[source.Display]
			if exists && currentPtr == nil {
				return nil, toolError("PATCH_FAILED", "cannot update a deleted file", "validation")
			}
			content := ""
			if exists && currentPtr != nil {
				content = *currentPtr
			} else {
				data, err := os.ReadFile(source.Abs)
				if err != nil {
					return nil, err
				}
				content = string(data)
			}
			updated, err := applyUpdateHunks(content, op.Hunks, source.Display)
			if err != nil {
				return nil, err
			}
			if op.MoveTo != "" {
				destPath, err := patchPathInBase(basePath, op.MoveTo)
				if err != nil {
					return nil, err
				}
				dest, err := r.ws.ResolveForWrite(destPath)
				if err != nil {
					return nil, err
				}
				if dest.Exists && dest.Display != source.Display {
					return nil, toolError("PATCH_FAILED", "cannot move over an existing file", "validation")
				}
				staged[source.Display] = nil
				staged[dest.Display] = &updated
				affected = append(affected, map[string]any{"path": source.Display, "operation": "move", "move_to": dest.Display})
				summaries = append(summaries, "R "+source.Display+" -> "+dest.Display)
			} else {
				staged[source.Display] = &updated
				affected = append(affected, map[string]any{"path": source.Display, "operation": "update"})
				summaries = append(summaries, "M "+source.Display)
			}
		}
	}
	if len(affected) == 0 {
		return nil, toolError("PATCH_FAILED", "no files were modified", "validation")
	}
	if !dryRun {
		if err := r.commitStaged(staged); err != nil {
			return nil, err
		}
	}
	return Result{"ok": true, "dry_run": dryRun, "workdir": basePath, "affected_files": affected, "summary": strings.Join(summaries, "\n")}, nil
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
	for _, hunk := range hunks {
		oldLines, newLines, err := parseUpdateHunk(hunk)
		if err != nil {
			return "", err
		}
		idxs := findAllSubsequences(lines, oldLines)
		if len(idxs) == 0 {
			return "", toolErrorDetails("PATCH_FAILED", "patch context did not match", "validation", map[string]any{"path": path})
		}
		if len(idxs) > 1 {
			return "", toolErrorDetails("PATCH_FAILED", "patch context matched multiple locations", "validation", map[string]any{"path": path, "matches": len(idxs)})
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

func (r *Runtime) commitStaged(staged map[string]*string) error {
	for rel, content := range staged {
		abs := filepath.Join(r.ws.Root(), filepath.FromSlash(rel))
		if content == nil {
			if err := os.Remove(abs); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(abs, []byte(*content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

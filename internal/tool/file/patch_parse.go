package file

import "strings"

type patchOperation struct {
	Kind       string
	Path       string
	AddContent string
	Hunks      [][]string
	MoveTo     string
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

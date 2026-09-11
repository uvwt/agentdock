package file

import "strings"

type patchUpdateChunk struct {
	Anchor    string
	Lines     []string
	EndOfFile bool
}

type patchOperation struct {
	Kind       string
	Path       string
	AddContent string
	Chunks     []patchUpdateChunk
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

			chunks := make([]patchUpdateChunk, 0)
			var current *patchUpdateChunk
			startChunk := func(anchor string) {
				chunks = append(chunks, patchUpdateChunk{Anchor: anchor})
				current = &chunks[len(chunks)-1]
			}
			for i < len(lines)-1 {
				raw := lines[i]
				if strings.HasPrefix(raw, "*** Add File: ") || strings.HasPrefix(raw, "*** Delete File: ") || strings.HasPrefix(raw, "*** Update File: ") {
					break
				}
				if raw == "*** End of File" {
					if current == nil {
						startChunk("")
					}
					if current.EndOfFile {
						return nil, toolError("PATCH_FAILED", "duplicate end-of-file marker", "validation")
					}
					current.EndOfFile = true
					i++
					continue
				}
				if strings.HasPrefix(raw, "*** ") {
					break
				}
				if current != nil && current.EndOfFile {
					return nil, toolError("PATCH_FAILED", "end-of-file marker must terminate its update chunk", "validation")
				}
				if raw == "@@" || strings.HasPrefix(raw, "@@ ") {
					anchor := strings.TrimPrefix(raw, "@@")
					if strings.HasPrefix(anchor, " ") {
						anchor = anchor[1:]
					}
					startChunk(anchor)
					i++
					continue
				}
				if current == nil {
					startChunk("")
				}
				current.Lines = append(current.Lines, raw)
				i++
			}
			operations = append(operations, patchOperation{Kind: "update", Path: path, Chunks: chunks, MoveTo: moveTo})
			continue
		}
		return nil, toolErrorDetails("PATCH_FAILED", "unrecognized patch line", "validation", map[string]any{"line": line})
	}
	return operations, nil
}

func applyUpdateHunks(content string, chunks []patchUpdateChunk, path string) (string, error) {
	if len(chunks) == 0 {
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
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	trailingNewline := strings.HasSuffix(normalized, "\n")
	body := normalized
	if trailingNewline {
		body = strings.TrimSuffix(body, "\n")
	}
	var lines []string
	if body != "" || normalized != "" {
		lines = strings.Split(body, "\n")
	}

	searchStart := 0
	for chunkIndex, chunk := range chunks {
		oldLines, newLines, err := parseUpdateChunk(chunk)
		if err != nil {
			return "", err
		}

		anchorPos := -1
		if chunk.Anchor != "" {
			matches := findPatchMatches(lines, []string{chunk.Anchor}, searchStart, false)
			if len(matches) == 0 {
				return "", patchContextError("patch anchor did not match", "CONTEXT_NOT_FOUND", path, chunkIndex, lines, []string{chunk.Anchor}, nil)
			}
			if len(matches) > 1 {
				return "", patchContextError("patch anchor matched multiple locations", "AMBIGUOUS_CONTEXT", path, chunkIndex, lines, []string{chunk.Anchor}, matches)
			}
			anchorPos = matches[0]
			searchStart = anchorPos + 1
		}

		if len(oldLines) == 0 {
			if len(newLines) == 0 {
				continue
			}
			// 有锚点的纯插入落在锚点行之后；无锚点的纯插入按 Codex 语义追加到文件末尾。
			insertAt := len(lines)
			if anchorPos >= 0 {
				insertAt = anchorPos + 1
			}
			updated := make([]string, 0, len(lines)+len(newLines))
			updated = append(updated, lines[:insertAt]...)
			updated = append(updated, newLines...)
			updated = append(updated, lines[insertAt:]...)
			lines = updated
			searchStart = insertAt + len(newLines)
			continue
		}

		matches := findPatchMatches(lines, oldLines, searchStart, chunk.EndOfFile)
		if len(matches) == 0 {
			return "", patchContextError("patch context did not match", "CONTEXT_NOT_FOUND", path, chunkIndex, lines, oldLines, nil)
		}
		if len(matches) > 1 {
			return "", patchContextError("patch context matched multiple locations", "AMBIGUOUS_CONTEXT", path, chunkIndex, lines, oldLines, matches)
		}
		idx := matches[0]
		// 匹配允许忽略行尾空白，但未修改的 context 行仍保留磁盘原文，避免容错匹配带来附带格式变更。
		newLines = materializeUpdateLines(chunk, lines[idx:idx+len(oldLines)])
		updated := make([]string, 0, len(lines)-len(oldLines)+len(newLines))
		updated = append(updated, lines[:idx]...)
		updated = append(updated, newLines...)
		updated = append(updated, lines[idx+len(oldLines):]...)
		lines = updated
		searchStart = idx + len(newLines)
	}

	result := strings.Join(lines, lineEnding)
	if trailingNewline {
		result += lineEnding
	}
	if hasBOM {
		result = "\ufeff" + result
	}
	return result, nil
}

func parseUpdateChunk(chunk patchUpdateChunk) ([]string, []string, error) {
	oldLines := make([]string, 0, len(chunk.Lines))
	newLines := make([]string, 0, len(chunk.Lines))
	for _, raw := range chunk.Lines {
		if raw == "" {
			// 部分模型或传输层会去掉空上下文行唯一的前导空格；按 Codex 的宽松解析视为空上下文。
			oldLines = append(oldLines, "")
			newLines = append(newLines, "")
			continue
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

func materializeUpdateLines(chunk patchUpdateChunk, matchedOld []string) []string {
	result := make([]string, 0, len(chunk.Lines))
	oldIndex := 0
	for _, raw := range chunk.Lines {
		if raw == "" {
			result = append(result, matchedOld[oldIndex])
			oldIndex++
			continue
		}
		switch raw[0] {
		case ' ':
			result = append(result, matchedOld[oldIndex])
			oldIndex++
		case '-':
			oldIndex++
		case '+':
			result = append(result, raw[1:])
		}
	}
	return result
}

func findPatchMatches(lines, needle []string, start int, endOfFile bool) []int {
	exact := findPatchMatchesWith(lines, needle, start, endOfFile, func(left, right string) bool { return left == right })
	if len(exact) > 0 {
		return exact
	}
	return findPatchMatchesWith(lines, needle, start, endOfFile, func(left, right string) bool {
		return strings.TrimRight(left, " \t") == strings.TrimRight(right, " \t")
	})
}

func findPatchMatchesWith(lines, needle []string, start int, endOfFile bool, equal func(string, string) bool) []int {
	if len(needle) == 0 {
		if endOfFile {
			return []int{len(lines)}
		}
		if start > len(lines) {
			return nil
		}
		return []int{start}
	}
	if start < 0 {
		start = 0
	}
	last := len(lines) - len(needle)
	if last < start {
		return nil
	}
	if endOfFile {
		start = last
	}
	matches := make([]int, 0)
	for i := start; i <= last; i++ {
		ok := true
		for j := range needle {
			if !equal(lines[i+j], needle[j]) {
				ok = false
				break
			}
		}
		if ok {
			matches = append(matches, i)
		}
		if endOfFile {
			break
		}
	}
	return matches
}

func patchContextError(message, code, path string, chunkIndex int, lines, needle []string, matches []int) error {
	diagnostic := map[string]any{
		"code":       code,
		"path":       path,
		"hunk_index": chunkIndex,
		"message":    message,
	}
	if len(matches) > 0 {
		diagnostic["nearby_context"] = patchContextsForMatches(lines, matches)
	} else {
		diagnostic["nearby_context"] = patchNearbyContext(lines, needle)
	}
	details := map[string]any{"path": path, "diagnostic": diagnostic}
	if len(matches) > 1 {
		details["matches"] = len(matches)
	}
	return toolErrorDetails("PATCH_FAILED", message, "validation", details)
}

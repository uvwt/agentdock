package file

import (
	"sort"
	"strings"
)

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

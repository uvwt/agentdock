package tools

import (
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

type textMeta struct {
	Start     int  `json:"start_line"`
	End       int  `json:"end_line"`
	Total     int  `json:"total_lines"`
	Truncated bool `json:"truncated"`
}

func sliceText(content string, startLine, endLine, maxBytes int) (string, textMeta) {
	if startLine < 1 {
		startLine = 1
	}
	lines := strings.Split(content, "\n")
	total := len(lines)
	if endLine <= 0 || endLine > total {
		endLine = total
	}
	if startLine > total {
		return "", textMeta{Start: startLine, End: endLine, Total: total}
	}
	selected := strings.Join(lines[startLine-1:endLine], "\n")
	truncated := false
	if maxBytes > 0 && len([]byte(selected)) > maxBytes {
		truncated = true
		selected = string([]byte(selected)[:maxBytes])
	}
	return selected, textMeta{Start: startLine, End: endLine, Total: total, Truncated: truncated}
}

func truncateString(value string, maxBytes int) string {
	if maxBytes <= 0 || len([]byte(value)) <= maxBytes {
		return value
	}
	return string([]byte(value)[:maxBytes])
}

func contextAround(lines []string, index, n int) ([]string, []string) {
	if n <= 0 {
		return nil, nil
	}
	start := index - n
	if start < 0 {
		start = 0
	}
	end := index + n + 1
	if end > len(lines) {
		end = len(lines)
	}
	return append([]string(nil), lines[start:index]...), append([]string(nil), lines[index+1:end]...)
}

func looksBinary(data []byte) bool {
	limit := len(data)
	if limit > 8192 {
		limit = 8192
	}
	for i := 0; i < limit; i++ {
		if data[i] == 0 {
			return true
		}
	}
	return false
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", ".reference", "node_modules", "target", "dist", "build", ".venv", "venv", ".tox", ".mypy_cache", ".pytest_cache", ".ruff_cache", "__pycache__":
		return true
	default:
		return false
	}
}

func matchesAny(path string, patterns []string) bool {
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if pattern == "**/*" || pattern == "*" {
			return true
		}
		if ok := globMatch(pattern, path); ok {
			return true
		}
	}
	return false
}

func globMatch(pattern, path string) bool {
	// 工作空间路径统一使用 slash。这里使用 doublestar 是为了避免继续维护
	// 自写 glob 逻辑；**/*.go、internal/**/*.go 这类常见代码检索模式由成熟库处理。
	path = strings.TrimPrefix(path, "./")
	pattern = strings.TrimPrefix(pattern, "./")
	ok, _ := doublestar.Match(pattern, path)
	return ok
}

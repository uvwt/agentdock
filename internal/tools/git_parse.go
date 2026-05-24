package tools

import (
	"strconv"
	"strings"
)

func parseGitStatus(output string) (branch, upstream string, ahead, behind int, files []map[string]any) {
	files = make([]map[string]any, 0)
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "## ") {
			header := strings.TrimPrefix(line, "## ")
			branch = header
			if parts := strings.SplitN(header, "...", 2); len(parts) == 2 {
				branch = parts[0]
				rest := parts[1]
				upstream = strings.Fields(rest)[0]
				if strings.Contains(rest, "ahead ") {
					ahead = parseCountAfter(rest, "ahead ")
				}
				if strings.Contains(rest, "behind ") {
					behind = parseCountAfter(rest, "behind ")
				}
			}
			continue
		}
		status := ""
		path := line
		if len(line) >= 3 {
			status = strings.TrimSpace(line[:2])
			path = strings.TrimSpace(line[3:])
		}
		files = append(files, map[string]any{"path": path, "status": status})
	}
	return branch, upstream, ahead, behind, files
}

func parseCountAfter(value, marker string) int {
	idx := strings.Index(value, marker)
	if idx < 0 {
		return 0
	}
	start := idx + len(marker)
	end := start
	for end < len(value) && value[end] >= '0' && value[end] <= '9' {
		end++
	}
	count, _ := strconv.Atoi(value[start:end])
	return count
}

func parseDiffFiles(diffText string) []map[string]any {
	files := make([]map[string]any, 0)
	var current map[string]any
	for _, line := range strings.Split(diffText, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			parts := strings.Fields(line)
			path := ""
			if len(parts) >= 4 {
				path = strings.TrimPrefix(parts[3], "b/")
			}
			current = map[string]any{"path": path, "status": "modified", "binary": false}
			files = append(files, current)
			continue
		}
		if current == nil {
			continue
		}
		if strings.HasPrefix(line, "new file mode") {
			current["status"] = "added"
		}
		if strings.HasPrefix(line, "deleted file mode") {
			current["status"] = "deleted"
		}
		if strings.HasPrefix(line, "Binary files") {
			current["binary"] = true
		}
	}
	return files
}

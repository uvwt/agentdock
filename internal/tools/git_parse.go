package tools

import (
	"strconv"
	"strings"
)

type gitStatusFile struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

type gitDiffFile struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	Binary bool   `json:"binary"`
}

func parseGitStatus(output string) (branch, upstream string, ahead, behind int, files []gitStatusFile) {
	files = make([]gitStatusFile, 0)
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
		files = append(files, gitStatusFile{Path: path, Status: status})
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

func parseDiffFiles(diffText string) []gitDiffFile {
	files := make([]gitDiffFile, 0)
	current := -1
	for _, line := range strings.Split(diffText, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			parts := strings.Fields(line)
			path := ""
			if len(parts) >= 4 {
				path = strings.TrimPrefix(parts[3], "b/")
			}
			files = append(files, gitDiffFile{Path: path, Status: "modified"})
			current = len(files) - 1
			continue
		}
		if current < 0 {
			continue
		}
		if strings.HasPrefix(line, "new file mode") {
			files[current].Status = "added"
		}
		if strings.HasPrefix(line, "deleted file mode") {
			files[current].Status = "deleted"
		}
		if strings.HasPrefix(line, "Binary files") {
			files[current].Binary = true
		}
	}
	return files
}

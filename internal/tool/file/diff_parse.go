package file

import (
	"strings"

	"github.com/uvwt/agentdock/internal/textutil"
)

type gitDiffFile struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	Binary bool   `json:"binary"`
}

// parseDiffFiles 解析统一 diff 的文件级元数据，既用于 Git diff，也用于 file_edit patch 预览。
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

func truncateBytes(data []byte, maxBytes int) (string, bool) {
	truncated := textutil.SafeTruncateBytes(data, maxBytes)
	return truncated.Text, truncated.Truncated
}

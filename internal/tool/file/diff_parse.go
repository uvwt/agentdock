package file

import (
	"strconv"
	"strings"

	"github.com/uvwt/agentdock/internal/textutil"
)

type gitDiffFile struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	Binary bool   `json:"binary"`
}

// parseDiffFiles extracts file metadata from Git-generated unified patches.
func parseDiffFiles(diffText string) []gitDiffFile {
	files := make([]gitDiffFile, 0)
	current := -1
	inHunk := false
	for _, line := range strings.Split(diffText, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, "diff --git ") {
			files = append(files, gitDiffFile{Path: gitDiffHeaderPath(strings.TrimPrefix(line, "diff --git ")), Status: "modified"})
			current = len(files) - 1
			inHunk = false
			continue
		}
		if current < 0 {
			continue
		}
		if strings.HasPrefix(line, "@@") {
			inHunk = true
		}
		if inHunk {
			continue
		}
		// File markers and rename headers disambiguate names containing spaces.
		if value, ok := strings.CutPrefix(line, "--- "); ok {
			value, _, _ = strings.Cut(value, "\t")
			if value != "/dev/null" {
				files[current].Path = strings.TrimPrefix(unquoteGitPath(value), "a/")
			}
		}
		if value, ok := strings.CutPrefix(line, "+++ "); ok {
			value, _, _ = strings.Cut(value, "\t")
			if value != "/dev/null" {
				files[current].Path = strings.TrimPrefix(unquoteGitPath(value), "b/")
			}
		}
		if value, ok := strings.CutPrefix(line, "rename to "); ok {
			files[current].Path = unquoteGitPath(value)
		}
		if value, ok := strings.CutPrefix(line, "copy to "); ok {
			files[current].Path = unquoteGitPath(value)
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

func unquoteGitPath(value string) string {
	if strings.HasPrefix(value, `"`) {
		if decoded, err := strconv.Unquote(value); err == nil {
			return decoded
		}
	}
	return value
}

func gitDiffHeaderPath(header string) string {
	if strings.HasPrefix(header, `"`) {
		for i := 1; i < len(header); i++ {
			if header[i] == '\\' {
				i++
			} else if header[i] == '"' {
				return strings.TrimPrefix(unquoteGitPath(strings.TrimPrefix(header[i+1:], " ")), "b/")
			}
		}
		return ""
	}
	if i := strings.Index(header, ` "b/`); i >= 0 {
		return strings.TrimPrefix(unquoteGitPath(header[i+1:]), "b/")
	}
	// Mode-only changes have no ---/+++ markers and both names are identical.
	for start := 0; start < len(header); {
		i := strings.Index(header[start:], " b/")
		if i < 0 {
			break
		}
		i += start
		if strings.TrimPrefix(header[:i], "a/") == header[i+3:] {
			return header[i+3:]
		}
		start = i + 1
	}
	if i := strings.Index(header, " b/"); i >= 0 {
		return header[i+3:]
	}
	return ""
}

func truncateBytes(data []byte, maxBytes int) (string, bool) {
	truncated := textutil.SafeTruncateBytes(data, maxBytes)
	return truncated.Text, truncated.Truncated
}

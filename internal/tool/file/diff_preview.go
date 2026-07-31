package file

import (
	"bytes"
	"fmt"
	"strings"

	anchoreddiff "github.com/rogpeppe/go-internal/diff"
	"github.com/uvwt/agentdock/internal/textutil"
)

const maxDiffOutputBytes = 64 << 20

type diffStats struct {
	FilesChanged int
	Insertions   int
	Deletions    int
}

func unifiedDiffPreview(path, oldContent, newContent string, maxBytes int) (string, bool, diffStats, error) {
	output := anchoreddiff.Diff("a/"+path, []byte(oldContent), "b/"+path, []byte(newContent))
	if len(output) > 0 {
		// 该实现会额外输出 diff 命令头；工具契约只返回 unified diff。
		if newline := bytes.IndexByte(output, '\n'); newline >= 0 {
			output = output[newline+1:]
		}
	}
	if len(output) > maxDiffOutputBytes {
		return "", false, diffStats{}, fmt.Errorf("diff output exceeds %d bytes (observed %d bytes)", maxDiffOutputBytes, len(output))
	}
	stats := countDiffStats(string(output))
	truncated := textutil.SafeTruncateBytes(output, maxBytes)
	return truncated.Text, truncated.Truncated, stats, nil
}

func countDiffStats(diffText string) diffStats {
	stats := diffStats{}
	if strings.TrimSpace(diffText) == "" {
		return stats
	}
	for _, line := range strings.Split(diffText, "\n") {
		switch {
		case strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "--- "):
			continue
		case strings.HasPrefix(line, "+"):
			stats.Insertions++
		case strings.HasPrefix(line, "-"):
			stats.Deletions++
		}
	}
	if stats.Insertions > 0 || stats.Deletions > 0 {
		stats.FilesChanged = 1
	}
	return stats
}

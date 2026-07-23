package file

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/uvwt/agentdock/internal/textutil"
)

const maxDiffProcessOutputBytes = 64 << 20

type diffStats struct {
	FilesChanged int
	Insertions   int
	Deletions    int
}

func unifiedDiffPreview(path, oldContent, newContent string, maxBytes int) (string, bool, diffStats, error) {
	dir, err := os.MkdirTemp("", "agentdock-diff-*")
	if err != nil {
		return "", false, diffStats{}, err
	}
	defer os.RemoveAll(dir)

	oldPath := filepath.Join(dir, "old")
	newPath := filepath.Join(dir, "new")
	if err := os.WriteFile(oldPath, []byte(oldContent), 0o600); err != nil {
		return "", false, diffStats{}, err
	}
	if err := os.WriteFile(newPath, []byte(newContent), 0o600); err != nil {
		return "", false, diffStats{}, err
	}
	cmd := exec.Command("diff", "-u", "--label", "a/"+path, "--label", "b/"+path, oldPath, newPath)
	output, totalBytes, outputTruncated, err := runBoundedCombinedOutput(cmd, maxDiffProcessOutputBytes)
	if outputTruncated {
		return "", false, diffStats{}, fmt.Errorf("diff output exceeds %d bytes (observed %d bytes)", maxDiffProcessOutputBytes, totalBytes)
	}
	if err != nil {
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
			return "", false, diffStats{}, err
		}
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

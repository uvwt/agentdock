package tools

import (
	"os"
	"strings"
	"unicode/utf8"

	"github.com/uvwt/agentdock/internal/atomicfile"
)

func (r *Runtime) editFile(args map[string]any) (Result, error) {
	path := stringArg(args, "path", "")
	if path == "" {
		return nil, toolError("INVALID_ARGUMENT", "path is required", "validation")
	}
	oldText := stringArg(args, "old", "")
	if oldText == "" {
		return nil, toolError("INVALID_ARGUMENT", "old is required", "validation")
	}
	newText := stringArg(args, "new", "")
	expected := intArg(args, "expected_matches", 1)
	if expected < 0 {
		expected = 0
	}
	replaceAll := boolArg(args, "replace_all", false)
	dryRun := boolArg(args, "dry_run", false)
	maxDiffBytes := intArg(args, "max_diff_bytes", 65536)

	p, err := r.ws.ResolveExisting(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(p.Abs)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, toolError("IS_DIRECTORY", "cannot edit directory", "validation")
	}
	data, err := os.ReadFile(p.Abs)
	if err != nil {
		return nil, err
	}
	if looksBinary(data) {
		return nil, toolErrorDetails("BINARY_FILE", "binary file edit blocked for text tool", "validation", map[string]any{"path": p.Display})
	}
	if !utf8.Valid(data) {
		return nil, toolErrorDetails("ENCODING_UNSUPPORTED", "file is not valid utf-8", "validation", map[string]any{"path": p.Display})
	}
	content := string(data)
	indexes := findStringIndexes(content, oldText)
	if expected > 0 && len(indexes) != expected {
		return nil, toolErrorDetails("MATCH_COUNT_MISMATCH", "old text matched an unexpected number of times", "validation", map[string]any{"path": p.Display, "matches": len(indexes), "expected_matches": expected, "nearby_context": editNearbyContext(content, indexes)})
	}
	if expected == 0 && len(indexes) > 0 {
		return nil, toolErrorDetails("MATCH_COUNT_MISMATCH", "old text matched but expected zero matches", "validation", map[string]any{"path": p.Display, "matches": len(indexes), "expected_matches": expected, "nearby_context": editNearbyContext(content, indexes)})
	}
	if len(indexes) == 0 {
		return nil, toolErrorDetails("MATCH_COUNT_MISMATCH", "old text did not match", "validation", map[string]any{"path": p.Display, "matches": 0, "expected_matches": expected})
	}

	updated := content
	if replaceAll {
		updated = strings.ReplaceAll(content, oldText, newText)
	} else {
		updated = strings.Replace(content, oldText, newText, 1)
	}
	diffPreview, diffTruncated, stats, err := unifiedDiffPreview(p.Display, content, updated, maxDiffBytes)
	if err != nil {
		return nil, err
	}
	result := Result{"ok": true, "path": p.Display, "dry_run": dryRun, "matches": len(indexes), "changed": updated != content, "diff_preview": diffPreview, "truncated": diffTruncated, "files_changed": stats.FilesChanged, "insertions": stats.Insertions, "deletions": stats.Deletions, "summary": editSummary(p.Display, updated != content)}
	if dryRun || updated == content {
		return result, nil
	}
	if err := atomicfile.Write(p.Abs, []byte(updated), info.Mode().Perm()); err != nil {
		return nil, err
	}
	return result, nil
}

func findStringIndexes(content, needle string) []int {
	indexes := []int{}
	offset := 0
	for {
		idx := strings.Index(content[offset:], needle)
		if idx < 0 {
			return indexes
		}
		abs := offset + idx
		indexes = append(indexes, abs)
		offset = abs + len(needle)
	}
}

func editNearbyContext(content string, indexes []int) []map[string]any {
	if len(indexes) == 0 {
		return nil
	}
	lines := strings.Split(content, "\n")
	out := make([]map[string]any, 0, len(indexes))
	byteOffset := 0
	indexCursor := 0
	for lineIndex, line := range lines {
		lineEnd := byteOffset + len(line)
		for indexCursor < len(indexes) && indexes[indexCursor] >= byteOffset && indexes[indexCursor] <= lineEnd {
			start := lineIndex - 2
			if start < 0 {
				start = 0
			}
			end := lineIndex + 3
			if end > len(lines) {
				end = len(lines)
			}
			out = append(out, map[string]any{"line": lineIndex + 1, "context_start_line": start + 1, "context": append([]string(nil), lines[start:end]...)})
			indexCursor++
			if len(out) >= 5 {
				return out
			}
		}
		byteOffset = lineEnd + 1
	}
	return out
}

func editSummary(path string, changed bool) string {
	if !changed {
		return "no changes for " + path
	}
	return "updated " + path
}

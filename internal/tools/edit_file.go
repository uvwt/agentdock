package tools

import (
	"strings"
	"unicode/utf8"
)

func (r *Runtime) editFile(args map[string]any) (Result, error) {
	path := stringArg(args, "path", "")
	if path == "" {
		return nil, toolError("INVALID_ARGUMENT", "path is required", "validation")
	}
	p, err := r.ws.ResolveExisting(path)
	if err != nil {
		return nil, err
	}
	read, err := readBoundedFile(p.Abs, int64(maxTextFileReadBytes))
	if err != nil {
		return nil, err
	}
	if read.Info.IsDir() {
		return nil, toolError("IS_DIRECTORY", "cannot edit directory", "validation")
	}
	if read.TooLarge {
		return nil, toolErrorDetails(
			"FILE_TOO_LARGE",
			"text file exceeds the file_edit input limit",
			"validation",
			map[string]any{"path": p.Display, "size_bytes": read.Size, "max_size_bytes": maxTextFileReadBytes},
		)
	}
	info := read.Info
	data := read.Data
	if looksBinary(data) {
		return nil, toolErrorDetails("BINARY_FILE", "binary file edit blocked for text tool", "validation", map[string]any{"path": p.Display})
	}
	if !utf8.Valid(data) {
		return nil, toolErrorDetails("ENCODING_UNSUPPORTED", "file is not valid utf-8", "validation", map[string]any{"path": p.Display})
	}
	content := string(data)
	result, updated, err := prepareTextReplacement(p.Display, content, args)
	if err != nil {
		return nil, err
	}
	if boolArg(args, "dry_run", false) || updated == content {
		return result, nil
	}
	staged := map[string]stagedPatchFile{
		p.Abs: {
			Abs:            p.Abs,
			Display:        p.Display,
			Content:        &updated,
			Mode:           info.Mode().Perm(),
			Original:       append([]byte(nil), data...),
			OriginalExists: true,
		},
	}
	if err := commitStagedPatch(staged); err != nil {
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

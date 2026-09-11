//go:build linux

package wslfilehelper

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/bmatcuk/doublestar/v4"
)

var defaultSkippedDirs = map[string]struct{}{
	".git": {}, ".reference": {}, "node_modules": {}, "target": {}, "dist": {}, "build": {},
	".venv": {}, "venv": {}, ".tox": {}, ".mypy_cache": {}, ".pytest_cache": {}, ".ruff_cache": {}, "__pycache__": {},
}

type ignoreRule struct {
	pattern       string
	negate        bool
	directoryOnly bool
	rooted        bool
}

func loadIgnoreRules(root string) []ignoreRule {
	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil || !utf8.Valid(data) {
		return nil
	}
	var rules []ignoreRule
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rule := ignoreRule{}
		if strings.HasPrefix(line, "!") {
			rule.negate = true
			line = strings.TrimPrefix(line, "!")
		}
		if strings.HasPrefix(line, "/") {
			rule.rooted = true
			line = strings.TrimPrefix(line, "/")
		}
		if strings.HasSuffix(line, "/") {
			rule.directoryOnly = true
			line = strings.TrimSuffix(line, "/")
		}
		if line != "" {
			rule.pattern = filepath.ToSlash(line)
			rules = append(rules, rule)
		}
	}
	return rules
}

func globMatches(pattern, rel string) bool {
	pattern = filepath.ToSlash(strings.TrimPrefix(pattern, "./"))
	rel = filepath.ToSlash(strings.TrimPrefix(rel, "./"))
	matched, err := doublestar.Match(pattern, rel)
	return err == nil && matched
}

func ignoredByRules(rel string, isDir bool, rules []ignoreRule) bool {
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	ignored := false
	for _, rule := range rules {
		if rule.directoryOnly && !isDir {
			continue
		}
		matched := false
		if rule.rooted {
			matched = globMatches(rule.pattern, rel)
		} else {
			for index := range parts {
				if globMatches(rule.pattern, strings.Join(parts[index:], "/")) {
					matched = true
					break
				}
			}
		}
		if matched {
			ignored = !rule.negate
		}
	}
	return ignored
}

type treeEntry struct {
	full  string
	rel   string
	info  fs.FileInfo
	depth int
}

func walkTree(root string, includeHidden, includeIgnored bool, maxDepth int, skipped *[]string, visit func(treeEntry) bool) error {
	rules := []ignoreRule(nil)
	if !includeIgnored {
		rules = loadIgnoreRules(root)
	}
	var walk func(string, int) (bool, error)
	walk = func(directory string, depth int) (bool, error) {
		entries, err := os.ReadDir(directory)
		if err != nil {
			if errors.Is(err, fs.ErrPermission) && directory != root {
				rel, _ := filepath.Rel(root, directory)
				*skipped = append(*skipped, filepath.ToSlash(rel))
				return true, nil
			}
			return false, err
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			full := filepath.Join(directory, entry.Name())
			rel, err := filepath.Rel(root, full)
			if err != nil {
				return false, err
			}
			rel = filepath.ToSlash(rel)
			info, err := os.Lstat(full)
			if err != nil {
				if errors.Is(err, fs.ErrPermission) {
					*skipped = append(*skipped, rel)
					continue
				}
				return false, err
			}
			isDir := info.IsDir()
			if !includeHidden && hiddenPath(rel) {
				continue
			}
			if !includeIgnored {
				if _, skip := defaultSkippedDirs[entry.Name()]; skip || ignoredByRules(rel, isDir, rules) {
					continue
				}
			}
			if !visit(treeEntry{full: full, rel: rel, info: info, depth: depth}) {
				return false, nil
			}
			if isDir && (maxDepth <= 0 || depth < maxDepth) {
				keepGoing, err := walk(full, depth+1)
				if err != nil || !keepGoing {
					return keepGoing, err
				}
			}
		}
		return true, nil
	}
	_, err := walk(root, 1)
	return err
}

func listDirectory(req *Request) (*Response, error) {
	root, err := checkedPath(req.Path, false)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fail("PATH_NOT_FOUND", "WSL path does not exist", map[string]any{"path": root})
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fail("NOT_A_DIRECTORY", "WSL path is not a directory", map[string]any{"path": root})
	}
	maxDepth := req.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 1
	}
	maxDepth = min(maxDepth, 20)
	maxEntries := req.MaxEntries
	if maxEntries <= 0 {
		maxEntries = 200
	}
	maxEntries = min(maxEntries, 5000)
	patterns := req.Patterns
	if len(patterns) == 0 {
		patterns = []string{"**/*"}
	}
	entryType := req.EntryType
	if entryType == "" {
		entryType = "any"
	}
	if entryType != "any" && entryType != "file" && entryType != "directory" {
		return nil, fail("INVALID_ARGUMENT", "entry_type must be any, file, or directory", map[string]any{"entry_type": entryType})
	}

	items := make([]DirEntry, 0)
	skipped := make([]string, 0)
	truncated := false
	err = walkTree(root, req.IncludeHidden, req.IncludeIgnored, maxDepth, &skipped, func(item treeEntry) bool {
		kind := "file"
		if item.info.IsDir() {
			kind = "directory"
		}
		if entryType != "any" && entryType != kind {
			return true
		}
		if !matchesAnyGlob(patterns, item.rel) || matchesAnyGlob(req.ExcludePatterns, item.rel) {
			return true
		}
		if len(items) >= maxEntries {
			truncated = true
			return false
		}
		items = append(items, DirEntry{
			Name: filepath.Base(item.full), Path: item.rel, Type: kind, SizeBytes: item.info.Size(),
			Modified: timestamp(item.info.ModTime()), IsHidden: hiddenPath(item.rel),
		})
		return true
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	skipped = uniqueSorted(skipped)
	return &Response{
		Path: root, Entries: &items, Truncated: boolPtr(truncated), Partial: boolPtr(len(skipped) > 0), SkippedPaths: &skipped,
	}, nil
}

func searchText(req *Request) (*Response, error) {
	root, err := checkedPath(req.Path, false)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fail("PATH_NOT_FOUND", "WSL path does not exist", map[string]any{"path": root})
	}
	if err != nil {
		return nil, err
	}
	if req.Query == "" {
		return nil, fail("INVALID_ARGUMENT", "query is required", nil)
	}
	contextLines := min(max(req.ContextLines, 0), 20)
	maxResults := req.MaxResults
	if maxResults <= 0 {
		maxResults = 100
	}
	maxResults = min(maxResults, 1000)
	pattern := req.Query
	if !req.Regex {
		pattern = regexp.QuoteMeta(pattern)
	}
	if !req.CaseSensitive {
		pattern = "(?i)" + pattern
	}
	matcher, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fail("INVALID_REGEX", err.Error(), nil)
	}

	type candidate struct{ full, rel string }
	candidates := make([]candidate, 0)
	if info.Mode().IsRegular() {
		candidates = append(candidates, candidate{full: root, rel: filepath.Base(root)})
	} else if info.IsDir() {
		skipped := []string{}
		if err := walkTree(root, req.IncludeHidden, req.IncludeIgnored, 0, &skipped, func(item treeEntry) bool {
			if item.info.Mode().IsRegular() {
				candidates = append(candidates, candidate{full: item.full, rel: item.rel})
			}
			return true
		}); err != nil {
			return nil, err
		}
	} else {
		return nil, fail("NOT_REGULAR_FILE", "search_text only supports regular files or directories", map[string]any{"path": root})
	}

	matches := make([]SearchMatch, 0)
	truncated := false
	for _, candidate := range candidates {
		if len(req.IncludeGlobs) > 0 && !matchesAnyGlob(req.IncludeGlobs, candidate.rel) {
			continue
		}
		if matchesAnyGlob(req.ExcludeGlobs, candidate.rel) {
			continue
		}
		fileInfo, err := os.Stat(candidate.full)
		if err != nil || fileInfo.Size() > maxTextFileBytes {
			continue
		}
		data, err := os.ReadFile(candidate.full)
		if err != nil || len(data) > maxTextFileBytes || !utf8.Valid(data) {
			continue
		}
		probe := data
		if len(probe) > 8192 {
			probe = probe[:8192]
		}
		if bytes.IndexByte(probe, 0) >= 0 {
			continue
		}
		lines := splitLines(string(data))
		for index, line := range lines {
			location := matcher.FindStringIndex(line)
			if location == nil {
				continue
			}
			beforeStart := max(0, index-contextLines)
			afterEnd := min(len(lines), index+contextLines+1)
			matches = append(matches, SearchMatch{
				Path: filepath.ToSlash(candidate.full), RelativePath: filepath.ToSlash(candidate.rel), Line: index + 1,
				Column: len([]byte(line[:location[0]])) + 1, Preview: truncateRunes(line, 500), MatchText: truncateRunes(line[location[0]:location[1]], 500),
				Before: append([]string(nil), lines[beforeStart:index]...), After: append([]string(nil), lines[index+1:afterEnd]...),
				ContextStartLine: beforeStart + 1, ContextEndLine: afterEnd,
			})
			if len(matches) >= maxResults {
				truncated = true
				break
			}
		}
		if truncated {
			break
		}
	}
	return &Response{
		Path: root, Query: req.Query, Engine: "go_wsl", Matches: &matches,
		TotalMatches: intPtr(len(matches)), Truncated: boolPtr(truncated),
	}, nil
}

func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	lines := strings.Split(normalized, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func truncateRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit])
}

func matchesAnyGlob(patterns []string, rel string) bool {
	for _, pattern := range patterns {
		if globMatches(pattern, rel) {
			return true
		}
	}
	return false
}

func uniqueSorted(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	sort.Strings(values)
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

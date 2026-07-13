package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uvwt/agentdock/internal/workspace"
)

type searchOptions struct {
	Query          string
	CaseSensitive  bool
	Regex          bool
	IncludeIgnored bool
	IncludeHidden  bool
	IncludeGlobs   []string
	ExcludeGlobs   []string
	MaxResults     int
	ContextLines   int
}

func (r *Runtime) searchText(ctx context.Context, args map[string]any) (Result, error) {
	selection, err := selectFileRuntime(args)
	if err != nil {
		return nil, err
	}
	if selection.isWSL() {
		return r.searchTextWSL(ctx, args, selection)
	}

	query := stringArg(args, "query", "")
	if query == "" {
		return nil, toolError("INVALID_ARGUMENT", "query is required", "validation")
	}
	p, err := r.ws.ResolveExisting(stringArg(args, "path", "."))
	if err != nil {
		return nil, err
	}
	includeGlobs := stringSliceArg(args, "include_globs")
	if glob := stringArg(args, "glob", ""); glob != "" {
		includeGlobs = append(includeGlobs, glob)
	}
	opts := searchOptions{
		Query:          query,
		CaseSensitive:  boolArg(args, "case_sensitive", false),
		Regex:          boolArg(args, "regex", false),
		IncludeIgnored: boolArg(args, "include_ignored", false),
		IncludeHidden:  boolArg(args, "include_hidden", false),
		IncludeGlobs:   includeGlobs,
		ExcludeGlobs:   stringSliceArg(args, "exclude_globs"),
		MaxResults:     boundedInt(intArg(args, "max_results", 100), 100, 1, 1000),
		ContextLines:   boundedInt(intArg(args, "context_lines", 0), 0, 0, 20),
	}
	if result, ok := r.searchTextRG(ctx, p, opts); ok {
		return addFileRuntimeResult(result, selection), nil
	}
	result, err := r.searchTextGo(ctx, p, opts)
	return addFileRuntimeResult(result, selection), err
}

func (r *Runtime) searchTextRG(ctx context.Context, p workspace.Path, opts searchOptions) (Result, bool) {
	rg, err := exec.LookPath("rg")
	if err != nil {
		return nil, false
	}
	args := []string{"--json", "--line-number", "--column", "--color", "never"}
	if !opts.Regex {
		args = append(args, "--fixed-strings")
	}
	if !opts.CaseSensitive {
		args = append(args, "--ignore-case")
	}
	if opts.IncludeIgnored {
		args = append(args, "--no-ignore")
	}
	if opts.IncludeHidden {
		args = append(args, "--hidden")
	}
	if opts.ContextLines > 0 {
		args = append(args, "--context", strconv.Itoa(opts.ContextLines))
	}
	for _, glob := range opts.IncludeGlobs {
		if strings.TrimSpace(glob) != "" {
			args = append(args, "--glob", glob)
		}
	}
	for _, glob := range opts.ExcludeGlobs {
		if strings.TrimSpace(glob) != "" {
			args = append(args, "--glob", "!"+glob)
		}
	}
	args = append(args, opts.Query, p.Abs)

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, rg, args...)
	cmd.Dir = p.Abs
	output, err := cmd.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
			return Result{"ok": true, "query": opts.Query, "engine": "rg", "matches": []map[string]any{}, "total_matches": 0, "truncated": false}, true
		}
		return nil, false
	}
	matches, truncated, ok := r.parseRGJSON(output, opts)
	if !ok {
		return nil, false
	}
	return Result{"ok": true, "query": opts.Query, "engine": "rg", "matches": matches, "total_matches": len(matches), "truncated": truncated}, true
}

func (r *Runtime) parseRGJSON(output []byte, opts searchOptions) ([]map[string]any, bool, bool) {
	matches := make([]map[string]any, 0)
	before := map[string][]string{}
	var lastMatch map[string]any
	var lastPath string
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		var event struct {
			Type string `json:"type"`
			Data struct {
				Path struct {
					Text string `json:"text"`
				} `json:"path"`
				Lines struct {
					Text string `json:"text"`
				} `json:"lines"`
				LineNumber int `json:"line_number"`
				Submatches []struct {
					Match struct {
						Text string `json:"text"`
					} `json:"match"`
					Start int `json:"start"`
				} `json:"submatches"`
			} `json:"data"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, false, false
		}
		path, err := r.ws.Relative(event.Data.Path.Text)
		if err != nil || path == "" {
			path = filepath.ToSlash(event.Data.Path.Text)
		}
		line := strings.TrimSuffix(event.Data.Lines.Text, "\n")
		switch event.Type {
		case "context":
			if lastMatch != nil && lastPath == path {
				after, _ := lastMatch["after"].([]string)
				lastMatch["after"] = append(after, line)
				lastMatch["context_end_line"] = event.Data.LineNumber
			} else {
				before[path] = append(before[path], line)
				if opts.ContextLines > 0 && len(before[path]) > opts.ContextLines {
					before[path] = before[path][len(before[path])-opts.ContextLines:]
				}
			}
		case "match":
			column := 1
			matchText := ""
			if len(event.Data.Submatches) > 0 {
				column = event.Data.Submatches[0].Start + 1
				matchText = event.Data.Submatches[0].Match.Text
			}
			beforeLines := append([]string(nil), before[path]...)
			before[path] = nil
			entry := map[string]any{"path": path, "line": event.Data.LineNumber, "column": column, "preview": truncateString(line, 500), "match_text": truncateString(matchText, 500), "before": beforeLines, "after": []string{}, "context_start_line": event.Data.LineNumber - len(beforeLines), "context_end_line": event.Data.LineNumber}
			matches = append(matches, entry)
			lastMatch = entry
			lastPath = path
			if opts.MaxResults > 0 && len(matches) >= opts.MaxResults {
				return matches, true, true
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, false, false
	}
	return matches, false, true
}

func (r *Runtime) searchTextGo(ctx context.Context, p workspace.Path, opts searchOptions) (Result, error) {
	var re *regexp.Regexp
	if opts.Regex || !opts.CaseSensitive {
		pattern := opts.Query
		if !opts.Regex {
			pattern = regexp.QuoteMeta(pattern)
		}
		if !opts.CaseSensitive {
			// 直接在原始行上做 Unicode 大小写折叠，避免 strings.ToLower 改变
			// UTF-8 字节长度后再用旧索引切片，导致列号错误或截断字符。
			pattern = "(?i:" + pattern + ")"
		}
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return nil, err
		}
		re = compiled
	}
	matches := make([]map[string]any, 0)
	ignore := loadIgnoreMatcher(r.ws.Root())
	walkErr := filepath.WalkDir(p.Abs, func(abs string, d os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return nil
		}
		rel, _ := r.ws.Relative(abs)
		if !opts.IncludeIgnored && ignore.Ignored(rel, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if !opts.IncludeIgnored && abs != p.Abs && shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			if !opts.IncludeHidden && abs != p.Abs && workspace.Hidden(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !opts.IncludeHidden && workspace.Hidden(d.Name()) {
			return nil
		}
		if len(opts.IncludeGlobs) > 0 && !matchesAny(rel, opts.IncludeGlobs) {
			return nil
		}
		if matchesAny(rel, opts.ExcludeGlobs) {
			return nil
		}
		data, err := os.ReadFile(abs)
		if err != nil || looksBinary(data) || !utf8.Valid(data) {
			return nil
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			ok := false
			column := 0
			matchText := ""
			if re != nil {
				idx := re.FindStringIndex(line)
				ok = idx != nil
				if ok {
					column = idx[0] + 1
					matchText = line[idx[0]:idx[1]]
				}
			} else if idx := strings.Index(line, opts.Query); idx >= 0 {
				ok = true
				column = idx + 1
				matchText = line[idx : idx+len(opts.Query)]
			}
			if !ok {
				continue
			}
			before, after := contextAround(lines, i, opts.ContextLines)
			matches = append(matches, map[string]any{"path": rel, "line": i + 1, "column": column, "preview": truncateString(line, 500), "match_text": truncateString(matchText, 500), "before": before, "after": after, "context_start_line": i + 1 - len(before), "context_end_line": i + 1 + len(after)})
			if opts.MaxResults > 0 && len(matches) >= opts.MaxResults {
				return filepath.SkipAll
			}
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return Result{"ok": true, "query": opts.Query, "engine": "go_fallback", "matches": matches, "total_matches": len(matches), "truncated": opts.MaxResults > 0 && len(matches) >= opts.MaxResults}, nil
}

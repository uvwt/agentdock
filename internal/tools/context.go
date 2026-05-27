package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const contextMaxFileBytes = 512 * 1024

func (r *Runtime) contextList(args map[string]any) (Result, error) {
	root, err := r.contextRoot(false)
	if err != nil {
		return nil, err
	}
	prefix := strings.TrimSpace(stringArg(args, "prefix", ""))
	maxEntries := intArg(args, "max_entries", 200)
	if maxEntries <= 0 || maxEntries > 1000 {
		maxEntries = 200
	}
	base := root
	if prefix != "" {
		base, err = r.resolveContextPath(prefix, false)
		if err != nil {
			return nil, err
		}
	}
	entries := []map[string]any{}
	if _, err := os.Stat(base); os.IsNotExist(err) {
		return Result{"ok": true, "root": root, "prefix": prefix, "entries": entries, "count": 0}, nil
	}
	err = filepath.WalkDir(base, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == base {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") && name != ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() && name == ".git" {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		info, _ := d.Info()
		entry := map[string]any{"path": rel, "name": name, "type": "file"}
		if d.IsDir() {
			entry["type"] = "directory"
		}
		if info != nil {
			entry["size_bytes"] = info.Size()
			entry["modified"] = info.ModTime().Format(time.RFC3339)
		}
		entries = append(entries, entry)
		if len(entries) >= maxEntries {
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return fmt.Sprint(entries[i]["path"]) < fmt.Sprint(entries[j]["path"]) })
	return Result{"ok": true, "root": root, "prefix": prefix, "entries": entries, "count": len(entries)}, nil
}

func (r *Runtime) contextRead(args map[string]any) (Result, error) {
	p := stringArg(args, "path", "")
	if p == "" {
		return nil, toolError("MISSING_PATH", "path is required", "validation")
	}
	abs, err := r.resolveContextPath(p, false)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	if len(data) > contextMaxFileBytes {
		return nil, toolError("FILE_TOO_LARGE", "context file is too large", "validation")
	}
	if !utf8.Valid(data) {
		return nil, toolError("UNSUPPORTED_ENCODING", "context file must be utf-8 text", "validation")
	}
	frontmatter, body := splitFrontmatter(string(data))
	return Result{"ok": true, "path": filepath.ToSlash(p), "content": string(data), "frontmatter": frontmatter, "body": body, "size_bytes": len(data)}, nil
}

func (r *Runtime) contextSearch(args map[string]any) (Result, error) {
	root, err := r.contextRoot(false)
	if err != nil {
		return nil, err
	}
	query := strings.TrimSpace(stringArg(args, "query", ""))
	if query == "" {
		return nil, toolError("MISSING_QUERY", "query is required", "validation")
	}
	maxResults := intArg(args, "max_results", 50)
	if maxResults <= 0 || maxResults > 200 {
		maxResults = 50
	}
	lower := strings.ToLower(query)
	results := []map[string]any{}
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return Result{"ok": true, "query": query, "results": results, "count": 0}, nil
	}
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !isContextTextFile(path) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || len(data) > contextMaxFileBytes || !utf8.Valid(data) {
			return nil
		}
		text := string(data)
		textLower := strings.ToLower(text)
		idx := strings.Index(textLower, lower)
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if idx < 0 && !strings.Contains(strings.ToLower(rel), lower) {
			return nil
		}
		snippet := rel
		if idx >= 0 {
			start := idx - 120
			if start < 0 {
				start = 0
			}
			end := idx + len(query) + 180
			if end > len(text) {
				end = len(text)
			}
			snippet = strings.TrimSpace(text[start:end])
		}
		frontmatter, _ := splitFrontmatter(text)
		results = append(results, map[string]any{"path": rel, "snippet": snippet, "frontmatter": frontmatter})
		if len(results) >= maxResults {
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return Result{"ok": true, "query": query, "results": results, "count": len(results)}, nil
}

func (r *Runtime) contextPack(args map[string]any) (Result, error) {
	project := strings.TrimSpace(stringArg(args, "project", ""))
	maxBytes := intArg(args, "max_bytes", 120000)
	if maxBytes <= 0 || maxBytes > 512000 {
		maxBytes = 120000
	}
	paths := []string{"shared/profile.md"}
	if project != "" {
		base := "shared/projects/" + safeContextSegment(project)
		paths = append(paths,
			base+"/overview.md",
			base+"/conventions.md",
			base+"/environment.md",
			base+"/session-handoff.md",
		)
		paths = append(paths, listContextFilesUnder(r, base+"/decisions", 10)...)
		paths = append(paths, listContextFilesUnder(r, base+"/runbooks", 10)...)
	}
	sections := []map[string]any{}
	total := 0
	for _, rel := range dedupeStrings(paths) {
		abs, err := r.resolveContextPath(rel, false)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil || !utf8.Valid(data) {
			continue
		}
		content := string(data)
		if total+len(content) > maxBytes {
			remaining := maxBytes - total
			if remaining <= 0 {
				break
			}
			content = content[:remaining]
		}
		frontmatter, body := splitFrontmatter(content)
		sections = append(sections, map[string]any{"path": rel, "frontmatter": frontmatter, "body": body, "content": content})
		total += len(content)
		if total >= maxBytes {
			break
		}
	}
	return Result{"ok": true, "project": project, "sections": sections, "count": len(sections), "bytes": total}, nil
}

func (r *Runtime) contextAppendNote(args map[string]any) (Result, error) {
	content := strings.TrimSpace(stringArg(args, "content", ""))
	if content == "" {
		return nil, toolError("MISSING_CONTENT", "content is required", "validation")
	}
	scope := safeContextSegment(stringArg(args, "scope", "inbox"))
	if scope == "" {
		scope = "inbox"
	}
	name := strings.TrimSpace(stringArg(args, "name", ""))
	if name == "" {
		name = time.Now().Format("20060102-150405") + "-note.md"
	}
	name = safeContextFilename(name)
	path := filepath.ToSlash(filepath.Join(scope, name))
	abs, err := r.resolveContextPath(path, true)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return nil, err
	}
	entry := fmt.Sprintf("---\ntype: note\nscope: %s\nsource: user-confirmed\nupdated_at: %s\n---\n\n%s\n", scope, time.Now().Format(time.RFC3339), content)
	if _, err := os.Stat(abs); err == nil {
		entry = "\n\n---\n\n" + content + "\n"
		file, err := os.OpenFile(abs, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		if _, err := file.WriteString(entry); err != nil {
			return nil, err
		}
	} else if err := os.WriteFile(abs, []byte(entry), 0o644); err != nil {
		return nil, err
	}
	return Result{"ok": true, "path": path, "bytes": len([]byte(entry))}, nil
}

func (r *Runtime) contextWrite(args map[string]any) (Result, error) {
	path := strings.TrimSpace(stringArg(args, "path", ""))
	content := stringArg(args, "content", "")
	if path == "" {
		return nil, toolError("MISSING_PATH", "path is required", "validation")
	}
	if content == "" {
		return nil, toolError("MISSING_CONTENT", "content is required", "validation")
	}
	if !strings.HasPrefix(filepath.ToSlash(path), "inbox/") && !boolArg(args, "confirmed", false) {
		return nil, toolErrorDetails("CONFIRMATION_REQUIRED", "writing outside inbox requires confirmed=true", "validation", map[string]any{"path": path})
	}
	path = filepath.ToSlash(path)
	if !isContextTextFile(path) {
		return nil, toolError("INVALID_CONTEXT_FILE", "context path must be a markdown or text file", "validation")
	}
	abs, err := r.resolveContextPath(path, true)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return nil, err
	}
	if _, err := os.Stat(abs); err == nil && !boolArg(args, "overwrite", false) {
		return nil, toolErrorDetails("FILE_EXISTS", "context file exists; set overwrite=true to replace", "validation", map[string]any{"path": path})
	}
	if !strings.HasPrefix(content, "---\n") {
		content = fmt.Sprintf("---\ntype: context\nsource: user-confirmed\nupdated_at: %s\n---\n\n%s", time.Now().Format(time.RFC3339), content)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return nil, err
	}
	return Result{"ok": true, "path": path, "bytes": len([]byte(content))}, nil
}

func (r *Runtime) contextRoot(create bool) (string, error) {
	root := r.cfg.ContextDir
	if root == "" {
		root = "context"
	}
	if !filepath.IsAbs(root) {
		base := r.cfg.AgentDockDir
		if base == "" {
			base = "AgentDock"
		}
		root = filepath.Join(base, root)
	}
	root = filepath.Clean(root)
	if create {
		if err := os.MkdirAll(root, 0o755); err != nil {
			return "", err
		}
	}
	return root, nil
}

func (r *Runtime) resolveContextPath(rel string, createRoot bool) (string, error) {
	root, err := r.contextRoot(createRoot)
	if err != nil {
		return "", err
	}
	rel = filepath.Clean(strings.TrimPrefix(filepath.FromSlash(rel), string(filepath.Separator)))
	if rel == "." || rel == "" || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", toolError("INVALID_CONTEXT_PATH", "context path must stay inside context directory", "validation")
	}
	abs := filepath.Clean(filepath.Join(root, rel))
	rootWithSep := root + string(filepath.Separator)
	if abs != root && !strings.HasPrefix(abs, rootWithSep) {
		return "", toolError("INVALID_CONTEXT_PATH", "context path escapes context directory", "validation")
	}
	return abs, nil
}

func isContextTextFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".md" || ext == ".markdown" || ext == ".txt"
}

func splitFrontmatter(content string) (map[string]any, string) {
	meta := map[string]any{}
	if !strings.HasPrefix(content, "---\n") {
		return meta, content
	}
	end := strings.Index(content[4:], "\n---\n")
	if end < 0 {
		return meta, content
	}
	front := content[4 : 4+end]
	body := content[4+end+5:]
	for _, line := range strings.Split(front, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.Trim(strings.TrimSpace(parts[1]), "\"")
		meta[key] = value
	}
	return meta, body
}

func safeContextSegment(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "-")
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "-_")
}

func safeContextFilename(value string) string {
	value = filepath.Base(filepath.ToSlash(value))
	if !isContextTextFile(value) {
		value += ".md"
	}
	clean := safeContextSegment(strings.TrimSuffix(value, filepath.Ext(value)))
	if clean == "" {
		clean = time.Now().Format("20060102-150405") + "-note"
	}
	return clean + strings.ToLower(filepath.Ext(value))
}

func listContextFilesUnder(r *Runtime, rel string, max int) []string {
	abs, err := r.resolveContextPath(rel, false)
	if err != nil {
		return nil
	}
	files := []string{}
	_ = filepath.WalkDir(abs, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !isContextTextFile(path) {
			return nil
		}
		relPath, _ := filepath.Rel(filepath.Dir(filepath.Dir(abs)), path)
		root, _ := r.contextRoot(false)
		relPath, _ = filepath.Rel(root, path)
		files = append(files, filepath.ToSlash(relPath))
		return nil
	})
	sort.Strings(files)
	if len(files) > max {
		files = files[len(files)-max:]
	}
	return files
}

func dedupeStrings(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

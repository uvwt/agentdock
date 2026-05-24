package tools

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

type ignoreMatcher struct {
	rules []ignoreRule
}

type ignoreRule struct {
	pattern string
	negate  bool
	dirOnly bool
	rooted  bool
}

func loadIgnoreMatcher(root string) ignoreMatcher {
	file, err := os.Open(filepath.Join(root, ".gitignore"))
	if err != nil {
		return ignoreMatcher{}
	}
	defer file.Close()
	matcher := ignoreMatcher{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
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
			rule.dirOnly = true
			line = strings.TrimSuffix(line, "/")
		}
		if line == "" {
			continue
		}
		rule.pattern = filepath.ToSlash(line)
		matcher.rules = append(matcher.rules, rule)
	}
	return matcher
}

func (m ignoreMatcher) Ignored(rel string, isDir bool) bool {
	rel = filepath.ToSlash(rel)
	ignored := false
	for _, rule := range m.rules {
		if rule.dirOnly && !isDir {
			continue
		}
		if matchIgnoreRule(rule, rel) {
			ignored = !rule.negate
		}
	}
	return ignored
}

func matchIgnoreRule(rule ignoreRule, rel string) bool {
	pattern := rule.pattern
	if rule.rooted {
		return globMatch(pattern, rel)
	}
	if globMatch(pattern, rel) || globMatch(pattern, filepath.Base(rel)) {
		return true
	}
	parts := strings.Split(rel, "/")
	for i := range parts {
		suffix := strings.Join(parts[i:], "/")
		if globMatch(pattern, suffix) {
			return true
		}
	}
	return false
}

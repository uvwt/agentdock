package tools

import "testing"

func TestParseGitStatus(t *testing.T) {
	branch, upstream, ahead, behind, files := parseGitStatus("## main...origin/main [ahead 2, behind 1]\n M README.md\n?? internal/tools/new.go\n")
	if branch != "main" {
		t.Fatalf("branch = %q", branch)
	}
	if upstream != "origin/main" {
		t.Fatalf("upstream = %q", upstream)
	}
	if ahead != 2 || behind != 1 {
		t.Fatalf("ahead/behind = %d/%d", ahead, behind)
	}
	if len(files) != 2 {
		t.Fatalf("files len = %d", len(files))
	}
}

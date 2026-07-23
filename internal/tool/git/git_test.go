package git

import "testing"

func TestParseGitStatus(t *testing.T) {
	branch, upstream, ahead, behind, files := parseGitStatus("## main...origin/main [ahead 2, behind 1]\n M README.md\n?? internal/app/new.go\n")
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
	if files[0].Path != "README.md" || files[0].Status != "M" {
		t.Fatalf("first file = %#v", files[0])
	}
	if files[1].Path != "internal/app/new.go" || files[1].Status != "??" {
		t.Fatalf("second file = %#v", files[1])
	}
}

func TestParseDiffFiles(t *testing.T) {
	files := parseDiffFiles("diff --git a/a.txt b/a.txt\nnew file mode 100644\ndiff --git a/b.png b/b.png\nBinary files a/b.png and b/b.png differ\n")
	if len(files) != 2 {
		t.Fatalf("files len = %d", len(files))
	}
	if files[0].Path != "a.txt" || files[0].Status != "added" || files[0].Binary {
		t.Fatalf("first diff file = %#v", files[0])
	}
	if files[1].Path != "b.png" || files[1].Status != "modified" || !files[1].Binary {
		t.Fatalf("second diff file = %#v", files[1])
	}
}

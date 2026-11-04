package file

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseDiffFilesPaths(t *testing.T) {
	for _, tt := range []struct {
		name, patch, path, status string
	}{
		{"plain", "diff --git a/plain.txt b/plain.txt\n", "plain.txt", "modified"},
		{"spaces", "diff --git a/my note.txt b/my note.txt\n", "my note.txt", "modified"},
		{"raw unicode", "diff --git a/中文.txt b/中文.txt\n", "中文.txt", "modified"},
		{"quoted unicode", "diff --git \"a/\\344\\270\\255.txt\" \"b/\\344\\270\\255.txt\"\n", "中.txt", "modified"},
		{"escaped characters", "diff --git \"a/tab\\tline\\n\\\".txt\" \"b/tab\\tline\\n\\\".txt\"\n", "tab\tline\n\".txt", "modified"},
		{"mode only", "diff --git a/dir b/note name.txt b/dir b/note name.txt\nold mode 100644\nnew mode 100755\n", "dir b/note name.txt", "modified"},
		{"quoted mode only", "diff --git \"a/\\344\\270\\255.txt\" \"b/\\344\\270\\255.txt\"\nold mode 100644\nnew mode 100755\n", "中.txt", "modified"},
		{"quoted source", "diff --git \"a/\\344\\270\\255.txt\" b/target.txt\n", "target.txt", "modified"},
		{"trailing space", "diff --git a/name  b/name \n", "name ", "modified"},
		{"added", "diff --git a/a new.txt b/a new.txt\nnew file mode 100644\n--- /dev/null\n+++ b/a new.txt\t\n", "a new.txt", "added"},
		{"deleted", "diff --git a/an old.txt b/an old.txt\ndeleted file mode 100644\n--- a/an old.txt\t\n+++ /dev/null\n", "an old.txt", "deleted"},
		{"deleted timestamp", "diff --git a/an old.txt b/an old.txt\ndeleted file mode 100644\n--- a/an old.txt\tdate\n+++ /dev/null\tdate\n", "an old.txt", "deleted"},
		{"rename", "diff --git a/old name.txt b/new name.txt\nsimilarity index 100%\nrename from old name.txt\nrename to new name.txt\n", "new name.txt", "modified"},
		{"quoted rename", "diff --git a/old.txt \"b/\\344\\270\\255.txt\"\nsimilarity index 100%\nrename from old.txt\nrename to \"\\344\\270\\255.txt\"\n", "中.txt", "modified"},
		{"copy", "diff --git a/dir b/source note.txt b/copy destination.txt\nsimilarity index 100%\ncopy from dir b/source note.txt\ncopy to copy destination.txt\n", "copy destination.txt", "modified"},
		{"quoted copy", "diff --git a/old.txt \"b/\\344\\270\\255.txt\"\nsimilarity index 100%\ncopy from old.txt\ncopy to \"\\344\\270\\255.txt\"\n", "中.txt", "modified"},
		{"prefix inside name", "diff --git a/b/file name.txt b/b/file name.txt\n", "b/file name.txt", "modified"},
		{"hunk is not header", "diff --git a/real name.txt b/real name.txt\n--- a/real name.txt\t\n+++ b/real name.txt\t\n@@ -1 +1 @@\n--- a/fake.txt\n+++ b/fake.txt\n", "real name.txt", "modified"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			want := []gitDiffFile{{Path: tt.path, Status: tt.status}}
			if got := parseDiffFiles(tt.patch); !reflect.DeepEqual(got, want) {
				t.Fatalf("parseDiffFiles() = %#v, want %#v", got, want)
			}
		})
	}
}

func TestParseDiffFilesKeepsEntriesAndBinaryStatus(t *testing.T) {
	patch := "diff --git a/a file.bin b/a file.bin\nnew file mode 100644\nBinary files /dev/null and b/a file.bin differ\n" +
		"diff --git a/b.txt b/b.txt\ndeleted file mode 100644\n--- a/b.txt\n+++ /dev/null\n"
	want := []gitDiffFile{{Path: "a file.bin", Status: "added", Binary: true}, {Path: "b.txt", Status: "deleted"}}
	if got := parseDiffFiles(patch); !reflect.DeepEqual(got, want) {
		t.Fatalf("parseDiffFiles() = %#v, want %#v", got, want)
	}
}

func TestFileEditGitPatchAffectedPaths(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	for _, quote := range []string{"true", "false"} {
		for _, action := range []string{"modify plain", "modify space", "modify unicode", "add", "delete", "rename", "copy", "copy unicode"} {
			t.Run(quote+"/"+action, func(t *testing.T) {
				svc, root := newCodeToolsRuntime(t)
				git := func(args ...string) []byte {
					t.Helper()
					all := append([]string{"-C", root, "-c", "core.quotePath=" + quote}, args...)
					out, err := exec.Command("git", all...).Output()
					if err != nil {
						t.Fatalf("git %v: %v", args, err)
					}
					return out
				}
				git("init", "--quiet")
				git("config", "core.autocrlf", "false")
				git("config", "core.eol", "lf")
				name := "a 中文 note.txt"
				if action == "modify plain" {
					name = "plain.txt"
				} else if action == "modify space" {
					name = "my note.txt"
				} else if action == "modify unicode" {
					name = "中文.txt"
				} else if action == "copy" || action == "copy unicode" {
					name = "dir b/source note.txt"
				}
				write := func(path, content string) {
					t.Helper()
					if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0600); err != nil {
						t.Fatal(err)
					}
				}
				write(name, "old\n")
				git("add", "--", name)
				git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "--quiet", "-m", "fixture")
				wantPath, status := name, "modified"
				switch action {
				case "add":
					wantPath, status = "added 中文 note.txt", "added"
					write(wantPath, "new\n")
				case "delete":
					status = "deleted"
					if err := os.Remove(filepath.Join(root, name)); err != nil {
						t.Fatal(err)
					}
				case "rename":
					wantPath = "renamed 中文 note.txt"
					if err := os.Rename(filepath.Join(root, name), filepath.Join(root, wantPath)); err != nil {
						t.Fatal(err)
					}
				case "copy", "copy unicode":
					wantPath = "copy destination.txt"
					if action == "copy unicode" {
						wantPath = "copied 中文 note.txt"
					}
					write(wantPath, "old\n")
				default:
					write(name, "new\n")
				}
				git("add", "--all")
				patch := string(git("diff", "--cached", "--no-ext-diff", "--no-color", "--find-renames", "--find-copies", "--find-copies-harder"))
				git("reset", "--hard", "--quiet", "HEAD")
				for _, dry := range []bool{true, false} {
					result, err := svc.Edit(context.Background(), EditRequest{Action: "patch", Patch: patch, DryRun: dry})
					if err != nil {
						t.Fatal(err)
					}
					want := []gitDiffFile{{Path: wantPath, Status: status}}
					if got := result["affected_files"]; !reflect.DeepEqual(got, want) {
						t.Errorf("dry_run=%v affected_files=%#v, want %#v", dry, got, want)
					}
					if dry {
						data, err := os.ReadFile(filepath.Join(root, name))
						if err != nil || string(data) != "old\n" {
							t.Fatalf("dry-run changed original: %q, %v", data, err)
						}
						if wantPath != name {
							if _, err := os.Stat(filepath.Join(root, wantPath)); !os.IsNotExist(err) {
								t.Fatalf("dry-run created target: %v", err)
							}
						}
					} else if action == "delete" {
						if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
							t.Fatalf("delete target still exists: %v", err)
						}
					} else {
						data, err := os.ReadFile(filepath.Join(root, wantPath))
						wantContent := "new\n"
						if action == "rename" || action == "copy" || action == "copy unicode" {
							wantContent = "old\n"
						}
						if err != nil || string(data) != wantContent {
							t.Fatalf("applied target=%q, %v; want %q", data, err, wantContent)
						}
						if action == "rename" {
							if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
								t.Fatalf("rename source still exists: %v", err)
							}
						}
						if action == "copy" || action == "copy unicode" {
							data, err := os.ReadFile(filepath.Join(root, name))
							if err != nil || string(data) != "old\n" {
								t.Fatalf("copy changed source: %q, %v", data, err)
							}
						}
					}
				}
			})
		}
	}
}

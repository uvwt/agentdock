package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/uvwt/agentdock/internal/workspace"
)

func TestListGitReposIncludesStartingRepository(t *testing.T) {
	service, root := newGitServiceForTest(t)
	initGitRepository(t, root)

	result, err := service.listGitRepos(context.Background(), map[string]any{"path": "."})
	if err != nil {
		t.Fatalf("listGitRepos() error = %v", err)
	}
	repos, ok := result["repos"].([]gitRepoSummary)
	if !ok || len(repos) != 1 {
		t.Fatalf("repos = %#v, want one repository", result["repos"])
	}
	if repos[0].Path != "." || repos[0].Branch != "main" || !repos[0].Clean {
		t.Fatalf("repository summary = %#v", repos[0])
	}
}

func TestListGitReposHonorsMaximumDepth(t *testing.T) {
	service, root := newGitServiceForTest(t)
	nested := filepath.Join(root, "group", "repository")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	initGitRepository(t, nested)

	shallow, err := service.listGitRepos(context.Background(), map[string]any{"path": ".", "max_depth": 1})
	if err != nil {
		t.Fatalf("shallow listGitRepos() error = %v", err)
	}
	if got := shallow["count"]; got != 0 {
		t.Fatalf("shallow count = %#v, want 0", got)
	}
	deep, err := service.listGitRepos(context.Background(), map[string]any{"path": ".", "max_depth": 2})
	if err != nil {
		t.Fatalf("deep listGitRepos() error = %v", err)
	}
	repos := deep["repos"].([]gitRepoSummary)
	if len(repos) != 1 || repos[0].Path != "group/repository" {
		t.Fatalf("deep repositories = %#v", repos)
	}
}

func TestListGitReposHonorsCanceledContext(t *testing.T) {
	service, _ := newGitServiceForTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := service.listGitRepos(ctx, map[string]any{"path": "."})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("listGitRepos() error = %v, want context.Canceled", err)
	}
}

func TestGitBlameAllowsChildNameBeginningWithTwoDots(t *testing.T) {
	service, root := newGitServiceForTest(t)
	initGitRepository(t, root)
	path := filepath.Join(root, "..config")
	if err := os.WriteFile(path, []byte("value=true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitForTest(t, root, "add", "..config")
	runGitForTest(t, root, "commit", "-m", "test: add dot config")

	result, err := service.gitBlame(context.Background(), map[string]any{"repo_path": ".", "path": "..config"})
	if err != nil {
		t.Fatalf("gitBlame() error = %v", err)
	}
	if !boolValue(result["command_ok"]) || result["path"] != "..config" {
		t.Fatalf("gitBlame() result = %#v", result)
	}
}

func TestGitMetadataQueriesPropagateEnvironmentFailure(t *testing.T) {
	root := t.TempDir()
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	service := New(ws, func(string, map[string]any) ([]string, error) {
		return nil, errors.New("environment unavailable")
	})
	initGitRepository(t, root)
	repo := gitRepo{Path: ".", Abs: root}
	if _, err := service.currentBranch(context.Background(), repo); err == nil {
		t.Fatal("currentBranch() error = nil, want environment failure")
	}
	if _, err := service.gitRemoteURL(context.Background(), repo, "origin"); err == nil {
		t.Fatal("gitRemoteURL() error = nil, want environment failure")
	}
}

func TestRelativePathDepthAndOutsideRoot(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "workspace")
	depth, err := relativePathDepth(root, filepath.Join(root, "one", "two"))
	if err != nil {
		t.Fatal(err)
	}
	if depth != 2 {
		t.Fatalf("depth = %d, want 2", depth)
	}
	for _, path := range []string{"..", filepath.Join("..", "outside"), filepath.Join(string(filepath.Separator), "absolute")} {
		if !pathOutsideRoot(path) {
			t.Fatalf("pathOutsideRoot(%q) = false, want true", path)
		}
	}
	for _, path := range []string{".", "..config", filepath.Join("nested", "file")} {
		if pathOutsideRoot(path) {
			t.Fatalf("pathOutsideRoot(%q) = true, want false", path)
		}
	}
}

func newGitServiceForTest(t *testing.T) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	service := New(ws, func(string, map[string]any) ([]string, error) {
		return append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1"), nil
	})
	return service, root
}

func initGitRepository(t *testing.T, path string) {
	t.Helper()
	runGitForTest(t, path, "init", "-b", "main")
	runGitForTest(t, path, "config", "user.name", "AgentDock Test")
	runGitForTest(t, path, "config", "user.email", "agentdock@example.invalid")
	if err := os.WriteFile(filepath.Join(path, "README.md"), []byte("# test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitForTest(t, path, "add", "README.md")
	runGitForTest(t, path, "commit", "-m", "test: initialize repository")
}

func runGitForTest(t *testing.T, path string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = path
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

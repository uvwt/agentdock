package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/textutil"
)

type gitRepo struct {
	Path string
	Abs  string
}

func (r *Runtime) resolveGitRepo(args map[string]any) (gitRepo, error) {
	// 设计背景：/workspace 是多项目工作区，里面可能同时有多个 Git 仓库。
	// Git 工具不能假设 /workspace 本身就是仓库，所以统一通过 repo_path
	// 选择具体项目；未传 repo_path 时才退回默认 cwd，保持兼容旧调用方式。
	raw := stringArg(args, "repo_path", "")
	if raw == "" {
		raw = stringArg(args, "workdir", "")
	}
	if raw == "" {
		raw = "."
	}
	p, err := r.ws.ResolveExisting(raw)
	if err != nil {
		return gitRepo{}, err
	}
	info, err := os.Stat(p.Abs)
	if err != nil {
		return gitRepo{}, err
	}
	if !info.IsDir() {
		return gitRepo{}, toolError("NOT_A_DIRECTORY", "repo_path is not a directory", "validation")
	}
	if _, err := os.Stat(filepath.Join(p.Abs, ".git")); err != nil {
		return gitRepo{}, toolErrorDetails("NOT_A_GIT_REPOSITORY", "repo_path is not a git repository", "validation", map[string]any{"repo_path": p.Display, "suggestion": "pass repo_path for one project under the workspace, or call workspace_repos"})
	}
	return gitRepo{Path: p.Display, Abs: p.Abs}, nil
}

func (r *Runtime) gitInRepo(ctx context.Context, repo gitRepo, maxBytes int, args ...string) (Result, error) {
	result, err := r.gitInDir(ctx, repo.Abs, maxBytes, args...)
	if result != nil {
		result["repo_path"] = repo.Path
	}
	return result, err
}

func (r *Runtime) gitInDir(ctx context.Context, dir string, maxBytes int, args ...string) (Result, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "git", args...)
	cmd.Dir = dir
	cmd.Env = r.commandEnv(nil)
	output, err := cmd.CombinedOutput()
	text, truncated := truncateBytes(output, maxBytes)
	text = redactSecrets(text, nil)
	result := Result{"ok": err == nil, "command": "git " + strings.Join(args, " "), "output": text, "truncated": truncated}
	if err != nil {
		result["error"] = err.Error()
		if diag := diagnoseGitOutput(text); diag != nil {
			result["diagnostic"] = diag
		}
	}
	return result, nil
}

func (r *Runtime) currentBranch(ctx context.Context, repo gitRepo) string {
	result, _ := r.gitInRepo(ctx, repo, 4096, "branch", "--show-current")
	return strings.TrimSpace(fmt.Sprint(result["output"]))
}

func (r *Runtime) gitRemoteURL(ctx context.Context, repo gitRepo, remote string) string {
	if remote == "" {
		remote = "origin"
	}
	result, _ := r.gitInRepo(ctx, repo, 4096, "remote", "get-url", remote)
	if !boolValue(result["ok"]) {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(result["output"]))
}

func boolValue(value any) bool {
	b, _ := value.(bool)
	return b
}

func truncateBytes(data []byte, maxBytes int) (string, bool) {
	truncated := textutil.SafeTruncateBytes(data, maxBytes)
	return truncated.Text, truncated.Truncated
}

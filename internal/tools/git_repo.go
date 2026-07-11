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
	// 设计背景：~/AgentDock 是默认工作目录，里面可能同时有多个 Git 仓库。
	// Git 工具统一通过 repo_path 选择具体项目；未传时使用默认工作目录。
	raw := stringArg(args, "repo_path", "")
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
		return gitRepo{}, toolErrorDetails("NOT_A_GIT_REPOSITORY", "repo_path is not a git repository", "validation", map[string]any{"repo_path": p.Display, "suggestion": "pass repo_path for a concrete repository, or call git_read action=repos"})
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
	commandEnv, err := r.commandEnv(nil)
	if err != nil {
		return nil, err
	}
	cmd.Env = commandEnv
	maxBytes = boundedInt(maxBytes, 65536, 1, 8<<20)
	output, outputTotal, outputTruncated, err := runBoundedCombinedOutput(cmd, maxBytes)
	text, responseTruncated := truncateBytes(output, maxBytes)
	text = redactSecrets(text, nil)
	result := Result{
		"ok":                 err == nil,
		"command":            "git " + strings.Join(args, " "),
		"output":             text,
		"truncated":          outputTruncated || responseTruncated,
		"output_total_bytes": outputTotal,
	}
	if err != nil {
		result["error"] = err.Error()
		if diag := diagnoseGitOutput(text); diag != nil {
			result["diagnostic"] = diag
		}
	}
	return result, nil
}

func (r *Runtime) currentBranch(ctx context.Context, repo gitRepo) (string, error) {
	result, err := r.gitInRepo(ctx, repo, 4096, "branch", "--show-current")
	if err != nil {
		return "", err
	}
	if !boolValue(result["ok"]) {
		return "", nil
	}
	return strings.TrimSpace(fmt.Sprint(result["output"])), nil
}

func (r *Runtime) gitRemoteURL(ctx context.Context, repo gitRepo, remote string) (string, error) {
	if remote == "" {
		remote = "origin"
	}
	result, err := r.gitInRepo(ctx, repo, 4096, "remote", "get-url", remote)
	if err != nil {
		return "", err
	}
	if !boolValue(result["ok"]) {
		return "", nil
	}
	return strings.TrimSpace(fmt.Sprint(result["output"])), nil
}

func boolValue(value any) bool {
	b, _ := value.(bool)
	return b
}

func pathOutsideRoot(relative string) bool {
	cleaned := filepath.Clean(relative)
	return filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator))
}

func truncateBytes(data []byte, maxBytes int) (string, bool) {
	truncated := textutil.SafeTruncateBytes(data, maxBytes)
	return truncated.Text, truncated.Truncated
}

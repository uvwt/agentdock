package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (r *Runtime) gitRepoStatus(ctx context.Context, args map[string]any) (Result, error) {
	repo, err := r.resolveGitRepo(args)
	if err != nil {
		return nil, err
	}
	result, err := r.gitInRepo(ctx, repo, intArg(args, "max_output_bytes", 65536), "status", "--short", "--branch")
	if err != nil {
		return nil, err
	}
	output, _ := result["output"].(string)
	branch, upstream, ahead, behind, files := parseGitStatus(output)
	result["repo_path"] = repo.Path
	result["branch"] = branch
	result["upstream"] = upstream
	result["ahead"] = ahead
	result["behind"] = behind
	result["files"] = files
	result["clean"] = len(files) == 0
	if remote := r.gitRemoteURL(ctx, repo, "origin"); remote != "" {
		result["remote"] = redactSecrets(remote, nil)
	}
	return result, nil
}

func (r *Runtime) gitDiff(ctx context.Context, args map[string]any) (Result, error) {
	repo, err := r.resolveGitRepo(args)
	if err != nil {
		return nil, err
	}
	gitArgs := append([]string{"diff", "--"}, stringSliceArg(args, "paths")...)
	result, err := r.gitInRepo(ctx, repo, intArg(args, "max_bytes", 262144), gitArgs...)
	if err != nil {
		return nil, err
	}
	output, _ := result["output"].(string)
	result["repo_path"] = repo.Path
	result["files"] = parseDiffFiles(output)
	return result, nil
}

func (r *Runtime) gitLog(ctx context.Context, args map[string]any) (Result, error) {
	repo, err := r.resolveGitRepo(args)
	if err != nil {
		return nil, err
	}
	limit := intArg(args, "limit", 20)
	if limit < 1 {
		limit = 1
	}
	if limit > 200 {
		limit = 200
	}
	result, err := r.gitInRepo(ctx, repo, intArg(args, "max_bytes", 65536), "log", "--date=iso-strict", "--pretty=format:%H%x09%an%x09%ad%x09%s", "-n", strconv.Itoa(limit))
	if err != nil {
		return nil, err
	}
	output, _ := result["output"].(string)
	commits := make([]map[string]any, 0)
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 4)
		commit := map[string]any{"raw": line}
		if len(parts) > 0 {
			commit["hash"] = parts[0]
		}
		if len(parts) > 1 {
			commit["author"] = parts[1]
		}
		if len(parts) > 2 {
			commit["date"] = parts[2]
		}
		if len(parts) > 3 {
			commit["subject"] = parts[3]
		}
		commits = append(commits, commit)
	}
	result["repo_path"] = repo.Path
	result["commits"] = commits
	return result, nil
}

func (r *Runtime) gitShow(ctx context.Context, args map[string]any) (Result, error) {
	repo, err := r.resolveGitRepo(args)
	if err != nil {
		return nil, err
	}
	result, err := r.gitInRepo(ctx, repo, intArg(args, "max_bytes", 262144), "show", "--stat", "--patch", stringArg(args, "rev", "HEAD"))
	if result != nil {
		result["repo_path"] = repo.Path
	}
	return result, err
}

func (r *Runtime) gitBlame(ctx context.Context, args map[string]any) (Result, error) {
	repo, err := r.resolveGitRepo(args)
	if err != nil {
		return nil, err
	}
	p, err := r.ws.ResolveExisting(stringArg(args, "path", ""))
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(repo.Abs, p.Abs)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return nil, toolErrorDetails("PATH_OUTSIDE_REPOSITORY", "path is outside repo_path", "validation", map[string]any{"repo_path": repo.Path, "path": p.Display})
	}
	result, err := r.gitInRepo(ctx, repo, intArg(args, "max_bytes", 262144), "blame", "--line-porcelain", "--", filepath.ToSlash(rel))
	if result != nil {
		result["repo_path"] = repo.Path
		result["path"] = filepath.ToSlash(rel)
	}
	return result, err
}

func (r *Runtime) gitFetch(ctx context.Context, args map[string]any) (Result, error) {
	repo, err := r.resolveGitRepo(args)
	if err != nil {
		return nil, err
	}
	remote := stringArg(args, "remote", "origin")
	return r.gitInRepo(ctx, repo, intArg(args, "max_bytes", 65536), "fetch", remote)
}

func (r *Runtime) gitPull(ctx context.Context, args map[string]any) (Result, error) {
	repo, err := r.resolveGitRepo(args)
	if err != nil {
		return nil, err
	}
	remote := stringArg(args, "remote", "origin")
	branch := stringArg(args, "branch", "")
	gitArgs := []string{"pull", remote}
	if branch != "" {
		gitArgs = append(gitArgs, branch)
	}
	return r.gitInRepo(ctx, repo, intArg(args, "max_bytes", 65536), gitArgs...)
}

func (r *Runtime) gitPush(ctx context.Context, args map[string]any) (Result, error) {
	repo, err := r.resolveGitRepo(args)
	if err != nil {
		return nil, err
	}
	remote := stringArg(args, "remote", "origin")
	branch := stringArg(args, "branch", "")
	if branch == "" {
		branch = r.currentBranch(ctx, repo)
	}
	if branch == "" {
		branch = "HEAD"
	}
	gitArgs := []string{"push", remote, branch}
	result, err := r.gitInRepo(ctx, repo, intArg(args, "max_bytes", 65536), gitArgs...)
	if result != nil {
		result["repo_path"] = repo.Path
		result["remote"] = remote
		result["branch"] = branch
		annotateGitPushResult(result)
	}
	return result, err
}

func annotateGitPushResult(result Result) {
	// Git credential helper 有时会把非阻塞问题打印成 fatal，但 git push
	// 自身仍然 exit 0 并完成远端更新。这里把“命令成功”和“输出警告”拆开，
	// 避免调用方只看到 fatal 文本就误判推送失败。
	output, _ := result["output"].(string)
	ok := boolValue(result["ok"])
	remoteUpdated := false
	upToDate := false
	warnings := make([]string, 0)
	for _, line := range strings.Split(output, "\n") {
		clean := strings.TrimSpace(line)
		if clean == "" {
			continue
		}
		lower := strings.ToLower(clean)
		if strings.HasPrefix(lower, "warning:") || strings.HasPrefix(lower, "fatal:") {
			warnings = append(warnings, clean)
		}
		if strings.Contains(lower, "everything up-to-date") || strings.Contains(lower, "everything up to date") {
			upToDate = true
		}
		if strings.Contains(clean, "->") && (strings.Contains(clean, "..") || strings.Contains(clean, "[new branch]") || strings.Contains(clean, "[deleted]") || strings.Contains(clean, "+") || strings.Contains(clean, "*")) {
			remoteUpdated = true
		}
	}

	fatalButNonBlocking := false
	if ok {
		for _, warning := range warnings {
			if strings.HasPrefix(strings.ToLower(warning), "fatal:") {
				fatalButNonBlocking = true
				break
			}
		}
	}

	status := "failed"
	if ok && remoteUpdated {
		status = "pushed"
	} else if ok && upToDate {
		status = "up_to_date"
	} else if ok {
		status = "succeeded"
	}

	result["push_succeeded"] = ok
	result["pushed"] = ok
	result["remote_updated"] = remoteUpdated
	result["up_to_date"] = upToDate
	result["warnings"] = warnings
	result["fatal_but_non_blocking"] = fatalButNonBlocking
	result["push_status"] = status
}

func (r *Runtime) gitCommit(ctx context.Context, args map[string]any) (Result, error) {
	repo, err := r.resolveGitRepo(args)
	if err != nil {
		return nil, err
	}
	message := stringArg(args, "message", "")
	if message == "" {
		return nil, toolError("INVALID_ARGUMENT", "message is required", "validation")
	}
	paths := stringSliceArg(args, "paths")
	if boolArg(args, "all", false) {
		if result, err := r.gitInRepo(ctx, repo, intArg(args, "max_bytes", 65536), "add", "-A"); err != nil || !boolValue(result["ok"]) {
			return result, err
		}
	} else if len(paths) > 0 {
		gitArgs := append([]string{"add", "--"}, paths...)
		if result, err := r.gitInRepo(ctx, repo, intArg(args, "max_bytes", 65536), gitArgs...); err != nil || !boolValue(result["ok"]) {
			return result, err
		}
	}
	result, err := r.gitInRepo(ctx, repo, intArg(args, "max_bytes", 65536), "commit", "-m", message)
	if result != nil {
		result["repo_path"] = repo.Path
	}
	return result, err
}

func (r *Runtime) gitClone(ctx context.Context, args map[string]any) (Result, error) {
	urlValue := stringArg(args, "url", "")
	if urlValue == "" {
		urlValue = stringArg(args, "repo", "")
	}
	if urlValue == "" {
		return nil, toolError("INVALID_ARGUMENT", "url is required", "validation")
	}
	dest := stringArg(args, "dest", "")
	if dest == "" {
		dest = strings.TrimSuffix(filepath.Base(urlValue), ".git")
	}
	p, err := r.ws.ResolveForWrite(dest)
	if err != nil {
		return nil, err
	}
	gitArgs := []string{"clone"}
	if depth := intArg(args, "depth", 0); depth > 0 {
		gitArgs = append(gitArgs, "--depth", strconv.Itoa(depth))
	}
	if branch := stringArg(args, "branch", ""); branch != "" {
		gitArgs = append(gitArgs, "--branch", branch)
	}
	gitArgs = append(gitArgs, urlValue, p.Display)
	result, err := r.gitInDir(ctx, r.ws.Root(), intArg(args, "max_bytes", 65536), gitArgs...)
	if result != nil {
		result["dest"] = p.Display
	}
	return result, err
}

func (r *Runtime) listGitRepos(ctx context.Context, args map[string]any) (Result, error) {
	// 给模型一个轻量的“仓库地图”。这样处理多项目工作区时，模型可以先
	// 发现有哪些 repo、各自分支和 clean/ahead 状态，再对指定 repo_path 操作，
	// 减少反复 ls + git status 的探测步骤，也避免误操作到错误项目。
	start, err := r.ws.ResolveExisting(stringArg(args, "path", "."))
	if err != nil {
		return nil, err
	}
	maxDepth := intArg(args, "max_depth", 3)
	if maxDepth < 1 {
		maxDepth = 1
	}
	repos := make([]map[string]any, 0)
	rootDepth := len(strings.Split(filepath.Clean(start.Abs), string(os.PathSeparator)))
	_ = filepath.WalkDir(start.Abs, func(abs string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if abs == start.Abs {
			return nil
		}
		if entry.IsDir() && shouldSkipDir(entry.Name()) && entry.Name() != ".git" {
			return filepath.SkipDir
		}
		depth := len(strings.Split(filepath.Clean(abs), string(os.PathSeparator))) - rootDepth
		if depth > maxDepth {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		if _, err := os.Stat(filepath.Join(abs, ".git")); err != nil {
			return nil
		}
		rel, _ := r.ws.Relative(abs)
		repo := gitRepo{Path: rel, Abs: abs}
		branch := r.currentBranch(ctx, repo)
		status, _ := r.gitInRepo(ctx, repo, 65536, "status", "--short", "--branch")
		_, upstream, ahead, behind, files := parseGitStatus(fmt.Sprint(status["output"]))
		repos = append(repos, map[string]any{"path": rel, "branch": branch, "upstream": upstream, "ahead": ahead, "behind": behind, "clean": len(files) == 0, "remote": redactSecrets(r.gitRemoteURL(ctx, repo, "origin"), nil)})
		return filepath.SkipDir
	})
	return Result{"ok": true, "path": start.Display, "repos": repos, "count": len(repos)}, nil
}

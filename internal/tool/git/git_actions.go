package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type gitLogCommit struct {
	Raw     string `json:"raw"`
	Hash    string `json:"hash,omitempty"`
	Author  string `json:"author,omitempty"`
	Date    string `json:"date,omitempty"`
	Subject string `json:"subject,omitempty"`
}

type gitRepoSummary struct {
	Path     string `json:"path"`
	Branch   string `json:"branch"`
	Upstream string `json:"upstream"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
	Clean    bool   `json:"clean"`
	Remote   string `json:"remote"`
}

func parseGitLogCommit(line string) gitLogCommit {
	commit := gitLogCommit{Raw: line}
	parts := strings.SplitN(line, "	", 4)
	if len(parts) > 0 {
		commit.Hash = parts[0]
	}
	if len(parts) > 1 {
		commit.Author = parts[1]
	}
	if len(parts) > 2 {
		commit.Date = parts[2]
	}
	if len(parts) > 3 {
		commit.Subject = parts[3]
	}
	return commit
}

func (s *Service) gitRepoStatus(ctx context.Context, args map[string]any) (Result, error) {
	repo, err := s.resolveGitRepo(args)
	if err != nil {
		return nil, err
	}
	result, err := s.gitInRepo(ctx, repo, intArg(args, "max_bytes", 65536), "status", "--short", "--branch")
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
	remote, err := s.gitRemoteURL(ctx, repo, "origin")
	if err != nil {
		return nil, err
	}
	if remote != "" {
		result["remote"] = redactSecrets(remote, nil)
	}
	return result, nil
}

func (s *Service) gitDiff(ctx context.Context, args map[string]any) (Result, error) {
	repo, err := s.resolveGitRepo(args)
	if err != nil {
		return nil, err
	}
	gitArgs := append([]string{"diff", "--"}, stringSliceArg(args, "paths")...)
	result, err := s.gitInRepo(ctx, repo, intArg(args, "max_bytes", 262144), gitArgs...)
	if err != nil {
		return nil, err
	}
	output, _ := result["output"].(string)
	result["repo_path"] = repo.Path
	result["files"] = parseDiffFiles(output)
	return result, nil
}

func (s *Service) gitLog(ctx context.Context, args map[string]any) (Result, error) {
	repo, err := s.resolveGitRepo(args)
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
	result, err := s.gitInRepo(ctx, repo, intArg(args, "max_bytes", 65536), "log", "--date=iso-strict", "--pretty=format:%H%x09%an%x09%ad%x09%s", "-n", strconv.Itoa(limit))
	if err != nil {
		return nil, err
	}
	output, _ := result["output"].(string)
	commits := make([]gitLogCommit, 0)
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		commits = append(commits, parseGitLogCommit(line))
	}
	result["repo_path"] = repo.Path
	result["commits"] = commits
	return result, nil
}

func (s *Service) gitShow(ctx context.Context, args map[string]any) (Result, error) {
	repo, err := s.resolveGitRepo(args)
	if err != nil {
		return nil, err
	}
	result, err := s.gitInRepo(ctx, repo, intArg(args, "max_bytes", 262144), "show", "--stat", "--patch", stringArg(args, "rev", "HEAD"))
	if result != nil {
		result["repo_path"] = repo.Path
	}
	return result, err
}

func (s *Service) gitBlame(ctx context.Context, args map[string]any) (Result, error) {
	repo, err := s.resolveGitRepo(args)
	if err != nil {
		return nil, err
	}
	p, err := s.ws.ResolveExisting(stringArg(args, "path", ""))
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(repo.Abs, p.Abs)
	if err != nil || pathOutsideRoot(rel) {
		return nil, toolErrorDetails("PATH_OUTSIDE_REPOSITORY", "path is outside repo_path", "validation", map[string]any{"repo_path": repo.Path, "path": p.Display})
	}
	result, err := s.gitInRepo(ctx, repo, intArg(args, "max_bytes", 262144), "blame", "--line-porcelain", "--", filepath.ToSlash(rel))
	if result != nil {
		result["repo_path"] = repo.Path
		result["path"] = filepath.ToSlash(rel)
	}
	return result, err
}

func (s *Service) gitFetch(ctx context.Context, args map[string]any) (Result, error) {
	repo, err := s.resolveGitRepo(args)
	if err != nil {
		return nil, err
	}
	remote := stringArg(args, "remote", "origin")
	return s.gitInRepo(ctx, repo, intArg(args, "max_bytes", 65536), "fetch", remote)
}

func (s *Service) gitPull(ctx context.Context, args map[string]any) (Result, error) {
	repo, err := s.resolveGitRepo(args)
	if err != nil {
		return nil, err
	}
	remote := stringArg(args, "remote", "origin")
	branch := stringArg(args, "branch", "")
	gitArgs := []string{"pull", remote}
	if branch != "" {
		gitArgs = append(gitArgs, branch)
	}
	return s.gitInRepo(ctx, repo, intArg(args, "max_bytes", 65536), gitArgs...)
}

func (s *Service) gitPush(ctx context.Context, args map[string]any) (Result, error) {
	repo, err := s.resolveGitRepo(args)
	if err != nil {
		return nil, err
	}
	remote := stringArg(args, "remote", "origin")
	branch := stringArg(args, "branch", "")
	if branch == "" {
		branch, err = s.currentBranch(ctx, repo)
		if err != nil {
			return nil, err
		}
	}
	if branch == "" {
		branch = "HEAD"
	}
	gitArgs := []string{"push", remote, branch}
	result, err := s.gitInRepo(ctx, repo, intArg(args, "max_bytes", 65536), gitArgs...)
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
	commandOK := boolValue(result["command_ok"])
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
	if commandOK {
		for _, warning := range warnings {
			if strings.HasPrefix(strings.ToLower(warning), "fatal:") {
				fatalButNonBlocking = true
				break
			}
		}
	}

	status := "failed"
	if commandOK && remoteUpdated {
		status = "pushed"
	} else if commandOK && upToDate {
		status = "up_to_date"
	} else if commandOK {
		status = "succeeded"
	}

	result["push_succeeded"] = commandOK
	result["remote_updated"] = remoteUpdated
	result["up_to_date"] = upToDate
	result["warnings"] = warnings
	result["fatal_but_non_blocking"] = fatalButNonBlocking
	result["push_status"] = status
}

func (s *Service) gitCommit(ctx context.Context, args map[string]any) (Result, error) {
	repo, err := s.resolveGitRepo(args)
	if err != nil {
		return nil, err
	}
	message := stringArg(args, "message", "")
	if message == "" {
		return nil, toolError("INVALID_ARGUMENT", "message is required", "validation")
	}
	paths := stringSliceArg(args, "paths")
	if boolArg(args, "all", false) {
		if result, err := s.gitInRepo(ctx, repo, intArg(args, "max_bytes", 65536), "add", "-A"); err != nil || !boolValue(result["command_ok"]) {
			return result, err
		}
	} else if len(paths) > 0 {
		gitArgs := append([]string{"add", "--"}, paths...)
		if result, err := s.gitInRepo(ctx, repo, intArg(args, "max_bytes", 65536), gitArgs...); err != nil || !boolValue(result["command_ok"]) {
			return result, err
		}
	}
	result, err := s.gitInRepo(ctx, repo, intArg(args, "max_bytes", 65536), "commit", "-m", message)
	if result != nil {
		result["repo_path"] = repo.Path
	}
	return result, err
}

func (s *Service) gitClone(ctx context.Context, args map[string]any) (Result, error) {
	urlValue := stringArg(args, "url", "")
	if urlValue == "" {
		return nil, toolError("INVALID_ARGUMENT", "url is required", "validation")
	}
	dest := stringArg(args, "dest", "")
	if dest == "" {
		dest = strings.TrimSuffix(filepath.Base(urlValue), ".git")
	}
	p, err := s.ws.ResolveForWrite(dest)
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
	result, err := s.gitInDir(ctx, s.ws.Root(), intArg(args, "max_bytes", 65536), gitArgs...)
	if result != nil {
		result["dest"] = p.Display
	}
	return result, err
}

func (s *Service) listGitRepos(ctx context.Context, args map[string]any) (Result, error) {
	// 给模型一个轻量的“仓库地图”。这样处理多项目工作区时，模型可以先
	// 发现有哪些 repo、各自分支和 clean/ahead 状态，再对指定 repo_path 操作，
	// 减少反复 ls + git status 的探测步骤，也避免误操作到错误项目。
	start, err := s.ws.ResolveExisting(stringArg(args, "path", "."))
	if err != nil {
		return nil, err
	}
	maxDepth := intArg(args, "max_depth", 3)
	if maxDepth < 1 {
		maxDepth = 1
	}
	repos := make([]gitRepoSummary, 0)
	walkErr := filepath.WalkDir(start.Abs, func(abs string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			return nil
		}
		if abs != start.Abs && shouldSkipDir(entry.Name()) && entry.Name() != ".git" {
			return filepath.SkipDir
		}
		depth, err := relativePathDepth(start.Abs, abs)
		if err != nil {
			return err
		}
		if depth > maxDepth {
			return filepath.SkipDir
		}
		gitMetadata := filepath.Join(abs, ".git")
		if _, err := os.Stat(gitMetadata); err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("inspect git metadata %s: %w", gitMetadata, err)
		}
		summary, err := s.summarizeGitRepo(ctx, abs)
		if err != nil {
			return err
		}
		repos = append(repos, summary)
		return filepath.SkipDir
	})
	if walkErr != nil {
		return nil, fmt.Errorf("discover git repositories: %w", walkErr)
	}
	return Result{"path": start.Display, "repos": repos, "count": len(repos)}, nil
}

func relativePathDepth(root, path string) (int, error) {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return 0, fmt.Errorf("resolve repository depth: %w", err)
	}
	if relative == "." {
		return 0, nil
	}
	return len(strings.Split(filepath.Clean(relative), string(os.PathSeparator))), nil
}

func (s *Service) summarizeGitRepo(ctx context.Context, abs string) (gitRepoSummary, error) {
	display, err := s.ws.Relative(abs)
	if err != nil {
		return gitRepoSummary{}, fmt.Errorf("resolve repository path %s: %w", abs, err)
	}
	repo := gitRepo{Path: display, Abs: abs}
	status, err := s.gitInRepo(ctx, repo, 65536, "status", "--short", "--branch")
	if err != nil {
		return gitRepoSummary{}, fmt.Errorf("read git status for %s: %w", display, err)
	}
	if !boolValue(status["command_ok"]) {
		return gitRepoSummary{}, fmt.Errorf("read git status for %s: %v", display, status["command_error"])
	}
	branch, upstream, ahead, behind, files := parseGitStatus(fmt.Sprint(status["output"]))
	remote, err := s.gitRemoteURL(ctx, repo, "origin")
	if err != nil {
		return gitRepoSummary{}, fmt.Errorf("read git remote for %s: %w", display, err)
	}
	return gitRepoSummary{
		Path: display, Branch: branch, Upstream: upstream,
		Ahead: ahead, Behind: behind, Clean: len(files) == 0,
		Remote: redactSecrets(remote, nil),
	}, nil
}

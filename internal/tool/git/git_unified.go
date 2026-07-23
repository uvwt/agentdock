package git

import (
	"context"
	"strings"
)

var readActions = []string{"repos", "status", "diff", "log", "show", "blame", "github_repo_access"}

func (s *Service) Read(ctx context.Context, args map[string]any) (Result, error) {
	action := strings.ToLower(stringArg(args, "action", ""))
	if action == "" {
		return nil, toolErrorDetails("MISSING_ACTION", "git_read requires action", "validation", map[string]any{"allowed": readActions})
	}
	switch action {
	case "repos":
		result, err := s.listGitRepos(ctx, args)
		if result != nil {
			result["action"] = "repos"
		}
		return result, err
	case "status":
		result, err := s.gitRepoStatus(ctx, args)
		if result != nil {
			result["action"] = "status"
		}
		return result, err
	case "diff":
		result, err := s.gitDiff(ctx, args)
		if result != nil {
			result["action"] = "diff"
		}
		return result, err
	case "log":
		result, err := s.gitLog(ctx, args)
		if result != nil {
			result["action"] = "log"
		}
		return result, err
	case "show":
		result, err := s.gitShow(ctx, args)
		if result != nil {
			result["action"] = "show"
		}
		return result, err
	case "blame":
		result, err := s.gitBlame(ctx, args)
		if result != nil {
			result["action"] = "blame"
		}
		return result, err
	case "github_repo_access":
		result, err := s.gitHubRepoAccess(ctx, args)
		if result != nil {
			result["action"] = "github_repo_access"
		}
		return result, err
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported git_read action", "validation", map[string]any{"action": action, "allowed": readActions})
	}
}

func (s *Service) Write(ctx context.Context, args map[string]any) (Result, error) {
	action := strings.ToLower(stringArg(args, "action", ""))
	switch action {
	case "clone":
		result, err := s.gitClone(ctx, args)
		if result != nil {
			result["action"] = "clone"
		}
		return result, err
	case "commit":
		result, err := s.gitCommit(ctx, args)
		if result != nil {
			result["action"] = "commit"
		}
		return result, err
	case "fetch":
		result, err := s.gitFetch(ctx, args)
		if result != nil {
			result["action"] = "fetch"
		}
		return result, err
	case "pull":
		result, err := s.gitPull(ctx, args)
		if result != nil {
			result["action"] = "pull"
		}
		return result, err
	case "push":
		result, err := s.gitPush(ctx, args)
		if result != nil {
			result["action"] = "push"
		}
		return result, err
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported git_write action", "validation", map[string]any{"action": action, "allowed": []string{"clone", "commit", "fetch", "pull", "push"}})
	}
}

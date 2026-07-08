package tools

import (
	"context"
	"strings"
)

func (r *Runtime) gitRead(ctx context.Context, args map[string]any) (Result, error) {
	action := strings.ToLower(stringArg(args, "action", ""))
	if action == "" {
		return nil, toolErrorDetails("MISSING_ACTION", "git_read requires action", "validation", map[string]any{"allowed": []string{"repos", "status", "diff", "log", "show", "blame"}})
	}
	switch action {
	case "repos":
		result, err := r.workspaceRepos(ctx, args)
		if result != nil {
			result["action"] = "repos"
		}
		return result, err
	case "status":
		result, err := r.gitRepoStatus(ctx, args)
		if result != nil {
			result["action"] = "status"
		}
		return result, err
	case "diff":
		result, err := r.gitDiff(ctx, args)
		if result != nil {
			result["action"] = "diff"
		}
		return result, err
	case "log":
		result, err := r.gitLog(ctx, args)
		if result != nil {
			result["action"] = "log"
		}
		return result, err
	case "show":
		result, err := r.gitShow(ctx, args)
		if result != nil {
			result["action"] = "show"
		}
		return result, err
	case "blame":
		result, err := r.gitBlame(ctx, args)
		if result != nil {
			result["action"] = "blame"
		}
		return result, err
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported git_read action", "validation", map[string]any{"action": action, "allowed": []string{"repos", "status", "diff", "log", "show", "blame"}})
	}
}

func (r *Runtime) gitWrite(ctx context.Context, args map[string]any) (Result, error) {
	action := strings.ToLower(stringArg(args, "action", ""))
	switch action {
	case "clone":
		result, err := r.gitClone(ctx, args)
		if result != nil {
			result["action"] = "clone"
		}
		return result, err
	case "commit":
		result, err := r.gitCommit(ctx, args)
		if result != nil {
			result["action"] = "commit"
		}
		return result, err
	case "fetch":
		result, err := r.gitFetch(ctx, args)
		if result != nil {
			result["action"] = "fetch"
		}
		return result, err
	case "pull":
		result, err := r.gitPull(ctx, args)
		if result != nil {
			result["action"] = "pull"
		}
		return result, err
	case "push":
		result, err := r.gitPush(ctx, args)
		if result != nil {
			result["action"] = "push"
		}
		return result, err
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported git_write action", "validation", map[string]any{"action": action, "allowed": []string{"clone", "commit", "fetch", "pull", "push"}})
	}
}

package tools

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func (r *Runtime) gitStatus(ctx context.Context, args map[string]any) (Result, error) {
	result, err := r.git(ctx, intArg(args, "max_output_bytes", 65536), "status", "--short", "--branch")
	if err != nil {
		return nil, err
	}
	output, _ := result["output"].(string)
	branch := ""
	files := make([]map[string]any, 0)
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" { continue }
		if strings.HasPrefix(line, "## ") { branch = strings.TrimPrefix(line, "## "); continue }
		status := ""
		path := line
		if len(line) >= 3 { status = strings.TrimSpace(line[:2]); path = strings.TrimSpace(line[3:]) }
		files = append(files, map[string]any{"path": path, "status": status})
	}
	result["branch"] = branch
	result["files"] = files
	result["clean"] = len(files) == 0
	return result, nil
}

func (r *Runtime) gitDiff(ctx context.Context, args map[string]any) (Result, error) {
	gitArgs := append([]string{"diff", "--"}, stringSliceArg(args, "paths")...)
	result, err := r.git(ctx, intArg(args, "max_bytes", 262144), gitArgs...)
	if err != nil { return nil, err }
	output, _ := result["output"].(string)
	result["files"] = parseDiffFiles(output)
	return result, nil
}

func (r *Runtime) gitLog(ctx context.Context, args map[string]any) (Result, error) {
	limit := intArg(args, "limit", 20)
	if limit < 1 { limit = 1 }
	if limit > 200 { limit = 200 }
	result, err := r.git(ctx, intArg(args, "max_bytes", 65536), "log", "--date=iso-strict", "--pretty=format:%H%x09%an%x09%ad%x09%s", "-n", strconv.Itoa(limit))
	if err != nil { return nil, err }
	output, _ := result["output"].(string)
	commits := make([]map[string]any, 0)
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" { continue }
		parts := strings.SplitN(line, "\t", 4)
		commit := map[string]any{"raw": line}
		if len(parts) > 0 { commit["hash"] = parts[0] }
		if len(parts) > 1 { commit["author"] = parts[1] }
		if len(parts) > 2 { commit["date"] = parts[2] }
		if len(parts) > 3 { commit["subject"] = parts[3] }
		commits = append(commits, commit)
	}
	result["commits"] = commits
	return result, nil
}

func parseDiffFiles(diffText string) []map[string]any {
	files := make([]map[string]any, 0)
	var current map[string]any
	for _, line := range strings.Split(diffText, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			parts := strings.Fields(line)
			path := ""
			if len(parts) >= 4 {
				path = strings.TrimPrefix(parts[3], "b/")
			}
			current = map[string]any{"path": path, "status": "modified", "binary": false}
			files = append(files, current)
			continue
		}
		if current == nil { continue }
		if strings.HasPrefix(line, "new file mode") { current["status"] = "added" }
		if strings.HasPrefix(line, "deleted file mode") { current["status"] = "deleted" }
		if strings.HasPrefix(line, "Binary files") { current["binary"] = true }
	}
	return files
}

func (r *Runtime) applyPatch(ctx context.Context, args map[string]any) (Result, error) {
	patch := stringArg(args, "patch", "")
	if patch == "" {
		return nil, toolError("INVALID_ARGUMENT", "patch is required", "validation")
	}
	if strings.HasPrefix(strings.TrimSpace(patch), "*** Begin Patch") {
		return r.applyEnvelopePatch(patch, boolArg(args, "dry_run", false))
	}
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "git", "apply", "--whitespace=nowarn", "-")
	if boolArg(args, "dry_run", false) {
		cmd = exec.CommandContext(cmdCtx, "git", "apply", "--check", "--whitespace=nowarn", "-")
	}
	cmd.Dir = r.ws.Root()
	cmd.Stdin = strings.NewReader(patch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, toolErrorDetails("PATCH_FAILED", "git apply failed", "runtime", map[string]any{"output": string(output), "reason": err.Error()})
	}
	if boolArg(args, "dry_run", false) {
		return Result{"ok": true, "summary": "patch validated", "dry_run": true}, nil
	}
	return Result{"ok": true, "summary": "patch applied", "dry_run": false}, nil
}

func (r *Runtime) git(ctx context.Context, maxBytes int, args ...string) (Result, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "git", args...)
	cmd.Dir = r.ws.Root()
	output, err := cmd.CombinedOutput()
	text, truncated := truncateBytes(output, maxBytes)
	result := Result{"ok": err == nil, "command": "git " + strings.Join(args, " "), "output": text, "truncated": truncated}
	if err != nil {
		result["error"] = err.Error()
	}
	return result, nil
}

func truncateBytes(data []byte, maxBytes int) (string, bool) {
	if maxBytes <= 0 || len(data) <= maxBytes {
		return string(data), false
	}
	return string(data[:maxBytes]), true
}


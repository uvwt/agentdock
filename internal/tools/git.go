package tools

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	workspacepkg "github.com/uvwt/agentdock/internal/workspace"
)

func (r *Runtime) applyPatch(ctx context.Context, args map[string]any) (Result, error) {
	patch := stringArg(args, "patch", "")
	if patch == "" {
		return nil, toolError("INVALID_ARGUMENT", "patch is required", "validation")
	}
	workdir, err := r.patchWorkdir(args)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(strings.TrimSpace(patch), "*** Begin Patch") {
		return r.applyEnvelopePatch(patch, boolArg(args, "dry_run", false), workdir.Display)
	}
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "git", "apply", "--whitespace=nowarn", "-")
	if boolArg(args, "dry_run", false) {
		cmd = exec.CommandContext(cmdCtx, "git", "apply", "--check", "--whitespace=nowarn", "-")
	}
	cmd.Dir = workdir.Abs
	cmd.Stdin = strings.NewReader(patch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, toolErrorDetails("PATCH_FAILED", "git apply failed", "runtime", map[string]any{"workdir": workdir.Display, "output": redactSecrets(string(output), nil), "reason": err.Error()})
	}
	if boolArg(args, "dry_run", false) {
		return Result{"ok": true, "summary": "patch validated", "dry_run": true, "workdir": workdir.Display}, nil
	}
	return Result{"ok": true, "summary": "patch applied", "dry_run": false, "workdir": workdir.Display}, nil
}

func (r *Runtime) patchWorkdir(args map[string]any) (workspacepkg.Path, error) {
	raw := stringArg(args, "repo_path", "")
	if raw == "" {
		raw = stringArg(args, "workdir", "")
	}
	if raw == "" {
		raw = "."
	}
	workdir, err := r.ws.ResolveExisting(raw)
	if err != nil {
		return workspacepkg.Path{}, err
	}
	info, err := os.Stat(workdir.Abs)
	if err != nil {
		return workspacepkg.Path{}, err
	}
	if !info.IsDir() {
		return workspacepkg.Path{}, toolError("NOT_A_DIRECTORY", "workdir/repo_path is not a directory", "validation")
	}
	return workdir, nil
}

package tools

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/textutil"
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
	preview := textutil.SafeTruncateString(patch, intArg(args, "max_diff_bytes", 65536))
	stats := countDiffStats(patch)
	affected := parseDiffFiles(patch)
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
		diagnostic := patchDiagnostic("GIT_APPLY_FAILED", workdir.Display, "git apply failed", redactSecrets(string(output), nil), err.Error())
		return nil, toolErrorDetails("PATCH_FAILED", "git apply failed", "runtime", map[string]any{"workdir": workdir.Display, "output": diagnostic["output"], "reason": err.Error(), "diagnostic": diagnostic})
	}
	if boolArg(args, "dry_run", false) {
		return Result{"ok": true, "summary": "patch validated", "dry_run": true, "workdir": workdir.Display, "affected_files": affected, "diff_preview": preview.Text, "truncated": preview.Truncated, "files_changed": stats.FilesChanged, "insertions": stats.Insertions, "deletions": stats.Deletions}, nil
	}
	return Result{"ok": true, "summary": "patch applied", "dry_run": false, "workdir": workdir.Display, "affected_files": affected, "diff_preview": preview.Text, "truncated": preview.Truncated, "files_changed": stats.FilesChanged, "insertions": stats.Insertions, "deletions": stats.Deletions}, nil
}

func patchDiagnostic(code, path, message, output, reason string) map[string]any {
	return map[string]any{"code": code, "path": path, "message": message, "output": output, "reason": reason}
}

func (r *Runtime) patchWorkdir(args map[string]any) (workspacepkg.Path, error) {
	raw := stringArg(args, "workdir", "")
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
		return workspacepkg.Path{}, toolError("NOT_A_DIRECTORY", "workdir is not a directory", "validation")
	}
	return workdir, nil
}

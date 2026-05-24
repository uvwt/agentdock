package tools

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

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
		return nil, toolErrorDetails("PATCH_FAILED", "git apply failed", "runtime", map[string]any{"output": redactSecrets(string(output), nil), "reason": err.Error()})
	}
	if boolArg(args, "dry_run", false) {
		return Result{"ok": true, "summary": "patch validated", "dry_run": true}, nil
	}
	return Result{"ok": true, "summary": "patch applied", "dry_run": false}, nil
}

package file

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/textutil"
	workspacepkg "github.com/uvwt/agentdock/internal/workspace"
)

func (svc *Service) applyPatch(ctx context.Context, request EditRequest) (Result, error) {
	patch := request.Patch
	if patch == "" {
		return nil, toolError("INVALID_ARGUMENT", "patch is required", "validation")
	}
	workdir, err := svc.patchWorkdir(request.Workdir)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(strings.TrimSpace(patch), "*** Begin Patch") {
		return svc.applyEnvelopePatch(patch, request.DryRun, workdir.Display)
	}
	maxDiffBytes := boundedInt(intValue(request.MaxDiffBytes, 65536), 65536, 1, maxTextOutputBytes)
	preview := textutil.SafeTruncateString(patch, maxDiffBytes)
	stats := countDiffStats(patch)
	affected := parseDiffFiles(patch)
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "git", "apply", "--whitespace=nowarn", "-")
	if request.DryRun {
		cmd = exec.CommandContext(cmdCtx, "git", "apply", "--check", "--whitespace=nowarn", "-")
	}
	cmd.Dir = workdir.Abs
	cmd.Stdin = strings.NewReader(patch)
	output, outputTotal, outputTruncated, err := runBoundedCombinedOutput(cmd, 1<<20)
	if err != nil {
		outputText, _ := truncateBytes(output, 1<<20)
		diagnostic := patchDiagnostic("GIT_APPLY_FAILED", workdir.Display, "git apply failed", redactSecrets(outputText, nil), err.Error())
		return nil, toolErrorDetails("PATCH_FAILED", "git apply failed", "runtime", map[string]any{
			"workdir": workdir.Display, "output": diagnostic["output"], "reason": err.Error(), "diagnostic": diagnostic,
			"output_total_bytes": outputTotal, "output_truncated": outputTruncated,
		})
	}
	if request.DryRun {
		return Result{"summary": "patch validated", "dry_run": true, "workdir": workdir.Display, "affected_files": affected, "diff_preview": preview.Text, "truncated": preview.Truncated, "files_changed": stats.FilesChanged, "insertions": stats.Insertions, "deletions": stats.Deletions}, nil
	}
	return Result{"summary": "patch applied", "dry_run": false, "workdir": workdir.Display, "affected_files": affected, "diff_preview": preview.Text, "truncated": preview.Truncated, "files_changed": stats.FilesChanged, "insertions": stats.Insertions, "deletions": stats.Deletions}, nil
}

func patchDiagnostic(code, path, message, output, reason string) map[string]any {
	return map[string]any{"code": code, "path": path, "message": message, "output": output, "reason": reason}
}

func (svc *Service) patchWorkdir(requested string) (workspacepkg.Path, error) {
	raw := requested
	if raw == "" {
		raw = "."
	}
	workdir, err := svc.ws.ResolveExisting(raw)
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

package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const defaultPlaywrightBrowsersPath = "/ms-playwright"

func (r *Runtime) browserSessionStart(ctx context.Context, args map[string]any) (Result, error) {
	return r.browserRunnerCall(ctx, "session_start", args)
}

func (r *Runtime) browserAction(ctx context.Context, args map[string]any) (Result, error) {
	return r.browserRunnerCall(ctx, "action", args)
}

func (r *Runtime) browserSnapshot(ctx context.Context, args map[string]any) (Result, error) {
	return r.browserRunnerCall(ctx, "snapshot", args)
}

func (r *Runtime) browserSessionClose(ctx context.Context, args map[string]any) (Result, error) {
	return r.browserRunnerCall(ctx, "session_close", args)
}

func (r *Runtime) browserRunnerCall(ctx context.Context, operation string, args map[string]any) (Result, error) {
	runner, err := r.browserRunnerScript()
	if err != nil {
		return nil, err
	}
	artifactDir, err := r.resolveControlForWrite(r.cfg.BrowserArtifactDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(artifactDir.Abs, 0o755); err != nil {
		return nil, err
	}
	payload := map[string]any{
		"operation":    operation,
		"args":         args,
		"workspace":    r.ws.Root(),
		"artifact_dir": artifactDir.Abs,
	}
	data, _ := json.Marshal(payload)
	timeout := time.Duration(intArg(args, "timeout_ms", 30000)) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if timeout > 5*time.Minute {
		timeout = 5 * time.Minute
	}
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "node", runner.Abs)
	cmd.Dir = filepath.Dir(runner.Abs)
	cmd.Env = r.commandEnv(map[string]any{
		"BROWSER_RUNNER_PAYLOAD": string(data),
		"BROWSER_ARTIFACT_DIR":   artifactDir.Abs,
		"WORKSPACE":              r.ws.Root(),
		// 官方浏览器增强镜像把 Chromium 固定安装在 /ms-playwright。
		// 这里显式传给 Node runner，避免子进程回退到 /workspace/.cache/ms-playwright 后找不到浏览器。
		"PLAYWRIGHT_BROWSERS_PATH": playwrightBrowsersPath(),
	})
	output, err := cmd.CombinedOutput()
	text, truncated := truncateBytes(output, intArg(args, "max_bytes", 262144))
	text = redactSecrets(text, nil)
	result := Result{"ok": err == nil, "operation": operation, "stdout": text, "truncated": truncated}
	if err != nil {
		result["error"] = err.Error()
	}
	var parsed map[string]any
	if parseErr := json.Unmarshal([]byte(text), &parsed); parseErr == nil {
		for key, value := range parsed {
			result[key] = value
		}
	} else if text != "" {
		result["json_error"] = parseErr.Error()
	}
	return result, nil
}

func (r *Runtime) browserRunnerScript() (controlPath, error) {
	runnerDir, err := r.resolveControlExisting(r.cfg.BrowserRunnerDir)
	if err != nil {
		return controlPath{}, toolErrorDetails("BROWSER_RUNNER_NOT_FOUND", "browser runner directory not found", "validation", map[string]any{"runner_dir": r.cfg.BrowserRunnerDir, "suggestion": "copy examples/browser-runner to the configured runner directory and run npm install && npx playwright install chromium"})
	}
	candidate := filepath.Join(r.cfg.BrowserRunnerDir, "browser-runner.js")
	runner, err := r.resolveControlExisting(candidate)
	if err != nil {
		return controlPath{}, toolErrorDetails("BROWSER_RUNNER_NOT_FOUND", "browser-runner.js not found", "validation", map[string]any{"runner_dir": runnerDir.Display})
	}
	return runner, nil
}

func playwrightBrowsersPath() string {
	if value := strings.TrimSpace(os.Getenv("PLAYWRIGHT_BROWSERS_PATH")); value != "" {
		return value
	}
	return defaultPlaywrightBrowsersPath
}

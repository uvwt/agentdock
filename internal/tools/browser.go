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
		"default_dir":  r.ws.Root(),
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
	env := map[string]string{
		"BROWSER_RUNNER_PAYLOAD": string(data),
		"BROWSER_ARTIFACT_DIR":   artifactDir.Abs,
		"AGENTDOCK_DEFAULT_DIR":  r.ws.Root(),
		"AGENTDOCK_SERVER_URL":   strings.TrimRight(r.cfg.OAuthServerURL, "/"),
	}
	// 浏览器增强 Docker 镜像会通过 ENV 固定浏览器安装目录。这里只在父进程存在该变量时转交给 Node runner；
	// macOS/裸机部署不强行写死路径，让 Playwright 使用本机默认缓存目录。
	if value := strings.TrimSpace(os.Getenv("PLAYWRIGHT_BROWSERS_PATH")); value != "" {
		env["PLAYWRIGHT_BROWSERS_PATH"] = value
	}
	cmd.Env = r.internalCommandEnv(env)
	output, err := cmd.CombinedOutput()
	// runner 可能按需返回 screenshot/image_base64。这里必须先解析完整 JSON，再截断 stdout 展示；
	// 否则大图会把 JSON 截断，导致结构化结果丢失。
	var parsed map[string]any
	parseErr := json.Unmarshal(output, &parsed)
	text, truncated := truncateBytes(output, intArg(args, "max_bytes", 262144))
	text = redactSecrets(text, nil)
	result := Result{"ok": err == nil, "operation": operation, "stdout": text, "truncated": truncated}
	if err != nil {
		result["error"] = err.Error()
	}
	if parseErr == nil {
		for key, value := range parsed {
			result[key] = value
		}
	} else if len(output) > 0 {
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

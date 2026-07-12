package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/config"
)

const browserRunnerOutputLimit = 8 << 20

func (r *Runtime) browserRunnerCall(ctx context.Context, operation string, args map[string]any) (Result, error) {
	runner, err := r.browserRunnerScript()
	if err != nil {
		return nil, err
	}
	artifactDir, err := r.resolveControlForWrite(config.BrowserArtifactDir)
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
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, toolErrorDetails("BROWSER_PAYLOAD_INVALID", "browser payload cannot be encoded as JSON", "validation", map[string]any{"reason": err.Error()})
	}
	timeout := boundedMilliseconds(intArg(args, "timeout_ms", 30000), 30000, int((5*time.Minute)/time.Millisecond))
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "node", runner.Abs)
	cmd.Dir = filepath.Dir(runner.Abs)
	env := map[string]string{
		"BROWSER_RUNNER_PAYLOAD": string(data),
		"BROWSER_ARTIFACT_DIR":   artifactDir.Abs,
		"AGENTDOCK_DEFAULT_DIR":  r.ws.Root(),
	}
	// 浏览器增强 Docker 镜像会通过 ENV 固定浏览器安装目录。这里只在父进程存在该变量时转交给 Node runner；
	// macOS/裸机部署不强行写死路径，让 Playwright 使用本机默认缓存目录。
	if value := strings.TrimSpace(os.Getenv("PLAYWRIGHT_BROWSERS_PATH")); value != "" {
		env["PLAYWRIGHT_BROWSERS_PATH"] = value
	}
	commandEnv, err := r.internalCommandEnv(env)
	if err != nil {
		return nil, err
	}
	cmd.Env = commandEnv
	output, outputTotal, outputTruncated, err := runBoundedCombinedOutput(cmd, browserRunnerOutputLimit)
	maxBytes := boundedInt(intArg(args, "max_bytes", 262144), 262144, 1, 1<<20)
	text, responseTruncated := truncateBytes(output, maxBytes)
	text = redactSecrets(text, nil)
	if outputTruncated {
		return Result{
			"ok":                 false,
			"operation":          operation,
			"error":              "browser runner output exceeded the capture limit",
			"stdout":             text,
			"truncated":          true,
			"output_total_bytes": outputTotal,
			"output_limit_bytes": browserRunnerOutputLimit,
		}, nil
	}
	var parsed map[string]any
	parseErr := json.Unmarshal(output, &parsed)
	result := Result{"ok": err == nil, "operation": operation, "output_total_bytes": outputTotal}
	if boolArg(args, "debug_stdout", false) || parseErr != nil {
		result["stdout"] = text
		result["truncated"] = responseTruncated
	}
	if err != nil {
		result["error"] = err.Error()
	}
	if parseErr == nil {
		for key, value := range parsed {
			result[key] = value
		}
		if err := r.normalizeBrowserScreenshot(ctx, result, args); err != nil {
			return nil, err
		}
	} else if len(output) > 0 {
		result["json_error"] = parseErr.Error()
	}
	return result, nil
}

func (r *Runtime) browserRunnerScript() (controlPath, error) {
	runnerDir, err := r.resolveControlExisting(config.BrowserRunnerDir)
	if err != nil {
		return controlPath{}, toolErrorDetails("BROWSER_RUNNER_NOT_FOUND", "browser runner directory not found", "validation", map[string]any{"runner_dir": config.BrowserRunnerDir, "suggestion": "copy examples/browser-runner to the configured runner directory and run npm install; on macOS prefer browser=chrome for system Chrome"})
	}
	candidate := filepath.Join(config.BrowserRunnerDir, "browser-runner.js")
	runner, err := r.resolveControlExisting(candidate)
	if err != nil {
		return controlPath{}, toolErrorDetails("BROWSER_RUNNER_NOT_FOUND", "browser-runner.js not found", "validation", map[string]any{"runner_dir": runnerDir.Display})
	}
	return runner, nil
}

func (r *Runtime) normalizeBrowserScreenshot(ctx context.Context, result Result, args map[string]any) error {
	pathValue := strings.TrimSpace(stringValue(result["screenshot_path"]))
	fileValue := strings.TrimSpace(stringValue(result["screenshot_file"]))
	deleteBrowserScreenshotScratchFields(result)
	if pathValue == "" {
		return nil
	}
	data, err := os.ReadFile(pathValue)
	if err != nil {
		return err
	}
	info, err := identifyImage(data)
	if err != nil {
		return toolError("BINARY_FILE", "browser screenshot is not a supported image", "validation")
	}
	if fileValue == "" {
		fileValue = filepath.Base(pathValue)
	}
	published, err := r.publishImageBytes(ctx, data, fileValue, info, intArg(args, "retention_seconds", 0))
	if err != nil {
		return err
	}
	result["screenshot"] = published
	return nil
}

func deleteBrowserScreenshotScratchFields(result Result) {
	for _, key := range []string{"artifact", "screenshot_path", "screenshot_file", "screenshot_artifact_id"} {
		delete(result, key)
	}
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

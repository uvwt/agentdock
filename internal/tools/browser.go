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
	// Docker 浏览器镜像会固定浏览器或 Playwright 缓存路径；裸机环境不写死，继续使用系统浏览器或本机缓存。
	for _, key := range []string{"AGENTDOCK_BROWSER_EXECUTABLE_PATH", "PLAYWRIGHT_BROWSERS_PATH"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			env[key] = value
		}
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
			"browser_ok":         false,
			"operation":          operation,
			"browser_error":      "browser runner output exceeded the capture limit",
			"stdout":             text,
			"truncated":          true,
			"output_total_bytes": outputTotal,
			"output_limit_bytes": browserRunnerOutputLimit,
		}, nil
	}
	var parsed map[string]any
	parseErr := json.Unmarshal(output, &parsed)
	result := Result{"browser_ok": err == nil, "operation": operation, "output_total_bytes": outputTotal}
	if boolArg(args, "debug_stdout", false) || parseErr != nil {
		result["stdout"] = text
		result["truncated"] = responseTruncated
	}
	if parseErr == nil {
		for key, value := range parsed {
			switch key {
			case "ok":
				result["browser_ok"] = value
			case "error":
				result["browser_error"] = value
			default:
				result[key] = value
			}
		}
		if err := r.normalizeBrowserScreenshot(ctx, result, args); err != nil {
			return nil, err
		}
	} else {
		result["browser_ok"] = false
		result["json_error"] = parseErr.Error()
		result["browser_error"] = "browser runner returned invalid JSON"
	}
	if err != nil {
		result["browser_ok"] = false
		if _, exists := result["browser_error"]; !exists {
			result["browser_error"] = err.Error()
		}
	}
	return result, nil
}

func (r *Runtime) browserRunnerScript() (controlPath, error) {
	runnerDir := filepath.Clean(strings.TrimSpace(r.cfg.BrowserRunnerDir))
	if runnerDir == "" || !filepath.IsAbs(runnerDir) {
		return controlPath{}, toolErrorDetails("BROWSER_RUNNER_NOT_FOUND", "browser runner directory is not configured", "validation", map[string]any{"runner_dir": runnerDir})
	}
	info, err := os.Lstat(runnerDir)
	if err != nil || !info.IsDir() {
		return controlPath{}, toolErrorDetails("BROWSER_RUNNER_NOT_FOUND", "browser runner directory not found", "validation", map[string]any{"runner_dir": runnerDir, "suggestion": "copy examples/browser-runner to the configured runner directory and run npm install; on macOS prefer browser=chrome for system Chrome"})
	}
	candidate := filepath.Join(runnerDir, "browser-runner.js")
	info, err = os.Lstat(candidate)
	if err != nil || !info.Mode().IsRegular() {
		return controlPath{}, toolErrorDetails("BROWSER_RUNNER_NOT_FOUND", "browser-runner.js not found", "validation", map[string]any{"runner_dir": runnerDir})
	}
	return controlPath{Abs: candidate, Display: candidate}, nil
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

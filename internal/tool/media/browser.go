package media

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/config"
	toolcore "github.com/uvwt/agentdock/internal/tool/core"
)

const browserRunnerOutputLimit = 8 << 20

func (s *Service) BrowserCall(ctx context.Context, operation string, args map[string]any) (Result, error) {
	runner, err := s.BrowserRunnerScript()
	if err != nil {
		return nil, err
	}
	artifactDir, err := s.resolveControlForWrite(config.BrowserArtifactDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(artifactDir.Abs, 0o755); err != nil {
		return nil, err
	}
	payload := map[string]any{
		"operation":    operation,
		"args":         args,
		"default_dir":  s.ws.Root(),
		"artifact_dir": artifactDir.Abs,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, toolErrorDetails("BROWSER_PAYLOAD_INVALID", "browser payload cannot be encoded as JSON", "validation", map[string]any{"reason": err.Error()})
	}
	timeout := browserRunnerTimeout(args)
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "node", runner.Abs)
	cmd.Dir = filepath.Dir(runner.Abs)
	env := map[string]string{
		"BROWSER_RUNNER_PAYLOAD": string(data),
		"BROWSER_ARTIFACT_DIR":   artifactDir.Abs,
		"AGENTDOCK_DEFAULT_DIR":  s.ws.Root(),
	}
	// Docker 浏览器镜像会固定浏览器或 Playwright 缓存路径；裸机环境不写死，继续使用系统浏览器或本机缓存。
	for _, key := range []string{"AGENTDOCK_BROWSER_EXECUTABLE_PATH", "PLAYWRIGHT_BROWSERS_PATH"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			env[key] = value
		}
	}
	commandEnv, err := s.commandEnv(env)
	if err != nil {
		return nil, err
	}
	cmd.Env = commandEnv
	stdout := toolcore.NewBoundedOutput(browserRunnerOutputLimit)
	stderr := toolcore.NewBoundedOutput(browserRunnerOutputLimit)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err = cmd.Run()
	output, outputTotal, outputTruncated := stdout.Snapshot()
	stderrOutput, stderrTotal, stderrTruncated := stderr.Snapshot()
	maxBytes := boundedInt(intArg(args, "max_bytes", 262144), 262144, 1, 1<<20)
	text, responseTruncated := truncateBytes(output, maxBytes)
	text = redactSecrets(text, nil)
	stderrText, stderrResponseTruncated := truncateBytes(stderrOutput, maxBytes)
	stderrText = redactSecrets(stderrText, nil)
	if outputTruncated {
		return Result{
			"browser_ok":         false,
			"operation":          operation,
			"browser_error":      "browser runner output exceeded the capture limit",
			"code":               "BROWSER_RUNNER_OUTPUT_TRUNCATED",
			"error":              browserProtocolError("BROWSER_RUNNER_OUTPUT_TRUNCATED", "browser runner output exceeded the capture limit", map[string]any{"output_limit_bytes": browserRunnerOutputLimit}),
			"stdout":             text,
			"truncated":          true,
			"output_total_bytes": outputTotal,
			"output_limit_bytes": browserRunnerOutputLimit,
			"stderr":             stderrText,
			"stderr_total_bytes": stderrTotal,
			"stderr_truncated":   stderrTruncated || stderrResponseTruncated,
		}, nil
	}
	var parsed map[string]any
	parseErr := json.Unmarshal(output, &parsed)
	result := Result{
		"browser_ok":         err == nil,
		"operation":          operation,
		"output_total_bytes": outputTotal,
		"stderr_total_bytes": stderrTotal,
	}
	if stderrText != "" {
		result["stderr"] = stderrText
		result["stderr_truncated"] = stderrTruncated || stderrResponseTruncated
	}
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
				result["error"] = value
				if message := browserErrorMessage(value); message != "" {
					result["browser_error"] = message
				}
			default:
				result[key] = value
			}
		}
		if err := s.normalizeBrowserScreenshot(ctx, result, args); err != nil {
			return nil, err
		}
	} else {
		result["browser_ok"] = false
		result["code"] = "BROWSER_RUNNER_PROTOCOL_ERROR"
		result["json_error"] = parseErr.Error()
		result["browser_error"] = "browser runner returned invalid JSON"
		result["error"] = browserProtocolError("BROWSER_RUNNER_PROTOCOL_ERROR", "browser runner returned invalid JSON", map[string]any{
			"json_error": parseErr.Error(),
			"exit_error": commandErrorString(err),
		})
	}
	if err != nil {
		result["browser_ok"] = false
		if _, exists := result["browser_error"]; !exists {
			result["browser_error"] = err.Error()
		}
		if _, exists := result["code"]; !exists {
			result["code"] = "BROWSER_RUNNER_EXITED"
		}
		if _, exists := result["error"]; !exists {
			result["error"] = browserProtocolError("BROWSER_RUNNER_EXITED", err.Error(), map[string]any{"exit_error": err.Error()})
		}
	}
	return result, nil
}

func browserRunnerTimeout(args map[string]any) time.Duration {
	requested := intArg(args, "timeout_ms", 30000)
	required := requested + 2000 // 给 runner 留出序列化结构化错误和回收子进程的时间。
	if actions, ok := args["actions"].([]any); ok {
		for _, raw := range actions {
			action, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			actionRequired := intArg(action, "timeout_ms", 0) + 5000
			if actionRequired > required {
				required = actionRequired
			}
		}
	}
	return boundedMilliseconds(required, 32000, int((5*time.Minute)/time.Millisecond))
}

func browserProtocolError(code, message string, details map[string]any) map[string]any {
	result := map[string]any{
		"code":    code,
		"message": message,
		"phase":   "protocol",
	}
	if len(details) > 0 {
		result["details"] = details
	}
	return result
}

func browserErrorMessage(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case map[string]any:
		return strings.TrimSpace(stringValue(typed["message"]))
	default:
		return ""
	}
}

func commandErrorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (s *Service) BrowserRunnerScript() (ControlPath, error) {
	runnerDir := filepath.Clean(strings.TrimSpace(s.cfg.BrowserRunnerDir))
	if runnerDir == "" || !filepath.IsAbs(runnerDir) {
		return ControlPath{}, toolErrorDetails("BROWSER_RUNNER_NOT_FOUND", "browser runner directory is not configured", "validation", map[string]any{"runner_dir": runnerDir})
	}
	info, err := os.Lstat(runnerDir)
	if err != nil || !info.IsDir() {
		return ControlPath{}, toolErrorDetails("BROWSER_RUNNER_NOT_FOUND", "browser runner directory not found", "validation", map[string]any{"runner_dir": runnerDir, "suggestion": "copy examples/browser-runner to the configured runner directory and run npm install; on macOS prefer browser=chrome for system Chrome"})
	}
	candidate := filepath.Join(runnerDir, "browser-runner.js")
	info, err = os.Lstat(candidate)
	if err != nil || !info.Mode().IsRegular() {
		return ControlPath{}, toolErrorDetails("BROWSER_RUNNER_NOT_FOUND", "browser-runner.js not found", "validation", map[string]any{"runner_dir": runnerDir})
	}
	return ControlPath{Abs: candidate, Display: candidate}, nil
}

func (s *Service) normalizeBrowserScreenshot(ctx context.Context, result Result, args map[string]any) error {
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
	published, err := s.publishImageBytes(ctx, data, fileValue, info, intArg(args, "retention_seconds", 0))
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

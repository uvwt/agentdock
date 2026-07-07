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
	env := map[string]string{
		"BROWSER_RUNNER_PAYLOAD": string(data),
		"BROWSER_ARTIFACT_DIR":   artifactDir.Abs,
		"WORKSPACE":              r.ws.Root(),
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

func (r *Runtime) browserProfile(ctx context.Context, args map[string]any) (Result, error) {
	action := strings.ToLower(stringArg(args, "action", "status"))
	site := strings.TrimSpace(stringArg(args, "site", ""))
	if site == "" {
		return nil, toolErrorDetails("MISSING_SITE", "browser_session profile_* actions require a site profile name", "validation", nil)
	}
	profileID := strings.Trim(strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '-'
	}, site), "-")
	if profileID == "" {
		return nil, toolErrorDetails("INVALID_SITE", "browser_session profile_* site cannot be used as a profile name", "validation", map[string]any{"site": site})
	}
	sessionID := "profile-" + profileID
	url := strings.TrimSpace(stringArg(args, "url", ""))
	if url == "" && strings.EqualFold(site, "volcengine-ark-quota") {
		url = "https://console.volcengine.com/ark/region:cn-beijing/subscription/coding-plan"
	}
	timeout := intArg(args, "timeout_ms", 30000)
	switch action {
	case "open", "start":
		if url == "" {
			return nil, toolErrorDetails("MISSING_URL", "browser_session action=profile_open needs url when opening a custom profile", "validation", map[string]any{"site": site})
		}
		result, err := r.browserRunnerCall(ctx, "session_start", map[string]any{
			"session_id": sessionID,
			"backend":    "playwright",
			"browser":    "chromium",
			"headless":   false,
			"profile_id": profileID,
			"url":        url,
			"viewport":   map[string]any{"width": 1280, "height": 900},
			"keep_open":  true,
			"timeout_ms": timeout,
		})
		if result != nil {
			result["profile_action"] = "open"
			result["site"] = site
			result["profile_id"] = profileID
			delete(result, "stdout")
		}
		return result, err
	case "close", "stop":
		result, err := r.browserRunnerCall(ctx, "session_close", map[string]any{"session_id": sessionID, "timeout_ms": timeout})
		if result != nil {
			result["profile_action"] = "close"
			result["site"] = site
			result["profile_id"] = profileID
			delete(result, "stdout")
		}
		return result, err
	case "status":
		result, err := r.browserRunnerCall(ctx, "snapshot", map[string]any{"session_id": sessionID, "max_text_chars": intArg(args, "max_text_chars", 2000), "capture_screenshot": false, "timeout_ms": timeout})
		if result != nil {
			result["profile_action"] = "status"
			result["site"] = site
			result["profile_id"] = profileID
			delete(result, "stdout")
		}
		return result, err
	case "snapshot":
		snapshotArgs := map[string]any{"session_id": sessionID, "max_text_chars": intArg(args, "max_text_chars", 8000), "max_interactive_elements": intArg(args, "max_interactive_elements", 40), "capture_screenshot": true, "timeout_ms": timeout}
		if boolArg(args, "full_page", false) {
			snapshotArgs["full_page"] = true
		}
		if boolArg(args, "include_image", false) {
			snapshotArgs["include_image"] = true
		}
		if boolArg(args, "include_screenshot_base64", false) {
			snapshotArgs["include_screenshot_base64"] = true
		}
		if maxImageBytes := intArg(args, "max_image_bytes", 0); maxImageBytes > 0 {
			snapshotArgs["max_image_bytes"] = maxImageBytes
		}
		result, err := r.browserRunnerCall(ctx, "snapshot", snapshotArgs)
		if result != nil {
			result["profile_action"] = "snapshot"
			result["site"] = site
			result["profile_id"] = profileID
			delete(result, "stdout")
		}
		return result, err
	case "save":
		stateTargetSkill := strings.TrimSpace(stringArg(args, "state_target_skill", ""))
		if stateTargetSkill == "" {
			stateTargetSkill = profileID
		}
		result, err := r.browserRunnerCall(ctx, "snapshot", map[string]any{"session_id": sessionID, "max_text_chars": intArg(args, "max_text_chars", 2000), "capture_screenshot": false, "save_storage_state": true, "state_target_skill": stateTargetSkill, "timeout_ms": timeout})
		if result != nil {
			result["profile_action"] = "save"
			result["site"] = site
			result["profile_id"] = profileID
			delete(result, "stdout")
		}
		return result, err
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported browser_session profile action", "validation", map[string]any{"action": action})
	}
}

package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func (r *Runtime) desktopSnapshot(ctx context.Context, args map[string]any) (Result, error) {
	if err := desktopSupported(); err != nil {
		return nil, err
	}
	artifactDir, err := r.resolveControlForWrite(filepath.Join(r.cfg.DesktopArtifactDir, "screenshots"))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(artifactDir.Abs, 0o755); err != nil {
		return nil, err
	}
	name := fmt.Sprintf("desktop-%d.png", time.Now().UnixMilli())
	path := filepath.Join(artifactDir.Abs, name)
	cmd := exec.CommandContext(ctx, "screencapture", "-x", path)
	cmd.Env = r.commandEnv(nil)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return Result{"ok": false, "error": err.Error(), "stdout": redactSecrets(string(output), nil)}, nil
	}
	res := Result{"ok": true, "screenshot_path": path, "screenshot_artifact_id": strings.TrimSuffix(name, ".png")}
	if boolArg(args, "include_screenshot_base64", false) {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		res["screenshot_mime_type"] = "image/png"
		res["screenshot_base64"] = base64.StdEncoding.EncodeToString(data)
	}
	return res, nil
}

func (r *Runtime) desktopFocusApp(ctx context.Context, args map[string]any) (Result, error) {
	if err := desktopSupported(); err != nil {
		return nil, err
	}
	app := strings.TrimSpace(stringArg(args, "app", ""))
	if app == "" {
		return nil, toolError("MISSING_APP", "app is required", "validation")
	}
	return r.runAppleScript(ctx, fmt.Sprintf(`tell application %q to activate`, app), args)
}

func (r *Runtime) desktopClick(ctx context.Context, args map[string]any) (Result, error) {
	if err := desktopSupported(); err != nil {
		return nil, err
	}
	x := intArg(args, "x", -1)
	y := intArg(args, "y", -1)
	if x < 0 || y < 0 {
		return nil, toolError("INVALID_COORDINATE", "x and y must be non-negative", "validation")
	}
	if _, err := exec.LookPath("cliclick"); err != nil {
		return nil, toolErrorDetails("CLICLICK_NOT_FOUND", "cliclick is required for desktop_click", "validation", map[string]any{"install": "brew install cliclick"})
	}
	cmd := exec.CommandContext(ctx, "cliclick", fmt.Sprintf("c:%d,%d", x, y))
	cmd.Env = r.commandEnv(nil)
	return commandResult("desktop_click", cmd)
}

func (r *Runtime) desktopType(ctx context.Context, args map[string]any) (Result, error) {
	if err := desktopSupported(); err != nil {
		return nil, err
	}
	text := stringArg(args, "text", "")
	if text == "" {
		return nil, toolError("MISSING_TEXT", "text is required", "validation")
	}
	if _, err := exec.LookPath("cliclick"); err != nil {
		return nil, toolErrorDetails("CLICLICK_NOT_FOUND", "cliclick is required for desktop_type", "validation", map[string]any{"install": "brew install cliclick"})
	}
	cmd := exec.CommandContext(ctx, "cliclick", "t:"+text)
	cmd.Env = r.commandEnv(nil)
	return commandResult("desktop_type", cmd)
}

func (r *Runtime) desktopHotkey(ctx context.Context, args map[string]any) (Result, error) {
	if err := desktopSupported(); err != nil {
		return nil, err
	}
	keys := strings.TrimSpace(stringArg(args, "keys", ""))
	if keys == "" {
		return nil, toolError("MISSING_KEYS", "keys is required, for example cmd+space or enter", "validation")
	}
	if _, err := exec.LookPath("cliclick"); err != nil {
		return nil, toolErrorDetails("CLICLICK_NOT_FOUND", "cliclick is required for desktop_hotkey", "validation", map[string]any{"install": "brew install cliclick"})
	}
	cmd := exec.CommandContext(ctx, "cliclick", "kp:"+normalizeCliclickKeys(keys))
	cmd.Env = r.commandEnv(nil)
	return commandResult("desktop_hotkey", cmd)
}

func (r *Runtime) runAppleScript(ctx context.Context, script string, args map[string]any) (Result, error) {
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	cmd.Env = r.commandEnv(nil)
	return commandResult("osascript", cmd)
}

func commandResult(operation string, cmd *exec.Cmd) (Result, error) {
	output, err := cmd.CombinedOutput()
	text := redactSecrets(string(output), nil)
	res := Result{"ok": err == nil, "operation": operation, "stdout": text}
	if err != nil {
		res["error"] = err.Error()
	}
	return res, nil
}

func desktopSupported() error {
	if runtime.GOOS != "darwin" {
		return toolErrorDetails("DESKTOP_UNSUPPORTED_OS", "desktop automation is currently macOS-only", "validation", map[string]any{"os": runtime.GOOS})
	}
	return nil
}

func normalizeCliclickKeys(keys string) string {
	keys = strings.ToLower(strings.TrimSpace(keys))
	replacements := map[string]string{"command": "cmd", "control": "ctrl", "option": "alt", "return": "enter"}
	parts := strings.FieldsFunc(keys, func(r rune) bool { return r == '+' || r == ',' || r == ' ' })
	for i, part := range parts {
		if replacement, ok := replacements[part]; ok {
			parts[i] = replacement
		} else {
			parts[i] = part
		}
	}
	return strings.Join(parts, "+")
}

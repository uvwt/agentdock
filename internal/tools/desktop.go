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

func (r *Runtime) desktopPreflight(ctx context.Context, args map[string]any) (Result, error) {
	warnings := []string{}
	checks := map[string]any{
		"os":        runtime.GOOS,
		"supported": runtime.GOOS == "darwin",
	}
	for _, bin := range []string{"screencapture", "osascript", "pbcopy", "pbpaste", "cliclick"} {
		_, err := exec.LookPath(bin)
		checks[bin] = err == nil
		if err != nil && bin == "cliclick" {
			warnings = append(warnings, "cliclick not found; desktop mouse/keyboard actions require: brew install cliclick")
		}
	}
	if runtime.GOOS != "darwin" {
		warnings = append(warnings, "desktop automation is currently macOS-only")
		return Result{"ok": false, "checks": checks, "warnings": warnings}, nil
	}
	if boolArg(args, "check_screenshot", true) {
		artifactDir, err := r.resolveControlForWrite(filepath.Join(r.cfg.DesktopArtifactDir, "preflight"))
		if err == nil {
			_ = os.MkdirAll(artifactDir.Abs, 0o755)
			path := filepath.Join(artifactDir.Abs, fmt.Sprintf("preflight-%d.png", time.Now().UnixMilli()))
			cmd := exec.CommandContext(ctx, "screencapture", "-x", path)
			cmd.Env = r.commandEnv(nil)
			out, runErr := cmd.CombinedOutput()
			checks["screenshot_ok"] = runErr == nil
			if runErr != nil {
				warnings = append(warnings, "screencapture failed; grant Screen Recording permission to the AgentDock process")
				checks["screenshot_error"] = redactSecrets(string(out), nil)
			}
		}
	}
	if boolArg(args, "check_applescript", true) {
		cmd := exec.CommandContext(ctx, "osascript", "-e", `tell application "System Events" to count processes`)
		cmd.Env = r.commandEnv(nil)
		out, err := cmd.CombinedOutput()
		checks["applescript_ok"] = err == nil
		if err != nil {
			warnings = append(warnings, "AppleScript/System Events failed; grant Accessibility/Automation permission to the AgentDock process")
			checks["applescript_error"] = redactSecrets(string(out), nil)
		}
	}
	return Result{"ok": len(warnings) == 0, "checks": checks, "warnings": warnings}, nil
}

func (r *Runtime) desktopWindowList(ctx context.Context, args map[string]any) (Result, error) {
	if err := desktopSupported(); err != nil {
		return nil, err
	}
	script := `
set output to ""
tell application "System Events"
  repeat with p in application processes whose background only is false
    set appName to name of p
    set isFront to frontmost of p
    set windowInfo to ""
    repeat with w in windows of p
      try
        set pos to position of w
        set sz to size of w
        set windowInfo to windowInfo & name of w & "::" & item 1 of pos & "," & item 2 of pos & "::" & item 1 of sz & "," & item 2 of sz & "||"
      on error
        set windowInfo to windowInfo & name of w & "::::||"
      end try
    end repeat
    set output to output & appName & tab & isFront & tab & windowInfo & linefeed
  end repeat
end tell
return output`
	res, err := r.runAppleScript(ctx, script, args)
	if err != nil || !boolResult(res, "ok") {
		return res, err
	}
	windows := []map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(stringResult(res, "stdout")), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		item := map[string]any{"app": firstPart(parts, 0), "frontmost": firstPart(parts, 1) == "true"}
		windowItems := []map[string]any{}
		if len(parts) > 2 {
			for _, encoded := range strings.Split(parts[2], "||") {
				if strings.TrimSpace(encoded) == "" {
					continue
				}
				fields := strings.Split(encoded, "::")
				window := map[string]any{"title": firstPart(fields, 0)}
				if x, y, ok := parsePair(firstPart(fields, 1)); ok {
					window["x"] = x
					window["y"] = y
				}
				if width, height, ok := parsePair(firstPart(fields, 2)); ok {
					window["width"] = width
					window["height"] = height
				}
				windowItems = append(windowItems, window)
			}
		}
		item["windows"] = windowItems
		windows = append(windows, item)
	}
	res["windows"] = windows
	res["count"] = len(windows)
	return res, nil
}

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
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return Result{"ok": false, "error": readErr.Error(), "screenshot_path": path}, nil
	}
	info, infoErr := identifyImage(data)
	if infoErr != nil {
		return Result{"ok": false, "error": infoErr.Error(), "screenshot_path": path}, nil
	}
	res := Result{"ok": true, "screenshot_path": path, "screenshot_artifact_id": strings.TrimSuffix(name, ".png"), "width": info.Width, "height": info.Height, "size_bytes": len(data), "mime_type": info.MIME, "image_attached": false}
	if base := strings.TrimRight(r.cfg.OAuthServerURL, "/"); base != "" {
		res["screenshot_url"] = base + "/artifacts/desktop/screenshots/" + name
	}
	if boolArg(args, "include_image", boolArg(args, "include_image_base64", false)) {
		crop := cropArg(args)
		maxBytes := intArg(args, "max_bytes", 750000)
		maxWidth := intArg(args, "max_width", 1280)
		maxHeight := intArg(args, "max_height", 1280)
		format := stringArg(args, "format", "jpeg")
		quality := intArg(args, "quality", 72)
		prepared, preparedInfo, original, warnings, ok := prepareImageBytes(data, crop, maxBytes, maxWidth, maxHeight, format, quality)
		res["original"] = original
		res["image_warnings"] = warnings
		res["image_attached"] = ok
		if ok {
			res["image_base64"] = base64.StdEncoding.EncodeToString(prepared)
			res["image_mime_type"] = preparedInfo.MIME
			res["image_width"] = preparedInfo.Width
			res["image_height"] = preparedInfo.Height
			res["image_size_bytes"] = len(prepared)
		} else {
			res["image_error"] = "prepared image exceeds max_bytes or could not be encoded; use screenshot_url/path or tighter crop/max_width"
		}
	}
	return res, nil
}

func (r *Runtime) desktopClipboardSet(ctx context.Context, args map[string]any) (Result, error) {
	if err := desktopSupported(); err != nil {
		return nil, err
	}
	text := stringArg(args, "text", "")
	cmd := exec.CommandContext(ctx, "sh", "-c", "cat | pbcopy")
	cmd.Env = r.commandEnv(nil)
	cmd.Stdin = strings.NewReader(text)
	output, err := cmd.CombinedOutput()
	res := Result{"ok": err == nil, "operation": "desktop_clipboard_set", "stdout": redactSecrets(string(output), nil), "bytes": len([]byte(text))}
	if err != nil {
		res["error"] = err.Error()
		return res, nil
	}
	if boolArg(args, "verify", true) {
		verify := exec.CommandContext(ctx, "pbpaste")
		verify.Env = r.commandEnv(nil)
		got, verifyErr := verify.CombinedOutput()
		if verifyErr != nil {
			res["verified"] = false
			res["verify_error"] = verifyErr.Error()
		} else {
			res["verified"] = string(got) == text
		}
	}
	return res, nil
}

func (r *Runtime) desktopClipboardGet(ctx context.Context, args map[string]any) (Result, error) {
	if err := desktopSupported(); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "pbpaste")
	cmd.Env = r.commandEnv(nil)
	res, err := commandResult("desktop_clipboard_get", cmd)
	if err == nil && boolResult(res, "ok") {
		res["text"] = stringResult(res, "stdout")
	}
	return res, err
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
	if err := requireCliclick("desktop_click"); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "cliclick", fmt.Sprintf("c:%d,%d", x, y))
	cmd.Env = r.commandEnv(nil)
	return r.desktopCommandResult(ctx, "desktop_click", cmd, args)
}

func (r *Runtime) desktopMove(ctx context.Context, args map[string]any) (Result, error) {
	if err := desktopSupported(); err != nil {
		return nil, err
	}
	x := intArg(args, "x", -1)
	y := intArg(args, "y", -1)
	if x < 0 || y < 0 {
		return nil, toolError("INVALID_COORDINATE", "x and y must be non-negative", "validation")
	}
	if err := requireCliclick("desktop_move"); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "cliclick", fmt.Sprintf("m:%d,%d", x, y))
	cmd.Env = r.commandEnv(nil)
	return r.desktopCommandResult(ctx, "desktop_move", cmd, args)
}

func (r *Runtime) desktopDoubleClick(ctx context.Context, args map[string]any) (Result, error) {
	if err := desktopSupported(); err != nil {
		return nil, err
	}
	x := intArg(args, "x", -1)
	y := intArg(args, "y", -1)
	if x < 0 || y < 0 {
		return nil, toolError("INVALID_COORDINATE", "x and y must be non-negative", "validation")
	}
	if err := requireCliclick("desktop_double_click"); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "cliclick", fmt.Sprintf("dc:%d,%d", x, y))
	cmd.Env = r.commandEnv(nil)
	return r.desktopCommandResult(ctx, "desktop_double_click", cmd, args)
}

func (r *Runtime) desktopScroll(ctx context.Context, args map[string]any) (Result, error) {
	if err := desktopSupported(); err != nil {
		return nil, err
	}
	dx := intArg(args, "dx", 0)
	dy := intArg(args, "dy", intArg(args, "amount", 0))
	if dx == 0 && dy == 0 {
		return nil, toolError("INVALID_SCROLL", "dx/dy or amount is required", "validation")
	}
	if err := requireCliclick("desktop_scroll"); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "cliclick", fmt.Sprintf("w:%d,%d", dx, dy))
	cmd.Env = r.commandEnv(nil)
	return r.desktopCommandResult(ctx, "desktop_scroll", cmd, args)
}

func (r *Runtime) desktopDrag(ctx context.Context, args map[string]any) (Result, error) {
	if err := desktopSupported(); err != nil {
		return nil, err
	}
	fromX := intArg(args, "from_x", -1)
	fromY := intArg(args, "from_y", -1)
	toX := intArg(args, "to_x", -1)
	toY := intArg(args, "to_y", -1)
	if fromX < 0 || fromY < 0 || toX < 0 || toY < 0 {
		return nil, toolError("INVALID_COORDINATE", "from_x/from_y/to_x/to_y must be non-negative", "validation")
	}
	if err := requireCliclick("desktop_drag"); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "cliclick", fmt.Sprintf("dd:%d,%d", fromX, fromY), fmt.Sprintf("m:%d,%d", toX, toY), fmt.Sprintf("du:%d,%d", toX, toY))
	cmd.Env = r.commandEnv(nil)
	return r.desktopCommandResult(ctx, "desktop_drag", cmd, args)
}

func (r *Runtime) desktopWait(ctx context.Context, args map[string]any) (Result, error) {
	ms := intArg(args, "timeout_ms", intArg(args, "ms", 1000))
	if ms < 0 {
		ms = 0
	}
	if ms > 60000 {
		ms = 60000
	}
	timer := time.NewTimer(time.Duration(ms) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return Result{"ok": false, "operation": "desktop_wait", "error": ctx.Err().Error()}, nil
	case <-timer.C:
		return Result{"ok": true, "operation": "desktop_wait", "waited_ms": ms}, nil
	}
}

func (r *Runtime) desktopType(ctx context.Context, args map[string]any) (Result, error) {
	if err := desktopSupported(); err != nil {
		return nil, err
	}
	text := stringArg(args, "text", "")
	if text == "" {
		return nil, toolError("MISSING_TEXT", "text is required", "validation")
	}
	if err := requireCliclick("desktop_type"); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "cliclick", "t:"+text)
	cmd.Env = r.commandEnv(nil)
	return r.desktopCommandResult(ctx, "desktop_type", cmd, args)
}

func (r *Runtime) desktopHotkey(ctx context.Context, args map[string]any) (Result, error) {
	if err := desktopSupported(); err != nil {
		return nil, err
	}
	keys := strings.TrimSpace(stringArg(args, "keys", ""))
	if keys == "" {
		return nil, toolError("MISSING_KEYS", "keys is required, for example cmd+space or enter", "validation")
	}
	if err := requireCliclick("desktop_hotkey"); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "cliclick", cliclickKeyArgs(keys)...)
	cmd.Env = r.commandEnv(nil)
	return r.desktopCommandResult(ctx, "desktop_hotkey", cmd, args)
}

func (r *Runtime) runAppleScript(ctx context.Context, script string, args map[string]any) (Result, error) {
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	cmd.Env = r.commandEnv(nil)
	return commandResult("osascript", cmd)
}

func commandResult(operation string, cmd *exec.Cmd) (Result, error) {
	output, err := cmd.CombinedOutput()
	text := redactSecrets(string(output), nil)
	res := Result{"ok": err == nil, "command_ok": err == nil, "operation": operation, "stdout": text}
	if err != nil {
		res["error"] = err.Error()
	}
	applyDesktopCommandWarnings(res, text)
	return res, nil
}

func (r *Runtime) desktopCommandResult(ctx context.Context, operation string, cmd *exec.Cmd, args map[string]any) (Result, error) {
	var before []byte
	var beforePath string
	verifyMode := strings.TrimSpace(stringArg(args, "verify", ""))
	captureBefore := boolArg(args, "before_snapshot", verifyMode == "screenshot_diff")
	captureAfter := boolArg(args, "after_snapshot", verifyMode == "screenshot_diff")
	if captureBefore {
		before, beforePath = r.captureDesktopTemp(ctx, operation+"-before")
	}
	res, err := commandResult(operation, cmd)
	res["effect_verified"] = false
	res["verification"] = "not_performed"
	if beforePath != "" {
		res["before_snapshot_path"] = beforePath
	}
	waitMS := intArg(args, "wait_ms", 0)
	if waitMS > 0 {
		if waitMS > 10000 {
			waitMS = 10000
		}
		time.Sleep(time.Duration(waitMS) * time.Millisecond)
	}
	if captureAfter {
		after, afterPath := r.captureDesktopTemp(ctx, operation+"-after")
		if afterPath != "" {
			res["after_snapshot_path"] = afterPath
		}
		if len(before) > 0 && len(after) > 0 {
			if diff, ok := imageDiffPercent(before, after); ok {
				res["verification"] = "screenshot_diff"
				res["diff_percent"] = diff
				threshold := float64Arg(args, "diff_threshold", 0.002)
				res["diff_threshold"] = threshold
				res["effect_verified"] = diff >= threshold
			}
		}
	}
	return res, err
}

func desktopSupported() error {
	if runtime.GOOS != "darwin" {
		return toolErrorDetails("DESKTOP_UNSUPPORTED_OS", "desktop automation is currently macOS-only", "validation", map[string]any{"os": runtime.GOOS})
	}
	return nil
}

func requireCliclick(tool string) error {
	if _, err := exec.LookPath("cliclick"); err != nil {
		return toolErrorDetails("CLICLICK_NOT_FOUND", "cliclick is required for "+tool, "validation", map[string]any{"install": "brew install cliclick"})
	}
	return nil
}

func parsePair(value string) (int, int, bool) {
	parts := strings.Split(value, ",")
	if len(parts) != 2 {
		return 0, 0, false
	}
	var a, b int
	if _, err := fmt.Sscanf(strings.TrimSpace(parts[0]), "%d", &a); err != nil {
		return 0, 0, false
	}
	if _, err := fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &b); err != nil {
		return 0, 0, false
	}
	return a, b, true
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

func applyDesktopCommandWarnings(res Result, text string) {
	lower := strings.ToLower(text)
	warnings := []string{}
	if strings.Contains(lower, "accessibility privileges not enabled") || strings.Contains(lower, "not allowed assistive access") || strings.Contains(lower, "不允许辅助访问") || strings.Contains(lower, "不允许发送按键") {
		warnings = append(warnings, "macOS Accessibility/Automation permission is not available; command may not affect the target app")
		res["ok"] = false
		res["permission_ok"] = false
		res["error_code"] = "ACCESSIBILITY_NOT_TRUSTED"
	} else {
		res["permission_ok"] = true
	}
	if len(warnings) > 0 {
		res["warnings"] = warnings
	}
}

func (r *Runtime) captureDesktopTemp(ctx context.Context, prefix string) ([]byte, string) {
	artifactDir, err := r.resolveControlForWrite(filepath.Join(r.cfg.DesktopArtifactDir, "verification"))
	if err != nil {
		return nil, ""
	}
	_ = os.MkdirAll(artifactDir.Abs, 0o755)
	path := filepath.Join(artifactDir.Abs, fmt.Sprintf("%s-%d.png", prefix, time.Now().UnixNano()))
	cmd := exec.CommandContext(ctx, "screencapture", "-x", path)
	cmd.Env = r.commandEnv(nil)
	if out, err := cmd.CombinedOutput(); err != nil || len(out) > 0 {
		if err != nil {
			return nil, ""
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, path
	}
	return data, path
}

func cropArg(args map[string]any) *imageCrop {
	m := mapArg(args, "crop")
	if m == nil {
		return nil
	}
	return &imageCrop{X: intArg(m, "x", 0), Y: intArg(m, "y", 0), Width: intArg(m, "width", 0), Height: intArg(m, "height", 0)}
}

func float64Arg(args map[string]any, key string, fallback float64) float64 {
	v, ok := args[key]
	if !ok || v == nil {
		return fallback
	}
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case string:
		var f float64
		if _, err := fmt.Sscanf(x, "%f", &f); err == nil {
			return f
		}
	}
	return fallback
}

func cliclickKeyArgs(keys string) []string {
	parts := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(keys)), func(r rune) bool { return r == '+' || r == ',' || r == ' ' })
	replacements := map[string]string{"command": "cmd", "control": "ctrl", "option": "alt", "return": "enter"}
	mods := []string{}
	main := ""
	for _, part := range parts {
		if part == "" {
			continue
		}
		if replacement, ok := replacements[part]; ok {
			part = replacement
		}
		switch part {
		case "cmd", "ctrl", "alt", "shift", "fn":
			mods = append(mods, part)
		default:
			main = part
		}
	}
	if main == "" && len(mods) > 0 {
		main = mods[len(mods)-1]
		mods = mods[:len(mods)-1]
	}
	args := []string{}
	for _, mod := range mods {
		args = append(args, "kd:"+mod)
	}
	if len([]rune(main)) == 1 {
		args = append(args, "t:"+main)
	} else if main != "" {
		args = append(args, "kp:"+main)
	}
	for i := len(mods) - 1; i >= 0; i-- {
		args = append(args, "ku:"+mods[i])
	}
	if len(args) == 0 {
		return []string{"kp:" + normalizeCliclickKeys(keys)}
	}
	return args
}

func boolResult(res Result, key string) bool {
	value, _ := res[key].(bool)
	return value
}

func stringResult(res Result, key string) string {
	value, _ := res[key].(string)
	return value
}

func firstPart(parts []string, index int) string {
	if index >= len(parts) {
		return ""
	}
	return parts[index]
}

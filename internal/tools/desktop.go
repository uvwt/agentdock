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
	var region *desktopRect
	if crop := mapArg(args, "crop"); crop != nil {
		region = &desktopRect{X: intArg(crop, "x", 0), Y: intArg(crop, "y", 0), Width: intArg(crop, "width", 0), Height: intArg(crop, "height", 0)}
		if region.Width <= 0 || region.Height <= 0 {
			return nil, toolError("INVALID_CROP", "crop width and height must be positive", "validation")
		}
	}
	path, name, out, err := r.captureDesktopScreenshot(ctx, "screenshots", "desktop", region)
	if err != nil {
		return Result{"ok": false, "operation": "desktop_snapshot", "error": err.Error(), "stdout": out, "error_layer": "screenshot"}, nil
	}
	res, buildErr := r.desktopScreenshotResult(path, name, "desktop_snapshot", args)
	if buildErr != nil {
		return Result{"ok": false, "operation": "desktop_snapshot", "error": buildErr.Error(), "screenshot_path": path, "error_layer": "screenshot"}, nil
	}
	res["action_coordinate_space"] = "screen_points"
	res["screenshot_coordinate_space"] = "image_pixels"
	if region != nil {
		res["screen_region"] = region.Result()
	}
	return res, nil
}

func (r *Runtime) desktopSnapshotApp(ctx context.Context, args map[string]any) (Result, error) {
	if err := desktopSupported(); err != nil {
		return nil, err
	}
	app := strings.TrimSpace(stringArg(args, "app", ""))
	if app == "" {
		return nil, toolError("MISSING_APP", "app is required", "validation")
	}
	info, failure := r.prepareDesktopAppWindow(ctx, args, "desktop_snapshot_app")
	if failure != nil {
		return failure, nil
	}
	if info == nil || info.Width <= 0 || info.Height <= 0 {
		return desktopFailure("desktop_snapshot_app", "WINDOW_NOT_VISIBLE", "target app has no visible window", "window"), nil
	}
	rel := desktopRect{X: 0, Y: 0, Width: info.Width, Height: info.Height}
	if crop := mapArg(args, "crop"); crop != nil {
		rel.X = intArg(crop, "x", 0)
		rel.Y = intArg(crop, "y", 0)
		rel.Width = intArg(crop, "width", info.Width-rel.X)
		rel.Height = intArg(crop, "height", info.Height-rel.Y)
	}
	if rel.Width <= 0 || rel.Height <= 0 {
		return nil, toolError("INVALID_CROP", "crop width and height must be positive", "validation")
	}
	abs := desktopRect{X: info.X + rel.X, Y: info.Y + rel.Y, Width: rel.Width, Height: rel.Height}
	path, name, out, err := r.captureDesktopScreenshot(ctx, "screenshots", "desktop-app", &abs)
	if err != nil {
		return Result{"ok": false, "operation": "desktop_snapshot_app", "error": err.Error(), "stdout": out, "error_layer": "screenshot", "target_window": info.Result()}, nil
	}
	res, buildErr := r.desktopScreenshotResult(path, name, "desktop_snapshot_app", args)
	if buildErr != nil {
		return Result{"ok": false, "operation": "desktop_snapshot_app", "error": buildErr.Error(), "screenshot_path": path, "error_layer": "screenshot", "target_window": info.Result()}, nil
	}
	res["target_window"] = info.Result()
	res["crop"] = rel.Result()
	res["screen_region"] = abs.Result()
	res["action_coordinate_space"] = "window_points"
	res["screenshot_coordinate_space"] = "image_pixels"
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
	app := strings.TrimSpace(stringArg(args, "app", ""))
	elementIndex := strings.TrimSpace(stringArg(args, "element_index", ""))
	clickCount := intArg(args, "click_count", 1)
	button := strings.ToLower(strings.TrimSpace(stringArg(args, "mouse_button", "left")))
	if elementIndex != "" {
		if app == "" {
			return nil, toolError("MISSING_APP", "app is required when element_index is provided", "validation")
		}
		if button == "" || button == "left" {
			return r.desktopAXClick(ctx, app, elementIndex, clickCount)
		}
		x, y, w, h, bounds, err := r.desktopAXElementBounds(ctx, app, elementIndex)
		if err != nil || !boolResult(bounds, "ok") {
			return bounds, err
		}
		args["x"] = x + w/2
		args["y"] = y + h/2
	}
	x := intArg(args, "x", -1)
	y := intArg(args, "y", -1)
	if x < 0 || y < 0 {
		return nil, toolError("INVALID_COORDINATE", "x/y or app+element_index is required", "validation")
	}
	if clickCount <= 0 {
		clickCount = 1
	}
	if clickCount > 5 {
		clickCount = 5
	}
	point, failure := r.resolveDesktopPoint(ctx, args, "desktop_click", desktopPoint{X: x, Y: y})
	if failure != nil {
		return failure, nil
	}
	x, y = point.X, point.Y
	if err := requireCliclick("desktop_click"); err != nil {
		return nil, err
	}
	buttonPrefix := "c"
	switch button {
	case "", "left":
		buttonPrefix = "c"
	case "right":
		buttonPrefix = "rc"
	case "middle":
		buttonPrefix = "mc"
	default:
		return nil, toolError("INVALID_MOUSE_BUTTON", "mouse_button must be left, right, or middle", "validation")
	}
	clicks := []string{}
	for i := 0; i < clickCount; i++ {
		clicks = append(clicks, fmt.Sprintf("%s:%d,%d", buttonPrefix, x, y))
	}
	cmd := exec.CommandContext(ctx, "cliclick", clicks...)
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
	point, failure := r.resolveDesktopPoint(ctx, args, "desktop_move", desktopPoint{X: x, Y: y})
	if failure != nil {
		return failure, nil
	}
	x, y = point.X, point.Y
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
	point, failure := r.resolveDesktopPoint(ctx, args, "desktop_double_click", desktopPoint{X: x, Y: y})
	if failure != nil {
		return failure, nil
	}
	x, y = point.X, point.Y
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
	app := strings.TrimSpace(stringArg(args, "app", ""))
	elementIndex := strings.TrimSpace(stringArg(args, "element_index", ""))
	direction := strings.ToLower(strings.TrimSpace(stringArg(args, "direction", "")))
	pages := float64Arg(args, "pages", 1)
	dx := intArg(args, "dx", 0)
	dy := intArg(args, "dy", intArg(args, "amount", 0))
	if direction != "" {
		amount := int(10 * pages)
		if amount == 0 {
			amount = 1
		}
		switch direction {
		case "up":
			dy = amount
		case "down":
			dy = -amount
		case "left":
			dx = amount
		case "right":
			dx = -amount
		default:
			return nil, toolError("INVALID_SCROLL_DIRECTION", "direction must be up, down, left, or right", "validation")
		}
	}
	if elementIndex != "" {
		if app == "" {
			return nil, toolError("MISSING_APP", "app is required when element_index is provided", "validation")
		}
		x, y, w, h, bounds, err := r.desktopAXElementBounds(ctx, app, elementIndex)
		if err != nil || !boolResult(bounds, "ok") {
			return bounds, err
		}
		if err := requireCliclick("desktop_scroll"); err != nil {
			return nil, err
		}
		cmd := exec.CommandContext(ctx, "cliclick", fmt.Sprintf("m:%d,%d", x+w/2, y+h/2), fmt.Sprintf("w:%d,%d", dx, dy))
		cmd.Env = r.commandEnv(nil)
		return r.desktopCommandResult(ctx, "desktop_scroll", cmd, args)
	}
	if dx == 0 && dy == 0 {
		return nil, toolError("INVALID_SCROLL", "dx/dy/amount or direction is required", "validation")
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
	info, failure := r.prepareDesktopAppWindow(ctx, args, "desktop_drag")
	if failure != nil {
		return failure, nil
	}
	from := desktopPoint{X: fromX, Y: fromY}
	to := desktopPoint{X: toX, Y: toY}
	if strings.EqualFold(strings.TrimSpace(stringArg(args, "space", "screen")), "window") {
		if info == nil {
			return desktopFailure("desktop_drag", "MISSING_APP", "app is required when space=window", "validation"), nil
		}
		if boolArg(args, "fail_if_coordinate_outside_window", false) && (!pointInsideWindow(from, info) || !pointInsideWindow(to, info)) {
			res := desktopFailure("desktop_drag", "COORDINATE_OUTSIDE_WINDOW", "coordinate is outside the target window", "validation")
			res["target_window"] = info.Result()
			return res, nil
		}
		from.X += info.X
		from.Y += info.Y
		to.X += info.X
		to.Y += info.Y
	}
	if err := requireCliclick("desktop_drag"); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "cliclick", buildDesktopDragCommands(args, from, to)...)
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
	app := strings.TrimSpace(stringArg(args, "app", ""))
	text := stringArg(args, "text", "")
	strategy := strings.ToLower(strings.TrimSpace(stringArg(args, "strategy", "auto")))
	if text == "" {
		return nil, toolError("MISSING_TEXT", "text is required", "validation")
	}
	if app != "" {
		if err := desktopRequireRecentAppState(desktopAppLookupKey(app)); err != nil {
			return nil, err
		}
		_ = r.desktopActivateApp(ctx, app)
	}
	useClipboard := strategy == "clipboard" || (strategy == "auto" && desktopPreferClipboardTyping(text))
	if useClipboard {
		clip, err := r.desktopClipboardSet(ctx, map[string]any{"text": text, "verify": true})
		if err != nil || !boolResult(clip, "ok") {
			return clip, err
		}
		pasteArgs := map[string]any{"keys": "cmd+v"}
		for k, v := range args {
			pasteArgs[k] = v
		}
		res, err := r.desktopHotkey(ctx, pasteArgs)
		res["operation"] = "desktop_type"
		res["strategy"] = "clipboard"
		res["clipboard_verified"] = clip["verified"]
		res["bytes"] = len([]byte(text))
		return res, err
	}
	if strategy != "auto" && strategy != "keyboard" {
		return nil, toolError("INVALID_TYPE_STRATEGY", "strategy must be auto, keyboard, or clipboard", "validation")
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
	verifyRequested := boolArg(args, "verify", false) || strings.EqualFold(strings.TrimSpace(stringArg(args, "verify", "")), "screenshot_diff")
	captureBefore := boolArg(args, "before_snapshot", verifyRequested)
	captureAfter := boolArg(args, "after_snapshot", verifyRequested)
	info, _ := r.desktopWindowInfoForArgs(ctx, args)
	region := verifyRegionFromArgs(args, info)
	if captureBefore {
		before, beforePath = r.captureDesktopTemp(ctx, operation+"-before", region)
	}
	res, err := commandResult(operation, cmd)
	res["effect_verified"] = false
	res["effect_changed"] = false
	res["verification"] = "not_requested"
	if beforePath != "" {
		res["before_snapshot_path"] = beforePath
		res["before_screenshot_path"] = beforePath
	}
	if info != nil {
		res["target_window"] = info.Result()
	}
	waitMS := intArg(args, "wait_ms", 0)
	if verifyRequested && waitMS == 0 {
		waitMS = 250
	}
	if waitMS > 0 {
		if waitMS > 10000 {
			waitMS = 10000
		}
		time.Sleep(time.Duration(waitMS) * time.Millisecond)
	}
	if captureAfter {
		after, afterPath := r.captureDesktopTemp(ctx, operation+"-after", region)
		if afterPath != "" {
			res["after_snapshot_path"] = afterPath
			res["after_screenshot_path"] = afterPath
		}
		if len(before) > 0 && len(after) > 0 {
			if diff, ok := imageDiffPercent(before, after); ok {
				res["verification"] = "screenshot_diff"
				res["diff_percent"] = diff
				res["diff_score"] = diff
				threshold := float64Arg(args, "diff_threshold", 0.002)
				res["diff_threshold"] = threshold
				res["effect_verified"] = true
				res["effect_changed"] = diff >= threshold
				if diff < threshold && verifyRequested {
					res["error_layer"] = "verification"
				}
			} else {
				res["verification"] = "diff_failed"
				res["error_layer"] = "verification"
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

func (r *Runtime) captureDesktopTemp(ctx context.Context, prefix string, region *desktopRect) ([]byte, string) {
	artifactDir, err := r.resolveControlForWrite(filepath.Join(r.cfg.DesktopArtifactDir, "verification"))
	if err != nil {
		return nil, ""
	}
	_ = os.MkdirAll(artifactDir.Abs, 0o755)
	path := filepath.Join(artifactDir.Abs, fmt.Sprintf("%s-%d.png", prefix, time.Now().UnixNano()))
	args := []string{"-x"}
	if region != nil {
		args = append(args, "-R", fmt.Sprintf("%d,%d,%d,%d", region.X, region.Y, region.Width, region.Height))
	}
	args = append(args, path)
	cmd := exec.CommandContext(ctx, "screencapture", args...)
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

func desktopPreferClipboardTyping(text string) bool {
	if len([]byte(text)) > 80 || strings.Contains(text, "\n") || strings.Contains(text, "\r") || strings.Contains(text, "\t") {
		return true
	}
	for _, r := range text {
		if r > 127 {
			return true
		}
	}
	return false
}

type desktopPoint struct {
	X int
	Y int
}

type desktopRect struct {
	X      int
	Y      int
	Width  int
	Height int
}

func (r desktopRect) Result() map[string]any {
	return map[string]any{"x": r.X, "y": r.Y, "width": r.Width, "height": r.Height}
}

type desktopWindowInfo struct {
	App       string
	Title     string
	Frontmost bool
	X         int
	Y         int
	Width     int
	Height    int
}

func (w *desktopWindowInfo) Result() map[string]any {
	if w == nil {
		return nil
	}
	return map[string]any{"app": w.App, "title": w.Title, "frontmost": w.Frontmost, "x": w.X, "y": w.Y, "width": w.Width, "height": w.Height}
}

func (r *Runtime) desktopScreenshotResult(path, name, operation string, args map[string]any) (Result, error) {
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return nil, readErr
	}
	info, infoErr := identifyImage(data)
	if infoErr != nil {
		return nil, infoErr
	}
	res := Result{"ok": true, "operation": operation, "screenshot_path": path, "screenshot_artifact_id": strings.TrimSuffix(name, ".png"), "width": info.Width, "height": info.Height, "size_bytes": len(data), "mime_type": info.MIME, "image_attached": false}
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

func (r *Runtime) captureDesktopScreenshot(ctx context.Context, subdir, prefix string, region *desktopRect) (string, string, string, error) {
	artifactDir, err := r.resolveControlForWrite(filepath.Join(r.cfg.DesktopArtifactDir, subdir))
	if err != nil {
		return "", "", "", err
	}
	if err := os.MkdirAll(artifactDir.Abs, 0o755); err != nil {
		return "", "", "", err
	}
	name := fmt.Sprintf("%s-%d.png", prefix, time.Now().UnixNano())
	path := filepath.Join(artifactDir.Abs, name)
	args := []string{"-x"}
	if region != nil {
		args = append(args, "-R", fmt.Sprintf("%d,%d,%d,%d", region.X, region.Y, region.Width, region.Height))
	}
	args = append(args, path)
	cmd := exec.CommandContext(ctx, "screencapture", args...)
	cmd.Env = r.commandEnv(nil)
	output, err := cmd.CombinedOutput()
	return path, name, redactSecrets(string(output), nil), err
}

func (r *Runtime) resolveDesktopPoint(ctx context.Context, args map[string]any, operation string, point desktopPoint) (desktopPoint, Result) {
	if !strings.EqualFold(strings.TrimSpace(stringArg(args, "space", "screen")), "window") {
		return point, nil
	}
	info, failure := r.prepareDesktopAppWindow(ctx, args, operation)
	if failure != nil {
		return point, failure
	}
	if info == nil {
		return point, desktopFailure(operation, "MISSING_APP", "app is required when space=window", "validation")
	}
	if boolArg(args, "fail_if_coordinate_outside_window", false) && !pointInsideWindow(point, info) {
		res := desktopFailure(operation, "COORDINATE_OUTSIDE_WINDOW", "coordinate is outside the target window", "validation")
		res["target_window"] = info.Result()
		return point, res
	}
	return desktopPoint{X: info.X + point.X, Y: info.Y + point.Y}, nil
}

func (r *Runtime) desktopWindowInfoForArgs(ctx context.Context, args map[string]any) (*desktopWindowInfo, Result) {
	app := strings.TrimSpace(stringArg(args, "app", ""))
	if app == "" {
		return nil, nil
	}
	return r.prepareDesktopAppWindow(ctx, args, "desktop_action")
}

func (r *Runtime) prepareDesktopAppWindow(ctx context.Context, args map[string]any, operation string) (*desktopWindowInfo, Result) {
	app := strings.TrimSpace(stringArg(args, "app", ""))
	if app == "" {
		return nil, nil
	}
	if boolArg(args, "focus_if_needed", false) {
		if err := r.desktopActivateApp(ctx, app); err != nil {
			return nil, desktopFailure(operation, "FOCUS_APP_FAILED", err.Error(), "focus")
		}
		time.Sleep(250 * time.Millisecond)
	}
	info, err := r.getDesktopWindowInfo(ctx, app)
	if err != nil {
		return nil, desktopFailure(operation, "WINDOW_QUERY_FAILED", err.Error(), "window")
	}
	if info == nil {
		if boolArg(args, "fail_if_window_not_visible", false) || strings.EqualFold(strings.TrimSpace(stringArg(args, "space", "screen")), "window") {
			return nil, desktopFailure(operation, "WINDOW_NOT_VISIBLE", "target app has no visible window", "window")
		}
		return nil, nil
	}
	if boolArg(args, "require_frontmost", false) && !info.Frontmost {
		res := desktopFailure(operation, "APP_NOT_FRONTMOST", "target app is not frontmost", "focus")
		res["target_window"] = info.Result()
		return nil, res
	}
	return info, nil
}

func (r *Runtime) getDesktopWindowInfo(ctx context.Context, app string) (*desktopWindowInfo, error) {
	lookup := desktopAppLookupKey(app)
	script := fmt.Sprintf(`
tell application "System Events"
  if not (exists application process %q) then return ""
  tell application process %q
    set isFront to frontmost
    repeat with w in windows
      try
        set pos to position of w
        set sz to size of w
        return name of w & tab & isFront & tab & item 1 of pos & "," & item 2 of pos & tab & item 1 of sz & "," & item 2 of sz
      end try
    end repeat
  end tell
end tell
return ""`, lookup, lookup)
	res, err := r.runAppleScript(ctx, script, nil)
	if err != nil {
		return nil, err
	}
	if !boolResult(res, "ok") {
		return nil, fmt.Errorf("%s", stringResult(res, "stdout"))
	}
	line := strings.TrimSpace(stringResult(res, "stdout"))
	if line == "" {
		return nil, nil
	}
	parts := strings.Split(line, "\t")
	if len(parts) < 4 {
		return nil, fmt.Errorf("unexpected window metadata: %s", line)
	}
	x, y, ok := parsePair(parts[2])
	if !ok {
		return nil, fmt.Errorf("unexpected window position: %s", parts[2])
	}
	w, h, ok := parsePair(parts[3])
	if !ok {
		return nil, fmt.Errorf("unexpected window size: %s", parts[3])
	}
	return &desktopWindowInfo{App: lookup, Title: parts[0], Frontmost: parts[1] == "true", X: x, Y: y, Width: w, Height: h}, nil
}

func buildDesktopDragCommands(args map[string]any, from, to desktopPoint) []string {
	commands := []string{fmt.Sprintf("dd:%d,%d", from.X, from.Y)}
	holdMS := boundedDesktopMS(intArg(args, "hold_ms", 0), 10000)
	if holdMS > 0 {
		commands = append(commands, fmt.Sprintf("w:%d", holdMS))
	}
	steps := intArg(args, "steps", 1)
	if steps < 1 {
		steps = 1
	}
	if steps > 200 {
		steps = 200
	}
	durationMS := boundedDesktopMS(intArg(args, "duration_ms", 0), 30000)
	perStep := 0
	if steps > 1 && durationMS > 0 {
		perStep = durationMS / steps
	}
	for i := 1; i <= steps; i++ {
		x := from.X + ((to.X-from.X)*i)/steps
		y := from.Y + ((to.Y-from.Y)*i)/steps
		commands = append(commands, fmt.Sprintf("m:%d,%d", x, y))
		if perStep > 0 && i < steps {
			commands = append(commands, fmt.Sprintf("w:%d", perStep))
		}
	}
	releaseWaitMS := boundedDesktopMS(intArg(args, "release_wait_ms", 0), 10000)
	if releaseWaitMS > 0 {
		commands = append(commands, fmt.Sprintf("w:%d", releaseWaitMS))
	}
	commands = append(commands, fmt.Sprintf("du:%d,%d", to.X, to.Y))
	return commands
}

func verifyRegionFromArgs(args map[string]any, info *desktopWindowInfo) *desktopRect {
	region := mapArg(args, "verify_region")
	if region == nil {
		return nil
	}
	rect := &desktopRect{X: intArg(region, "x", 0), Y: intArg(region, "y", 0), Width: intArg(region, "width", 0), Height: intArg(region, "height", 0)}
	if rect.Width <= 0 || rect.Height <= 0 {
		return nil
	}
	space := strings.TrimSpace(fmt.Sprint(region["space"]))
	if strings.EqualFold(space, "window") && info != nil {
		rect.X += info.X
		rect.Y += info.Y
	}
	return rect
}

func desktopFailure(operation, code, message, layer string) Result {
	return Result{"ok": false, "operation": operation, "command_ok": false, "effect_verified": false, "effect_changed": false, "error": message, "error_code": code, "error_layer": layer}
}

func pointInsideWindow(p desktopPoint, w *desktopWindowInfo) bool {
	return w != nil && p.X >= 0 && p.Y >= 0 && p.X < w.Width && p.Y < w.Height
}

func boundedDesktopMS(ms, max int) int {
	if ms < 0 {
		return 0
	}
	if ms > max {
		return max
	}
	return ms
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

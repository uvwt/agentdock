package tools

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var desktopStateGuard sync.Map

const desktopAXSep = "\x1f"

func (r *Runtime) desktopListApps(ctx context.Context, args map[string]any) (Result, error) {
	if err := desktopSupported(); err != nil {
		return nil, err
	}
	script := `
on run argv
  set sep to ASCII character 31
  set output to ""
  tell application "System Events"
    repeat with p in application processes whose background only is false
      set appName to ""
      set pidText to ""
      set bundleText to ""
      set frontText to "false"
      try
        set appName to name of p as text
      end try
      try
        set pidText to unix id of p as text
      end try
      try
        set bundleText to bundle identifier of p as text
      end try
      try
        set frontText to frontmost of p as text
      end try
      set output to output & "RUNNING" & sep & appName & sep & pidText & sep & bundleText & sep & frontText & linefeed
    end repeat
  end tell
  return output
end run`
	res, err := runAppleScriptArgs(ctx, r, "desktop_list_apps", script)
	if err != nil || !boolResult(res, "ok") {
		return res, err
	}
	running := []map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(stringResult(res, "stdout")), "\n") {
		parts := strings.Split(line, desktopAXSep)
		if len(parts) < 5 || parts[0] != "RUNNING" {
			continue
		}
		item := map[string]any{"name": parts[1], "bundle_id": parts[3], "frontmost": parts[4] == "true"}
		if pid, convErr := strconv.Atoi(parts[2]); convErr == nil {
			item["pid"] = pid
		}
		running = append(running, item)
	}
	recent := r.desktopRecentApps(ctx, intArg(args, "max_recent", 50))
	res["running"] = running
	res["count"] = len(running)
	res["recent"] = recent
	return res, nil
}

func (r *Runtime) desktopGetAppState(ctx context.Context, args map[string]any) (Result, error) {
	if err := desktopSupported(); err != nil {
		return nil, err
	}
	app := strings.TrimSpace(stringArg(args, "app", ""))
	if app == "" {
		return nil, toolError("MISSING_APP", "app is required", "validation")
	}
	lookup := desktopAppLookupKey(app)
	if boolArg(args, "activate", true) {
		if err := r.desktopActivateApp(ctx, app); err != nil {
			return Result{"ok": false, "operation": "desktop_get_app_state", "app": app, "error": err.Error()}, nil
		}
		select {
		case <-ctx.Done():
			return Result{"ok": false, "operation": "desktop_get_app_state", "app": app, "error": ctx.Err().Error()}, nil
		case <-time.After(350 * time.Millisecond):
		}
	}
	maxDepth := intArg(args, "ax_max_depth", 8)
	maxNodes := intArg(args, "ax_max_nodes", 300)
	if maxDepth < 0 {
		maxDepth = 0
	}
	if maxDepth > 20 {
		maxDepth = 20
	}
	if maxNodes <= 0 {
		maxNodes = 300
	}
	if maxNodes > 2000 {
		maxNodes = 2000
	}
	axState, axErr := r.desktopReadAXState(ctx, lookup, maxDepth, maxNodes)
	snap, snapErr := r.desktopSnapshot(ctx, args)
	res := Result{"ok": true, "operation": "desktop_get_app_state", "app": app, "lookup_app": lookup, "ax_max_depth": maxDepth, "ax_max_nodes": maxNodes}
	if snap != nil {
		for k, v := range snap {
			res[k] = v
		}
	}
	if snapErr != nil {
		res["screenshot_error"] = snapErr.Error()
	}
	if axErr != nil {
		res["accessibility_ok"] = false
		res["accessibility_error"] = axErr.Error()
		res["warnings"] = []string{"failed to read accessibility tree; grant Accessibility permission and ensure the app has a visible window"}
	} else {
		for k, v := range axState {
			res[k] = v
		}
		res["accessibility_ok"] = true
	}
	res["coordinate_space"] = map[string]any{"origin": "top_left_global_display", "x_y_units": "macos_screen_points_for_actions; screenshot pixels are reported separately", "screenshot_width": res["width"], "screenshot_height": res["height"]}
	desktopRememberAppState(lookup)
	return res, nil
}

func (r *Runtime) desktopSetValue(ctx context.Context, args map[string]any) (Result, error) {
	if err := desktopSupported(); err != nil {
		return nil, err
	}
	app := strings.TrimSpace(stringArg(args, "app", ""))
	index := strings.TrimSpace(stringArg(args, "element_index", ""))
	value := stringArg(args, "value", "")
	if app == "" {
		return nil, toolError("MISSING_APP", "app is required", "validation")
	}
	if index == "" {
		return nil, toolError("MISSING_ELEMENT_INDEX", "element_index is required", "validation")
	}
	lookup := desktopAppLookupKey(app)
	if err := desktopRequireRecentAppState(lookup); err != nil {
		return nil, err
	}
	_ = r.desktopActivateApp(ctx, app)
	script := desktopAXOperateScript("set_value")
	res, err := runAppleScriptArgs(ctx, r, "desktop_set_value", script, lookup, index, value)
	res["app"] = app
	res["element_index"] = index
	res["bytes"] = len([]byte(value))
	return res, err
}

func (r *Runtime) desktopPerformSecondaryAction(ctx context.Context, args map[string]any) (Result, error) {
	if err := desktopSupported(); err != nil {
		return nil, err
	}
	app := strings.TrimSpace(stringArg(args, "app", ""))
	index := strings.TrimSpace(stringArg(args, "element_index", ""))
	action := strings.TrimSpace(stringArg(args, "action", ""))
	if app == "" {
		return nil, toolError("MISSING_APP", "app is required", "validation")
	}
	if index == "" {
		return nil, toolError("MISSING_ELEMENT_INDEX", "element_index is required", "validation")
	}
	if action == "" {
		return nil, toolError("MISSING_ACTION", "action is required", "validation")
	}
	lookup := desktopAppLookupKey(app)
	if err := desktopRequireRecentAppState(lookup); err != nil {
		return nil, err
	}
	_ = r.desktopActivateApp(ctx, app)
	res, err := runAppleScriptArgs(ctx, r, "desktop_perform_secondary_action", desktopAXOperateScript("action"), lookup, index, action)
	res["app"] = app
	res["element_index"] = index
	res["action"] = action
	return res, err
}

func (r *Runtime) desktopAXClick(ctx context.Context, app, index string, clickCount int) (Result, error) {
	lookup := desktopAppLookupKey(app)
	if err := desktopRequireRecentAppState(lookup); err != nil {
		return nil, err
	}
	_ = r.desktopActivateApp(ctx, app)
	if clickCount <= 0 {
		clickCount = 1
	}
	if clickCount > 5 {
		clickCount = 5
	}
	res, err := runAppleScriptArgs(ctx, r, "desktop_click", desktopAXOperateScript("click"), lookup, index, strconv.Itoa(clickCount))
	res["app"] = app
	res["element_index"] = index
	res["click_count"] = clickCount
	res["effect_verified"] = false
	res["verification"] = "not_performed"
	return res, err
}

func (r *Runtime) desktopAXElementBounds(ctx context.Context, app, index string) (int, int, int, int, Result, error) {
	lookup := desktopAppLookupKey(app)
	if err := desktopRequireRecentAppState(lookup); err != nil {
		return 0, 0, 0, 0, nil, err
	}
	res, err := runAppleScriptArgs(ctx, r, "desktop_element_bounds", desktopAXOperateScript("bounds"), lookup, index, "")
	if err != nil || !boolResult(res, "ok") {
		return 0, 0, 0, 0, res, err
	}
	parts := strings.Split(strings.TrimSpace(stringResult(res, "stdout")), desktopAXSep)
	if len(parts) < 6 || parts[0] != "BOUNDS" {
		res["ok"] = false
		res["error"] = "invalid bounds response"
		return 0, 0, 0, 0, res, nil
	}
	x, _ := strconv.Atoi(parts[2])
	y, _ := strconv.Atoi(parts[3])
	w, _ := strconv.Atoi(parts[4])
	h, _ := strconv.Atoi(parts[5])
	res["x"] = x
	res["y"] = y
	res["width"] = w
	res["height"] = h
	return x, y, w, h, res, nil
}

func (r *Runtime) desktopActivateApp(ctx context.Context, app string) error {
	app = strings.TrimSpace(app)
	if app == "" {
		return toolError("MISSING_APP", "app is required", "validation")
	}
	var cmd *exec.Cmd
	switch {
	case strings.HasPrefix(app, "/") || strings.HasSuffix(strings.ToLower(app), ".app"):
		cmd = exec.CommandContext(ctx, "open", app)
	case strings.Count(app, ".") >= 2 && !strings.Contains(app, " "):
		cmd = exec.CommandContext(ctx, "open", "-b", app)
	default:
		cmd = exec.CommandContext(ctx, "open", "-a", app)
	}
	cmd.Env = r.commandEnv(nil)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("activate app failed: %s: %s", err.Error(), redactSecrets(string(out), nil))
	}
	return nil
}

func (r *Runtime) desktopReadAXState(ctx context.Context, app string, maxDepth, maxNodes int) (Result, error) {
	script := `
on run argv
  set appQuery to item 1 of argv
  set maxDepth to (item 2 of argv) as integer
  set maxNodes to (item 3 of argv) as integer
  set sep to ASCII character 31
  set output to ""
  set nodeCount to 0
  script walker
    property sep : missing value
    property output : ""
    property maxDepth : 8
    property maxNodes : 300
    property nodeCount : 0
    on joinList(xs, d)
      set oldDelims to AppleScript's text item delimiters
      set AppleScript's text item delimiters to d
      set t to xs as text
      set AppleScript's text item delimiters to oldDelims
      return t
    end joinList
    on cleanText(v)
      try
        set t to v as text
        set oldDelims to AppleScript's text item delimiters
        set AppleScript's text item delimiters to {return, linefeed, tab, sep}
        set parts to text items of t
        set AppleScript's text item delimiters to " "
        set t to parts as text
        set AppleScript's text item delimiters to oldDelims
        return t
      on error
        return ""
      end try
    end cleanText
    on lineFor(e, idx, parentIdx, depth)
      set roleText to ""
      set subroleText to ""
      set titleText to ""
      set descriptionText to ""
      set valueText to ""
      set enabledText to ""
      set focusedText to ""
      set xText to ""
      set yText to ""
      set wText to ""
      set hText to ""
      set actionsText to ""
      set settableText to "false"
      try
        set roleText to my cleanText(role of e)
      end try
      try
        set subroleText to my cleanText(subrole of e)
      end try
      try
        set titleText to my cleanText(title of e)
      end try
      try
        set descriptionText to my cleanText(description of e)
      end try
      try
        set valueText to my cleanText(value of e)
      end try
      try
        set enabledText to enabled of e as text
      end try
      try
        set focusedText to focused of e as text
      end try
      try
        tell application "System Events" to set posValue to position of e
        set xText to item 1 of posValue as text
        set yText to item 2 of posValue as text
      end try
      try
        tell application "System Events" to set sizeValue to size of e
        set wText to item 1 of sizeValue as text
        set hText to item 2 of sizeValue as text
      end try
      try
        set actionNames to {}
        repeat with a in actions of e
          try
            set end of actionNames to my cleanText(name of a)
          end try
        end repeat
        set actionsText to my joinList(actionNames, ",")
      end try
      if roleText contains "text" or roleText contains "Text" or roleText contains "field" or roleText contains "Field" or roleText contains "slider" or roleText contains "Slider" or roleText contains "combo" or roleText contains "Combo" then set settableText to "true"
      return "NODE" & sep & idx & sep & parentIdx & sep & (depth as text) & sep & roleText & sep & subroleText & sep & titleText & sep & descriptionText & sep & valueText & sep & enabledText & sep & focusedText & sep & xText & sep & yText & sep & wText & sep & hText & sep & actionsText & sep & settableText
    end lineFor
    on walk(e, idx, parentIdx, depth)
      if nodeCount >= maxNodes then return
      set nodeCount to nodeCount + 1
      set output to output & my lineFor(e, idx, parentIdx, depth) & linefeed
      if depth >= maxDepth then return
      try
        tell application "System Events" to set childList to UI elements of e
        set i to 1
        repeat with c in childList
          if nodeCount >= maxNodes then exit repeat
          my walk(c, idx & "." & (i as text), idx, (depth + 1))
          set i to i + 1
        end repeat
      end try
    end walk
  end script
  set walker's sep to sep
  set walker's maxDepth to maxDepth
  set walker's maxNodes to maxNodes
  tell application "System Events"
    set matches to application processes whose name is appQuery
    if (count of matches) is 0 then
      try
        set matches to application processes whose bundle identifier is appQuery
      end try
    end if
    if (count of matches) is 0 then error "APP_NOT_RUNNING: " & appQuery
    set p to item 1 of matches
    set appName to name of p as text
    set pidText to ""
    set bundleText to ""
    set frontText to "false"
    try
      set pidText to unix id of p as text
    end try
    try
      set bundleText to bundle identifier of p as text
    end try
    try
      set frontText to frontmost of p as text
    end try
    set winTitle to ""
    set winX to ""
    set winY to ""
    set winW to ""
    set winH to ""
    tell p
      if (count of windows) is 0 then error "NO_WINDOWS: " & appName
      set w to window 1
      try
        set winTitle to name of w as text
      end try
      try
        set winPos to position of w
        set winX to item 1 of winPos as text
        set winY to item 2 of winPos as text
      end try
      try
        set winSize to size of w
        set winW to item 1 of winSize as text
        set winH to item 2 of winSize as text
      end try
      set output to "META" & sep & appName & sep & pidText & sep & bundleText & sep & frontText & sep & winTitle & sep & winX & sep & winY & sep & winW & sep & winH & linefeed
      tell walker to walk(w, "0", "", 0)
      set output to output & walker's output
    end tell
  end tell
  return output
end run`
	res, err := runAppleScriptArgs(ctx, r, "desktop_get_app_state_ax", script, app, strconv.Itoa(maxDepth), strconv.Itoa(maxNodes))
	if err != nil || !boolResult(res, "ok") {
		msg := stringResult(res, "stdout")
		if strings.TrimSpace(msg) == "" {
			msg = stringResult(res, "error")
		}
		return res, fmt.Errorf(msg)
	}
	return parseDesktopAXState(stringResult(res, "stdout")), nil
}

func desktopAXOperateScript(mode string) string {
	return `
on run argv
  set appQuery to item 1 of argv
  set targetIndex to item 2 of argv
  set payload to ""
  if (count of argv) >= 3 then set payload to item 3 of argv
  set sep to ASCII character 31
  script finder
    property targetIndex : ""
    property payload : ""
    property sep : missing value
    property found : false
    property resultText : ""
    on cleanText(v)
      try
        return v as text
      on error
        return ""
      end try
    end cleanText
    on operate(e, idx)
      if idx is not targetIndex then return false
      set found to true
      if "` + mode + `" is "click" then
        set n to payload as integer
        if n < 1 then set n to 1
        repeat n times
          tell application "System Events" to click e
          delay 0.05
        end repeat
        set resultText to "CLICK" & sep & idx
      else if "` + mode + `" is "set_value" then
        set beforeText to ""
        set afterText to ""
        try
          set beforeText to my cleanText(value of e)
        end try
        tell application "System Events" to set value of e to payload
        delay 0.05
        try
          set afterText to my cleanText(value of e)
        end try
        set resultText to "SET" & sep & idx & sep & beforeText & sep & afterText
      else if "` + mode + `" is "action" then
        tell application "System Events" to perform action (payload as text) of e
        set resultText to "ACTION" & sep & idx & sep & payload
      else if "` + mode + `" is "bounds" then
        set xText to ""
        set yText to ""
        set wText to ""
        set hText to ""
        try
          tell application "System Events" to set posValue to position of e
          set xText to item 1 of posValue as text
          set yText to item 2 of posValue as text
        end try
        try
          tell application "System Events" to set sizeValue to size of e
          set wText to item 1 of sizeValue as text
          set hText to item 2 of sizeValue as text
        end try
        set resultText to "BOUNDS" & sep & idx & sep & xText & sep & yText & sep & wText & sep & hText
      end if
      return true
    end operate
    on walk(e, idx)
      if found then return
      if my operate(e, idx) then return
      try
        tell application "System Events" to set childList to UI elements of e
        set i to 1
        repeat with c in childList
          my walk(c, idx & "." & (i as text))
          if found then exit repeat
          set i to i + 1
        end repeat
      end try
    end walk
  end script
  set finder's targetIndex to targetIndex
  set finder's payload to payload
  set finder's sep to sep
  tell application "System Events"
    set matches to application processes whose name is appQuery
    if (count of matches) is 0 then
      try
        set matches to application processes whose bundle identifier is appQuery
      end try
    end if
    if (count of matches) is 0 then error "APP_NOT_RUNNING: " & appQuery
    set p to item 1 of matches
    tell p
      if (count of windows) is 0 then error "NO_WINDOWS: " & appQuery
      set w to window 1
      tell finder to walk(w, "0")
    end tell
  end tell
  if finder's found is false then error "ELEMENT_NOT_FOUND: " & targetIndex
  return finder's resultText
end run`
}

func runAppleScriptArgs(ctx context.Context, r *Runtime, operation, script string, argv ...string) (Result, error) {
	cmdArgs := append([]string{"-e", script}, argv...)
	cmd := exec.CommandContext(ctx, "osascript", cmdArgs...)
	cmd.Env = r.commandEnv(nil)
	return commandResult(operation, cmd)
}

func parseDesktopAXState(stdout string) Result {
	res := Result{"accessibility_tree": []map[string]any{}, "node_count": 0}
	nodes := []map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		parts := strings.Split(line, desktopAXSep)
		if len(parts) == 0 {
			continue
		}
		switch parts[0] {
		case "META":
			if len(parts) >= 10 {
				res["resolved_app"] = parts[1]
				if pid, err := strconv.Atoi(parts[2]); err == nil {
					res["pid"] = pid
				}
				res["bundle_id"] = parts[3]
				res["frontmost"] = parts[4] == "true"
				win := map[string]any{"title": parts[5]}
				if x, err := strconv.Atoi(parts[6]); err == nil {
					win["x"] = x
				}
				if y, err := strconv.Atoi(parts[7]); err == nil {
					win["y"] = y
				}
				if w, err := strconv.Atoi(parts[8]); err == nil {
					win["width"] = w
				}
				if h, err := strconv.Atoi(parts[9]); err == nil {
					win["height"] = h
				}
				res["window"] = win
			}
		case "NODE":
			if len(parts) >= 17 {
				n := map[string]any{"index": parts[1], "parent_index": parts[2], "role": parts[4], "subrole": parts[5], "title": parts[6], "description": parts[7], "value": parts[8], "enabled": parts[9] == "true", "focused": parts[10] == "true", "settable": parts[16] == "true"}
				if depth, err := strconv.Atoi(parts[3]); err == nil {
					n["depth"] = depth
				}
				if x, err := strconv.Atoi(parts[11]); err == nil {
					n["x"] = x
				}
				if y, err := strconv.Atoi(parts[12]); err == nil {
					n["y"] = y
				}
				if w, err := strconv.Atoi(parts[13]); err == nil {
					n["width"] = w
				}
				if h, err := strconv.Atoi(parts[14]); err == nil {
					n["height"] = h
				}
				actions := []string{}
				if parts[15] != "" {
					for _, a := range strings.Split(parts[15], ",") {
						if s := strings.TrimSpace(a); s != "" {
							actions = append(actions, s)
						}
					}
				}
				n["actions"] = actions
				nodes = append(nodes, n)
			}
		}
	}
	res["accessibility_tree"] = nodes
	res["node_count"] = len(nodes)
	return res
}

func (r *Runtime) desktopRecentApps(ctx context.Context, maxRecent int) []map[string]any {
	if maxRecent <= 0 {
		maxRecent = 50
	}
	if maxRecent > 200 {
		maxRecent = 200
	}
	query := `kMDItemContentType == "com.apple.application-bundle" && kMDItemLastUsedDate >= $time.now(-1209600)`
	cmd := exec.CommandContext(ctx, "mdfind", query)
	cmd.Env = r.commandEnv(nil)
	out, err := cmd.Output()
	if err != nil {
		return []map[string]any{}
	}
	type recentApp struct {
		item map[string]any
		last string
	}
	items := []recentApp{}
	for _, appPath := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		appPath = strings.TrimSpace(appPath)
		if appPath == "" || !strings.HasSuffix(strings.ToLower(appPath), ".app") {
			continue
		}
		meta := r.desktopAppMetadata(ctx, appPath)
		if meta == nil {
			continue
		}
		items = append(items, recentApp{item: meta, last: stringFromAny(meta["last_used"])})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].last > items[j].last })
	outItems := []map[string]any{}
	for i, item := range items {
		if i >= maxRecent {
			break
		}
		outItems = append(outItems, item.item)
	}
	return outItems
}

func (r *Runtime) desktopAppMetadata(ctx context.Context, appPath string) map[string]any {
	cmd := exec.CommandContext(ctx, "mdls", "-raw", "-name", "kMDItemDisplayName", "-name", "kMDItemCFBundleIdentifier", "-name", "kMDItemLastUsedDate", "-name", "kMDItemUseCount", appPath)
	cmd.Env = r.commandEnv(nil)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	for len(lines) < 4 {
		lines = append(lines, "")
	}
	clean := func(value string) string {
		value = strings.TrimSpace(value)
		if value == "" || value == "(null)" {
			return ""
		}
		return strings.Trim(value, "\"")
	}
	name := clean(lines[0])
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(appPath), filepath.Ext(appPath))
	}
	item := map[string]any{"name": name, "path": appPath}
	if bundleID := clean(lines[1]); bundleID != "" {
		item["bundle_id"] = bundleID
	}
	if lastUsed := clean(lines[2]); lastUsed != "" {
		item["last_used"] = lastUsed
	}
	if countText := clean(lines[3]); countText != "" {
		if count, err := strconv.Atoi(countText); err == nil {
			item["usage_count_14d"] = count
		}
	}
	return item
}

func stringFromAny(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func desktopRememberAppState(app string) {
	if app = desktopStateKey(app); app != "" {
		desktopStateGuard.Store(app, time.Now())
	}
}

func desktopRequireRecentAppState(app string) error {
	key := desktopStateKey(app)
	if key == "" {
		return nil
	}
	value, ok := desktopStateGuard.Load(key)
	if !ok {
		return toolErrorDetails("NEED_APP_STATE", "call desktop_get_app_state for this app before acting", "validation", map[string]any{"app": app})
	}
	when, _ := value.(time.Time)
	if time.Since(when) > 90*time.Second {
		return toolErrorDetails("NEED_APP_STATE", "desktop_get_app_state is stale; call it again before acting", "validation", map[string]any{"app": app, "max_age_seconds": 90})
	}
	return nil
}

func desktopStateKey(app string) string {
	return strings.ToLower(strings.TrimSpace(desktopAppLookupKey(app)))
}

func desktopAppLookupKey(app string) string {
	app = strings.TrimSpace(app)
	if app == "" {
		return ""
	}
	base := filepath.Base(app)
	if strings.HasSuffix(strings.ToLower(base), ".app") {
		return strings.TrimSuffix(base, filepath.Ext(base))
	}
	return app
}

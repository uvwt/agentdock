#!/usr/bin/env python3
import datetime
import hashlib
import hmac
import json
import os
import platform
import secrets
import shutil
import stat
import struct
import subprocess
import sys
import time
import urllib.parse
from pathlib import Path

VERSION = "1.0.11"
SEP = "\x1f"
STATE_MAX_AGE_SECONDS = 90


def emit(value):
    print(json.dumps(value, ensure_ascii=False, separators=(",", ":")))


def load_input():
    raw = sys.stdin.read().strip()
    if not raw:
        return {}
    try:
        value = json.loads(raw)
    except json.JSONDecodeError as exc:
        emit({"ok": False, "error_code": "INVALID_INPUT", "error": f"输入不是有效 JSON: {exc}"})
        raise SystemExit(0)
    if not isinstance(value, dict):
        emit({"ok": False, "error_code": "INVALID_INPUT", "error": "输入必须是 JSON 对象"})
        raise SystemExit(0)
    return value


def bool_arg(args, key, default=False):
    value = args.get(key, default)
    if isinstance(value, bool):
        return value
    if isinstance(value, str):
        return value.strip().lower() in {"1", "true", "yes", "y", "on"}
    if isinstance(value, (int, float)):
        return value != 0
    return default


def int_arg(args, key, default=0):
    value = args.get(key, default)
    if isinstance(value, bool):
        return default
    if isinstance(value, int):
        return value
    if isinstance(value, float):
        return int(value)
    if isinstance(value, str):
        try:
            return int(float(value.strip()))
        except ValueError:
            return default
    return default


def float_arg(args, key, default=0.0):
    value = args.get(key, default)
    if isinstance(value, bool):
        return default
    if isinstance(value, (int, float)):
        return float(value)
    if isinstance(value, str):
        try:
            return float(value.strip())
        except ValueError:
            return default
    return default


def str_arg(args, key, default=""):
    value = args.get(key, default)
    if value is None:
        return default
    return str(value)


def map_arg(args, key):
    value = args.get(key)
    return value if isinstance(value, dict) else None


def error_result(operation, code, message, layer="validation", **extra):
    out = {"ok": False, "operation": operation, "command_ok": False, "effect_verified": False, "effect_changed": False, "error": message, "error_code": code, "error_layer": layer}
    out.update(extra)
    return out


def default_dir():
    return Path(os.environ.get("AGENTDOCK_DEFAULT_DIR") or "~/AgentDock").expanduser().resolve()


def agentdock_root():
    return Path(os.environ.get("AGENTDOCK_HOME") or "~/.agentdock").expanduser().resolve()


def skill_data_root():
    return agentdock_root() / "skill-data" / "desktop"


def ensure_private_dir(path):
    path.mkdir(parents=True, exist_ok=True)
    try:
        path.chmod(stat.S_IRWXU)
    except OSError:
        pass
    return path


def secure_private_file(path):
    try:
        path.chmod(stat.S_IRUSR | stat.S_IWUSR)
    except OSError:
        pass
    return path


def artifact_root():
    return ensure_private_dir(skill_data_root() / "artifacts")


def public_artifact_root():
    path = agentdock_root() / "public-artifacts"
    path.mkdir(parents=True, exist_ok=True)
    return path


def public_secret_path():
    return agentdock_root() / "secrets" / "public-url-secret"


def ensure_public_secret():
    path = public_secret_path()
    if path.exists():
        value = path.read_text(encoding="utf-8").strip()
        try:
            data = bytes.fromhex(value)
        except ValueError:
            data = b""
        if len(data) >= 32:
            try:
                path.chmod(stat.S_IRUSR | stat.S_IWUSR)
            except OSError:
                pass
            return data
        raise RuntimeError("public url secret is invalid")
    path.parent.mkdir(parents=True, exist_ok=True)
    try:
        path.parent.chmod(stat.S_IRWXU)
    except OSError:
        pass
    data = secrets.token_bytes(32)
    path.write_text(data.hex() + "\n", encoding="utf-8")
    try:
        path.chmod(stat.S_IRUSR | stat.S_IWUSR)
    except OSError:
        pass
    return data


def public_base_url():
    return (os.environ.get("AGENTDOCK_SERVER_URL") or "").strip().rstrip("/")


def rfc3339(timestamp):
    return datetime.datetime.fromtimestamp(timestamp, datetime.UTC).isoformat().replace("+00:00", "Z")


def safe_artifact_filename(value, default="desktop-snapshot.png"):
    name = Path(str(value or default).replace("\\", "/")).name.strip()
    if not name or name in {".", ".."}:
        name = default
    if len(name) > 240:
        suffix = Path(name).suffix
        stem = name[:-len(suffix)] if suffix else name
        name = stem[: max(1, 240 - len(suffix))] + suffix
    return name


def sign_public_url(secret, artifact_id, filename, expires, sha256):
    payload = f"{artifact_id}\n{filename}\n{expires}\n{sha256}".encode("utf-8")
    return hmac.new(secret, payload, hashlib.sha256).hexdigest()


def publish_public_image(path, data, info, args):
    now = int(time.time())
    retention = int_arg(args, "retention_seconds", 86400)
    if retention <= 0:
        retention = 86400
    retention = min(retention, 7 * 24 * 60 * 60)
    expires = now + retention
    artifact_id = secrets.token_hex(16)
    filename = safe_artifact_filename(path.name)
    target_dir = public_artifact_root() / artifact_id
    target_dir.mkdir(mode=0o700, parents=True, exist_ok=False)
    payload_path = target_dir / "payload"
    payload_path.write_bytes(data)
    try:
        payload_path.chmod(stat.S_IRUSR | stat.S_IWUSR)
    except OSError:
        pass
    sha = hashlib.sha256(data).hexdigest()
    metadata = {
        "artifact_id": artifact_id,
        "filename": filename,
        "mime_type": info["mime_type"],
        "size_bytes": len(data),
        "sha256": sha,
        "created_at": rfc3339(now),
        "expires_at": rfc3339(expires),
        "archive": False,
        "width": info["width"],
        "height": info["height"],
    }
    (target_dir / "metadata.json").write_text(json.dumps(metadata, ensure_ascii=False, indent=2), encoding="utf-8")
    try:
        (target_dir / "metadata.json").chmod(stat.S_IRUSR | stat.S_IWUSR)
    except OSError:
        pass
    base_url = public_base_url()
    if base_url:
        secret = ensure_public_secret()
        sig = sign_public_url(secret, artifact_id, filename, expires, sha)
        metadata["url"] = f"{base_url}/artifacts/public/{urllib.parse.quote(artifact_id)}/{urllib.parse.quote(filename)}?expires={expires}&sig={urllib.parse.quote(sig)}"
    return metadata


def state_file():
    return ensure_private_dir(skill_data_root()) / "state.json"


def load_state():
    path = state_file()
    if not path.exists():
        return {"app_state_seen": {}}
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
        if isinstance(data, dict):
            data.setdefault("app_state_seen", {})
            return data
    except Exception:
        pass
    return {"app_state_seen": {}}


def save_state(state):
    path = state_file()
    tmp = path.with_suffix(".tmp")
    tmp.write_text(json.dumps(state, ensure_ascii=False, indent=2), encoding="utf-8")
    secure_private_file(tmp)
    tmp.replace(path)
    secure_private_file(path)


def app_lookup_key(app):
    value = str(app or "").strip()
    if not value:
        return ""
    base = Path(value).name
    if base.lower().endswith(".app"):
        return base[:-4]
    return value


def remember_app_state(app):
    key = app_lookup_key(app).strip().lower()
    if not key:
        return
    state = load_state()
    state.setdefault("app_state_seen", {})[key] = time.time()
    save_state(state)


def require_recent_app_state(app):
    key = app_lookup_key(app).strip().lower()
    if not key:
        return None
    seen = load_state().get("app_state_seen", {}).get(key)
    if not seen:
        return error_result("desktop_action", "NEED_APP_STATE", "call observe action=app_state for this app before acting", app=app)
    if time.time() - float(seen) > STATE_MAX_AGE_SECONDS:
        return error_result("desktop_action", "NEED_APP_STATE", "app_state is stale; call observe action=app_state again before acting", app=app, max_age_seconds=STATE_MAX_AGE_SECONDS)
    return None


def is_darwin():
    return platform.system().lower() == "darwin"


def ensure_darwin(operation="desktop"):
    if not is_darwin():
        return error_result(operation, "DESKTOP_UNSUPPORTED_OS", "desktop automation is currently macOS-only", os=platform.system())
    return None


def command_exists(name):
    return shutil.which(name) is not None


def run_process(argv, input_text=None, timeout=30, operation="command", allow_failure=True):
    try:
        completed = subprocess.run(argv, input=input_text, text=True, capture_output=True, timeout=timeout, check=False, env=os.environ.copy())
    except subprocess.TimeoutExpired:
        return {"ok": False, "command_ok": False, "operation": operation, "error": "command timed out", "error_code": "COMMAND_TIMEOUT", "argv": argv[:1]}
    stdout = (completed.stdout or "") + (completed.stderr or "")
    res = {"ok": completed.returncode == 0, "command_ok": completed.returncode == 0, "operation": operation, "stdout": stdout, "exit_code": completed.returncode}
    if completed.returncode != 0:
        res["error"] = f"command exited with {completed.returncode}"
    apply_command_warnings(res, stdout)
    if completed.returncode != 0 and not allow_failure:
        res["ok"] = False
    return res


def run_applescript(script, argv=None, operation="osascript", timeout=30):
    return run_process(["osascript", "-e", script, *(argv or [])], timeout=timeout, operation=operation)


def applescript_string(value):
    # AppleScript 字符串必须转义，避免应用名里出现引号时拼坏脚本。
    return json.dumps(str(value))


def apply_command_warnings(res, text):
    lower = (text or "").lower()
    if any(marker in lower for marker in ["accessibility privileges not enabled", "not allowed assistive access", "不允许辅助访问", "不允许发送按键"]):
        res["ok"] = False
        res["permission_ok"] = False
        res["error_code"] = "ACCESSIBILITY_NOT_TRUSTED"
        res["warnings"] = ["macOS Accessibility/Automation permission is not available; command may not affect the target app"]
    else:
        res.setdefault("permission_ok", True)


def parse_pair(value):
    parts = str(value).split(",")
    if len(parts) != 2:
        return None
    try:
        return int(parts[0].strip()), int(parts[1].strip())
    except ValueError:
        return None


def preflight(args):
    checks = {"os": platform.system().lower(), "supported": is_darwin(), "skill_version": VERSION}
    warnings = []
    for name in ["screencapture", "osascript", "pbcopy", "pbpaste", "cliclick"]:
        exists = command_exists(name)
        checks[name] = exists
        if name == "cliclick" and not exists:
            warnings.append("cliclick not found; desktop mouse/keyboard actions require: brew install cliclick")
    if not is_darwin():
        warnings.append("desktop automation is currently macOS-only")
        return {"ok": False, "checks": checks, "warnings": warnings}
    if bool_arg(args, "check_screenshot", True):
        path = artifact_root() / "preflight" / f"preflight-{int(time.time()*1000)}.png"
        ensure_private_dir(path.parent)
        res = run_process(["screencapture", "-x", str(path)], timeout=15, operation="screencapture")
        if res.get("ok") and path.exists():
            secure_private_file(path)
        checks["screenshot_ok"] = res.get("ok", False)
        if not res.get("ok"):
            warnings.append("screencapture failed; grant Screen Recording permission to the AgentDock process")
            checks["screenshot_error"] = res.get("stdout") or res.get("error")
    if bool_arg(args, "check_applescript", True):
        res = run_applescript('tell application "System Events" to count processes', operation="applescript_preflight", timeout=15)
        checks["applescript_ok"] = res.get("ok", False)
        if not res.get("ok"):
            warnings.append("AppleScript/System Events failed; grant Accessibility/Automation permission to the AgentDock process")
            checks["applescript_error"] = res.get("stdout") or res.get("error")
    return {"ok": len(warnings) == 0, "checks": checks, "warnings": warnings}


def png_info(data):
    if data[:8] == b"\x89PNG\r\n\x1a\n" and len(data) >= 24:
        width, height = struct.unpack(">II", data[16:24])
        return {"width": width, "height": height, "mime_type": "image/png"}
    return {"width": 0, "height": 0, "mime_type": "application/octet-stream"}


def capture_screenshot(subdir, prefix, region=None):
    directory = ensure_private_dir(artifact_root() / subdir)
    path = directory / f"{prefix}-{time.time_ns()}.png"
    argv = ["screencapture", "-x"]
    if region:
        argv += ["-R", f"{region['x']},{region['y']},{region['width']},{region['height']}"]
    argv.append(str(path))
    res = run_process(argv, timeout=20, operation="screencapture")
    if not res.get("ok"):
        return None, res
    secure_private_file(path)
    return path, res


def screenshot_result(path, operation, args):
    data = path.read_bytes()
    info = png_info(data)
    try:
        screenshot = publish_public_image(path, data, info, args)
    except Exception as exc:
        return error_result(operation, "PUBLIC_ARTIFACT_PUBLISH_FAILED", f"发布截图失败: {exc}", layer="runtime")
    return {"ok": True, "operation": operation, "screenshot": screenshot}


def snapshot(args):
    unsupported = ensure_darwin("desktop_snapshot")
    if unsupported:
        return unsupported
    crop = map_arg(args, "crop")
    region = None
    if crop:
        region = {"x": int_arg(crop, "x", 0), "y": int_arg(crop, "y", 0), "width": int_arg(crop, "width", 0), "height": int_arg(crop, "height", 0)}
        if region["width"] <= 0 or region["height"] <= 0:
            return error_result("desktop_snapshot", "INVALID_CROP", "crop width and height must be positive")
    path, res = capture_screenshot("screenshots", "desktop", region)
    if not path:
        return {"ok": False, "operation": "desktop_snapshot", "error": res.get("error"), "stdout": res.get("stdout"), "error_layer": "screenshot"}
    out = screenshot_result(path, "desktop_snapshot", args)
    out["action_coordinate_space"] = "screen_points"
    out["screenshot_coordinate_space"] = "image_pixels"
    if region:
        out["screen_region"] = region
    return out


def window_list(args=None):
    unsupported = ensure_darwin("desktop_window_list")
    if unsupported:
        return unsupported
    script = r'''
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
return output'''
    res = run_applescript(script, operation="desktop_window_list", timeout=30)
    if not res.get("ok"):
        return res
    windows = []
    for line in res.get("stdout", "").strip().splitlines():
        parts = line.split("\t")
        app_name = parts[0] if parts else ""
        item = {"app": app_name, "frontmost": len(parts) > 1 and parts[1] == "true", "windows": []}
        if len(parts) > 2:
            for encoded in parts[2].split("||"):
                if not encoded.strip():
                    continue
                fields = encoded.split("::")
                win = {"title": fields[0] if fields else ""}
                if len(fields) > 1 and (pair := parse_pair(fields[1])):
                    win.update({"x": pair[0], "y": pair[1]})
                if len(fields) > 2 and (pair := parse_pair(fields[2])):
                    win.update({"width": pair[0], "height": pair[1]})
                item["windows"].append(win)
        windows.append(item)
    res["windows"] = windows
    res["count"] = len(windows)
    return res


def activate_app(app):
    app = str(app or "").strip()
    if not app:
        return error_result("desktop_focus", "MISSING_APP", "app is required")
    if app.startswith("/") or app.lower().endswith(".app"):
        argv = ["open", app]
    elif app.count(".") >= 2 and " " not in app:
        argv = ["open", "-b", app]
    else:
        argv = ["open", "-a", app]
    return run_process(argv, timeout=20, operation="desktop_focus")


def get_window_info(app):
    lookup = app_lookup_key(app)
    app_literal = applescript_string(lookup)
    script = f'''
tell application "System Events"
  if not (exists application process {app_literal}) then return ""
  tell application process {app_literal}
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
return ""'''
    res = run_applescript(script, operation="desktop_window_info", timeout=20)
    if not res.get("ok"):
        return None, res
    line = res.get("stdout", "").strip()
    if not line:
        return None, None
    parts = line.split("\t")
    if len(parts) < 4:
        return None, error_result("desktop_window_info", "WINDOW_QUERY_FAILED", f"unexpected window metadata: {line}", layer="window")
    pos = parse_pair(parts[2])
    size = parse_pair(parts[3])
    if not pos or not size:
        return None, error_result("desktop_window_info", "WINDOW_QUERY_FAILED", f"unexpected window bounds: {line}", layer="window")
    return {"app": lookup, "title": parts[0], "frontmost": parts[1] == "true", "x": pos[0], "y": pos[1], "width": size[0], "height": size[1]}, None


def prepare_window(args, operation):
    app = str_arg(args, "app", "").strip()
    if not app:
        return None, None
    if bool_arg(args, "focus_if_needed", False):
        focused = activate_app(app)
        if not focused.get("ok"):
            return None, error_result(operation, "FOCUS_APP_FAILED", focused.get("error", "focus failed"), layer="focus")
        time.sleep(0.25)
    info, failure = get_window_info(app)
    if failure:
        return None, failure
    if not info:
        if bool_arg(args, "fail_if_window_not_visible", False) or str_arg(args, "space", "screen").lower() == "window":
            return None, error_result(operation, "WINDOW_NOT_VISIBLE", "target app has no visible window", layer="window")
        return None, None
    if bool_arg(args, "require_frontmost", False) and not info.get("frontmost"):
        return None, error_result(operation, "APP_NOT_FRONTMOST", "target app is not frontmost", layer="focus", target_window=info)
    return info, None


def snapshot_app(args):
    unsupported = ensure_darwin("desktop_snapshot_app")
    if unsupported:
        return unsupported
    app = str_arg(args, "app", "").strip()
    if not app:
        return error_result("desktop_snapshot_app", "MISSING_APP", "app is required")
    info, failure = prepare_window(args, "desktop_snapshot_app")
    if failure:
        return failure
    if not info or info.get("width", 0) <= 0 or info.get("height", 0) <= 0:
        return error_result("desktop_snapshot_app", "WINDOW_NOT_VISIBLE", "target app has no visible window", layer="window")
    rel = {"x": 0, "y": 0, "width": info["width"], "height": info["height"]}
    crop = map_arg(args, "crop")
    if crop:
        rel.update({"x": int_arg(crop, "x", 0), "y": int_arg(crop, "y", 0)})
        rel["width"] = int_arg(crop, "width", info["width"] - rel["x"])
        rel["height"] = int_arg(crop, "height", info["height"] - rel["y"])
    if rel["width"] <= 0 or rel["height"] <= 0:
        return error_result("desktop_snapshot_app", "INVALID_CROP", "crop width and height must be positive")
    abs_region = {"x": info["x"] + rel["x"], "y": info["y"] + rel["y"], "width": rel["width"], "height": rel["height"]}
    path, res = capture_screenshot("screenshots", "desktop-app", abs_region)
    if not path:
        return {"ok": False, "operation": "desktop_snapshot_app", "error": res.get("error"), "stdout": res.get("stdout"), "error_layer": "screenshot", "target_window": info}
    out = screenshot_result(path, "desktop_snapshot_app", args)
    out.update({"target_window": info, "crop": rel, "screen_region": abs_region, "action_coordinate_space": "window_points", "screenshot_coordinate_space": "image_pixels"})
    return out


def list_apps(args):
    unsupported = ensure_darwin("desktop_list_apps")
    if unsupported:
        return unsupported
    script = r'''
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
end run'''
    res = run_applescript(script, operation="desktop_list_apps", timeout=30)
    if not res.get("ok"):
        return res
    running = []
    for line in res.get("stdout", "").strip().splitlines():
        parts = line.split(SEP)
        if len(parts) < 5 or parts[0] != "RUNNING":
            continue
        item = {"name": parts[1], "bundle_id": parts[3], "frontmost": parts[4] == "true"}
        try:
            item["pid"] = int(parts[2])
        except ValueError:
            pass
        running.append(item)
    res["running"] = running
    res["count"] = len(running)
    res["recent"] = recent_apps(int_arg(args, "max_recent", 50))
    return res


def recent_apps(max_recent):
    max_recent = min(max(max_recent or 50, 1), 200)
    query = 'kMDItemContentType == "com.apple.application-bundle" && kMDItemLastUsedDate >= $time.now(-1209600)'
    completed = subprocess.run(["mdfind", query], text=True, capture_output=True, timeout=20, check=False)
    if completed.returncode != 0:
        return []
    items = []
    for app_path in completed.stdout.strip().splitlines():
        if not app_path.lower().endswith(".app"):
            continue
        meta = app_metadata(app_path)
        if meta:
            items.append(meta)
    items.sort(key=lambda item: item.get("last_used", ""), reverse=True)
    return items[:max_recent]


def app_metadata(app_path):
    completed = subprocess.run(["mdls", "-raw", "-name", "kMDItemDisplayName", "-name", "kMDItemCFBundleIdentifier", "-name", "kMDItemLastUsedDate", "-name", "kMDItemUseCount", app_path], text=True, capture_output=True, timeout=10, check=False)
    if completed.returncode != 0:
        return None
    lines = completed.stdout.rstrip("\n").split("\n")
    while len(lines) < 4:
        lines.append("")
    def clean(v):
        v = v.strip().strip('"')
        return "" if v == "(null)" else v
    name = clean(lines[0]) or Path(app_path).stem
    item = {"name": name, "path": app_path}
    if clean(lines[1]): item["bundle_id"] = clean(lines[1])
    if clean(lines[2]): item["last_used"] = clean(lines[2])
    try:
        item["usage_count_14d"] = int(clean(lines[3]))
    except ValueError:
        pass
    return item


def read_ax_state(app, max_depth, max_nodes):
    script = r'''
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
end run'''
    res = run_applescript(script, [app, str(max_depth), str(max_nodes)], operation="desktop_get_app_state_ax", timeout=45)
    if not res.get("ok"):
        return None, res
    return parse_ax_state(res.get("stdout", "")), None


def parse_ax_state(stdout):
    res = {"accessibility_tree": [], "node_count": 0}
    nodes = []
    for line in stdout.strip().splitlines():
        parts = line.split(SEP)
        if not parts:
            continue
        if parts[0] == "META" and len(parts) >= 10:
            res["resolved_app"] = parts[1]
            try: res["pid"] = int(parts[2])
            except ValueError: pass
            res["bundle_id"] = parts[3]
            res["frontmost"] = parts[4] == "true"
            win = {"title": parts[5]}
            for key, idx in [("x", 6), ("y", 7), ("width", 8), ("height", 9)]:
                try: win[key] = int(parts[idx])
                except ValueError: pass
            res["window"] = win
        elif parts[0] == "NODE" and len(parts) >= 17:
            node = {"index": parts[1], "parent_index": parts[2], "role": parts[4], "subrole": parts[5], "title": parts[6], "description": parts[7], "value": parts[8], "enabled": parts[9] == "true", "focused": parts[10] == "true", "settable": parts[16] == "true"}
            for key, idx in [("depth", 3), ("x", 11), ("y", 12), ("width", 13), ("height", 14)]:
                try: node[key] = int(parts[idx])
                except ValueError: pass
            node["actions"] = [a.strip() for a in parts[15].split(",") if a.strip()]
            nodes.append(node)
    res["accessibility_tree"] = nodes
    res["node_count"] = len(nodes)
    return res


def app_state(args):
    unsupported = ensure_darwin("desktop_get_app_state")
    if unsupported:
        return unsupported
    app = str_arg(args, "app", "").strip()
    if not app:
        return error_result("desktop_get_app_state", "MISSING_APP", "app is required")
    lookup = app_lookup_key(app)
    if bool_arg(args, "activate", True):
        focused = activate_app(app)
        if not focused.get("ok"):
            return {"ok": False, "operation": "desktop_get_app_state", "app": app, "error": focused.get("error") or focused.get("stdout")}
        time.sleep(0.35)
    max_depth = min(max(int_arg(args, "ax_max_depth", 8), 0), 20)
    max_nodes = min(max(int_arg(args, "ax_max_nodes", 300), 1), 2000)
    ax_state, ax_err = read_ax_state(lookup, max_depth, max_nodes)
    snap = snapshot(args)
    res = {"ok": True, "operation": "desktop_get_app_state", "app": app, "lookup_app": lookup, "ax_max_depth": max_depth, "ax_max_nodes": max_nodes}
    if isinstance(snap, dict):
        res.update(snap)
    if ax_err:
        res["accessibility_ok"] = False
        res["accessibility_error"] = ax_err.get("stdout") or ax_err.get("error")
        res["warnings"] = ["failed to read accessibility tree; grant Accessibility permission and ensure the app has a visible window"]
    else:
        res.update(ax_state)
        res["accessibility_ok"] = True
    res["coordinate_space"] = {"origin": "top_left_global_display", "x_y_units": "macos_screen_points_for_actions; screenshot pixels are reported separately", "screenshot_width": res.get("width"), "screenshot_height": res.get("height")}
    remember_app_state(lookup)
    return res


def ax_operate_script(mode):
    return f'''
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
      if "{mode}" is "click" then
        set n to payload as integer
        if n < 1 then set n to 1
        repeat n times
          tell application "System Events" to click e
          delay 0.05
        end repeat
        set resultText to "CLICK" & sep & idx
      else if "{mode}" is "set_value" then
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
      else if "{mode}" is "action" then
        tell application "System Events" to perform action (payload as text) of e
        set resultText to "ACTION" & sep & idx & sep & payload
      else if "{mode}" is "bounds" then
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
end run'''


def ax_bounds(app, index):
    err = require_recent_app_state(app)
    if err:
        return None, err
    res = run_applescript(ax_operate_script("bounds"), [app_lookup_key(app), index, ""], operation="desktop_element_bounds", timeout=30)
    if not res.get("ok"):
        return None, res
    parts = res.get("stdout", "").strip().split(SEP)
    if len(parts) < 6 or parts[0] != "BOUNDS":
        return None, {**res, "ok": False, "error": "invalid bounds response"}
    try:
        bounds = {"x": int(parts[2]), "y": int(parts[3]), "width": int(parts[4]), "height": int(parts[5])}
    except ValueError:
        return None, {**res, "ok": False, "error": "invalid bounds response"}
    res.update(bounds)
    return bounds, res


def require_cliclick(operation):
    if not command_exists("cliclick"):
        return error_result(operation, "CLICLICK_NOT_FOUND", f"cliclick is required for {operation}", install="brew install cliclick")
    return None


def point_inside_window(point, info):
    return bool(info and point["x"] >= 0 and point["y"] >= 0 and point["x"] < info.get("width", 0) and point["y"] < info.get("height", 0))


def resolve_point(args, operation, point):
    if str_arg(args, "space", "screen").lower() != "window":
        return point, None, None
    info, failure = prepare_window(args, operation)
    if failure:
        return point, None, failure
    if not info:
        return point, None, error_result(operation, "MISSING_APP", "app is required when space=window")
    if bool_arg(args, "fail_if_coordinate_outside_window", False) and not point_inside_window(point, info):
        return point, info, error_result(operation, "COORDINATE_OUTSIDE_WINDOW", "coordinate is outside the target window", target_window=info)
    return {"x": info["x"] + point["x"], "y": info["y"] + point["y"]}, info, None


def capture_temp(prefix, region=None):
    path, res = capture_screenshot("verification", prefix, region)
    if not path:
        return b"", ""
    try:
        return path.read_bytes(), str(path)
    except Exception:
        return b"", str(path)


def diff_percent(a, b):
    if not a or not b:
        return None
    size = max(len(a), len(b))
    same = sum(1 for x, y in zip(a, b) if x == y)
    return (size - same) / float(size)


def verify_region(args, info):
    region = map_arg(args, "verify_region")
    if not region:
        return None
    rect = {"x": int_arg(region, "x", 0), "y": int_arg(region, "y", 0), "width": int_arg(region, "width", 0), "height": int_arg(region, "height", 0)}
    if rect["width"] <= 0 or rect["height"] <= 0:
        return None
    if str(region.get("space", "")).lower() == "window" and info:
        rect["x"] += info["x"]
        rect["y"] += info["y"]
    return rect


def command_action_result(operation, argv, args):
    verify = bool_arg(args, "verify", False) or str_arg(args, "verify", "").lower() == "screenshot_diff"
    capture_before = bool_arg(args, "before_snapshot", verify)
    capture_after = bool_arg(args, "after_snapshot", verify)
    info, _ = prepare_window(args, operation)
    region = verify_region(args, info)
    before, before_path = capture_temp(operation + "-before", region) if capture_before else (b"", "")
    res = run_process(argv, timeout=45, operation=operation)
    res.update({"effect_verified": False, "effect_changed": False, "verification": "not_requested"})
    if before_path:
        res["before_snapshot_path"] = before_path
        res["before_screenshot_path"] = before_path
    if info:
        res["target_window"] = info
    wait_ms = int_arg(args, "wait_ms", 250 if verify else 0)
    if wait_ms > 0:
        time.sleep(min(wait_ms, 10000) / 1000.0)
    if capture_after:
        after, after_path = capture_temp(operation + "-after", region)
        if after_path:
            res["after_snapshot_path"] = after_path
            res["after_screenshot_path"] = after_path
        diff = diff_percent(before, after)
        if diff is not None:
            threshold = float_arg(args, "diff_threshold", 0.002)
            res.update({"verification": "screenshot_diff", "diff_percent": diff, "diff_score": diff, "diff_threshold": threshold, "effect_verified": True, "effect_changed": diff >= threshold})
            if diff < threshold and verify:
                res["error_layer"] = "verification"
        else:
            res.update({"verification": "diff_failed", "error_layer": "verification"})
    return res


def normalize_keys(keys):
    replacements = {"command": "cmd", "control": "ctrl", "option": "alt", "return": "enter"}
    raw = str(keys).lower().strip().replace(",", "+").replace(" ", "+")
    parts = [p for p in raw.split("+") if p]
    return [replacements.get(p, p) for p in parts]


def cliclick_key_args(keys):
    parts = normalize_keys(keys)
    mods, main = [], ""
    for part in parts:
        if part in {"cmd", "ctrl", "alt", "shift", "fn"}:
            mods.append(part)
        else:
            main = part
    if not main and mods:
        main = mods.pop()
    out = ["kd:" + m for m in mods]
    if len(main) == 1:
        out.append("t:" + main)
    elif main:
        out.append("kp:" + main)
    out.extend("ku:" + m for m in reversed(mods))
    return out or ["kp:" + "+".join(parts)]


def prefer_clipboard_typing(text):
    return len(text.encode("utf-8")) > 80 or any(ch in text for ch in "\n\r\t") or any(ord(ch) > 127 for ch in text)


def clipboard_write(args):
    unsupported = ensure_darwin("desktop_clipboard_set")
    if unsupported:
        return unsupported
    text = str_arg(args, "text", "")
    res = run_process(["sh", "-c", "cat | pbcopy"], input_text=text, timeout=10, operation="desktop_clipboard_set")
    res["bytes"] = len(text.encode("utf-8"))
    if res.get("ok") and bool_arg(args, "verify", True):
        got = subprocess.run(["pbpaste"], text=True, capture_output=True, timeout=10, check=False).stdout
        res["verified"] = got == text
    return res


def clipboard_read(args):
    unsupported = ensure_darwin("desktop_clipboard_get")
    if unsupported:
        return unsupported
    res = run_process(["pbpaste"], timeout=10, operation="desktop_clipboard_get")
    if res.get("ok"):
        text = res.get("stdout", "")
        max_bytes = int_arg(args, "max_bytes", 0)
        if max_bytes > 0 and len(text.encode("utf-8")) > max_bytes:
            encoded = text.encode("utf-8")[:max_bytes]
            text = encoded.decode("utf-8", errors="ignore")
            res["truncated"] = True
        res["text"] = text
    return res


def build_drag_commands(args, start, end):
    commands = [f"dd:{start['x']},{start['y']}"]
    hold = max(0, min(int_arg(args, "hold_ms", 0), 10000))
    if hold: commands.append(f"w:{hold}")
    steps = max(1, min(int_arg(args, "steps", 1), 200))
    duration = max(0, min(int_arg(args, "duration_ms", 0), 30000))
    per_step = duration // steps if steps > 1 and duration > 0 else 0
    for i in range(1, steps + 1):
        x = start["x"] + ((end["x"] - start["x"]) * i) // steps
        y = start["y"] + ((end["y"] - start["y"]) * i) // steps
        commands.append(f"m:{x},{y}")
        if per_step and i < steps:
            commands.append(f"w:{per_step}")
    release = max(0, min(int_arg(args, "release_wait_ms", 0), 10000))
    if release: commands.append(f"w:{release}")
    commands.append(f"du:{end['x']},{end['y']}")
    return commands


def act(args):
    unsupported = ensure_darwin("desktop_action")
    if unsupported:
        return unsupported
    action = str_arg(args, "action", "").lower().strip()
    if action == "focus":
        return activate_app(str_arg(args, "app", ""))
    if action == "wait":
        ms = max(0, min(int_arg(args, "ms", 1000), 60000))
        time.sleep(ms / 1000.0)
        return {"ok": True, "operation": "desktop_wait", "waited_ms": ms}
    if action in {"click", "double_click", "move", "scroll", "drag", "type", "hotkey"}:
        if (err := require_cliclick("desktop_" + action)):
            return err
    app = str_arg(args, "app", "").strip()
    element_index = str_arg(args, "element_index", "").strip()
    if action == "click":
        click_count = max(1, min(int_arg(args, "click_count", 1), 5))
        button = str_arg(args, "mouse_button", "left").lower().strip() or "left"
        if element_index:
            if not app:
                return error_result("desktop_click", "MISSING_APP", "app is required when element_index is provided")
            if button == "left":
                if (err := require_recent_app_state(app)):
                    return err
                _ = activate_app(app)
                res = run_applescript(ax_operate_script("click"), [app_lookup_key(app), element_index, str(click_count)], operation="desktop_click", timeout=30)
                res.update({"app": app, "element_index": element_index, "click_count": click_count, "effect_verified": False, "verification": "not_performed"})
                return res
            bounds, failure = ax_bounds(app, element_index)
            if failure and not bounds:
                return failure
            args["x"] = bounds["x"] + bounds["width"] // 2
            args["y"] = bounds["y"] + bounds["height"] // 2
        point = {"x": int_arg(args, "x", -1), "y": int_arg(args, "y", -1)}
        if point["x"] < 0 or point["y"] < 0:
            return error_result("desktop_click", "INVALID_COORDINATE", "x/y or app+element_index is required")
        point, _, failure = resolve_point(args, "desktop_click", point)
        if failure: return failure
        prefix = {"left": "c", "right": "rc", "middle": "mc"}.get(button)
        if not prefix:
            return error_result("desktop_click", "INVALID_MOUSE_BUTTON", "mouse_button must be left, right, or middle")
        return command_action_result("desktop_click", ["cliclick", *[f"{prefix}:{point['x']},{point['y']}" for _ in range(click_count)]], args)
    if action == "move":
        point = {"x": int_arg(args, "x", -1), "y": int_arg(args, "y", -1)}
        if point["x"] < 0 or point["y"] < 0:
            return error_result("desktop_move", "INVALID_COORDINATE", "x and y must be non-negative")
        point, _, failure = resolve_point(args, "desktop_move", point)
        if failure: return failure
        return command_action_result("desktop_move", ["cliclick", f"m:{point['x']},{point['y']}"], args)
    if action == "double_click":
        point = {"x": int_arg(args, "x", -1), "y": int_arg(args, "y", -1)}
        if point["x"] < 0 or point["y"] < 0:
            return error_result("desktop_double_click", "INVALID_COORDINATE", "x and y must be non-negative")
        point, _, failure = resolve_point(args, "desktop_double_click", point)
        if failure: return failure
        return command_action_result("desktop_double_click", ["cliclick", f"dc:{point['x']},{point['y']}"], args)
    if action == "scroll":
        direction = str_arg(args, "direction", "").lower().strip()
        dx = int_arg(args, "dx", 0)
        dy = int_arg(args, "dy", int_arg(args, "amount", 0))
        if direction:
            amount = int(10 * float_arg(args, "pages", 1)) or 1
            mapping = {"up": (0, amount), "down": (0, -amount), "left": (amount, 0), "right": (-amount, 0)}
            if direction not in mapping:
                return error_result("desktop_scroll", "INVALID_SCROLL_DIRECTION", "direction must be up, down, left, or right")
            dx, dy = mapping[direction]
        if element_index:
            if not app:
                return error_result("desktop_scroll", "MISSING_APP", "app is required when element_index is provided")
            bounds, failure = ax_bounds(app, element_index)
            if failure and not bounds: return failure
            return command_action_result("desktop_scroll", ["cliclick", f"m:{bounds['x'] + bounds['width']//2},{bounds['y'] + bounds['height']//2}", f"w:{dx},{dy}"], args)
        if dx == 0 and dy == 0:
            return error_result("desktop_scroll", "INVALID_SCROLL", "dx/dy/amount or direction is required")
        return command_action_result("desktop_scroll", ["cliclick", f"w:{dx},{dy}"], args)
    if action == "drag":
        start = {"x": int_arg(args, "from_x", -1), "y": int_arg(args, "from_y", -1)}
        end = {"x": int_arg(args, "to_x", -1), "y": int_arg(args, "to_y", -1)}
        if min(start["x"], start["y"], end["x"], end["y"]) < 0:
            return error_result("desktop_drag", "INVALID_COORDINATE", "from_x/from_y/to_x/to_y must be non-negative")
        if str_arg(args, "space", "screen").lower() == "window":
            info, failure = prepare_window(args, "desktop_drag")
            if failure: return failure
            if not info: return error_result("desktop_drag", "MISSING_APP", "app is required when space=window")
            if bool_arg(args, "fail_if_coordinate_outside_window", False) and (not point_inside_window(start, info) or not point_inside_window(end, info)):
                return error_result("desktop_drag", "COORDINATE_OUTSIDE_WINDOW", "coordinate is outside the target window", target_window=info)
            start = {"x": info["x"] + start["x"], "y": info["y"] + start["y"]}
            end = {"x": info["x"] + end["x"], "y": info["y"] + end["y"]}
        return command_action_result("desktop_drag", ["cliclick", *build_drag_commands(args, start, end)], args)
    if action == "type":
        text = str_arg(args, "text", "")
        strategy = str_arg(args, "strategy", "auto").lower().strip()
        if text == "": return error_result("desktop_type", "MISSING_TEXT", "text is required")
        if app:
            if (err := require_recent_app_state(app)):
                return err
            _ = activate_app(app)
        if strategy == "clipboard" or (strategy == "auto" and prefer_clipboard_typing(text)):
            clip = clipboard_write({"text": text, "verify": True})
            if not clip.get("ok"):
                return clip
            res = act({**args, "action": "hotkey", "keys": "cmd+v"})
            res.update({"operation": "desktop_type", "strategy": "clipboard", "clipboard_verified": clip.get("verified"), "bytes": len(text.encode("utf-8"))})
            return res
        if strategy not in {"auto", "keyboard"}:
            return error_result("desktop_type", "INVALID_TYPE_STRATEGY", "strategy must be auto, keyboard, or clipboard")
        return command_action_result("desktop_type", ["cliclick", "t:" + text], args)
    if action == "hotkey":
        keys = str_arg(args, "keys", "").strip()
        if not keys: return error_result("desktop_hotkey", "MISSING_KEYS", "keys is required, for example cmd+space or enter")
        return command_action_result("desktop_hotkey", ["cliclick", *cliclick_key_args(keys)], args)
    if action == "set_value":
        if not app: return error_result("desktop_set_value", "MISSING_APP", "app is required")
        if not element_index: return error_result("desktop_set_value", "MISSING_ELEMENT_INDEX", "element_index is required")
        if (err := require_recent_app_state(app)): return err
        _ = activate_app(app)
        value = str_arg(args, "value", "")
        res = run_applescript(ax_operate_script("set_value"), [app_lookup_key(app), element_index, value], operation="desktop_set_value", timeout=30)
        res.update({"app": app, "element_index": element_index, "bytes": len(value.encode("utf-8"))})
        return res
    if action == "secondary_action":
        if not app: return error_result("desktop_perform_secondary_action", "MISSING_APP", "app is required")
        if not element_index: return error_result("desktop_perform_secondary_action", "MISSING_ELEMENT_INDEX", "element_index is required")
        ax_action = str_arg(args, "ax_action", "").strip()
        if not ax_action:
            return error_result("desktop_perform_secondary_action", "MISSING_ACTION", "ax_action is required")
        if (err := require_recent_app_state(app)): return err
        _ = activate_app(app)
        res = run_applescript(ax_operate_script("action"), [app_lookup_key(app), element_index, ax_action], operation="desktop_perform_secondary_action", timeout=30)
        res.update({"app": app, "element_index": element_index, "action": ax_action})
        return res
    return error_result("desktop_action", "INVALID_ACTION", "unsupported desktop act action", action=action)


def observe(args):
    action = str_arg(args, "action", "").lower().strip()
    if action == "preflight":
        return preflight(args)
    if action == "list_apps":
        return list_apps(args)
    if action == "app_state":
        return app_state(args)
    if action == "window_list":
        return window_list(args)
    if action == "snapshot":
        return snapshot(args)
    if action == "snapshot_app":
        return snapshot_app(args)
    return error_result("desktop_observe", "INVALID_ACTION", "unsupported desktop observe action", action=action)


def main():
    args = load_input()
    op = str(args.pop("skill_action", "status"))
    try:
        if op == "status":
            result = preflight(args)
        elif op == "observe":
            result = observe(args)
        elif op == "act":
            result = act(args)
        elif op == "clipboard-read":
            result = clipboard_read(args)
        elif op == "clipboard-write":
            result = clipboard_write(args)
        else:
            result = error_result("desktop", "INVALID_OPERATION", f"unsupported operation: {op}")
    except Exception as exc:
        result = {"ok": False, "operation": op, "error_code": "UNHANDLED_EXCEPTION", "error": str(exc)}
    emit(result)


if __name__ == "__main__":
    main()

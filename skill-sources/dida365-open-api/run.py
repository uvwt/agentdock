#!/usr/bin/env python3
import base64
import json
import os
import secrets
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

VERSION = "0.1.3"
AGENTDOCK_HOME = Path(os.environ.get("AGENTDOCK_HOME", Path.home() / ".agentdock"))
STATE_DIR = AGENTDOCK_HOME / "skill-data" / "dida365-open-api"
STATE_FILE = STATE_DIR / "state.json"
DEFAULT_SCOPES = ["tasks:read", "tasks:write"]

REGIONS = {
    "cn": {
        "name": "Dida365",
        "auth_base": "https://dida365.com",
        "api_base": "https://api.dida365.com",
        "developer_docs": "https://developer.dida365.com/docs/openapi.md",
    },
    "global": {
        "name": "TickTick",
        "auth_base": "https://ticktick.com",
        "api_base": "https://api.ticktick.com",
        "developer_docs": "https://developer.ticktick.com/docs/openapi.md",
    },
}


def emit(value):
    print(json.dumps(value, ensure_ascii=False, separators=(",", ":")))


def fail(code, message, details=None, exit_code=0):
    payload = {"ok": False, "error": {"code": code, "message": message}}
    if details is not None:
        payload["error"]["details"] = details
    emit(payload)
    raise SystemExit(exit_code)


def load_input():
    raw = sys.stdin.read().strip()
    if not raw:
        return {}
    try:
        value = json.loads(raw)
    except json.JSONDecodeError as exc:
        fail("INVALID_INPUT", "Input is not valid JSON", {"reason": str(exc)})
    if not isinstance(value, dict):
        fail("INVALID_INPUT", "Input must be a JSON object")
    return value


def load_state():
    if not STATE_FILE.exists():
        return {}
    try:
        data = json.loads(STATE_FILE.read_text(encoding="utf-8"))
    except Exception as exc:
        fail("STATE_READ_FAILED", "Could not read local Dida365 state", {"reason": str(exc)})
    if not isinstance(data, dict):
        fail("STATE_READ_FAILED", "Local Dida365 state is not a JSON object")
    return data


def save_state(state):
    STATE_DIR.mkdir(parents=True, exist_ok=True)
    tmp = STATE_FILE.with_suffix(".json.tmp")
    tmp.write_text(json.dumps(state, ensure_ascii=False, indent=2), encoding="utf-8")
    os.chmod(tmp, 0o600)
    tmp.replace(STATE_FILE)


def region_config(region):
    key = region or "cn"
    if key not in REGIONS:
        fail("INVALID_REGION", "region must be cn or global", {"region": region})
    return REGIONS[key]


def state_region(state):
    return state.get("region") or os.environ.get("DIDA365_REGION") or "cn"


def scrub_state(state):
    token_present = bool(state.get("access_token") or os.environ.get("DIDA365_ACCESS_TOKEN"))
    expires_at = float(state.get("expires_at", 0) or 0)
    return {
        "ok": True,
        "skill_version": VERSION,
        "region": state_region(state),
        "service": region_config(state_region(state))["name"],
        "configured": bool(state.get("client_id") or os.environ.get("DIDA365_CLIENT_ID")),
        "authenticated": token_present,
        "token_expired": bool(token_present and expires_at and expires_at <= time.time()),
        "redirect_uri": state.get("redirect_uri") or os.environ.get("DIDA365_REDIRECT_URI"),
        "scopes": state.get("scopes", []),
        "state_file": str(STATE_FILE),
        "developer_docs": region_config(state_region(state))["developer_docs"],
        "official_mcp": "https://mcp.ticktick.com",
    }


def require_str(args, name):
    value = args.get(name)
    if not isinstance(value, str) or not value.strip():
        fail("INVALID_INPUT", f"{name} must be a non-empty string")
    return value.strip()


def optional_body(args, skip=()):
    return {k: v for k, v in args.items() if k not in set(skip) and v is not None}


def ensure_open_path(path):
    if not isinstance(path, str) or not path.startswith("/open/v1/"):
        fail("INVALID_PATH", "path must start with /open/v1/")
    if ".." in path or "//" in path:
        fail("INVALID_PATH", "path must be a normalized /open/v1 path")
    return path


def http_request(method, url, headers=None, body=None, timeout=25):
    req_headers = dict(headers or {})
    data = None
    if body is not None:
        if isinstance(body, bytes):
            data = body
        else:
            data = json.dumps(body, ensure_ascii=False).encode("utf-8")
            req_headers.setdefault("Content-Type", "application/json")
    req = urllib.request.Request(url, data=data, method=method, headers=req_headers)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            raw = resp.read().decode("utf-8", errors="replace")
            if not raw:
                return {"_status": resp.status}
            try:
                parsed = json.loads(raw)
            except json.JSONDecodeError:
                return {"_status": resp.status, "text": raw}
            if isinstance(parsed, dict):
                parsed["_status"] = resp.status
            return parsed
    except urllib.error.HTTPError as exc:
        raw = exc.read().decode("utf-8", errors="replace")[:4000]
        details = {"status": exc.code, "body": raw}
        try:
            details["json"] = json.loads(raw)
        except Exception:
            pass
        fail("DIDA365_HTTP_ERROR", "Dida365/TickTick API request failed", details)
    except urllib.error.URLError as exc:
        fail("DIDA365_NETWORK_ERROR", "Dida365/TickTick API request failed", {"reason": str(exc)})


def token_request(state, data):
    cfg = region_config(state_region(state))
    client_id = state.get("client_id") or os.environ.get("DIDA365_CLIENT_ID")
    client_secret = state.get("client_secret") or os.environ.get("DIDA365_CLIENT_SECRET")
    if not client_id or not client_secret:
        fail("NOT_CONFIGURED", "OAuth client_id/client_secret are missing; run auth-url first")
    auth = base64.b64encode(f"{client_id}:{client_secret}".encode("utf-8")).decode("ascii")
    body = urllib.parse.urlencode(data).encode("utf-8")
    return http_request(
        "POST",
        f"{cfg['auth_base']}/oauth/token",
        {"Authorization": f"Basic {auth}", "Content-Type": "application/x-www-form-urlencoded"},
        body,
        timeout=30,
    )


def refresh_if_needed(state):
    env_token = os.environ.get("DIDA365_ACCESS_TOKEN")
    if env_token:
        state = dict(state)
        state["access_token"] = env_token
        return state
    if not state.get("access_token") and not state.get("refresh_token"):
        fail("NOT_AUTHENTICATED", "Run auth-url and finish-auth, or set-token with an existing access token")
    expires_at = float(state.get("expires_at", 0) or 0)
    if state.get("access_token") and (not expires_at or expires_at - time.time() > 60):
        return state
    if not state.get("refresh_token"):
        if state.get("access_token"):
            return state
        fail("TOKEN_EXPIRED", "No usable access token or refresh token is available")
    token = token_request(state, {"grant_type": "refresh_token", "refresh_token": state["refresh_token"]})
    state["access_token"] = token.get("access_token") or state.get("access_token")
    if token.get("refresh_token"):
        state["refresh_token"] = token["refresh_token"]
    if token.get("expires_in"):
        state["expires_at"] = time.time() + int(token["expires_in"])
    save_state(state)
    return state


def api_call(method, path, state, query=None, body=None):
    path = ensure_open_path(path)
    state = refresh_if_needed(state)
    cfg = region_config(state_region(state))
    url = f"{cfg['api_base']}{path}"
    if query:
        clean_query = {k: v for k, v in query.items() if v is not None}
        url += "?" + urllib.parse.urlencode(clean_query, doseq=True)
    return http_request(method, url, {"Authorization": f"Bearer {state['access_token']}"}, body)


def emit_api(payload_key, value):
    if isinstance(value, dict) and set(value.keys()) == {"_status"}:
        emit({"ok": True, "status": value["_status"]})
    else:
        emit({"ok": True, payload_key: value})


def handle_status(args):
    state = load_state()
    payload = scrub_state(state)
    if args.get("validate"):
        projects = api_call("GET", "/open/v1/project", state)
        payload["validated"] = True
        payload["project_count"] = len(projects) if isinstance(projects, list) else None
    emit(payload)


def handle_auth_url(args):
    client_id = require_str(args, "client_id")
    client_secret = require_str(args, "client_secret")
    redirect_uri = require_str(args, "redirect_uri")
    region = args.get("region") or "cn"
    cfg = region_config(region)
    scopes = args.get("scopes") or DEFAULT_SCOPES
    if not isinstance(scopes, list) or not all(isinstance(s, str) and s.strip() for s in scopes):
        fail("INVALID_SCOPES", "scopes must be an array of non-empty strings")
    state_value = secrets.token_urlsafe(24)
    state = load_state()
    state.update({
        "region": region,
        "client_id": client_id,
        "client_secret": client_secret,
        "redirect_uri": redirect_uri,
        "scopes": scopes,
        "pending_state": state_value,
        "created_at": time.time(),
    })
    save_state(state)
    query = {
        "scope": " ".join(scopes),
        "client_id": client_id,
        "state": state_value,
        "redirect_uri": redirect_uri,
        "response_type": "code",
    }
    emit({
        "ok": True,
        "region": region,
        "service": cfg["name"],
        "auth_url": f"{cfg['auth_base']}/oauth/authorize?{urllib.parse.urlencode(query)}",
        "redirect_uri": redirect_uri,
        "scopes": scopes,
        "next_step": "Open auth_url, approve access, then run finish-auth with callback_url or code+state.",
    })


def handle_finish_auth(args):
    state = load_state()
    if not state.get("client_id"):
        fail("NO_PENDING_AUTH", "Run auth-url first")
    code = args.get("code")
    got_state = args.get("state")
    callback_url = args.get("callback_url")
    if callback_url:
        parsed = urllib.parse.urlparse(callback_url)
        qs = urllib.parse.parse_qs(parsed.query)
        code = (qs.get("code") or [None])[0]
        got_state = (qs.get("state") or [None])[0]
        err = (qs.get("error") or [None])[0]
        if err:
            fail("OAUTH_ERROR", "OAuth provider returned an error", {"error": err})
    if not code:
        fail("MISSING_CODE", "Provide callback_url or code")
    if state.get("pending_state") and got_state != state.get("pending_state"):
        fail("STATE_MISMATCH", "OAuth state does not match the pending auth request")
    token = token_request(state, {
        "grant_type": "authorization_code",
        "code": code,
        "scope": " ".join(state.get("scopes") or DEFAULT_SCOPES),
        "redirect_uri": state.get("redirect_uri"),
    })
    if not token.get("access_token"):
        fail("TOKEN_EXCHANGE_FAILED", "Token response did not include access_token", {"keys": sorted(token.keys())})
    state["access_token"] = token["access_token"]
    if token.get("refresh_token"):
        state["refresh_token"] = token["refresh_token"]
    if token.get("expires_in"):
        state["expires_at"] = time.time() + int(token["expires_in"])
    state.pop("pending_state", None)
    save_state(state)
    emit({"ok": True, "authenticated": True, "region": state_region(state), "expires_in": token.get("expires_in"), "scope": token.get("scope")})


def handle_set_token(args):
    token = require_str(args, "access_token")
    region = args.get("region") or "cn"
    region_config(region)
    state = load_state()
    state["region"] = region
    state["access_token"] = token
    state.pop("refresh_token", None)
    if args.get("expires_in"):
        state["expires_at"] = time.time() + int(args["expires_in"])
    else:
        state.pop("expires_at", None)
    save_state(state)
    emit({"ok": True, "region": region, "authenticated": True})


def op_list_projects(args):
    emit_api("projects", api_call("GET", "/open/v1/project", load_state()))


def op_get_project(args):
    project_id = require_str(args, "project_id")
    emit_api("project", api_call("GET", f"/open/v1/project/{urllib.parse.quote(project_id, safe='')}", load_state()))


def op_get_project_data(args):
    project_id = require_str(args, "project_id")
    emit_api("data", api_call("GET", f"/open/v1/project/{urllib.parse.quote(project_id, safe='')}/data", load_state()))


def op_create_project(args):
    body = optional_body(args)
    if body.get("kind") in {"task", "note"}:
        body["kind"] = body["kind"].upper()
    emit_api("project", api_call("POST", "/open/v1/project", load_state(), body=body))


def op_update_project(args):
    project_id = require_str(args, "project_id")
    body = optional_body(args, skip=("project_id",))
    if body.get("kind") in {"task", "note"}:
        body["kind"] = body["kind"].upper()
    emit_api("project", api_call("POST", f"/open/v1/project/{urllib.parse.quote(project_id, safe='')}", load_state(), body=body))


def op_delete_project(args):
    project_id = require_str(args, "project_id")
    result = api_call("DELETE", f"/open/v1/project/{urllib.parse.quote(project_id, safe='')}", load_state())
    emit({"ok": True, "status": result.get("_status", 200), "project_id": project_id})


def op_create_task(args):
    emit_api("task", api_call("POST", "/open/v1/task", load_state(), body=args))


def op_get_task(args):
    project_id = require_str(args, "project_id")
    task_id = require_str(args, "task_id")
    path = f"/open/v1/project/{urllib.parse.quote(project_id, safe='')}/task/{urllib.parse.quote(task_id, safe='')}"
    emit_api("task", api_call("GET", path, load_state()))


def op_update_task(args):
    task_id = require_str(args, "task_id")
    task = args.get("task")
    if not isinstance(task, dict):
        fail("INVALID_TASK", "task must be an object")
    emit_api("task", api_call("POST", f"/open/v1/task/{urllib.parse.quote(task_id, safe='')}", load_state(), body=task))


def op_complete_task(args):
    project_id = require_str(args, "project_id")
    task_id = require_str(args, "task_id")
    path = f"/open/v1/project/{urllib.parse.quote(project_id, safe='')}/task/{urllib.parse.quote(task_id, safe='')}/complete"
    result = api_call("POST", path, load_state())
    emit({"ok": True, "status": result.get("_status", 200), "project_id": project_id, "task_id": task_id})


def op_delete_task(args):
    project_id = require_str(args, "project_id")
    task_id = require_str(args, "task_id")
    path = f"/open/v1/project/{urllib.parse.quote(project_id, safe='')}/task/{urllib.parse.quote(task_id, safe='')}"
    result = api_call("DELETE", path, load_state())
    emit({"ok": True, "status": result.get("_status", 200), "project_id": project_id, "task_id": task_id})


def op_move_task(args):
    moves = args.get("moves")
    if not isinstance(moves, list) or not moves:
        fail("INVALID_MOVES", "moves must be a non-empty array")
    emit_api("results", api_call("POST", "/open/v1/task/move", load_state(), body=moves))


def op_filter_tasks(args):
    emit_api("tasks", api_call("POST", "/open/v1/task/filter", load_state(), body=args))


def op_completed_tasks(args):
    emit_api("tasks", api_call("POST", "/open/v1/task/completed", load_state(), body=args))


def op_list_habits(args):
    emit_api("habits", api_call("GET", "/open/v1/habit", load_state()))


def op_list_focuses(args):
    query = {"from": require_str(args, "from"), "to": require_str(args, "to"), "type": args.get("type")}
    if query["type"] not in (0, 1):
        fail("INVALID_FOCUS_TYPE", "type must be 0 for Pomodoro or 1 for Timing")
    emit_api("focuses", api_call("GET", "/open/v1/focus", load_state(), query=query))


def op_request(args):
    method = require_str(args, "method").upper()
    if method not in {"GET", "POST", "DELETE"}:
        fail("INVALID_METHOD", "method must be GET, POST, or DELETE")
    path = ensure_open_path(require_str(args, "path"))
    query = args.get("query") or None
    if query is not None and not isinstance(query, dict):
        fail("INVALID_QUERY", "query must be an object")
    result = api_call(method, path, load_state(), query=query, body=args.get("body"))
    emit({"ok": True, "status": result.get("_status") if isinstance(result, dict) else 200, "data": result})


OPS = {
    "status": handle_status,
    "auth-url": handle_auth_url,
    "finish-auth": handle_finish_auth,
    "set-token": handle_set_token,
    "list-projects": op_list_projects,
    "get-project": op_get_project,
    "get-project-data": op_get_project_data,
    "create-project": op_create_project,
    "update-project": op_update_project,
    "delete-project": op_delete_project,
    "create-task": op_create_task,
    "get-task": op_get_task,
    "update-task": op_update_task,
    "complete-task": op_complete_task,
    "delete-task": op_delete_task,
    "move-task": op_move_task,
    "filter-tasks": op_filter_tasks,
    "list-completed-tasks": op_completed_tasks,
    "list-habits": op_list_habits,
    "list-focuses": op_list_focuses,
    "request": op_request,
}


def main():
    args = load_input()
    op = str(args.pop("skill_action", "status"))
    handler = OPS.get(op)
    if not handler:
        fail("UNKNOWN_OPERATION", "Unknown operation", {"operation": op, "known": sorted(OPS)})
    handler(args)


if __name__ == "__main__":
    main()

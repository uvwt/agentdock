#!/usr/bin/env python3
"""
ntfy.sh notification helper for AgentDock.
Pure stdlib HTTP client supporting send / event / health / subscribe operations.
"""
import json
import os
import sys
import time
import urllib.parse
import urllib.request
import urllib.error
import pathlib

# --- Helpers --- #

def _redact(s):
    if not s:
        return s
    if len(s) <= 4:
        return "****"
    return s[:2] + "****" + s[-2:]


def _safe_header(val):
    """Encode header value to be safe for HTTP (latin-1 compatible)."""
    if not val:
        return ""
    if isinstance(val, str):
        # Try encoding as latin-1, if it fails use UTF-8 percent encoding fallback
        try:
            val.encode("latin-1")
            return val
        except UnicodeEncodeError:
            # URL-encode the value for non-ASCII chars
            import urllib.parse
            return urllib.parse.quote(val, safe="")
    return str(val)

def _load_env_file(path):
    env = {}
    if not path:
        return env
    p = pathlib.Path(path)
    if not p.exists():
        return env
    for line in p.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        if "=" not in line:
            continue
        k, v = line.split("=", 1)
        env[k.strip()] = v.strip().strip('"').strip("'")
    return env

def _merge_env(params):
    env = dict(os.environ)
    env_file = params.get("env_file") or os.environ.get("NTFY_ENV_FILE")
    if env_file:
        env.update(_load_env_file(env_file))
    return env

def _get_server_url(params, env):
    url = params.get("server_url") or env.get("NTFY_SERVER_URL") or "https://ntfy.sh"
    url = url.rstrip("/")
    parsed = urllib.parse.urlparse(url)
    if parsed.scheme not in ("http", "https"):
        raise ValueError("server_url must be http or https, got: " + parsed.scheme)
    if "@" in (parsed.netloc or ""):
        raise ValueError("server_url must not contain embedded credentials")
    return url

def _get_topic(params, env):
    topic = params.get("topic") or env.get("NTFY_TOPIC")
    if not topic:
        raise ValueError("topic is required (set NTFY_TOPIC env or pass topic param)")
    if not all(c.isalnum() or c in "_-" for c in topic):
        raise ValueError("invalid topic name: " + repr(topic))
    return topic

def _build_auth_headers(env):
    headers = {}
    token = env.get("NTFY_TOKEN")
    if token:
        headers["Authorization"] = "Bearer " + token
        return headers
    user = env.get("NTFY_USERNAME")
    pw = env.get("NTFY_PASSWORD")
    if user and pw:
        import base64
        cred = base64.b64encode((user + ":" + pw).encode()).decode()
        headers["Authorization"] = "Basic " + cred
    return headers

def _priority_header(priority):
    mapping = {"default": "3", "high": "4", "urgent": "5"}
    return mapping.get(priority, "3")

def _config_summary(env, server_url, topic):
    return {
        "server_url": server_url,
        "topic": topic or "(not set)",
        "auth": "token" if env.get("NTFY_TOKEN") else ("basic" if env.get("NTFY_USERNAME") else "none"),
        "token_preview": _redact(env.get("NTFY_TOKEN", "")),
        "username": env.get("NTFY_USERNAME", ""),
    }

# --- HTTP --- #

def _http_request(method, url, headers, data=None, timeout=30):
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            body = resp.read()
            return resp.status, dict(resp.headers), body
    except urllib.error.HTTPError as e:
        body = e.read().decode("utf-8", errors="replace")
        raise RuntimeError("HTTP " + str(e.code) + " from " + url + ": " + body)
    except urllib.error.URLError as e:
        raise RuntimeError("connection error to " + url + ": " + str(e.reason))


# --- Operations --- #

def op_send(params):
    env = _merge_env(params)
    server_url = _get_server_url(params, env)
    topic = _get_topic(params, env)
    body = params.get("body", "")
    if not body:
        raise ValueError("body is required")
    headers = _build_auth_headers(env)
    headers["Title"] = _safe_header(params.get("title", ""))
    headers["Priority"] = _priority_header(params.get("priority", "default"))
    if params.get("tags"):
        headers["Tags"] = _safe_header(params["tags"])
    if params.get("click"):
        headers["Click"] = _safe_header(params["click"])
    if params.get("icon"):
        headers["Icon"] = _safe_header(params["icon"])
    ct = "text/markdown" if params.get("markdown") else "text/plain"
    headers["Content-Type"] = ct
    if params.get("dry_run"):
        return {
            "ok": True, "sent": False, "dry_run": True,
            "topic": topic, "server_url": server_url,
            "body_preview": body[:200],
            "headers": {k: v for k, v in headers.items() if k != "Authorization"},
        }
    url = server_url + "/" + topic
    status, resp_headers, resp_body = _http_request("POST", url, headers, data=body.encode("utf-8"), timeout=30)
    return {
        "ok": True, "sent": True, "status": status,
        "topic": topic, "server_url": server_url,
        "message_id": resp_headers.get("Message-ID", resp_headers.get("message-id", "")),
        "response_preview": resp_body.decode("utf-8", errors="replace")[:500],
    }

def op_event(params):
    env = _merge_env(params)
    server_url = _get_server_url(params, env)
    topic = _get_topic(params, env)
    severity = params.get("severity", "info")
    device = params.get("device", "")
    service = params.get("service", "")
    message = params.get("message", "")
    details = params.get("details", {})
    ts = time.strftime("%Y-%m-%d %H:%M:%S", time.localtime())
    priority = params.get("priority", "default")
    if severity == "error" and priority == "default":
        priority = "urgent"
    elif severity == "warning" and priority == "default":
        priority = "high"
    lines = []
    if message:
        lines.append("**[" + severity.upper() + "]** " + message)
    else:
        lines.append("**[" + severity.upper() + "]**")
    if device:
        lines.append("Device: `" + device + "`")
    if service:
        lines.append("Service: `" + service + "`")
    lines.append("Time: " + ts)
    if details:
        lines.append("")
        lines.append("| Key | Value |")
        lines.append("| --- | --- |")
        for k, v in details.items():
            lines.append("| " + str(k) + " | " + str(v) + " |")
    body = "\n".join(lines)
    tags = params.get("tags", "")
    if not tags:
        tag_map = {"info": "information_source", "warning": "warning", "error": "rotating_light", "success": "white_check_mark"}
        tags = tag_map.get(severity, "information_source")
    send_params = {
        "body": body,
        "title": "[" + severity.upper() + "] " + (service or device or "Event"),
        "priority": priority, "tags": tags, "markdown": True,
        "dry_run": params.get("dry_run", False),
        "server_url": server_url, "topic": topic,
        "env_file": params.get("env_file"),
    }
    if params.get("icon"):
        send_params["icon"] = params["icon"]
    return op_send(send_params)

def op_health(params):
    env = _merge_env(params)
    server_url = _get_server_url(params, env)
    topic = params.get("topic") or env.get("NTFY_TOPIC")
    result = {
        "ok": True, "ready": True,
        "config": _config_summary(env, server_url, topic),
        "timestamp": time.strftime("%Y-%m-%dT%H:%M:%S%z", time.localtime()),
    }
    if params.get("live"):
        if not topic:
            result["ready"] = False
            result["live_check"] = {"ok": False, "error": "no topic configured for live check"}
            return result
        try:
            test_params = {
                "body": "Health check from AgentDock ntfy skill",
                "title": "Health Check", "priority": "default",
                "tags": "white_check_mark",
                "server_url": server_url, "topic": topic,
                "env_file": params.get("env_file"),
            }
            live_result = op_send(test_params)
            result["live_check"] = {"ok": True, "send_result": live_result}
        except Exception as e:
            result["live_check"] = {"ok": False, "error": str(e)}
            result["ready"] = False
    else:
        result["live_check"] = "skipped (set live=true to test)"
    return result

def op_subscribe(params):
    env = _merge_env(params)
    server_url = _get_server_url(params, env)
    topic = _get_topic(params, env)
    headers = _build_auth_headers(env)
    since = params.get("since", "all")
    limit = str(params.get("limit", 10))
    poll = params.get("poll", False)
    url = server_url + "/" + topic + "/json?since=" + urllib.parse.quote(since) + "&limit=" + limit
    if poll:
        headers["Poll"] = "1"
    status, resp_headers, resp_body = _http_request("GET", url, headers, timeout=30)
    messages = []
    for line in resp_body.decode("utf-8", errors="replace").splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            messages.append(json.loads(line))
        except json.JSONDecodeError:
            continue
    return {
        "ok": True, "topic": topic, "server_url": server_url,
        "message_count": len(messages), "messages": messages,
    }


# --- Entry point --- #

OPERATIONS = {
    "send": op_send,
    "event": op_event,
    "health": op_health,
    "subscribe": op_subscribe,
}

def main():
    raw = sys.stdin.read().strip()
    if raw:
        try:
            params = json.loads(raw)
        except json.JSONDecodeError as e:
            print(json.dumps({"ok": False, "error": "invalid JSON input: " + str(e)}))
            return 0
    else:
        params = {}
    if not isinstance(params, dict):
        print(json.dumps({"ok": False, "error": "input must be a JSON object"}))
        return 0
    operation = str(params.pop("skill_action", "health"))
    if operation not in OPERATIONS:
        available = ", ".join(sorted(OPERATIONS.keys()))
        print(json.dumps({"ok": False, "error": "unknown operation: " + repr(operation) + "; available: " + available}))
        return 0
    try:
        result = OPERATIONS[operation](params)
        print(json.dumps(result, ensure_ascii=False, indent=2, sort_keys=True))
        return 0
    except Exception as e:
        print(json.dumps({"ok": False, "error": str(e), "operation": operation}, ensure_ascii=False))
        return 0

if __name__ == "__main__":
    raise SystemExit(main())


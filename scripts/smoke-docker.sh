#!/usr/bin/env sh
set -eu

BASE_URL="${AGENTDOCK_SMOKE_URL:-http://127.0.0.1:18766}"
TIMEOUT="${AGENTDOCK_SMOKE_TIMEOUT_SECONDS:-5}"

if ! command -v python3 >/dev/null 2>&1; then
  echo "agentdock smoke failed: python3 is required to validate MCP JSON responses" >&2
  exit 1
fi

export BASE_URL
export TIMEOUT
export AGENTDOCK_AUTH_TOKEN="${AGENTDOCK_AUTH_TOKEN:-}"

python3 - <<'PY'
import json
import os
import sys
import time
import urllib.error
import urllib.request

base_url = os.environ["BASE_URL"].rstrip("/")
timeout = float(os.environ["TIMEOUT"])
token = os.environ.get("AGENTDOCK_AUTH_TOKEN", "")
attempts = int(os.environ.get("AGENTDOCK_SMOKE_ATTEMPTS", "10"))


def request(method, path, payload=None):
    data = None
    headers = {}
    if payload is not None:
        data = json.dumps(payload).encode("utf-8")
        headers["content-type"] = "application/json"
    if token:
        headers["authorization"] = f"Bearer {token}"
    req = urllib.request.Request(base_url + path, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            body = resp.read().decode("utf-8")
            return resp.status, body
    except urllib.error.HTTPError as exc:
        body = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"{method} {path} returned HTTP {exc.code}: {body.strip()}") from exc
    except urllib.error.URLError as exc:
        raise RuntimeError(f"{method} {path} failed: {exc.reason}") from exc


def mcp(method, params=None, request_id=1):
    payload = {"jsonrpc": "2.0", "id": request_id, "method": method}
    if params is not None:
        payload["params"] = params
    _, body = request("POST", "/mcp", payload)
    try:
        parsed = json.loads(body)
    except json.JSONDecodeError as exc:
        raise RuntimeError(f"MCP {method} returned non-JSON response: {body[:200]}") from exc
    if "error" in parsed:
        raise RuntimeError(f"MCP {method} returned error: {parsed['error']}")
    if "result" not in parsed:
        raise RuntimeError(f"MCP {method} response missing result: {parsed}")
    return parsed["result"]


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


try:
    print(f"==> healthz {base_url}/healthz")
    health_body = None
    last_error = None
    for attempt in range(1, attempts + 1):
        try:
            _, health_body = request("GET", "/healthz")
            break
        except Exception as exc:
            last_error = exc
            if attempt == attempts:
                raise
            time.sleep(1)
    if health_body is None:
        raise RuntimeError(f"healthz did not respond: {last_error}")
    health = json.loads(health_body)
    require(health.get("ok") is True, f"healthz did not return ok=true: {health}")
    print("healthz ok")

    print("==> MCP initialize")
    init = mcp("initialize", {"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "agentdock-smoke", "version": "0"}})
    require(init.get("serverInfo", {}).get("name") == "agentdock", f"unexpected initialize result: {init}")
    print("initialize ok")

    print("==> MCP tools/list")
    tools = mcp("tools/list", request_id=2).get("tools", [])
    tool_names = {tool.get("name") for tool in tools if isinstance(tool, dict)}
    require("server_info" in tool_names, f"server_info not exposed; tools={sorted(tool_names)}")
    print(f"tools/list ok ({len(tool_names)} tools)")

    print("==> MCP tools/call server_info")
    call = mcp("tools/call", {"name": "server_info", "arguments": {}}, request_id=3)
    require(call.get("isError") is False, f"server_info returned an error envelope: {call}")
    info = call.get("structuredContent") or {}
    require(info.get("ok") is True, f"server_info did not return ok=true: {info}")
    require(info.get("endpoint_path") == "/mcp", f"unexpected endpoint_path: {info}")
    print(f"server_info ok (profile={info.get('tool_profile')}, auth_enabled={info.get('auth_enabled')})")

    print("agentdock smoke ok")
except Exception as exc:
    print(f"agentdock smoke failed: {exc}", file=sys.stderr)
    if "HTTP 401" in str(exc):
        print("hint: set AGENTDOCK_AUTH_TOKEN to the same bearer token used by the running container.", file=sys.stderr)
    else:
        print("hint: ensure docker compose is running and AGENTDOCK_SMOKE_URL points at the published AgentDock port.", file=sys.stderr)
    sys.exit(1)
PY

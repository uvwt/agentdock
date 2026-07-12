#!/usr/bin/env python3
import json
import os
import sys
import urllib.error
import urllib.request

ENDPOINT = "https://i.weread.qq.com/api/agent/gateway"
VERSION = "1.0.6"
ALLOWED_PREFIXES = (
    "/_list",
    "/store/",
    "/book/",
    "/shelf/",
    "/user/",
    "/review/",
    "/readdata/",
)


def emit(value):
    print(json.dumps(value, ensure_ascii=False, separators=(",", ":")))


def fail(code, message, details=None, exit_code=1):
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
        fail("INVALID_INPUT", "输入不是有效 JSON", {"reason": str(exc)})
    if not isinstance(value, dict):
        fail("INVALID_INPUT", "输入必须是 JSON 对象")
    return value


def gateway(api_name, params=None):
    if not isinstance(api_name, str) or not api_name.startswith(ALLOWED_PREFIXES):
        fail("API_NOT_ALLOWED", "api_name 不在微信读书 Skill 允许范围内", {"api_name": api_name})
    token = os.environ.get("WEREAD_API_KEY", "").strip()
    if not token:
        fail("MISSING_API_KEY", "宿主 AgentDock 环境未配置 WEREAD_API_KEY")
    body = {"api_name": api_name, "skill_version": VERSION}
    if params:
        if not isinstance(params, dict):
            fail("INVALID_PARAMS", "params 必须是 JSON 对象")
        for key, value in params.items():
            if key in {"api_name", "skill_version"}:
                continue
            body[key] = value
    request = urllib.request.Request(
        ENDPOINT,
        data=json.dumps(body, ensure_ascii=False).encode("utf-8"),
        headers={
            "Authorization": "Bearer " + token,
            "Content-Type": "application/json",
            "User-Agent": "AgentDock-WeRead/1.0.6",
        },
        method="POST",
    )
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            raw = response.read()
            status = response.status
    except urllib.error.HTTPError as exc:
        raw = exc.read()
        text = raw.decode("utf-8", errors="replace")[:4000]
        fail("HTTP_ERROR", "微信读书接口返回 HTTP 错误", {"status": exc.code, "body": text})
    except urllib.error.URLError as exc:
        fail("NETWORK_ERROR", "无法连接微信读书接口", {"reason": str(exc.reason)})
    try:
        data = json.loads(raw.decode("utf-8"))
    except Exception:
        fail("INVALID_RESPONSE", "微信读书接口返回非 JSON 数据", {"status": status})
    if isinstance(data, dict) and data.get("upgrade_info"):
        fail("UPGRADE_REQUIRED", "微信读书 Skill 需要升级", data.get("upgrade_info"))
    return data


def main():
    args = load_input()
    operation = str(args.pop("skill_action", "status"))
    if operation == "status":
        data = gateway("/_list")
        count = None
        if isinstance(data, dict):
            for key in ("apis", "data", "list"):
                if isinstance(data.get(key), list):
                    count = len(data[key])
                    break
        emit({"ok": True, "configured": True, "endpoint": ENDPOINT, "skill_version": VERSION, "api_count": count})
        return
    if operation == "list-apis":
        emit({"ok": True, "data": gateway("/_list")})
        return
    if operation == "search":
        params = {"keyword": args["keyword"], "scope": args.get("scope", 10)}
        if "maxIdx" in args:
            params["maxIdx"] = args["maxIdx"]
        if "count" in args:
            params["count"] = args["count"]
        emit({"ok": True, "data": gateway("/store/search", params)})
        return
    if operation == "shelf":
        emit({"ok": True, "data": gateway("/shelf/sync")})
        return
    if operation == "call":
        emit({"ok": True, "data": gateway(args["api_name"], args.get("params", {}))})
        return
    fail("UNKNOWN_OPERATION", "未知操作", {"operation": operation})


if __name__ == "__main__":
    main()

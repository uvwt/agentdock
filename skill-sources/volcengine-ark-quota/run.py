#!/usr/bin/env python3
"""Read-only Skill for querying VolcEngine Ark Coding Plan quota usage."""
from __future__ import annotations

import json
import os
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from datetime import datetime, timezone
from http.cookiejar import Cookie, CookieJar
from pathlib import Path
from typing import Any

PAGE_URL = "https://console.volcengine.com/ark/region:{region}/subscription/coding-plan"
API_URL = "https://console.volcengine.com/api/top/ark/{region}/2024-01-01/{action}"
USER_AGENT = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) AgentDock/volcengine-ark-quota Safari/537.36"
SENSITIVE_KEY_NAMES = {"cookie", "cookie_header", "csrf_token", "token", "authorization", "secret", "password", "apikey", "api_key", "x-csrf-token", "x-csrf"}
CSRF_NAMES = ("csrf", "csrf_token", "csrftoken", "csrfToken", "XSRF-TOKEN", "_csrf")
LEVEL_LABELS = {
    "session": "当前会话",
    "weekly": "近 1 周",
    "monthly": "近 1 月",
}
LEVEL_ORDER = {"session": 0, "weekly": 1, "monthly": 2}


class SkillError(RuntimeError):
    def __init__(self, message: str, code: str, details: Any = None) -> None:
        super().__init__(message)
        self.code = code
        self.details = details


def emit(value: dict[str, Any]) -> int:
    print(json.dumps(value, ensure_ascii=False, indent=2, sort_keys=True))
    return 0


def load_input() -> dict[str, Any]:
    raw = sys.stdin.read().strip()
    if not raw:
        return {}
    try:
        value = json.loads(raw)
    except json.JSONDecodeError as exc:
        raise SkillError(f"Invalid JSON input: {exc}", "bad_json") from exc
    if not isinstance(value, dict):
        raise SkillError("Skill input must be a JSON object.", "bad_json")
    return value


def clean_region(value: Any) -> str:
    region = str(value or "cn-beijing").strip()
    if not re.fullmatch(r"[a-z]{2}(?:-[a-z0-9]+)+", region):
        raise SkillError("region must look like cn-beijing.", "bad_region")
    return region


def timeout_from(value: Any, default: int = 20) -> int:
    try:
        timeout = int(value if value is not None else default)
    except (TypeError, ValueError) as exc:
        raise SkillError("timeout_seconds must be an integer.", "bad_timeout") from exc
    return max(3, min(timeout, 60))


def default_storage_path() -> Path:
    configured = os.environ.get("VOLCENGINE_ARK_STORAGE_STATE", "").strip()
    if configured:
        return Path(configured).expanduser()
    agentdock_home = Path(os.environ.get("AGENTDOCK_HOME", Path.home() / ".agentdock"))
    return agentdock_home / "skill-data" / "volcengine-ark-quota" / "storage_state.json"


def make_cookie(name: str, value: str, domain: str, path: str = "/", secure: bool = True) -> Cookie:
    return Cookie(
        version=0,
        name=name,
        value=value,
        port=None,
        port_specified=False,
        domain=domain,
        domain_specified=domain.startswith("."),
        domain_initial_dot=domain.startswith("."),
        path=path or "/",
        path_specified=True,
        secure=secure,
        expires=None,
        discard=True,
        comment=None,
        comment_url=None,
        rest={"HttpOnly": None},
        rfc2109=False,
    )


def add_raw_cookie_header(jar: CookieJar, header: str) -> int:
    count = 0
    for part in header.split(";"):
        if "=" not in part:
            continue
        name, value = part.split("=", 1)
        name, value = name.strip(), value.strip()
        if not name:
            continue
        for domain in (".volcengine.com", "console.volcengine.com"):
            jar.set_cookie(make_cookie(name, value, domain=domain))
        count += 1
    return count


def add_storage_state(jar: CookieJar, path: Path) -> int:
    if not path.is_file():
        return 0
    try:
        state = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise SkillError(f"storage_state_path is not a readable Playwright JSON file: {path}", "bad_storage_state") from exc
    cookies = state.get("cookies") if isinstance(state, dict) else None
    if not isinstance(cookies, list):
        raise SkillError("storage_state_path has no cookies array.", "bad_storage_state")
    count = 0
    for item in cookies:
        if not isinstance(item, dict):
            continue
        name = str(item.get("name") or "").strip()
        value = str(item.get("value") or "")
        domain = str(item.get("domain") or "").strip() or ".volcengine.com"
        path_value = str(item.get("path") or "/")
        if not name:
            continue
        expires = item.get("expires")
        if isinstance(expires, (int, float)) and expires > 0 and expires < time.time():
            continue
        jar.set_cookie(make_cookie(name, value, domain=domain, path=path_value, secure=bool(item.get("secure", True))))
        count += 1
    return count


def build_cookie_jar(inp: dict[str, Any]) -> tuple[CookieJar, dict[str, Any]]:
    jar = CookieJar()
    sources: dict[str, Any] = {"cookie_env": False, "cookie_input": False, "storage_state": None, "cookie_count": 0}
    env_cookie = os.environ.get("VOLCENGINE_ARK_COOKIE", "").strip()
    if env_cookie:
        sources["cookie_count"] += add_raw_cookie_header(jar, env_cookie)
        sources["cookie_env"] = True
    input_cookie = str(inp.get("cookie") or "").strip()
    if input_cookie:
        sources["cookie_count"] += add_raw_cookie_header(jar, input_cookie)
        sources["cookie_input"] = True
    storage_path = Path(str(inp.get("storage_state_path") or default_storage_path())).expanduser()
    try:
        added = add_storage_state(jar, storage_path)
    except SkillError:
        raise
    if added:
        sources["storage_state"] = str(storage_path)
        sources["cookie_count"] += added
    else:
        sources["storage_state"] = str(storage_path) if storage_path.exists() else None
    return jar, sources


def find_csrf(jar: CookieJar, explicit: str | None = None) -> str | None:
    if explicit:
        return explicit
    env = os.environ.get("VOLCENGINE_ARK_CSRF_TOKEN", "").strip()
    if env:
        return env
    exact_console: list[str] = []
    exact_volc: list[str] = []
    fallback: list[str] = []
    for cookie in jar:
        lname = cookie.name.lower()
        domain = (cookie.domain or "").lower().lstrip(".")
        if lname == "csrftoken" and (domain == "console.volcengine.com" or domain.endswith(".console.volcengine.com")):
            exact_console.append(cookie.value)
        elif lname == "csrftoken" and (domain == "volcengine.com" or domain.endswith(".volcengine.com")):
            exact_volc.append(cookie.value)
        elif any(name.lower() in lname for name in CSRF_NAMES) and (domain == "volcengine.com" or domain.endswith(".volcengine.com")):
            fallback.append(cookie.value)
    if exact_console:
        return exact_console[-1]
    if exact_volc:
        return exact_volc[-1]
    return fallback[-1] if fallback else None


def request_json(opener: urllib.request.OpenerDirector, url: str, *, data: dict[str, Any] | None = None, headers: dict[str, str] | None = None, timeout: int = 20, method: str = "GET") -> tuple[int, dict[str, Any] | str, dict[str, str]]:
    body = None
    merged = {"User-Agent": USER_AGENT, "Accept": "application/json, text/plain, */*"}
    if headers:
        merged.update(headers)
    if data is not None:
        body = json.dumps(data, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
        merged.setdefault("Content-Type", "application/json")
    req = urllib.request.Request(url, data=body, headers=merged, method=method)
    try:
        with opener.open(req, timeout=timeout) as response:
            raw = response.read()
            status = int(response.status)
            response_headers = {k.lower(): v for k, v in response.headers.items()}
    except urllib.error.HTTPError as exc:
        raw = exc.read()
        status = int(exc.code)
        response_headers = {k.lower(): v for k, v in exc.headers.items()}
    except urllib.error.URLError as exc:
        raise SkillError("Unable to reach VolcEngine console endpoint.", "network_error", str(exc)) from exc
    text = raw.decode("utf-8", errors="replace")
    try:
        return status, json.loads(text), response_headers
    except json.JSONDecodeError:
        return status, text[:2000], response_headers


def page_reachable(region: str, timeout: int) -> dict[str, Any]:
    opener = urllib.request.build_opener()
    url = PAGE_URL.format(region=region)
    req = urllib.request.Request(url, headers={"User-Agent": USER_AGENT, "Accept": "text/html,application/xhtml+xml"})
    try:
        with opener.open(req, timeout=timeout) as response:
            sample = response.read(65536).decode("utf-8", errors="replace")
            return {
                "ok": 200 <= int(response.status) < 400,
                "status": int(response.status),
                "url": response.geturl(),
                "title_hint": "Codingplan" if "coding-plan" in sample or "Coding Plan" in sample else None,
                "has_coding_plan_module": "coding-plan-pane" in sample,
            }
    except Exception as exc:
        return {"ok": False, "error": str(exc)}


def api_error(payload: Any) -> dict[str, Any] | None:
    if not isinstance(payload, dict):
        return None
    meta = payload.get("ResponseMetadata")
    if isinstance(meta, dict):
        err = meta.get("Error")
        if isinstance(err, dict):
            return {
                "code": err.get("Code") or err.get("code"),
                "message": err.get("Message") or err.get("message"),
                "request_id": meta.get("RequestId"),
                "action": meta.get("Action"),
            }
    err = payload.get("Error") or payload.get("error")
    if isinstance(err, dict):
        return {"code": err.get("Code") or err.get("code"), "message": err.get("Message") or err.get("message")}
    return None


def call_action(opener: urllib.request.OpenerDirector, jar: CookieJar, region: str, action: str, body: dict[str, Any], timeout: int, csrf_token: str | None) -> dict[str, Any]:
    url = API_URL.format(region=region, action=urllib.parse.quote(action, safe=""))
    referer = PAGE_URL.format(region=region)
    base_headers = {
        "Origin": "https://console.volcengine.com",
        "Referer": referer,
    }
    token = find_csrf(jar, csrf_token)
    headers = dict(base_headers)
    if token:
        headers["X-Csrf-Token"] = token
    status, payload, _ = request_json(opener, url, data=body, headers=headers, timeout=timeout, method="POST")
    err = api_error(payload)
    if (not token or (err and err.get("code") == "InvalidCSRFToken")):
        token = find_csrf(jar, csrf_token)
        if token:
            headers = dict(base_headers)
            headers["X-Csrf-Token"] = token
            status, payload, _ = request_json(opener, url, data=body, headers=headers, timeout=timeout, method="POST")
            err = api_error(payload)
    if isinstance(payload, dict):
        payload.setdefault("_http_status", status)
    return {"status": status, "payload": payload, "error": err, "csrf_present": bool(token)}


def iso_from_seconds(value: Any) -> str | None:
    try:
        if value is None or value == "":
            return None
        ts = float(value)
        if ts > 10_000_000_000:
            ts = ts / 1000.0
        return datetime.fromtimestamp(ts, timezone.utc).isoformat().replace("+00:00", "Z")
    except Exception:
        return None


def redact(value: Any) -> Any:
    if isinstance(value, dict):
        result = {}
        for k, v in value.items():
            key = str(k).strip().lower().replace("-", "_")
            if key in {name.replace("-", "_") for name in SENSITIVE_KEY_NAMES} or key.endswith("_token") or key.endswith("_secret"):
                result[k] = "[REDACTED]" if v not in (None, "") else v
            else:
                result[k] = redact(v)
        return result
    if isinstance(value, list):
        return [redact(v) for v in value]
    if isinstance(value, str):
        # Mask obvious access key and cookie-like long values in nested provider responses.
        if len(value) > 48 and re.search(r"[A-Za-z0-9_\-]{24,}", value):
            return value[:6] + "…" + value[-4:]
    return value


def normalize_usage(payload: dict[str, Any]) -> list[dict[str, Any]]:
    rows = payload.get("QuotaUsage")
    if not isinstance(rows, list) and isinstance(payload.get("Result"), dict):
        rows = payload["Result"].get("QuotaUsage")
    if not isinstance(rows, list) and isinstance(payload.get("Data"), dict):
        rows = payload["Data"].get("QuotaUsage")
    if not isinstance(rows, list):
        return []
    result: list[dict[str, Any]] = []
    now = time.time()
    for row in rows:
        if not isinstance(row, dict):
            continue
        level = str(row.get("Level") or row.get("level") or "").strip()
        percent = row.get("Percent", row.get("percent"))
        try:
            percent_value = float(percent)
        except (TypeError, ValueError):
            percent_value = None
        reset = row.get("ResetTimestamp", row.get("resetTimestamp", row.get("reset_timestamp")))
        reset_at = iso_from_seconds(reset)
        reset_seconds = None
        try:
            reset_float = float(reset)
            if reset_float > 10_000_000_000:
                reset_float /= 1000.0
            reset_seconds = max(0, int(reset_float - now))
        except Exception:
            pass
        item = {
            "level": level,
            "label": LEVEL_LABELS.get(level, level or None),
            "used_percent": round(percent_value, 2) if percent_value is not None else None,
            "remaining_percent": round(max(0.0, 100.0 - percent_value), 2) if percent_value is not None else None,
            "reset_at": reset_at,
            "reset_seconds": reset_seconds,
        }
        for key in ("Used", "Limit", "UsedTokens", "TotalTokens", "Unit", "ResetTimestamp", "UpdateTimestamp"):
            if key in row:
                item[key[0].lower() + key[1:]] = row[key]
        result.append(item)
    result.sort(key=lambda x: LEVEL_ORDER.get(str(x.get("level") or ""), 99))
    return result


def normalize_plan(payload: dict[str, Any]) -> dict[str, Any] | None:
    rows = payload.get("InfoList")
    if not isinstance(rows, list) and isinstance(payload.get("Result"), dict):
        rows = payload["Result"].get("InfoList")
    if not isinstance(rows, list) and isinstance(payload.get("Data"), dict):
        rows = payload["Data"].get("InfoList")
    if not isinstance(rows, list) or not rows:
        return None
    row = rows[0] if isinstance(rows[0], dict) else {}
    if not row:
        return None
    return {
        "plan_type": row.get("BizInfo") or row.get("PlanType"),
        "resource_type": row.get("ResourceType"),
        "resource_name": row.get("ResourceName"),
        "status": row.get("Status"),
        "quantity": row.get("Quantity"),
        "start_time": row.get("StartTime"),
        "end_time": row.get("EndTime"),
        "enable_auto_renew": row.get("EnableAutoRenew"),
        "instance_id": row.get("InstanceID") or row.get("InstanceId"),
    }


def normalize_models(payload: dict[str, Any]) -> list[dict[str, Any]]:
    rows = payload.get("Data")
    if not isinstance(rows, list):
        return []
    result = []
    for row in rows:
        if isinstance(row, dict):
            result.append({
                "model_id": row.get("ModelId"),
                "model_name": row.get("ModelName"),
                "enabled": row.get("Enabled"),
                "description": row.get("Description"),
            })
    return result


def status(inp: dict[str, Any]) -> dict[str, Any]:
    region = clean_region(inp.get("region"))
    timeout = timeout_from(inp.get("timeout_seconds"), 10)
    jar, sources = build_cookie_jar(inp)
    page = page_reachable(region, timeout)
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
    probe = call_action(opener, jar, region, "GetCodingPlanUsage", {}, timeout, None)
    err = probe.get("error")
    endpoint_ok = bool(err and err.get("code") in ("InvalidCSRFToken", "NotLogin")) or isinstance(probe.get("payload"), dict)
    return {
        "ok": bool(page.get("ok")) and endpoint_ok,
        "operation": "status",
        "region": region,
        "page": page,
        "endpoint": {
            "reachable": endpoint_ok,
            "http_status": probe.get("status"),
            "provider_error_code": err.get("code") if err else None,
            "csrf_present_after_probe": probe.get("csrf_present"),
        },
        "auth_sources": {
            "cookie_env_present": sources["cookie_env"],
            "cookie_input_present": sources["cookie_input"],
            "storage_state_path": sources["storage_state"],
            "cookie_count": sources["cookie_count"],
        },
        "auth_configured": bool(sources["cookie_count"]),
        "notes": [
            "真实额度查询需要已登录 console.volcengine.com 的 Cookie 或 Playwright storage_state。",
            "未登录时接口通常返回 NotLogin；缺 CSRF 时会自动探测并重试。",
        ],
    }


def query(inp: dict[str, Any]) -> dict[str, Any]:
    region = clean_region(inp.get("region"))
    timeout = timeout_from(inp.get("timeout_seconds"), 20)
    jar, sources = build_cookie_jar(inp)
    if not sources["cookie_count"]:
        raise SkillError("No VolcEngine login cookies found. Provide storage_state_path, cookie input, or VOLCENGINE_ARK_COOKIE.", "auth_missing", sources)
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
    explicit_csrf = str(inp.get("csrf_token") or "").strip() or None
    usage = call_action(opener, jar, region, "GetCodingPlanUsage", {}, timeout, explicit_csrf)
    usage_err = usage.get("error")
    if usage_err:
        code = str(usage_err.get("code") or "")
        if code in {"NotLogin", "InvalidCSRFToken"}:
            return {
                "ok": False,
                "operation": "query",
                "code": "auth_failed" if code == "NotLogin" else "csrf_failed",
                "provider_error": usage_err,
                "region": region,
                "auth_sources": {
                    "storage_state_path": sources["storage_state"],
                    "cookie_count": sources["cookie_count"],
                    "csrf_present": usage.get("csrf_present"),
                },
                "message": "登录态不可用或已过期，请重新导出 console.volcengine.com 的 storage_state/Cookie。",
            }
        return {"ok": False, "operation": "query", "code": "provider_error", "provider_error": usage_err, "region": region}
    payload = usage.get("payload") if isinstance(usage.get("payload"), dict) else {}
    result: dict[str, Any] = {
        "ok": True,
        "operation": "query",
        "region": region,
        "quota_periods": normalize_usage(payload),
        "update_timestamp": (payload.get("UpdateTimestamp") or (payload.get("Result", {}) if isinstance(payload.get("Result"), dict) else {}).get("UpdateTimestamp")) if isinstance(payload, dict) else None,
        "auth_sources": {
            "storage_state_path": sources["storage_state"],
            "cookie_count": sources["cookie_count"],
            "csrf_present": usage.get("csrf_present"),
        },
    }
    project_name = str(inp.get("project_name") or "").strip()
    if bool(inp.get("include_plan", True)):
        plan_body: dict[str, Any] = {"ResourceTypes": ["CodingPlan"], "ResourceNames": [""], "BizInfos": ["lite", "pro"]}
        if project_name:
            plan_body["ProjectName"] = project_name
        plan = call_action(opener, jar, region, "ListSubscribeTrade", plan_body, timeout, explicit_csrf)
        if plan.get("error"):
            result["plan_error"] = plan["error"]
        elif isinstance(plan.get("payload"), dict):
            result["plan"] = normalize_plan(plan["payload"])
            if inp.get("include_raw"):
                result["raw_plan"] = redact(plan["payload"])
    if bool(inp.get("include_models", False)):
        model_body = {"AccountId": ""}
        models = call_action(opener, jar, region, "ListArkCodeLatestModel", model_body, timeout, explicit_csrf)
        if models.get("error"):
            result["models_error"] = models["error"]
        elif isinstance(models.get("payload"), dict):
            result["models"] = normalize_models(models["payload"])
            if inp.get("include_raw"):
                result["raw_models"] = redact(models["payload"])
    if inp.get("include_raw"):
        result["raw_usage"] = redact(payload)
    return result


def main() -> int:
    operation = "status"
    try:
        inp = load_input()
        operation = str(inp.pop("skill_action", "status"))
        if operation == "status":
            return emit(status(inp))
        if operation == "query":
            return emit(query(inp))
        raise SkillError(f"Unsupported operation: {operation}", "bad_operation")
    except SkillError as exc:
        payload = {"ok": False, "operation": operation, "code": exc.code, "error": str(exc)}
        if exc.details is not None:
            payload["details"] = redact(exc.details)
        return emit(payload)
    except Exception as exc:  # defensive JSON error path
        return emit({"ok": False, "operation": operation, "code": "unexpected_error", "error": str(exc)})


if __name__ == "__main__":
    raise SystemExit(main())

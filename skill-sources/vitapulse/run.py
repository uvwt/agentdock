#!/usr/bin/env python3
from __future__ import annotations

import datetime as dt
import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request
from typing import Any

SKILL_VERSION = os.getenv("AGENTDOCK_SKILL_VERSION", "0.2.2")
DEFAULT_BASE_URL = "http://127.0.0.1:8787"


class SkillError(Exception):
    def __init__(self, code: str, message: str, *, http_status: int | None = None):
        super().__init__(message)
        self.code = code
        self.message = message
        self.http_status = http_status


class VitaPulseClient:
    def __init__(self, timeout_seconds: int):
        base_url = (os.getenv("VITAPULSE_BASE_URL", DEFAULT_BASE_URL) or DEFAULT_BASE_URL).strip().rstrip("/")
        parsed = urllib.parse.urlparse(base_url)
        if parsed.scheme not in {"http", "https"} or not parsed.netloc:
            raise SkillError("invalid_configuration", "VITAPULSE_BASE_URL must be an absolute HTTP(S) URL")
        token = os.getenv("VITAPULSE_API_TOKEN", "").strip()
        if not token:
            raise SkillError("auth_not_configured", "VITAPULSE_API_TOKEN is required")
        self.base_url = base_url
        self.token = token
        self.timeout_seconds = timeout_seconds

    def request(
        self,
        method: str,
        path: str,
        *,
        query: dict[str, Any] | None = None,
        body: dict[str, Any] | None = None,
        authenticated: bool = True,
    ) -> Any:
        url = self.base_url + path
        if query:
            clean = {key: value for key, value in query.items() if value not in (None, "")}
            if clean:
                url += "?" + urllib.parse.urlencode(clean)
        data = None
        headers = {"Accept": "application/json", "User-Agent": f"AgentDock-VitaPulse/{SKILL_VERSION}"}
        if authenticated:
            headers["Authorization"] = "Bearer " + self.token
        if body is not None:
            data = json.dumps(body, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
            headers["Content-Type"] = "application/json"
        request = urllib.request.Request(url, data=data, headers=headers, method=method)
        try:
            with urllib.request.urlopen(request, timeout=self.timeout_seconds) as response:
                raw = response.read()
        except urllib.error.HTTPError as exc:
            raw = exc.read()
            message = f"VitaPulse API returned HTTP {exc.code}"
            try:
                payload = json.loads(raw.decode("utf-8"))
                if isinstance(payload, dict) and isinstance(payload.get("error"), str):
                    message = payload["error"]
            except (UnicodeDecodeError, json.JSONDecodeError):
                pass
            raise SkillError("http_error", message, http_status=exc.code) from None
        except urllib.error.URLError as exc:
            raise SkillError("connection_failed", f"cannot reach VitaPulse: {getattr(exc, 'reason', exc)}") from None
        except TimeoutError:
            raise SkillError("timeout", "VitaPulse request timed out") from None
        if not raw:
            return None
        try:
            return json.loads(raw.decode("utf-8"))
        except (UnicodeDecodeError, json.JSONDecodeError):
            raise SkillError("invalid_response", "VitaPulse returned invalid JSON") from None


def timeout_from(data: dict[str, Any]) -> int:
    value = data.get("timeout_seconds", 15)
    if isinstance(value, bool) or not isinstance(value, int) or not 3 <= value <= 60:
        raise SkillError("invalid_input", "timeout_seconds must be an integer between 3 and 60")
    return value


def integer(data: dict[str, Any], name: str, default: int, minimum: int, maximum: int) -> int:
    value = data.get(name, default)
    if isinstance(value, bool) or not isinstance(value, int) or not minimum <= value <= maximum:
        raise SkillError("invalid_input", f"{name} must be an integer between {minimum} and {maximum}")
    return value


def required_text(data: dict[str, Any], name: str, maximum: int = 256) -> str:
    value = data.get(name)
    if not isinstance(value, str) or not value.strip() or len(value) > maximum:
        raise SkillError("invalid_input", f"{name} is required and must be at most {maximum} characters")
    return value.strip()


def operation_status(data: dict[str, Any]) -> dict[str, Any]:
    client = VitaPulseClient(timeout_from(data))
    health = client.request("GET", "/healthz", authenticated=False)
    capabilities = client.request("GET", "/api/v1/capabilities", authenticated=False)
    freshness = client.request("GET", "/api/v1/health/freshness")
    return {
        "ok": True,
        "operation": "status",
        "version": SKILL_VERSION,
        "endpoint": client.base_url,
        "ready": True,
        "authenticated": True,
        "health": health,
        "capabilities": capabilities,
        "freshness": freshness,
    }


def operation_today_summary(data: dict[str, Any]) -> dict[str, Any]:
    client = VitaPulseClient(timeout_from(data))
    timezone = str(data.get("timezone") or os.getenv("TZ") or "Asia/Shanghai")
    today = dt.datetime.now().astimezone().date().isoformat()
    payload = client.request("GET", "/api/v1/health/daily", query={
        "start_date": data.get("date", today),
        "end_date": data.get("date", today),
        "timezone": timezone,
        "type_identifier": data.get("type_identifier", ""),
    })
    return {"ok": True, "operation": "today_summary", **payload}


def operation_latest_metric(data: dict[str, Any]) -> dict[str, Any]:
    client = VitaPulseClient(timeout_from(data))
    type_identifier = required_text(data, "type_identifier")
    payload = client.request("GET", "/api/v1/health/latest", query={"type_identifier": type_identifier})
    return {"ok": True, "operation": "latest_metric", "type_identifier": type_identifier, **payload}


def operation_trend(data: dict[str, Any]) -> dict[str, Any]:
    client = VitaPulseClient(timeout_from(data))
    type_identifier = required_text(data, "type_identifier")
    days = integer(data, "days", 30, 1, 366)
    timezone = str(data.get("timezone") or os.getenv("TZ") or "Asia/Shanghai")
    end = dt.datetime.now().astimezone().date()
    start = end - dt.timedelta(days=days - 1)
    payload = client.request("GET", "/api/v1/health/trends", query={
        "type_identifier": type_identifier,
        "start_date": start.isoformat(),
        "end_date": end.isoformat(),
        "timezone": timezone,
    })
    return {"ok": True, "operation": "trend", "days": days, **payload}


def operation_query_samples(data: dict[str, Any]) -> dict[str, Any]:
    client = VitaPulseClient(timeout_from(data))
    payload = client.request("GET", "/api/v1/health/samples", query={
        "type_identifier": data.get("type_identifier", ""),
        "start": data.get("start", ""),
        "end": data.get("end", ""),
        "limit": integer(data, "limit", 50, 1, 500),
        "include_raw": bool(data.get("include_raw", False)),
        "include_deleted": bool(data.get("include_deleted", False)),
    })
    return {"ok": True, "operation": "query_samples", **payload}


def operation_freshness(data: dict[str, Any]) -> dict[str, Any]:
    client = VitaPulseClient(timeout_from(data))
    payload = client.request("GET", "/api/v1/health/freshness")
    return {"ok": True, "operation": "freshness", **payload}


def operation_sync_request(data: dict[str, Any]) -> dict[str, Any]:
    client = VitaPulseClient(timeout_from(data))
    body: dict[str, Any] = {}
    if isinstance(data.get("device_id"), str) and data["device_id"].strip():
        body["device_id"] = data["device_id"].strip()
    payload = client.request("POST", "/api/v1/sync-requests", body=body)
    return {"ok": True, "operation": "sync_request", "immediate_execution_guaranteed": False, "request": payload}


def operation_sync_request_status(data: dict[str, Any]) -> dict[str, Any]:
    client = VitaPulseClient(timeout_from(data))
    request_id = required_text(data, "request_id", 128)
    payload = client.request("GET", "/api/v1/sync-requests/" + urllib.parse.quote(request_id, safe=""))
    return {"ok": True, "operation": "sync_request_status", "request": payload}


def operation_clinical_summary(data: dict[str, Any]) -> dict[str, Any]:
    client = VitaPulseClient(timeout_from(data))
    payload = client.request("GET", "/api/v1/health/clinical", query={
        "type_identifier": data.get("type_identifier", ""),
        "start": data.get("start", ""),
        "end": data.get("end", ""),
        "limit": integer(data, "limit", 20, 1, 100),
        "include_content": bool(data.get("include_content", False)),
    })
    return {"ok": True, "operation": "clinical_summary", **payload}


OPERATIONS = {
    "status": operation_status,
    "today_summary": operation_today_summary,
    "latest_metric": operation_latest_metric,
    "trend": operation_trend,
    "query_samples": operation_query_samples,
    "freshness": operation_freshness,
    "sync_request": operation_sync_request,
    "sync_request_status": operation_sync_request_status,
    "clinical_summary": operation_clinical_summary,
}


def emit(payload: dict[str, Any], exit_code: int) -> None:
    print(json.dumps(payload, ensure_ascii=False, separators=(",", ":")))
    raise SystemExit(exit_code)


def main() -> None:
    operation = "status"
    try:
        raw = sys.stdin.read().strip()
        data = json.loads(raw) if raw else {}
        if not isinstance(data, dict):
            raise SkillError("invalid_input", "input must be a JSON object")
        operation = str(data.pop("skill_action", "status")).strip()
        handler = OPERATIONS.get(operation)
        if handler is None:
            raise SkillError("unsupported_operation", f"unsupported operation: {operation}")
        emit(handler(data), 0)
    except SkillError as exc:
        error: dict[str, Any] = {"code": exc.code, "message": exc.message}
        if exc.http_status is not None:
            error["http_status"] = exc.http_status
        emit({"ok": False, "operation": operation, "error": error}, 1)
    except (json.JSONDecodeError, UnicodeDecodeError):
        emit({"ok": False, "operation": operation, "error": {"code": "invalid_input", "message": "stdin must contain valid JSON"}}, 1)
    except Exception:
        emit({"ok": False, "operation": operation, "error": {"code": "internal_error", "message": "unexpected VitaPulse Skill error"}}, 1)


if __name__ == "__main__":
    main()

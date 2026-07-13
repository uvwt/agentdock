#!/usr/bin/env python3
"""Poke V2 inbound API helper for the AgentDock Skill."""
from __future__ import annotations

import copy
import json
import os
import re
import socket
import sys
import urllib.error
import urllib.request
from typing import Any

SKILL_NAME = "poke-api"
SKILL_VERSION = "0.1.7"
API_ENDPOINT = "https://poke.com/api/v1/inbound/api-message"
MAX_REQUEST_BYTES = 512 * 1024
MAX_RESPONSE_BYTES = 1024 * 1024
SECRET_FIELD_NAMES = {
    "authorization",
    "api_key",
    "apikey",
    "token",
    "access_token",
    "refresh_token",
    "secret",
    "password",
}
BEARER_RE = re.compile(r"(?i)bearer\s+[A-Za-z0-9._~+/=-]+")


class PokeError(RuntimeError):
    def __init__(
        self,
        message: str,
        *,
        code: str = "poke_error",
        http_status: int | None = None,
        response: Any = None,
    ) -> None:
        super().__init__(message)
        self.code = code
        self.http_status = http_status
        self.response = response


def emit(payload: dict[str, Any]) -> int:
    print(json.dumps(payload, ensure_ascii=False, indent=2, sort_keys=True))
    return 0


def load_input() -> dict[str, Any]:
    raw = sys.stdin.read().strip()
    if not raw:
        return {}
    try:
        value = json.loads(raw)
    except json.JSONDecodeError as exc:
        raise PokeError(f"Invalid JSON input: {exc}", code="bad_json") from exc
    if not isinstance(value, dict):
        raise PokeError("Skill input must be a JSON object.", code="bad_json")
    return value


def api_key(*, allow_missing: bool = False) -> str | None:
    value = os.environ.get("POKE_API_KEY", "").strip()
    if not value:
        if allow_missing:
            return None
        raise PokeError(
            "POKE_API_KEY is not configured in the private Skill environment.",
            code="missing_api_key",
        )
    if any(ch.isspace() for ch in value):
        raise PokeError("POKE_API_KEY contains whitespace.", code="invalid_api_key")
    return value


def redact(value: Any, secret: str | None = None) -> Any:
    if isinstance(value, dict):
        result: dict[str, Any] = {}
        for key, item in value.items():
            if str(key).lower() in SECRET_FIELD_NAMES:
                result[str(key)] = "<redacted>"
            else:
                result[str(key)] = redact(item, secret)
        return result
    if isinstance(value, list):
        return [redact(item, secret) for item in value]
    if isinstance(value, str):
        text = value.replace(secret, "<redacted>") if secret else value
        return BEARER_RE.sub("Bearer <redacted>", text)
    return value


def parse_response(raw: bytes) -> Any:
    if not raw:
        return None
    text = raw.decode("utf-8", errors="replace")
    try:
        return json.loads(text)
    except json.JSONDecodeError:
        return text


def read_limited(stream: Any) -> bytes:
    raw = stream.read(MAX_RESPONSE_BYTES + 1)
    if len(raw) > MAX_RESPONSE_BYTES:
        raise PokeError("Poke API response exceeded 1 MiB.", code="response_too_large")
    return raw


def build_payload(args: dict[str, Any]) -> tuple[dict[str, Any], bytes]:
    message = args.get("message")
    context = args.get("context")

    if message is not None and not isinstance(message, str):
        raise PokeError("message must be a string.", code="bad_message")
    if isinstance(message, str) and not message.strip():
        raise PokeError("message must not be empty.", code="bad_message")
    if context is not None and not isinstance(context, dict):
        raise PokeError("context must be a JSON object.", code="bad_context")

    payload: dict[str, Any] = copy.deepcopy(context) if isinstance(context, dict) else {}
    if isinstance(message, str):
        payload["message"] = message.strip()

    inherited_message = payload.get("message")
    if not payload or not isinstance(inherited_message, str) or not inherited_message.strip():
        raise PokeError(
            "Provide message or context containing a non-empty message field.",
            code="missing_message",
        )

    try:
        encoded = json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
    except (TypeError, ValueError) as exc:
        raise PokeError("context is not JSON serializable.", code="bad_context") from exc
    if len(encoded) > MAX_REQUEST_BYTES:
        raise PokeError("Poke API request exceeds 512 KiB.", code="request_too_large")
    return payload, encoded


def timeout_seconds(args: dict[str, Any]) -> int:
    value = args.get("timeout_seconds", 20)
    if isinstance(value, bool) or not isinstance(value, int) or not 1 <= value <= 60:
        raise PokeError("timeout_seconds must be an integer from 1 to 60.", code="bad_timeout")
    return value


def post_message(encoded: bytes, key: str, timeout: int, *, endpoint: str = API_ENDPOINT) -> dict[str, Any]:
    request = urllib.request.Request(
        endpoint,
        data=encoded,
        headers={
            "Authorization": f"Bearer {key}",
            "Content-Type": "application/json",
            "Accept": "application/json",
            "User-Agent": f"AgentDock-{SKILL_NAME}/{SKILL_VERSION}",
        },
        method="POST",
    )

    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            status = response.status
            raw = read_limited(response)
    except urllib.error.HTTPError as exc:
        status = exc.code
        raw = read_limited(exc)
    except urllib.error.URLError as exc:
        reason = getattr(exc, "reason", "unreachable")
        raise PokeError(f"Cannot reach Poke API: {reason}", code="network_error") from exc
    except (TimeoutError, socket.timeout) as exc:
        raise PokeError("Poke API request timed out.", code="timeout") from exc

    parsed = parse_response(raw)
    safe_response = redact(parsed, key)
    api_success = safe_response.get("success") if isinstance(safe_response, dict) else None
    response_payload = safe_response if isinstance(safe_response, dict) else {"body": safe_response}
    ok = 200 <= status < 300 and api_success is not False

    if ok:
        code = None
    elif status == 401:
        code = "unauthorized"
    elif status == 403:
        code = "forbidden"
    elif status == 429:
        code = "rate_limited"
    else:
        code = "api_error"

    result: dict[str, Any] = {
        "ok": ok,
        "operation": "send",
        "endpoint": endpoint,
        "http_status": status,
        "response": response_payload,
    }
    if code:
        result["code"] = code
    return result


def status_operation() -> dict[str, Any]:
    configured = api_key(allow_missing=True) is not None
    return {
        "ok": True,
        "operation": "status",
        "skill": SKILL_NAME,
        "version": SKILL_VERSION,
        "api_version": "v2",
        "endpoint": API_ENDPOINT,
        "configured": configured,
        "ready": configured,
        "legacy_endpoint_supported": False,
        "operations": ["status", "send"],
    }


def send_operation(args: dict[str, Any]) -> dict[str, Any]:
    payload, encoded = build_payload(args)
    dry_run = bool(args.get("dry_run", False))
    timeout = timeout_seconds(args)

    if dry_run:
        return {
            "ok": True,
            "operation": "send",
            "endpoint": API_ENDPOINT,
            "dry_run": True,
            "request_bytes": len(encoded),
            "request": payload,
        }

    key = api_key()
    return post_message(encoded, key or "", timeout)


def error_payload(exc: PokeError, operation: str) -> dict[str, Any]:
    payload: dict[str, Any] = {
        "ok": False,
        "operation": operation,
        "endpoint": API_ENDPOINT,
        "code": exc.code,
        "error": redact(str(exc), os.environ.get("POKE_API_KEY")),
    }
    if exc.http_status is not None:
        payload["http_status"] = exc.http_status
    if exc.response is not None:
        payload["response"] = redact(exc.response, os.environ.get("POKE_API_KEY"))
    return payload


def main() -> int:
    operation = "status"
    try:
        args = load_input()
        operation = str(args.pop("skill_action", "status"))
        if operation == "status":
            return emit(status_operation())
        if operation == "send":
            return emit(send_operation(args))
        raise PokeError(f"Unsupported operation: {operation}", code="bad_operation")
    except PokeError as exc:
        return emit(error_payload(exc, operation))
    except Exception as exc:  # Defensive boundary: never print secret-bearing details.
        return emit(
            {
                "ok": False,
                "operation": operation,
                "endpoint": API_ENDPOINT,
                "code": "unexpected_error",
                "error": f"Unexpected failure: {type(exc).__name__}",
            }
        )


if __name__ == "__main__":
    raise SystemExit(main())

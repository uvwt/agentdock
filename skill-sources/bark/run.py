#!/usr/bin/env python3
"""AgentDock Skill helper helper for Bark push notifications."""
from __future__ import annotations

import datetime as _dt
import json
import os
import socket
import ssl
import sys
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path
from typing import Any


SKILL_VERSION = "0.1.8"
DEFAULT_SERVER_URL = "https://api.day.app"
KEY_ENV_KEYS = ("BARK_DEVICE_KEY", "BARK_KEY")
SERVER_ENV_KEYS = ("BARK_SERVER_URL", "BARK_BASE_URL", "BARK_URL")
LEVELS = {"active", "timeSensitive", "passive", "critical"}


class BarkError(RuntimeError):
    def __init__(self, message: str, *, code: str = "bark_error", details: Any = None) -> None:
        super().__init__(message)
        self.code = code
        self.details = details


def json_out(payload: dict[str, Any], *, exit_code: int = 0) -> int:
    print(json.dumps(payload, ensure_ascii=False, indent=2, sort_keys=True))
    return exit_code


def load_input() -> dict[str, Any]:
    raw = sys.stdin.read().strip()
    if not raw:
        return {}
    try:
        value = json.loads(raw)
    except json.JSONDecodeError as exc:
        raise BarkError(f"Invalid JSON input: {exc}", code="bad_json") from exc
    if not isinstance(value, dict):
        raise BarkError("Skill input must be a JSON object.", code="bad_json")
    return value


def load_env_file(path: Path) -> None:
    if not path.exists() or not path.is_file():
        return
    for raw_line in path.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        key = key.strip()
        value = value.strip().strip('"').strip("'")
        if key and key not in os.environ:
            os.environ[key] = value


def load_env_candidates(explicit: str | None = None) -> None:
    candidates: list[Path] = []
    if explicit:
        candidates.append(Path(explicit).expanduser())
    if os.environ.get("BARK_ENV_FILE"):
        candidates.append(Path(os.environ["BARK_ENV_FILE"]).expanduser())
    if os.environ.get("WORKSPACE"):
        candidates.append(Path(os.environ["WORKSPACE"]).expanduser() / ".env")
    candidates.append(Path.cwd() / ".env")
    candidates.append(Path(__file__).resolve().parent / ".env")

    seen: set[Path] = set()
    for candidate in candidates:
        try:
            resolved = candidate.resolve()
        except OSError:
            resolved = candidate
        if resolved in seen:
            continue
        seen.add(resolved)
        load_env_file(resolved)


def first_env(keys: tuple[str, ...]) -> str | None:
    for key in keys:
        value = os.environ.get(key)
        if value:
            return value
    return None


def normalize_server_url(value: str) -> str:
    parsed = urllib.parse.urlsplit(value.strip())
    if parsed.scheme not in {"http", "https"}:
        raise BarkError("server_url must use http or https", code="bad_server_url")
    if not parsed.hostname:
        raise BarkError("server_url must include a host", code="bad_server_url")
    if parsed.username or parsed.password:
        raise BarkError("server_url must not contain credentials", code="bad_server_url")
    if parsed.query or parsed.fragment:
        raise BarkError("server_url must not contain query or fragment", code="bad_server_url")
    path = parsed.path.rstrip("/")
    return urllib.parse.urlunsplit((parsed.scheme, parsed.netloc, path, "", ""))


def resolve_server_url(args: dict[str, Any]) -> str:
    explicit = args.get("server_url")
    if explicit is not None:
        if not isinstance(explicit, str):
            raise BarkError("server_url must be a string", code="bad_server_url")
        return normalize_server_url(explicit)
    env_value = first_env(SERVER_ENV_KEYS)
    if env_value:
        return normalize_server_url(env_value)
    return DEFAULT_SERVER_URL


def resolve_device_key(*, allow_missing: bool = False) -> str | None:
    key = first_env(KEY_ENV_KEYS)
    key = key.strip() if key else ""
    if not key and not allow_missing:
        raise BarkError(
            "Missing BARK_DEVICE_KEY. Put it in the environment or a local .env file.",
            code="missing_device_key",
        )
    if key and any(ch.isspace() for ch in key):
        raise BarkError("BARK_DEVICE_KEY must not contain whitespace.", code="bad_device_key")
    return key or None


def bool_int(value: Any) -> int:
    return 1 if bool(value) else 0


def compact_payload(args: dict[str, Any], *, require_body: bool = True) -> dict[str, Any]:
    body = args.get("body") or args.get("message") or args.get("text")
    if require_body and not body:
        raise BarkError("Missing notification body.", code="missing_body")

    payload: dict[str, Any] = {}
    for key in ("title", "subtitle"):
        if args.get(key):
            payload[key] = str(args[key])
    if body:
        payload["body"] = str(body)

    level = args.get("level")
    if level:
        if level not in LEVELS:
            raise BarkError(f"Unsupported Bark level: {level}", code="bad_level")
        payload["level"] = level

    simple_keys = ("copy", "sound", "icon", "group", "url")
    for key in simple_keys:
        if args.get(key) is not None:
            payload[key] = str(args[key])

    if args.get("badge") is not None:
        try:
            badge = int(args["badge"])
        except (TypeError, ValueError) as exc:
            raise BarkError("badge must be an integer", code="bad_badge") from exc
        if badge < 0:
            raise BarkError("badge must be >= 0", code="bad_badge")
        payload["badge"] = badge

    bool_fields = {
        "auto_copy": "autoCopy",
        "is_archive": "isArchive",
        "call": "call",
    }
    for source, target in bool_fields.items():
        if args.get(source) is not None:
            payload[target] = bool_int(args[source])

    return payload


def redacted_key(key: str | None) -> str | None:
    if not key:
        return None
    if len(key) <= 8:
        return "<redacted>"
    return key[:4] + "..." + key[-4:]


def parse_body(raw: bytes) -> Any:
    if not raw:
        return None
    text = raw.decode("utf-8", errors="replace")
    if text[:1] in {"{", "["}:
        try:
            return json.loads(text)
        except json.JSONDecodeError:
            pass
    return text


def post_push(server_url: str, device_key: str, payload: dict[str, Any], *, timeout: int = 20) -> dict[str, Any]:
    url = server_url.rstrip("/") + "/push"
    body = dict(payload)
    body["device_key"] = device_key
    request = urllib.request.Request(
        url,
        data=json.dumps(body, ensure_ascii=False).encode("utf-8"),
        headers={
            "Content-Type": "application/json",
            "Accept": "application/json",
            "User-Agent": f"AgentDock-Bark-Skill/{SKILL_VERSION}",
        },
        method="POST",
    )
    insecure_tls = os.environ.get("BARK_INSECURE_TLS") == "1"
    context = ssl._create_unverified_context() if insecure_tls else None
    try:
        with urllib.request.urlopen(request, timeout=timeout, context=context) as response:
            raw = response.read()
            status = response.status
    except urllib.error.HTTPError as exc:
        raw = exc.read()
        status = exc.code
    except urllib.error.URLError as exc:
        raise BarkError(f"Cannot reach Bark API: {exc.reason}", code="network_error") from exc
    except TimeoutError as exc:
        raise BarkError("Bark API request timed out.", code="timeout") from exc

    parsed = parse_body(raw)
    api_code = parsed.get("code") if isinstance(parsed, dict) else None
    ok = 200 <= status < 300 and (api_code in (None, 200))
    return {"ok": ok, "http_status": status, "response": parsed}


def build_get_url(server_url: str, device_key: str, args: dict[str, Any]) -> str:
    payload = compact_payload(args)
    title = str(payload.pop("title", "")).strip()
    body = str(payload.pop("body", "")).strip()
    if title:
        path = "/" + "/".join(urllib.parse.quote(part, safe="") for part in (device_key, title, body))
    else:
        path = "/" + "/".join(urllib.parse.quote(part, safe="") for part in (device_key, body))
    query = urllib.parse.urlencode(payload, doseq=True)
    return server_url.rstrip("/") + path + (("?" + query) if query else "")


def redacted_get_url(full_url: str, device_key: str) -> str:
    return full_url.replace("/" + urllib.parse.quote(device_key, safe=""), "/<redacted>")


def build_event_args(args: dict[str, Any]) -> dict[str, Any]:
    device = args.get("device") or os.environ.get("DOCK_DEVICE") or socket.gethostname()
    service = args.get("service") or args.get("name") or "Dock"
    severity = (args.get("severity") or args.get("status") or "info").upper()
    title = args.get("title") or f"{service} {severity}"
    message = args.get("message") or args.get("body") or args.get("text") or ""
    now = _dt.datetime.now().astimezone().strftime("%Y-%m-%d %H:%M:%S %Z")

    lines = [
        f"[{severity}] {title}",
        f"Device: {device}",
        f"Service: {service}",
        f"Time: {now}",
    ]
    if message:
        lines.extend(["", str(message)])

    details = args.get("details")
    if isinstance(details, dict):
        lines.append("")
        for key, value in details.items():
            lines.append(f"{key}: {value}")
    elif details:
        lines.extend(["", str(details)])

    result = dict(args)
    result["title"] = title
    result["body"] = "\n".join(lines)
    return result


def action_send(args: dict[str, Any]) -> dict[str, Any]:
    load_env_candidates(args.get("env_file"))
    dry_run = bool(args.get("dry_run"))
    server_url = resolve_server_url(args)
    device_key = resolve_device_key(allow_missing=dry_run)
    payload = compact_payload(args)

    if dry_run:
        return {
            "ok": True,
            "action": "send",
            "dry_run": True,
            "server_url": server_url,
            "device_key": redacted_key(device_key),
            "request": payload,
        }

    response = post_push(server_url, device_key or "", payload)
    return {
        "ok": bool(response["ok"]),
        "action": "send",
        "server_url": server_url,
        "device_key": redacted_key(device_key),
        "http_status": response["http_status"],
        "response": response["response"],
    }


def action_event(args: dict[str, Any]) -> dict[str, Any]:
    return action_send(build_event_args(args))


def action_health(args: dict[str, Any]) -> dict[str, Any]:
    load_env_candidates(args.get("env_file"))
    server_url = resolve_server_url(args)
    device_key = resolve_device_key(allow_missing=True)
    live = bool(args.get("live"))
    result: dict[str, Any] = {
        "ok": True,
        "action": "health",
        "server_url": server_url,
        "device_key_configured": bool(device_key),
        "device_key": redacted_key(device_key),
        "live": live,
        "ready": bool(device_key),
    }
    if not device_key:
        result["message"] = "BARK_DEVICE_KEY is not configured; set it before live Bark sends."
        return result
    if live:
        response = post_push(
            server_url,
            device_key,
            {
                "title": "AgentDock Bark health",
                "body": "AgentDock Bark skill connectivity check.",
                "group": "AgentDock",
            },
        )
        result["ok"] = bool(response["ok"])
        result["http_status"] = response["http_status"]
        result["response"] = response["response"]
    return result


def action_url(args: dict[str, Any]) -> dict[str, Any]:
    load_env_candidates(args.get("env_file"))
    server_url = resolve_server_url(args)
    device_key = resolve_device_key()
    full_url = build_get_url(server_url, device_key or "", args)
    return {
        "ok": True,
        "action": "url",
        "server_url": server_url,
        "device_key": redacted_key(device_key),
        "url": redacted_get_url(full_url, device_key or ""),
    }


def main() -> int:
    args = load_input()
    operation = str(args.pop("skill_action", "health"))
    try:
        if operation == "send":
            result = action_send(args)
        elif operation == "event":
            result = action_event(args)
        elif operation == "health":
            result = action_health(args)
        elif operation == "url":
            result = action_url(args)
        else:
            raise BarkError(f"Unsupported operation: {operation}", code="bad_operation")
        return json_out(result)
    except BarkError as exc:
        return json_out(
            {"ok": False, "error": str(exc), "code": exc.code, "details": exc.details},
            exit_code=0,
        )


if __name__ == "__main__":
    raise SystemExit(main())

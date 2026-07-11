#!/usr/bin/env python3
"""
Small Telegram Bot API helper for Dock/AgentDock.

It intentionally uses only Python's standard library and Telegram's official
Bot API endpoint. Bot tokens are read from environment variables or .env files;
there is no --token flag, so secrets do not land in shell history.
"""

from __future__ import annotations

import argparse
import datetime as _dt
import json
import os
import socket
import sys
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path
from typing import Any


API_BASE = "https://api.telegram.org"
TOKEN_ENV_KEYS = ("TELEGRAM_BOT_TOKEN", "TG_BOT_TOKEN")
CHAT_ENV_KEYS = ("TELEGRAM_CHAT_ID", "TG_CHAT_ID")
ALLOWED_PARSE_MODES = {"MarkdownV2", "Markdown", "HTML"}


class TelegramError(RuntimeError):
    def __init__(self, message: str, *, code: str = "telegram_error", details: Any = None) -> None:
        super().__init__(message)
        self.code = code
        self.details = details


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
    if os.environ.get("TELEGRAM_ENV_FILE"):
        candidates.append(Path(os.environ["TELEGRAM_ENV_FILE"]).expanduser())
    if os.environ.get("WORKSPACE"):
        candidates.append(Path(os.environ["WORKSPACE"]).expanduser() / ".env")
    candidates.append(Path.cwd() / ".env")
    candidates.append(Path(__file__).resolve().parent / ".env")

    seen: set[Path] = set()
    for candidate in candidates:
        resolved = candidate.resolve()
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


def require_token(*, allow_missing: bool = False) -> str | None:
    token = first_env(TOKEN_ENV_KEYS)
    if token:
        token = token.strip()
    if not token and not allow_missing:
        raise TelegramError(
            "Missing TELEGRAM_BOT_TOKEN. Put it in the environment or a local .env file.",
            code="missing_token",
        )
    if token and (":" not in token or len(token) < 20):
        raise TelegramError("TELEGRAM_BOT_TOKEN does not look like a Telegram bot token.", code="bad_token")
    return token


def resolve_chat_id(value: str | None) -> str:
    chat_id = value or first_env(CHAT_ENV_KEYS)
    if not chat_id:
        raise TelegramError(
            "Missing TELEGRAM_CHAT_ID. Set it in the environment/.env or pass chat_id in operation input.",
            code="missing_chat_id",
        )
    return str(chat_id).strip()


def json_out(payload: dict[str, Any], *, exit_code: int = 0) -> int:
    print(json.dumps(payload, ensure_ascii=False, indent=2, sort_keys=True))
    return exit_code


def call_api(token: str, method: str, payload: dict[str, Any] | None = None, *, timeout: int = 20) -> dict[str, Any]:
    url = f"{API_BASE}/bot{token}/{method}"
    body = json.dumps(payload or {}).encode("utf-8")
    request = urllib.request.Request(
        url,
        data=body,
        headers={"Content-Type": "application/json"},
        method="POST",
    )

    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            data = json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        try:
            error_body = json.loads(exc.read().decode("utf-8"))
        except Exception:
            error_body = {"status": exc.code, "reason": exc.reason}
        raise TelegramError("Telegram API returned an error.", code="http_error", details=error_body) from exc
    except urllib.error.URLError as exc:
        raise TelegramError(f"Cannot reach Telegram API: {exc.reason}", code="network_error") from exc
    except socket.timeout as exc:
        raise TelegramError("Telegram API request timed out.", code="timeout") from exc

    if not data.get("ok"):
        raise TelegramError("Telegram API returned ok=false.", code="api_not_ok", details=data)
    return data


def compact_updates(data: dict[str, Any], *, limit: int = 20) -> list[dict[str, Any]]:
    result = data.get("result") or []
    compact: list[dict[str, Any]] = []
    for update in result[:limit]:
        message = update.get("message") or update.get("channel_post") or {}
        chat = message.get("chat") or {}
        sender = message.get("from") or {}
        compact.append(
            {
                "update_id": update.get("update_id"),
                "message_id": message.get("message_id"),
                "date": message.get("date"),
                "chat_id": chat.get("id"),
                "chat_title": chat.get("title") or chat.get("username") or chat.get("first_name"),
                "chat_type": chat.get("type"),
                "from": sender.get("username") or sender.get("first_name"),
                "text": message.get("text"),
            }
        )
    return compact


def build_event_text(args: dict[str, Any]) -> str:
    device = args.get("device") or os.environ.get("DOCK_DEVICE") or socket.gethostname()
    service = args.get("service") or args.get("name") or "Dock"
    severity = (args.get("severity") or args.get("status") or "info").upper()
    title = args.get("title") or f"{service} {severity}"
    message = args.get("message") or args.get("text") or ""
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

    url = args.get("url")
    if url:
        lines.extend(["", str(url)])
    return "\n".join(lines)


def action_send(args: argparse.Namespace | dict[str, Any]) -> dict[str, Any]:
    data = vars(args) if isinstance(args, argparse.Namespace) else args
    load_env_candidates(data.get("env_file"))
    dry_run = bool(data.get("dry_run"))
    token = require_token(allow_missing=dry_run)
    chat_id = resolve_chat_id(data.get("chat_id"))
    text = data.get("text")
    if not text and data.get("stdin"):
        text = sys.stdin.read()
    if not text:
        raise TelegramError("Missing message text.", code="missing_text")

    payload: dict[str, Any] = {
        "chat_id": chat_id,
        "text": str(text),
        "disable_web_page_preview": bool(data.get("disable_web_page_preview", True)),
    }
    parse_mode = data.get("parse_mode")
    if parse_mode:
        if parse_mode not in ALLOWED_PARSE_MODES:
            raise TelegramError(f"Unsupported parse mode: {parse_mode}", code="bad_parse_mode")
        payload["parse_mode"] = parse_mode
    if data.get("silent"):
        payload["disable_notification"] = True

    if dry_run:
        return {"ok": True, "action": "send", "dry_run": True, "request": {**payload, "text_length": len(str(text))}}

    response = call_api(token or "", "sendMessage", payload)
    message = response.get("result", {})
    return {
        "ok": True,
        "action": "send",
        "chat_id": chat_id,
        "message_id": message.get("message_id"),
        "date": message.get("date"),
    }


def action_event(args: argparse.Namespace | dict[str, Any]) -> dict[str, Any]:
    data = vars(args) if isinstance(args, argparse.Namespace) else dict(args)
    data["text"] = build_event_text(data)
    data.setdefault("disable_web_page_preview", True)
    return action_send(data)


def action_updates(args: argparse.Namespace | dict[str, Any]) -> dict[str, Any]:
    data = vars(args) if isinstance(args, argparse.Namespace) else args
    load_env_candidates(data.get("env_file"))
    token = require_token()
    payload: dict[str, Any] = {
        "limit": int(data.get("limit") or 20),
        "timeout": int(data.get("poll_timeout") or 0),
        "allowed_updates": ["message", "channel_post"],
    }
    if data.get("offset") is not None:
        payload["offset"] = int(data["offset"])
    response = call_api(token or "", "getUpdates", payload, timeout=max(20, payload["timeout"] + 5))
    return {"ok": True, "action": "updates", "updates": compact_updates(response, limit=payload["limit"])}


def action_health(args: argparse.Namespace | dict[str, Any]) -> dict[str, Any]:
    data = vars(args) if isinstance(args, argparse.Namespace) else args
    load_env_candidates(data.get("env_file"))
    token = require_token()
    response = call_api(token or "", "getMe", {})
    bot = response.get("result", {})
    return {
        "ok": True,
        "action": "health",
        "bot": {
            "id": bot.get("id"),
            "username": bot.get("username"),
            "first_name": bot.get("first_name"),
            "can_join_groups": bot.get("can_join_groups"),
            "can_read_all_group_messages": bot.get("can_read_all_group_messages"),
            "supports_inline_queries": bot.get("supports_inline_queries"),
        },
    }


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Official Telegram Bot API helper for Dock.")
    parser.add_argument("--env-file", help="Optional .env file. Tokens are never accepted as CLI args.")
    sub = parser.add_subparsers(dest="command", required=True)

    send = sub.add_parser("send", help="Send a Telegram message.")
    send.add_argument("--chat-id")
    send.add_argument("--text")
    send.add_argument("--stdin", action="store_true", help="Read message text from stdin.")
    send.add_argument("--parse-mode", choices=sorted(ALLOWED_PARSE_MODES))
    send.add_argument("--silent", action="store_true")
    send.add_argument("--disable-web-page-preview", action=argparse.BooleanOptionalAction, default=True)
    send.add_argument("--dry-run", action="store_true")

    event = sub.add_parser("event", help="Send a formatted Dock event notification.")
    event.add_argument("--chat-id")
    event.add_argument("--title")
    event.add_argument("--service")
    event.add_argument("--severity", default="info")
    event.add_argument("--message")
    event.add_argument("--device")
    event.add_argument("--url")
    event.add_argument("--silent", action="store_true")
    event.add_argument("--dry-run", action="store_true")

    updates = sub.add_parser("updates", help="Read compact getUpdates output.")
    updates.add_argument("--limit", type=int, default=20)
    updates.add_argument("--offset", type=int)
    updates.add_argument("--poll-timeout", type=int, default=0)

    sub.add_parser("health", help="Call getMe and return bot identity.")

    return parser


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)

    try:
        if args.command == "send":
            result = action_send(args)
        elif args.command == "event":
            result = action_event(args)
        elif args.command == "updates":
            result = action_updates(args)
        elif args.command == "health":
            result = action_health(args)
        else:
            raise TelegramError(f"Unsupported command: {args.command}", code="bad_command")
        return json_out(result)
    except TelegramError as exc:
        return json_out({"ok": False, "error": str(exc), "code": exc.code, "details": exc.details}, exit_code=2)


if __name__ == "__main__":
    raise SystemExit(main())

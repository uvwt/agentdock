#!/usr/bin/env python3
"""
Telegram helper script. The action and its arguments are read from one JSON object on stdin.
"""

from __future__ import annotations

import json
import os
import sys

import telegram_bot


def read_input() -> dict:
    raw = sys.stdin.read().strip()
    if not raw:
        return {}
    try:
        data = json.loads(raw)
    except json.JSONDecodeError as exc:
        raise telegram_bot.TelegramError(f"Invalid JSON input: {exc}", code="bad_json") from exc
    if not isinstance(data, dict):
        raise telegram_bot.TelegramError("Skill input must be a JSON object.", code="bad_json")
    return data


def main() -> int:
    args = read_input()
    operation = str(args.pop("skill_action", "health"))

    try:
        if operation == "send":
            result = telegram_bot.action_send(args)
        elif operation == "event":
            result = telegram_bot.action_event(args)
        elif operation == "updates":
            result = telegram_bot.action_updates(args)
        elif operation == "health":
            result = telegram_bot.action_health(args)
        else:
            raise telegram_bot.TelegramError(f"Unsupported operation: {operation}", code="bad_operation")
        return telegram_bot.json_out(result)
    except telegram_bot.TelegramError as exc:
        return telegram_bot.json_out(
            {"ok": False, "error": str(exc), "code": exc.code, "details": exc.details},
            exit_code=2,
        )


if __name__ == "__main__":
    raise SystemExit(main())

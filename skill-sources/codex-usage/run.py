#!/usr/bin/env python3
"""Read-only AgentDock Skill for querying ChatGPT Codex usage."""
from __future__ import annotations

import base64
import json
import os
import pwd
import sys
import time
import urllib.error
import urllib.request
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

WHAM_BASE_URL = "https://chatgpt.com/backend-api/wham"
USAGE_URL = WHAM_BASE_URL + "/usage"
RESET_CREDITS_URL = WHAM_BASE_URL + "/rate-limit-reset-credits"


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


def codex_dir() -> Path:
    configured = os.environ.get("CODEX_HOME", "").strip()
    if configured:
        path = Path(configured).expanduser()
        if not path.is_absolute():
            raise SkillError("CODEX_HOME must be an absolute path.", "bad_codex_home")
        return path.parent if path.name == "auth.json" else path
    try:
        return Path(pwd.getpwuid(os.getuid()).pw_dir) / ".codex"
    except (KeyError, OSError) as exc:
        raise SkillError("Unable to resolve the current system user's home directory.", "home_unavailable") from exc


def decode_claims(value: Any) -> dict[str, Any]:
    if not isinstance(value, str) or value.count(".") < 2:
        return {}
    try:
        part = value.split(".", 2)[1]
        part += "=" * (-len(part) % 4)
        result = json.loads(base64.urlsafe_b64decode(part.encode("ascii")))
        return result if isinstance(result, dict) else {}
    except Exception:
        return {}


def account_id_from(data: dict[str, Any]) -> str | None:
    direct = data.get("account_id")
    if isinstance(direct, str) and direct.strip():
        return direct.strip()
    for field in ("access_token", "id_token"):
        claims = decode_claims(data.get(field))
        candidates: list[Any] = [claims.get("chatgpt_account_id"), claims.get("account_id")]
        for namespace in ("https://api.openai.com/auth", "https://api.openai.com/profile"):
            nested = claims.get(namespace)
            if isinstance(nested, dict):
                candidates.extend((nested.get("chatgpt_account_id"), nested.get("account_id")))
        for candidate in candidates:
            if isinstance(candidate, str) and candidate.strip():
                return candidate.strip()
    return None


def read_login() -> tuple[Path, dict[str, Any], dict[str, Any]]:
    path = codex_dir() / "auth.json"
    try:
        auth = json.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError as exc:
        raise SkillError("Codex CLI login was not found. Run `codex login` first.", "auth_missing") from exc
    except PermissionError as exc:
        raise SkillError("Codex CLI login file is not readable.", "auth_unreadable") from exc
    except (OSError, json.JSONDecodeError) as exc:
        raise SkillError("Codex CLI login file is invalid.", "auth_invalid") from exc
    if not isinstance(auth, dict) or not isinstance(auth.get("tokens"), dict):
        raise SkillError("Codex CLI login file has no ChatGPT OAuth session.", "oauth_missing")
    return path, auth, auth["tokens"]


def expiry_of(data: dict[str, Any]) -> float | None:
    exp = decode_claims(data.get("access_token")).get("exp")
    return float(exp) if isinstance(exp, (int, float)) else None


def iso_time(value: float | int | None) -> str | None:
    if value is None:
        return None
    return datetime.fromtimestamp(float(value), timezone.utc).isoformat().replace("+00:00", "Z")


def status() -> dict[str, Any]:
    path = codex_dir() / "auth.json"
    if not path.is_file():
        return {
            "ok": True,
            "operation": "status",
            "auth_path": str(path),
            "auth_configured": False,
            "token_valid": False,
            "account_id_available": False,
        }
    try:
        _, auth, data = read_login()
    except SkillError as exc:
        return {
            "ok": False,
            "operation": "status",
            "auth_path": str(path),
            "auth_configured": True,
            "code": exc.code,
            "error": str(exc),
        }
    access = data.get("access_token")
    expiry = expiry_of(data)
    return {
        "ok": True,
        "operation": "status",
        "auth_path": str(path),
        "auth_configured": True,
        "auth_mode": auth.get("auth_mode") or "chatgpt",
        "access_token_present": isinstance(access, str) and bool(access),
        "access_token_expires_at": iso_time(expiry),
        "token_valid": bool(access and (expiry is None or expiry > time.time())),
        "account_id_available": bool(account_id_from(data)),
        "last_refresh": auth.get("last_refresh"),
    }


def parse_error(exc: urllib.error.HTTPError) -> tuple[str | None, str | None]:
    try:
        payload = json.loads(exc.read(65536).decode("utf-8", errors="replace"))
    except Exception:
        return None, None
    if not isinstance(payload, dict):
        return None, None
    error = payload.get("error")
    if isinstance(error, dict):
        code = error.get("code") or error.get("type")
        message = error.get("message")
        return (str(code) if code else None, str(message) if message else None)
    if isinstance(error, str):
        message = payload.get("error_description") or payload.get("message")
        return error, (str(message) if message else None)
    return None, None


def fetch_endpoint(url: str, timeout: int, label: str) -> dict[str, Any]:
    _, _, data = read_login()
    access = data.get("access_token")
    if not isinstance(access, str) or not access:
        raise SkillError("Codex access token is missing. Run `codex login`.", "access_token_missing")
    expiry = expiry_of(data)
    if expiry is not None and expiry <= time.time():
        raise SkillError("Codex access token has expired. Run Codex once or sign in again, then retry.", "access_token_expired")
    headers = {
        "Accept": "application/json",
        "Authorization": "Bearer " + access,
        "User-Agent": "codex-cli",
    }
    account_id = account_id_from(data)
    if account_id:
        headers["ChatGPT-Account-Id"] = account_id
    request = urllib.request.Request(url, headers=headers, method="GET")
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            raw = response.read()
    except urllib.error.HTTPError as exc:
        code, message = parse_error(exc)
        raise SkillError(
            message or f"Codex {label} request failed with HTTP {exc.code}.",
            f"{label}_http_error",
            {"http_status": exc.code, "provider_code": code},
        ) from exc
    except urllib.error.URLError as exc:
        raise SkillError(f"Unable to reach the Codex {label} endpoint.", "network_error") from exc
    except TimeoutError as exc:
        raise SkillError(f"Codex {label} request timed out.", "timeout") from exc
    try:
        payload = json.loads(raw.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise SkillError(f"Codex {label} endpoint returned invalid JSON.", "invalid_response") from exc
    if not isinstance(payload, dict):
        raise SkillError(f"Codex {label} endpoint returned an unexpected response.", "invalid_response")
    return payload


def fetch_usage(timeout: int) -> dict[str, Any]:
    return fetch_endpoint(USAGE_URL, timeout, "usage")


def fetch_reset_credits(timeout: int) -> dict[str, Any]:
    return fetch_endpoint(RESET_CREDITS_URL, timeout, "reset_credits")


def reset_info(value: Any) -> tuple[str | None, int | None]:
    if isinstance(value, (int, float)):
        timestamp = float(value)
        return iso_time(timestamp), max(0, int(timestamp - time.time()))
    if isinstance(value, str) and value.strip():
        text = value.strip()
        try:
            normalized = text[:-1] + "+00:00" if text.endswith("Z") else text
            parsed = datetime.fromisoformat(normalized)
            if parsed.tzinfo is None:
                parsed = parsed.replace(tzinfo=timezone.utc)
            return parsed.astimezone(timezone.utc).isoformat().replace("+00:00", "Z"), max(0, int(parsed.timestamp() - time.time()))
        except ValueError:
            return text, None
    return None, None



SENSITIVE_RESPONSE_KEYS = {"account_id", "user_id", "email"}


def redacted(value: Any) -> Any:
    """Preserve API response shape while hiding account identifiers."""
    if isinstance(value, dict):
        out: dict[str, Any] = {}
        for key, item in value.items():
            out[key] = "<redacted>" if key in SENSITIVE_RESPONSE_KEYS and item is not None else redacted(item)
        return out
    if isinstance(value, list):
        return [redacted(item) for item in value]
    return value


def add_local_time_fields(value: Any) -> Any:
    if isinstance(value, list):
        return [add_local_time_fields(item) for item in value]
    if not isinstance(value, dict):
        return redacted(value)
    out: dict[str, Any] = {}
    for key, raw in value.items():
        if key in SENSITIVE_RESPONSE_KEYS:
            out[key] = "<redacted>" if raw is not None else None
        else:
            out[key] = add_local_time_fields(raw)
        if key in ("granted_at", "expires_at", "redeem_started_at", "redeemed_at"):
            utc, seconds = reset_info(raw)
            if utc is not None:
                out[key + "_utc"] = utc
                try:
                    text = utc[:-1] + "+00:00" if utc.endswith("Z") else utc
                    out[key + "_local"] = datetime.fromisoformat(text).astimezone().isoformat()
                except ValueError:
                    out[key + "_local"] = None
                out[key + "_in_seconds"] = seconds
    return out


def number_or_none(value: Any) -> float | int | None:
    if isinstance(value, bool):
        return None
    if isinstance(value, (int, float)):
        return value
    return None


def window(label: str, value: Any) -> dict[str, Any] | None:
    if value is None:
        return None
    if not isinstance(value, dict):
        return {"label": label, "raw": redacted(value)}
    used_raw = number_or_none(value.get("used_percent"))
    used = max(0.0, min(100.0, float(used_raw))) if used_raw is not None else None
    reset_at, reset_in_seconds = reset_info(value.get("reset_at"))
    item: dict[str, Any] = {
        "label": label,
        "used_percent": round(used, 2) if used is not None else None,
        "remaining_percent": round(100.0 - used, 2) if used is not None else None,
        "limit_window_seconds": number_or_none(value.get("limit_window_seconds")),
        "reset_after_seconds": number_or_none(value.get("reset_after_seconds")),
        "reset_at": reset_at,
        "reset_at_raw": value.get("reset_at"),
        "reset_in_seconds": reset_in_seconds,
        "raw": redacted(value),
    }
    for key, val in value.items():
        item.setdefault(key, redacted(val))
    return item


def query(args: dict[str, Any]) -> dict[str, Any]:
    timeout = args.get("timeout_seconds", 15)
    if isinstance(timeout, bool) or not isinstance(timeout, int) or not 5 <= timeout <= 30:
        raise SkillError("timeout_seconds must be an integer from 5 to 30.", "bad_timeout")
    payload = fetch_usage(timeout)
    limits = payload.get("rate_limit") if isinstance(payload.get("rate_limit"), dict) else {}
    session = window("session", limits.get("primary_window"))
    weekly = window("weekly", limits.get("secondary_window"))
    credits = payload.get("credits") if isinstance(payload.get("credits"), dict) else None
    credit_summary = redacted(credits) if credits is not None else None
    reset_credits = payload.get("rate_limit_reset_credits") if isinstance(payload.get("rate_limit_reset_credits"), dict) else None
    reset_credit_summary = redacted(reset_credits) if reset_credits is not None else None
    available_reset_count = None
    if reset_credits is not None:
        available = reset_credits.get("available_count")
        if isinstance(available, (int, float)) and not isinstance(available, bool):
            available_reset_count = int(available)
    reset_credits_api = None
    reset_credits_api_error = None
    try:
        reset_credits_api = add_local_time_fields(fetch_reset_credits(timeout))
        if isinstance(reset_credits_api, dict):
            available = reset_credits_api.get("available_count")
            if isinstance(available, (int, float)) and not isinstance(available, bool):
                available_reset_count = int(available)
    except SkillError as exc:
        reset_credits_api_error = {"code": exc.code, "error": str(exc), "details": exc.details}
    rate_limit_summary = redacted(limits) if isinstance(limits, dict) else None
    if isinstance(rate_limit_summary, dict):
        rate_limit_summary["primary_window"] = session
        rate_limit_summary["secondary_window"] = weekly
    redacted_payload = redacted(payload)
    return {
        "ok": bool(payload),
        "operation": "query",
        "source": "chatgpt_codex_usage_api",
        "fetched_at": iso_time(time.time()),
        "account": {
            "account_id_present": bool(payload.get("account_id")),
            "user_id_present": bool(payload.get("user_id")),
            "email_present": bool(payload.get("email")),
        },
        "usage_api": redacted_payload,
        "plan": payload.get("plan_type") if isinstance(payload.get("plan_type"), str) else None,
        "plan_type": payload.get("plan_type") if isinstance(payload.get("plan_type"), str) else None,
        "rate_limit": rate_limit_summary,
        "session": session,
        "weekly": weekly,
        "windows": [item for item in (session, weekly) if item],
        "rate_limit_reset_credits": reset_credit_summary,
        "reset_credits": reset_credit_summary,
        "reset_credits_api": reset_credits_api,
        "reset_credits_api_error": reset_credits_api_error,
        "available_reset_count": available_reset_count,
        "credits": credit_summary,
        "spend_control": redacted(payload.get("spend_control")),
        "rate_limit_reached_type": payload.get("rate_limit_reached_type"),
        "additional_rate_limits": redacted(payload.get("additional_rate_limits")),
        "code_review_rate_limit": redacted(payload.get("code_review_rate_limit")),
        "promo": redacted(payload.get("promo")),
        "referral_beacon": redacted(payload.get("referral_beacon")),
        "raw_response_redacted": redacted_payload,
        "message": None if payload else "No Codex usage data was returned.",
    }


def main() -> int:
    try:
        args = load_input()
        operation = str(args.pop("skill_action", "status"))
        if operation == "status":
            return emit(status())
        if operation == "query":
            return emit(query(args))
        raise SkillError(f"Unsupported operation: {operation}", "bad_operation")
    except SkillError as exc:
        return emit({"ok": False, "code": exc.code, "error": str(exc), "details": exc.details})
    except Exception as exc:
        return emit({"ok": False, "code": "unexpected_error", "error": f"Unexpected failure: {type(exc).__name__}"})


if __name__ == "__main__":
    raise SystemExit(main())

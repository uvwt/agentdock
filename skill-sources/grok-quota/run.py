#!/usr/bin/env python3
"""Read-only helper for probing Grok Build/free quota state."""
from __future__ import annotations

import hashlib
import json
import os
import pwd
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

CLI_RESPONSES_URL = "https://cli-chat-proxy.grok.com/v1/responses"
OIDC_DISCOVERY_URL = "https://auth.x.ai/.well-known/openid-configuration"
XAI_CLIENT_ID = "b1a00492-073a-47ea-816f-4c329264a828"
CLIENT_VERSION = "0.2.93"
DEFAULT_MODEL = "grok-4.5"
MAX_RESPONSE_BYTES = 2 * 1024 * 1024
MAX_AUTH_BYTES = 1024 * 1024
TOKEN_USAGE_PATTERN = re.compile(
    r"tokens\s*\(actual/limit\)\s*:\s*([\d_,]+)\s*/\s*([\d_,]+)",
    re.IGNORECASE,
)
MODEL_PATTERN = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$")
EMAIL_PATTERN = re.compile(r"[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}", re.IGNORECASE)
JWT_PATTERN = re.compile(r"\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{5,}\b")
BEARER_PATTERN = re.compile(r"(?i)bearer\s+[A-Za-z0-9._~+/=-]+")
SECRET_FIELD_PATTERN = re.compile(
    r'(?i)(access_token|refresh_token|id_token|authorization|cookie)(["\'\s:=]+)([^,}\s"\']+|"[^"]*"|\'[^\']*\')'
)


class SkillError(RuntimeError):
    def __init__(self, message: str, code: str, details: Any = None) -> None:
        super().__init__(message)
        self.code = code
        self.details = details


class NoRedirectHandler(urllib.request.HTTPRedirectHandler):
    # OAuth 和 Bearer 请求不得自动跳转，避免凭据离开已校验的上游域名。
    def redirect_request(self, req: Any, fp: Any, code: int, msg: str, headers: Any, newurl: str) -> None:
        return None


HTTP_OPENER = urllib.request.build_opener(NoRedirectHandler())


@dataclass(frozen=True)
class Credential:
    path: Path
    account_ref: str
    source_kind: str
    access_token: str
    refresh_token: str
    token_endpoint: str
    client_id: str
    expired_at: str
    email_present: bool
    subject_present: bool


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


def system_home() -> Path:
    try:
        return Path(pwd.getpwuid(os.getuid()).pw_dir)
    except (KeyError, OSError):
        return Path.home()


def require_absolute_path(raw: Any, field: str) -> Path | None:
    if raw is None or raw == "":
        return None
    if not isinstance(raw, str):
        raise SkillError(f"{field} must be a string.", f"bad_{field}")
    path = Path(raw).expanduser()
    if not path.is_absolute():
        raise SkillError(f"{field} must be an absolute path.", f"bad_{field}")
    return path


def credential_sources(args: dict[str, Any]) -> tuple[list[Path], list[Path]]:
    explicit_file = require_absolute_path(args.get("auth_file"), "auth_file")
    explicit_dir = require_absolute_path(args.get("auth_dir"), "auth_dir")

    files: list[Path] = []
    directories: list[Path] = []
    if explicit_file:
        files.append(explicit_file)
    if explicit_dir:
        directories.append(explicit_dir)

    env_file = os.environ.get("GROK_QUOTA_AUTH_FILE", "").strip()
    if env_file:
        files.append(require_absolute_path(env_file, "GROK_QUOTA_AUTH_FILE") or Path(env_file))

    configured_dir = False
    for env_name in ("GROK_QUOTA_AUTH_DIR", "CLIPROXY_AUTH_DIR"):
        raw = os.environ.get(env_name, "").strip()
        if raw:
            configured_dir = True
            directories.append(require_absolute_path(raw, env_name) or Path(raw))

    home = system_home()
    if not explicit_file and not explicit_dir:
        grok_home = os.environ.get("GROK_HOME", "").strip()
        if grok_home:
            files.append((require_absolute_path(grok_home, "GROK_HOME") or Path(grok_home)) / "auth.json")
        files.append(home / ".grok" / "auth.json")

    if not explicit_file and not explicit_dir and not env_file and not configured_dir:
        directories.extend((home / ".cli-proxy-api", home / ".config" / "cli-proxy-api"))

    return dedupe_paths(files), dedupe_paths(directories)


def dedupe_paths(paths: list[Path]) -> list[Path]:
    output: list[Path] = []
    seen: set[str] = set()
    for path in paths:
        key = str(path)
        if key in seen:
            continue
        seen.add(key)
        output.append(path)
    return output


def auth_candidates(files: list[Path], directories: list[Path]) -> list[Path]:
    candidates = [path for path in files if path.is_file()]
    for directory in directories:
        if not directory.is_dir():
            continue
        candidates.extend(path for path in directory.rglob("*.json") if path.is_file())
    return sorted(dedupe_paths(candidates), key=lambda path: str(path))


def is_xai_issuer(raw_url: str) -> bool:
    try:
        parsed = urllib.parse.urlparse(raw_url)
    except ValueError:
        return False
    host = (parsed.hostname or "").lower()
    return parsed.scheme == "https" and (host == "x.ai" or host.endswith(".x.ai"))


def read_auth_records(path: Path) -> list[tuple[dict[str, Any], str, str]]:
    try:
        if path.stat().st_size > MAX_AUTH_BYTES:
            return []
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError):
        return []
    if not isinstance(value, dict):
        return []

    provider = str(value.get("type") or value.get("provider") or "").strip().lower()
    if provider in {"xai", "x-ai", "grok"}:
        return [(value, "cliproxyapi", "")]

    records: list[tuple[dict[str, Any], str, str]] = []
    for entry_key, raw_record in value.items():
        if not isinstance(raw_record, dict):
            continue
        key_issuer, _, key_client_id = str(entry_key).partition("::")
        issuer = str(raw_record.get("oidc_issuer") or key_issuer).strip()
        if not is_xai_issuer(issuer):
            continue
        access_token = str(raw_record.get("key") or raw_record.get("access_token") or "").strip()
        refresh_token = str(raw_record.get("refresh_token") or "").strip()
        if not access_token and not refresh_token:
            continue
        normalized = {
            "type": "xai",
            "access_token": access_token,
            "refresh_token": refresh_token,
            "token_endpoint": raw_record.get("token_endpoint"),
            "expires_at": raw_record.get("expires_at"),
            "email": raw_record.get("email"),
            "sub": raw_record.get("user_id") or raw_record.get("principal_id"),
            "client_id": raw_record.get("oidc_client_id") or key_client_id,
        }
        records.append((normalized, "grok_build_cli", str(entry_key)))
    return records


def account_reference(path: Path, data: dict[str, Any], identity_hint: str = "") -> str:
    identity = str(data.get("sub") or data.get("email") or "")
    source = f"{path.absolute()}\0{identity_hint}\0{identity}".encode("utf-8", errors="replace")
    return hashlib.sha256(source).hexdigest()[:12]


def discover_credentials(args: dict[str, Any]) -> tuple[list[Credential], dict[str, Any]]:
    files, directories = credential_sources(args)
    credentials: list[Credential] = []
    scanned_count = 0
    for path in auth_candidates(files, directories):
        scanned_count += 1
        for data, source_kind, identity_hint in read_auth_records(path):
            credentials.append(
                Credential(
                    path=path,
                    account_ref=account_reference(path, data, identity_hint),
                    source_kind=source_kind,
                    access_token=str(data.get("access_token") or "").strip(),
                    refresh_token=str(data.get("refresh_token") or "").strip(),
                    token_endpoint=str(data.get("token_endpoint") or "").strip(),
                    client_id=str(data.get("client_id") or "").strip(),
                    expired_at=str(data.get("expired") or data.get("expires_at") or "").strip(),
                    email_present=bool(str(data.get("email") or "").strip()),
                    subject_present=bool(str(data.get("sub") or "").strip()),
                )
            )
    source_summary = {
        "explicit_file_configured": bool(args.get("auth_file") or os.environ.get("GROK_QUOTA_AUTH_FILE")),
        "explicit_dir_configured": bool(
            args.get("auth_dir")
            or os.environ.get("GROK_QUOTA_AUTH_DIR")
            or os.environ.get("CLIPROXY_AUTH_DIR")
        ),
        "candidate_files_scanned": scanned_count,
        "xai_accounts_found": len(credentials),
    }
    return credentials, source_summary


def parse_timestamp(value: str) -> datetime | None:
    value = value.strip()
    if not value:
        return None
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return None
    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=timezone.utc)
    return parsed.astimezone(timezone.utc)


def credential_summary(credential: Credential) -> dict[str, Any]:
    expires_at = parse_timestamp(credential.expired_at)
    now = datetime.now(timezone.utc)
    return {
        "account_ref": credential.account_ref,
        "credential_source": credential.source_kind,
        "access_token_present": bool(credential.access_token),
        "refresh_token_present": bool(credential.refresh_token),
        "expires_at": expires_at.isoformat().replace("+00:00", "Z") if expires_at else None,
        "expired": expires_at <= now if expires_at else None,
        "email_present": credential.email_present,
        "subject_present": credential.subject_present,
    }


def status(args: dict[str, Any]) -> dict[str, Any]:
    reject_unknown(args, {"auth_file", "auth_dir"})
    credentials, source_summary = discover_credentials(args)
    return {
        "ok": True,
        "operation": "status",
        "source": "local_xai_oauth_files",
        "sources": source_summary,
        "accounts": [credential_summary(item) for item in credentials],
        "message": None if credentials else "No local xAI/Grok OAuth credentials were found.",
    }


def reject_unknown(args: dict[str, Any], allowed: set[str]) -> None:
    unknown = sorted(set(args) - allowed)
    if unknown:
        raise SkillError(f"Unsupported input fields: {', '.join(unknown)}", "bad_input")


def select_credential(credentials: list[Credential], account_ref: Any) -> Credential:
    if not credentials:
        raise SkillError(
            "No local xAI/Grok OAuth credentials were found.",
            "credentials_not_found",
            {"hint": "Configure GROK_QUOTA_AUTH_DIR or pass an absolute auth_dir path."},
        )
    if account_ref is not None:
        if not isinstance(account_ref, str) or not account_ref.strip():
            raise SkillError("account_ref must be a non-empty string.", "bad_account_ref")
        for credential in credentials:
            if credential.account_ref == account_ref.strip():
                return credential
        raise SkillError("The requested account_ref was not found.", "account_not_found")
    if len(credentials) > 1:
        raise SkillError(
            "Multiple xAI accounts were found; choose one account_ref from status.",
            "account_selection_required",
            {"account_refs": [item.account_ref for item in credentials]},
        )
    return credentials[0]


def validate_xai_endpoint(raw_url: str, purpose: str) -> str:
    try:
        parsed = urllib.parse.urlparse(raw_url)
    except ValueError as exc:
        raise SkillError(f"Invalid {purpose} endpoint.", "unsafe_endpoint") from exc
    host = (parsed.hostname or "").lower()
    if parsed.scheme != "https" or (host != "x.ai" and not host.endswith(".x.ai")):
        raise SkillError(f"Refusing non-x.ai {purpose} endpoint.", "unsafe_endpoint")
    return raw_url


def read_http_body(response: Any) -> bytes:
    body = response.read(MAX_RESPONSE_BYTES + 1)
    if len(body) > MAX_RESPONSE_BYTES:
        raise SkillError("Upstream response exceeded the safety limit.", "response_too_large")
    return body


def request_json(url: str, timeout: int, *, form: dict[str, str] | None = None) -> dict[str, Any]:
    data = None
    headers = {"Accept": "application/json"}
    method = "GET"
    if form is not None:
        data = urllib.parse.urlencode(form).encode("utf-8")
        headers["Content-Type"] = "application/x-www-form-urlencoded"
        method = "POST"
    request = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with HTTP_OPENER.open(request, timeout=timeout) as response:
            body = read_http_body(response)
    except urllib.error.HTTPError as exc:
        body = read_http_body(exc)
        raise SkillError(
            f"xAI OAuth request failed with HTTP {exc.code}.",
            "oauth_http_error",
            {"status_code": exc.code, "provider_error": safe_provider_error(body)},
        ) from exc
    except (urllib.error.URLError, TimeoutError, OSError) as exc:
        raise SkillError("xAI OAuth request failed.", "oauth_network_error") from exc
    try:
        value = json.loads(body)
    except json.JSONDecodeError as exc:
        raise SkillError("xAI OAuth returned invalid JSON.", "oauth_bad_json") from exc
    if not isinstance(value, dict):
        raise SkillError("xAI OAuth returned an invalid payload.", "oauth_bad_json")
    return value


def discover_token_endpoint(timeout: int) -> str:
    discovery = request_json(OIDC_DISCOVERY_URL, timeout)
    endpoint = str(discovery.get("token_endpoint") or "").strip()
    if not endpoint:
        raise SkillError("xAI discovery did not return token_endpoint.", "oauth_discovery_invalid")
    return validate_xai_endpoint(endpoint, "token")


def refresh_access_token(credential: Credential, timeout: int) -> tuple[str, bool]:
    if not credential.refresh_token:
        raise SkillError("The xAI access token is expired and no refresh token is available.", "token_expired")
    endpoint = credential.token_endpoint or discover_token_endpoint(timeout)
    endpoint = validate_xai_endpoint(endpoint, "token")
    payload = request_json(
        endpoint,
        timeout,
        form={
            "grant_type": "refresh_token",
            "client_id": credential.client_id or XAI_CLIENT_ID,
            "refresh_token": credential.refresh_token,
        },
    )
    access_token = str(payload.get("access_token") or "").strip()
    if not access_token:
        raise SkillError("xAI token refresh returned no access_token.", "oauth_refresh_invalid")
    return access_token, True


def should_refresh(credential: Credential) -> bool:
    if not credential.access_token:
        return True
    expires_at = parse_timestamp(credential.expired_at)
    if expires_at is None:
        return False
    return expires_at.timestamp() <= time.time() + 60


def probe_request(access_token: str, model: str, timeout: int) -> tuple[int, bytes, str]:
    payload = {
        "model": model,
        "stream": True,
        "max_output_tokens": 1,
        "input": [{"type": "message", "role": "user", "content": "Reply with OK."}],
    }
    request = urllib.request.Request(
        CLI_RESPONSES_URL,
        data=json.dumps(payload, separators=(",", ":")).encode("utf-8"),
        method="POST",
        headers={
            "Accept": "text/event-stream",
            "Authorization": f"Bearer {access_token}",
            "Connection": "Keep-Alive",
            "Content-Type": "application/json",
            "User-Agent": f"xai-grok-workspace/{CLIENT_VERSION}",
            "X-XAI-Token-Auth": "xai-grok-cli",
            "x-grok-client-version": CLIENT_VERSION,
        },
    )
    try:
        with HTTP_OPENER.open(request, timeout=timeout) as response:
            return response.status, read_http_body(response), response.headers.get("Content-Type", "")
    except urllib.error.HTTPError as exc:
        return exc.code, read_http_body(exc), exc.headers.get("Content-Type", "")
    except (urllib.error.URLError, TimeoutError, OSError) as exc:
        raise SkillError("Grok quota probe failed before receiving an HTTP response.", "probe_network_error") from exc


def sanitize_text(value: str) -> str:
    value = BEARER_PATTERN.sub("Bearer [REDACTED]", value)
    value = JWT_PATTERN.sub("[REDACTED_JWT]", value)
    value = EMAIL_PATTERN.sub("[REDACTED_EMAIL]", value)
    value = SECRET_FIELD_PATTERN.sub(r"\1\2[REDACTED]", value)
    return value[:1000]


def decode_json(body: bytes) -> dict[str, Any]:
    try:
        value = json.loads(body)
    except (UnicodeDecodeError, json.JSONDecodeError):
        return {}
    return value if isinstance(value, dict) else {}


def provider_error_parts(body: bytes) -> tuple[str, str]:
    payload = decode_json(body)
    code = str(payload.get("code") or "").strip()
    error = payload.get("error")
    message = ""
    if isinstance(error, str):
        message = error
    elif isinstance(error, dict):
        code = str(error.get("code") or code).strip()
        message = str(error.get("message") or error.get("error") or "")
    if not message:
        message = str(payload.get("message") or "")
    if not message:
        message = body.decode("utf-8", errors="replace")
    return sanitize_text(code), sanitize_text(message)


def safe_provider_error(body: bytes) -> dict[str, str | None]:
    code, message = provider_error_parts(body)
    return {"code": code or None, "message": message or None}


def parse_exhausted_quota(status_code: int, body: bytes) -> dict[str, Any] | None:
    if status_code != 429:
        return None
    provider_code, message = provider_error_parts(body)
    lowered = f"{provider_code}\n{message}".lower()
    exhausted = (
        "free-usage-exhausted" in lowered
        or "included free usage" in lowered
        or "tokens (actual/limit)" in lowered
    )
    if not exhausted:
        return None

    match = TOKEN_USAGE_PATTERN.search(message)
    actual = int(match.group(1).replace(",", "").replace("_", "")) if match else None
    limit = int(match.group(2).replace(",", "").replace("_", "")) if match else None
    remaining = max(limit - actual, 0) if actual is not None and limit is not None else None
    overage = max(actual - limit, 0) if actual is not None and limit is not None else None
    used_percent = round(actual / limit * 100, 2) if actual is not None and limit else None
    return {
        "exhausted": True,
        "actual_tokens": actual,
        "limit_tokens": limit,
        "remaining_tokens": remaining,
        "overage_tokens": overage,
        "used_percent": used_percent,
        "reset_policy": "rolling_24_hours",
        "reset_at": None,
        "exact_reset_time_available": False,
        "provider_code": provider_code or None,
        "provider_message": message or None,
    }


def parse_sse_usage(body: bytes) -> dict[str, Any] | None:
    completed: dict[str, Any] | None = None
    for raw_line in body.decode("utf-8", errors="replace").splitlines():
        line = raw_line.strip()
        if not line.startswith("data:"):
            continue
        raw_data = line[5:].strip()
        if not raw_data or raw_data == "[DONE]":
            continue
        try:
            event = json.loads(raw_data)
        except json.JSONDecodeError:
            continue
        if not isinstance(event, dict):
            continue
        if event.get("type") == "response.completed":
            completed = event
    if completed is None:
        payload = decode_json(body)
        response = payload.get("response") if isinstance(payload.get("response"), dict) else payload
    else:
        response = completed.get("response") if isinstance(completed.get("response"), dict) else completed
    usage = response.get("usage") if isinstance(response, dict) else None
    return usage if isinstance(usage, dict) else None


def query(args: dict[str, Any]) -> dict[str, Any]:
    reject_unknown(args, {"auth_file", "auth_dir", "account_ref", "model", "timeout_seconds"})
    timeout = args.get("timeout_seconds", 20)
    if isinstance(timeout, bool) or not isinstance(timeout, int) or not 5 <= timeout <= 30:
        raise SkillError("timeout_seconds must be an integer from 5 to 30.", "bad_timeout")
    model = args.get("model", DEFAULT_MODEL)
    if not isinstance(model, str) or not MODEL_PATTERN.fullmatch(model.strip()):
        raise SkillError("model contains unsupported characters or length.", "bad_model")
    model = model.strip()

    credentials, _ = discover_credentials(args)
    credential = select_credential(credentials, args.get("account_ref"))
    access_token = credential.access_token
    refreshed_in_memory = False
    if should_refresh(credential):
        access_token, refreshed_in_memory = refresh_access_token(credential, timeout)

    status_code, body, content_type = probe_request(access_token, model, timeout)
    if status_code == 401 and credential.refresh_token and not refreshed_in_memory:
        access_token, refreshed_in_memory = refresh_access_token(credential, timeout)
        status_code, body, content_type = probe_request(access_token, model, timeout)

    common = {
        "operation": "query",
        "source": "grok_cli_chat_proxy_responses",
        "account_ref": credential.account_ref,
        "model": model,
        "http_status": status_code,
        "refreshed_in_memory": refreshed_in_memory,
        "probe_may_consume_tokens": True,
        "fetched_at": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
    }
    exhausted = parse_exhausted_quota(status_code, body)
    if exhausted is not None:
        return {
            "ok": True,
            **common,
            "available": False,
            "quota": exhausted,
            "message": "Grok included free usage is exhausted for the rolling 24-hour window.",
        }
    if 200 <= status_code < 300:
        return {
            "ok": True,
            **common,
            "available": True,
            "quota": {
                "exhausted": False,
                "actual_tokens": None,
                "limit_tokens": None,
                "remaining_tokens": None,
                "reset_policy": "rolling_24_hours",
                "exact_remaining_available": False,
            },
            "probe_usage": parse_sse_usage(body),
            "content_type": content_type or None,
            "message": "Grok is currently available; the successful response does not expose exact rolling quota remaining.",
        }
    provider_error = safe_provider_error(body)
    code = "unauthorized" if status_code in {401, 403} else "rate_limited" if status_code == 429 else "upstream_error"
    return {
        "ok": False,
        **common,
        "available": False,
        "code": code,
        "provider_error": provider_error,
        "message": f"Grok quota probe returned HTTP {status_code} without an exhausted quota payload.",
    }


def main() -> int:
    try:
        args = load_input()
        operation = str(args.pop("skill_action", "status"))
        if operation == "status":
            return emit(status(args))
        if operation == "query":
            return emit(query(args))
        raise SkillError(f"Unsupported operation: {operation}", "bad_operation")
    except SkillError as exc:
        return emit({"ok": False, "code": exc.code, "error": str(exc), "details": exc.details})
    except Exception as exc:
        return emit(
            {
                "ok": False,
                "code": "unexpected_error",
                "error": f"Unexpected failure: {type(exc).__name__}",
            }
        )


if __name__ == "__main__":
    raise SystemExit(main())

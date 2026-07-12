#!/usr/bin/env python3
"""AgentDock Skill wrapper for the OpenList v4 HTTP API."""
from __future__ import annotations

import hashlib
import json
import os
import ssl
import sys
from pathlib import Path
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.parse import quote, urlencode, urlsplit, urlunsplit
from urllib.request import Request, urlopen

SKILL_VERSION = "0.2.3"
UPSTREAM_REPOSITORY = "https://github.com/OpenListTeam/OpenList"
DEFAULT_BASE_URL = "http://127.0.0.1:5244"


class SkillInputError(ValueError):
    pass


def emit(value: Any) -> None:
    print(json.dumps(value, ensure_ascii=False, separators=(",", ":")))


def load_input() -> dict[str, Any]:
    raw = sys.stdin.read().strip()
    if not raw:
        return {}
    try:
        value = json.loads(raw)
    except json.JSONDecodeError as exc:
        raise SkillInputError(f"invalid JSON input: {exc}") from exc
    if not isinstance(value, dict):
        raise SkillInputError("input must be a JSON object")
    return value


def control_dir() -> Path:
    here = Path(__file__).resolve()
    for parent in here.parents:
        if parent.name == "AgentDock":
            return parent
    return Path(os.environ.get("AGENTDOCK_DIR", str(Path.home() / ".agentdock")))


def session_path() -> Path:
    configured = os.environ.get("OPENLIST_SESSION_FILE", "").strip()
    if configured:
        return Path(configured).expanduser()
    return control_dir() / "skill-data" / "openlist" / "session.json"


def load_session() -> dict[str, str]:
    path = session_path()
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (FileNotFoundError, PermissionError, json.JSONDecodeError, OSError):
        return {}
    if not isinstance(value, dict):
        return {}
    result: dict[str, str] = {}
    for key in ("base_url", "token"):
        item = value.get(key)
        if isinstance(item, str) and item:
            result[key] = item
    return result


def save_session(base_url: str, token: str) -> Path:
    path = session_path()
    path.parent.mkdir(parents=True, exist_ok=True)
    try:
        path.parent.chmod(0o700)
    except OSError:
        pass
    tmp = path.with_suffix(".tmp")
    tmp.write_text(json.dumps({"base_url": base_url, "token": token}), encoding="utf-8")
    tmp.chmod(0o600)
    tmp.replace(path)
    try:
        path.chmod(0o600)
    except OSError:
        pass
    return path


def clear_session() -> bool:
    path = session_path()
    try:
        path.unlink()
        return True
    except FileNotFoundError:
        return False


def normalize_base_url(value: str) -> str:
    parsed = urlsplit(value.strip())
    if parsed.scheme not in {"http", "https"}:
        raise SkillInputError("base_url must use http or https")
    if not parsed.hostname:
        raise SkillInputError("base_url must include a host")
    if parsed.username or parsed.password:
        raise SkillInputError("base_url must not contain credentials")
    if parsed.query or parsed.fragment:
        raise SkillInputError("base_url must not contain query or fragment")
    path = parsed.path.rstrip("/")
    return urlunsplit((parsed.scheme, parsed.netloc, path, "", ""))


def resolve_base_url(payload: dict[str, Any]) -> str:
    explicit = payload.get("base_url")
    if explicit is not None:
        if not isinstance(explicit, str):
            raise SkillInputError("base_url must be a string")
        return normalize_base_url(explicit)
    env_value = os.environ.get("OPENLIST_URL", "").strip()
    if env_value:
        return normalize_base_url(env_value)
    saved = load_session().get("base_url")
    if saved:
        return normalize_base_url(saved)
    return DEFAULT_BASE_URL


def resolve_token(payload: dict[str, Any], base_url: str) -> str:
    explicit = payload.get("token")
    if explicit is not None:
        if not isinstance(explicit, str):
            raise SkillInputError("token must be a string")
        return explicit
    env_value = os.environ.get("OPENLIST_TOKEN", "")
    if env_value:
        return env_value
    saved = load_session()
    if saved.get("base_url") == base_url:
        return saved.get("token", "")
    return ""


def parse_response_body(raw: bytes, content_type: str) -> Any:
    if not raw:
        return None
    text = raw.decode("utf-8", errors="replace")
    if "json" in content_type.lower() or text[:1] in {"{", "["}:
        try:
            return json.loads(text)
        except json.JSONDecodeError:
            pass
    return text


def request_api(
    base_url: str,
    endpoint: str,
    *,
    method: str = "GET",
    token: str = "",
    body: Any = None,
    query: dict[str, Any] | None = None,
    timeout: int = 30,
) -> dict[str, Any]:
    if not endpoint.startswith("/"):
        endpoint = "/" + endpoint
    url = base_url.rstrip("/") + endpoint
    if query:
        compact = {k: v for k, v in query.items() if v is not None}
        if compact:
            url += ("&" if "?" in url else "?") + urlencode(compact, doseq=True)
    headers = {
        "Accept": "application/json",
        "User-Agent": f"AgentDock-OpenList-Skill/{SKILL_VERSION}",
    }
    data = None
    if body is not None:
        data = json.dumps(body, ensure_ascii=False).encode("utf-8")
        headers["Content-Type"] = "application/json"
    if token:
        headers["Authorization"] = token
    request = Request(url, data=data, headers=headers, method=method.upper())
    insecure_tls = bool(os.environ.get("OPENLIST_INSECURE_TLS") == "1")
    context = ssl._create_unverified_context() if insecure_tls else None
    try:
        with urlopen(request, timeout=timeout, context=context) as response:
            raw = response.read()
            status = response.status
            content_type = response.headers.get("Content-Type", "")
    except HTTPError as exc:
        raw = exc.read()
        status = exc.code
        content_type = exc.headers.get("Content-Type", "") if exc.headers else ""
    except URLError as exc:
        return {
            "ok": False,
            "http_status": None,
            "endpoint": endpoint,
            "error": f"connection failed: {exc.reason}",
        }
    except TimeoutError:
        return {
            "ok": False,
            "http_status": None,
            "endpoint": endpoint,
            "error": f"request timed out after {timeout}s",
        }

    parsed = parse_response_body(raw, content_type)
    api_code = parsed.get("code") if isinstance(parsed, dict) else None
    ok = 200 <= status < 300 and (not isinstance(api_code, int) or 200 <= api_code < 300)
    return {
        "ok": ok,
        "http_status": status,
        "endpoint": endpoint,
        "response": parsed,
    }


def body_from(payload: dict[str, Any], keys: list[str], defaults: dict[str, Any] | None = None) -> dict[str, Any]:
    body = dict(defaults or {})
    for key in keys:
        if key in payload:
            body[key] = payload[key]
    return body


def require(payload: dict[str, Any], key: str) -> Any:
    if key not in payload:
        raise SkillInputError(f"missing required field: {key}")
    return payload[key]


def authenticated_call(
    payload: dict[str, Any],
    endpoint: str,
    *,
    method: str = "POST",
    body: Any = None,
    query: dict[str, Any] | None = None,
) -> dict[str, Any]:
    base_url = resolve_base_url(payload)
    token = resolve_token(payload, base_url)
    if not token:
        return {
            "ok": False,
            "http_status": None,
            "endpoint": endpoint,
            "error": "no token available; run login, pass token, or set OPENLIST_TOKEN",
        }
    return request_api(base_url, endpoint, method=method, token=token, body=body, query=query)


def op_status(payload: dict[str, Any]) -> dict[str, Any]:
    base_url = resolve_base_url(payload)
    result = request_api(base_url, "/api/public/settings", method="GET", timeout=15)
    result.update(
        {
            "service": "openlist",
            "base_url": base_url,
            "skill_version": SKILL_VERSION,
            "upstream": UPSTREAM_REPOSITORY,
        }
    )
    return result


def op_login(payload: dict[str, Any]) -> dict[str, Any]:
    base_url = resolve_base_url(payload)
    body = {
        "username": require(payload, "username"),
        "password": require(payload, "password"),
        "otp_code": payload.get("otp_code", ""),
    }
    result = request_api(base_url, "/api/auth/login", method="POST", body=body, timeout=20)
    response = result.get("response")
    token = ""
    if isinstance(response, dict) and isinstance(response.get("data"), dict):
        token_value = response["data"].get("token")
        if isinstance(token_value, str):
            token = token_value
    saved = False
    if result.get("ok") and token and payload.get("save_token", True):
        save_session(base_url, token)
        saved = True
    if token and not payload.get("return_token", False):
        response = json.loads(json.dumps(response))
        response.get("data", {}).pop("token", None)
        result["response"] = response
    result.update({"base_url": base_url, "token_saved": saved, "token_returned": bool(token and payload.get("return_token", False))})
    return result


def op_logout(payload: dict[str, Any]) -> dict[str, Any]:
    base_url = resolve_base_url(payload)
    token = resolve_token(payload, base_url)
    if token:
        result = request_api(base_url, "/api/auth/logout", method="GET", token=token, timeout=15)
    else:
        result = {"ok": True, "http_status": None, "endpoint": "/api/auth/logout", "response": None}
    if payload.get("clear_local", True):
        result["local_session_removed"] = clear_session()
    return result


def op_me(payload: dict[str, Any]) -> dict[str, Any]:
    return authenticated_call(payload, "/api/me", method="GET")


def op_list(payload: dict[str, Any]) -> dict[str, Any]:
    body = body_from(payload, ["path", "password", "page", "per_page", "refresh"], {"path": "/", "page": 1, "per_page": 100, "refresh": False})
    base_url = resolve_base_url(payload)
    token = resolve_token(payload, base_url)
    return request_api(base_url, "/api/fs/list", method="POST", token=token, body=body)


def op_get(payload: dict[str, Any]) -> dict[str, Any]:
    body = body_from(payload, ["path", "password"])
    require(body, "path")
    base_url = resolve_base_url(payload)
    token = resolve_token(payload, base_url)
    return request_api(base_url, "/api/fs/get", method="POST", token=token, body=body)


def op_dirs(payload: dict[str, Any]) -> dict[str, Any]:
    body = body_from(payload, ["path", "password", "force_root"], {"path": "/", "force_root": False})
    return authenticated_call(payload, "/api/fs/dirs", body=body)


def op_search(payload: dict[str, Any]) -> dict[str, Any]:
    body = body_from(payload, ["parent", "keywords", "scope", "page", "per_page", "password"], {"parent": "/", "scope": 0, "page": 1, "per_page": 100})
    require(body, "keywords")
    return authenticated_call(payload, "/api/fs/search", body=body)


def op_mkdir(payload: dict[str, Any]) -> dict[str, Any]:
    return authenticated_call(payload, "/api/fs/mkdir", body={"path": require(payload, "path")})


def op_rename(payload: dict[str, Any]) -> dict[str, Any]:
    body = body_from(payload, ["path", "name", "overwrite"], {"overwrite": False})
    require(body, "path")
    require(body, "name")
    return authenticated_call(payload, "/api/fs/rename", body=body)


def op_move_copy(payload: dict[str, Any], endpoint: str) -> dict[str, Any]:
    body = body_from(payload, ["src_dir", "dst_dir", "names", "overwrite", "skip_existing", "merge"], {"overwrite": False, "skip_existing": False, "merge": False})
    for key in ("src_dir", "dst_dir", "names"):
        require(body, key)
    return authenticated_call(payload, endpoint, body=body)


def op_remove(payload: dict[str, Any]) -> dict[str, Any]:
    body = body_from(payload, ["dir", "names"])
    require(body, "dir")
    require(body, "names")
    return authenticated_call(payload, "/api/fs/remove", body=body)



def op_upload(payload: dict[str, Any]) -> dict[str, Any]:
    path = require(payload, "path")
    content = require(payload, "content")
    if not isinstance(path, str) or not path.startswith("/"):
        raise SkillInputError("path must be an absolute OpenList path")
    if not isinstance(content, str):
        raise SkillInputError("content must be a string")
    encoding = payload.get("encoding", "utf-8")
    if encoding != "utf-8":
        raise SkillInputError("only utf-8 encoding is supported")
    data = content.encode("utf-8")
    max_bytes = int(payload.get("max_bytes", 1048576))
    if max_bytes < 1 or max_bytes > 10485760:
        raise SkillInputError("max_bytes must be between 1 and 10485760")
    if len(data) > max_bytes:
        raise SkillInputError(f"content exceeds max_bytes ({len(data)} > {max_bytes})")
    base_url = resolve_base_url(payload)
    token = resolve_token(payload, base_url)
    if not token:
        return {
            "ok": False,
            "http_status": None,
            "endpoint": "/api/fs/put",
            "error": "no token available; run login, pass token, or set OPENLIST_TOKEN",
        }
    url = base_url.rstrip("/") + "/api/fs/put"
    headers = {
        "Accept": "application/json",
        "Authorization": token,
        "Content-Type": str(payload.get("content_type", "text/plain; charset=utf-8")),
        "File-Path": quote(path, safe="/"),
        "Overwrite": "true" if payload.get("overwrite", False) else "false",
        "As-Task": "true" if payload.get("as_task", False) else "false",
        "X-File-Size": str(len(data)),
        "X-File-Sha256": hashlib.sha256(data).hexdigest(),
        "User-Agent": f"AgentDock-OpenList-Skill/{SKILL_VERSION}",
    }
    request = Request(url, data=data, headers=headers, method="PUT")
    insecure_tls = bool(os.environ.get("OPENLIST_INSECURE_TLS") == "1")
    context = ssl._create_unverified_context() if insecure_tls else None
    try:
        with urlopen(request, timeout=60, context=context) as response:
            raw = response.read()
            status = response.status
            content_type = response.headers.get("Content-Type", "")
    except HTTPError as exc:
        raw = exc.read()
        status = exc.code
        content_type = exc.headers.get("Content-Type", "") if exc.headers else ""
    except URLError as exc:
        return {"ok": False, "http_status": None, "endpoint": "/api/fs/put", "error": f"connection failed: {exc.reason}"}
    except TimeoutError:
        return {"ok": False, "http_status": None, "endpoint": "/api/fs/put", "error": "request timed out after 60s"}
    parsed = parse_response_body(raw, content_type)
    api_code = parsed.get("code") if isinstance(parsed, dict) else None
    ok = 200 <= status < 300 and (not isinstance(api_code, int) or 200 <= api_code < 300)
    return {
        "ok": ok,
        "http_status": status,
        "endpoint": "/api/fs/put",
        "path": path,
        "size": len(data),
        "sha256": hashlib.sha256(data).hexdigest(),
        "response": parsed,
    }

def op_storage_list(payload: dict[str, Any]) -> dict[str, Any]:
    query = body_from(payload, ["page", "per_page"], {"page": 1, "per_page": 100})
    return authenticated_call(payload, "/api/admin/storage/list", method="GET", query=query)


def op_driver_list(payload: dict[str, Any]) -> dict[str, Any]:
    return authenticated_call(payload, "/api/admin/driver/list", method="GET")


def op_api_request(payload: dict[str, Any]) -> dict[str, Any]:
    endpoint = require(payload, "endpoint")
    if not isinstance(endpoint, str) or not endpoint.startswith("/api/"):
        raise SkillInputError("endpoint must start with /api/")
    method = str(payload.get("method", "GET")).upper()
    if method not in {"GET", "POST", "PUT", "PATCH", "DELETE"}:
        raise SkillInputError("method must be GET, POST, PUT, PATCH, or DELETE")
    base_url = resolve_base_url(payload)
    token = resolve_token(payload, base_url)
    return request_api(base_url, endpoint, method=method, token=token, body=payload.get("body"), query=payload.get("query"))


def op_session_status(_: dict[str, Any]) -> dict[str, Any]:
    saved = load_session()
    path = session_path()
    return {
        "ok": True,
        "session_file": str(path),
        "exists": path.exists(),
        "base_url": saved.get("base_url"),
        "has_token": bool(saved.get("token")),
    }


def op_session_clear(_: dict[str, Any]) -> dict[str, Any]:
    return {"ok": True, "removed": clear_session(), "session_file": str(session_path())}


OPERATIONS = {
    "status": op_status,
    "login": op_login,
    "logout": op_logout,
    "me": op_me,
    "list": op_list,
    "get": op_get,
    "dirs": op_dirs,
    "search": op_search,
    "mkdir": op_mkdir,
    "rename": op_rename,
    "move": lambda payload: op_move_copy(payload, "/api/fs/move"),
    "copy": lambda payload: op_move_copy(payload, "/api/fs/copy"),
    "remove": op_remove,
    "upload": op_upload,
    "storage-list": op_storage_list,
    "driver-list": op_driver_list,
    "api-request": op_api_request,
    "session-status": op_session_status,
    "session-clear": op_session_clear,
}


def main() -> None:
    try:
        payload = load_input()
        operation = str(payload.pop("skill_action", "status"))
        handler = OPERATIONS.get(operation)
        if handler is None:
            raise SkillInputError(f"unsupported operation: {operation}")
        emit(handler(payload))
    except SkillInputError as exc:
        emit({"ok": False, "error": str(exc), "error_type": "input"})
    except Exception as exc:  # defensive final boundary: always emit valid JSON
        emit({"ok": False, "error": str(exc), "error_type": type(exc).__name__})


if __name__ == "__main__":
    main()

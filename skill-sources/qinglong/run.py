#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import sqlite3
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path
from typing import Any


SKILL_VERSION = "0.1.7"
DEFAULT_BASE_URL = "http://127.0.0.1:5700"
DEFAULT_DATA_DIR = Path("/Volumes/KIOXIA/Docker/qinglong/data")
DEFAULT_COMPOSE_DIR = Path("/Volumes/KIOXIA/Docker/qinglong")
AGENTDOCK_HOME = Path(os.environ.get("AGENTDOCK_HOME", Path.home() / ".agentdock"))
DEFAULT_CONFIG_PATH = AGENTDOCK_HOME / "skill-data" / "qinglong" / "config.json"
SENSITIVE_KEYS = {
    "access_token",
    "api_key",
    "authorization",
    "client_secret",
    "cookie",
    "password",
    "secret",
    "token",
}


class QingLongError(RuntimeError):
    def __init__(self, message: str, *, code: str = "qinglong_error", details: Any = None) -> None:
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
        data = json.loads(raw)
    except json.JSONDecodeError as exc:
        raise QingLongError(f"Invalid JSON input: {exc}", code="bad_json") from exc
    if not isinstance(data, dict):
        raise QingLongError("Skill input must be a JSON object.", code="bad_json")
    return data


def normalize_base_url(value: str) -> str:
    parsed = urllib.parse.urlsplit(value.strip())
    if parsed.scheme not in {"http", "https"}:
        raise QingLongError("base_url must use http or https", code="bad_base_url")
    if not parsed.hostname:
        raise QingLongError("base_url must include a host", code="bad_base_url")
    if parsed.username or parsed.password:
        raise QingLongError("base_url must not contain credentials", code="bad_base_url")
    return urllib.parse.urlunsplit((parsed.scheme, parsed.netloc, parsed.path.rstrip("/"), "", ""))


def config_path(args: dict[str, Any]) -> Path:
    value = args.get("config_path") or os.environ.get("QINGLONG_CONFIG")
    return Path(value).expanduser() if value else DEFAULT_CONFIG_PATH


def load_config(args: dict[str, Any]) -> dict[str, Any]:
    path = config_path(args)
    if not path.exists():
        return {}
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise QingLongError(f"Cannot read QingLong config file: {exc}", code="bad_config") from exc
    if not isinstance(data, dict):
        raise QingLongError("QingLong config file must contain a JSON object.", code="bad_config")
    return data


def base_url(args: dict[str, Any]) -> str:
    cfg = load_config(args)
    value = args.get("base_url") or os.environ.get("QINGLONG_BASE_URL") or cfg.get("base_url") or DEFAULT_BASE_URL
    if not isinstance(value, str):
        raise QingLongError("base_url must be a string", code="bad_base_url")
    return normalize_base_url(value)


def data_dir(args: dict[str, Any]) -> Path:
    value = args.get("data_dir") or os.environ.get("QINGLONG_DATA_DIR")
    return Path(value).expanduser() if value else DEFAULT_DATA_DIR


def db_path(args: dict[str, Any]) -> Path:
    value = args.get("db_path") or os.environ.get("QINGLONG_DB_PATH")
    return Path(value).expanduser() if value else data_dir(args) / "db" / "database.sqlite"


def panel_token_path(args: dict[str, Any]) -> Path:
    value = args.get("token_path") or os.environ.get("QINGLONG_TOKEN_PATH")
    return Path(value).expanduser() if value else data_dir(args) / "config" / "token.json"


def keyv_path(args: dict[str, Any]) -> Path:
    value = args.get("keyv_path") or os.environ.get("QINGLONG_KEYV_PATH")
    return Path(value).expanduser() if value else data_dir(args) / "db" / "keyv.sqlite"


def compose_dir(args: dict[str, Any]) -> Path:
    value = args.get("compose_dir") or os.environ.get("QINGLONG_COMPOSE_DIR")
    return Path(value).expanduser() if value else DEFAULT_COMPOSE_DIR


def redacted(value: Any) -> str | None:
    if value is None:
        return None
    text = str(value)
    if not text:
        return ""
    if len(text) <= 8:
        return "<redacted>"
    return f"{text[:4]}...{text[-4:]}"


def redact_tree(value: Any) -> Any:
    if isinstance(value, dict):
        out: dict[str, Any] = {}
        for key, child in value.items():
            if key.lower() in SENSITIVE_KEYS:
                out[key] = redacted(child)
            else:
                out[key] = redact_tree(child)
        return out
    if isinstance(value, list):
        return [redact_tree(x) for x in value]
    return value


def ql_timestamp() -> str:
    return str(int(time.time()))


def read_system_app(args: dict[str, Any]) -> dict[str, str] | None:
    env_id = os.environ.get("QINGLONG_CLIENT_ID")
    env_secret = os.environ.get("QINGLONG_CLIENT_SECRET")
    if env_id and env_secret:
        return {"source": "environment", "client_id": env_id, "client_secret": env_secret}

    cfg = load_config(args)
    cfg_id = cfg.get("client_id")
    cfg_secret = cfg.get("client_secret")
    if isinstance(cfg_id, str) and isinstance(cfg_secret, str) and cfg_id and cfg_secret:
        return {"source": str(config_path(args)), "client_id": cfg_id, "client_secret": cfg_secret}

    path = db_path(args)
    if not path.exists():
        return None
    try:
        con = sqlite3.connect(str(path))
        con.row_factory = sqlite3.Row
        row = con.execute(
            "select client_id, client_secret from Apps where name = ? limit 1",
            ("system",),
        ).fetchone()
    except sqlite3.Error as exc:
        raise QingLongError(f"Cannot read QingLong sqlite app credentials: {exc}", code="sqlite_error") from exc
    finally:
        try:
            con.close()
        except Exception:
            pass
    if not row:
        return None
    return {
        "source": str(path),
        "client_id": row["client_id"],
        "client_secret": row["client_secret"],
    }


def request_json(
    method: str,
    url: str,
    *,
    token: str | None = None,
    body: Any = None,
    timeout: int = 20,
) -> tuple[int | None, Any]:
    headers = {
        "Accept": "application/json",
        "User-Agent": f"AgentDock-QingLong-Skill/{SKILL_VERSION}",
    }
    data = None
    if body is not None:
        data = json.dumps(body, ensure_ascii=False).encode("utf-8")
        headers["Content-Type"] = "application/json;charset=UTF-8"
    if token:
        headers["Authorization"] = f"Bearer {token}"
    req = urllib.request.Request(url, data=data, headers=headers, method=method.upper())
    try:
        with urllib.request.urlopen(req, timeout=timeout) as response:
            raw = response.read()
            status = response.status
    except urllib.error.HTTPError as exc:
        raw = exc.read()
        status = exc.code
    except urllib.error.URLError as exc:
        raise QingLongError(f"Cannot reach QingLong API: {exc.reason}", code="network_error") from exc
    text = raw.decode("utf-8", errors="replace")
    if not text:
        return status, None
    try:
        return status, json.loads(text)
    except json.JSONDecodeError:
        return status, text


def make_url(root: str, api_prefix: str, path: str, query: dict[str, Any] | None = None) -> str:
    if api_prefix not in {"/open", "/api"}:
        raise QingLongError("api_prefix must be /open or /api", code="bad_api_prefix")
    if not path.startswith("/"):
        path = "/" + path
    query_items: dict[str, Any] = {}
    if query:
        query_items.update(query)
    suffix = "?" + urllib.parse.urlencode(query_items, doseq=True) if query_items else ""
    return root.rstrip("/") + api_prefix + path + suffix


def get_token(args: dict[str, Any]) -> dict[str, Any]:
    app = read_system_app(args)
    if not app:
        raise QingLongError(
            "Missing QingLong app credentials. Set QINGLONG_CLIENT_ID/QINGLONG_CLIENT_SECRET or provide a readable QingLong sqlite database.",
            code="missing_credentials",
        )
    root = base_url(args)
    url = make_url(
        root,
        "/open",
        "/auth/token",
        {"client_id": app["client_id"], "client_secret": app["client_secret"]},
    )
    http_status, payload = request_json("GET", url)
    if not isinstance(payload, dict):
        raise QingLongError("Unexpected QingLong token response.", code="bad_token_response", details=payload)
    if payload.get("code") != 200:
        raise QingLongError(
            str(payload.get("message") or "QingLong token exchange failed."),
            code="auth_failed",
            details=redact_tree(payload),
        )
    data = payload.get("data") or {}
    token = data.get("token")
    if not token:
        raise QingLongError("QingLong token response did not include a token.", code="bad_token_response")
    return {
        "token": token,
        "http_status": http_status,
        "expiration": data.get("expiration"),
        "client_id_redacted": redacted(app["client_id"]),
        "credential_source": app["source"],
    }


def get_panel_token(args: dict[str, Any]) -> dict[str, Any]:
    env_token = os.environ.get("QINGLONG_API_TOKEN")
    if env_token:
        return {"token": env_token, "source": "environment", "expiration": None, "valid": True}

    keyv = keyv_path(args)
    if keyv.exists():
        try:
            con = sqlite3.connect(str(keyv))
            con.row_factory = sqlite3.Row
            row = con.execute("select value from keyv where key = ? limit 1", ("keyv:authInfo",)).fetchone()
        except sqlite3.Error as exc:
            raise QingLongError(f"Cannot read QingLong keyv authInfo: {exc}", code="sqlite_error") from exc
        finally:
            try:
                con.close()
            except Exception:
                pass
        if row:
            try:
                outer = json.loads(row["value"])
                auth_info = outer.get("value", outer)
            except (TypeError, json.JSONDecodeError) as exc:
                raise QingLongError(f"Cannot parse QingLong keyv authInfo: {exc}", code="bad_panel_token") from exc
            token = None
            tokens = auth_info.get("tokens") if isinstance(auth_info, dict) else None
            desktop = tokens.get("desktop") if isinstance(tokens, dict) else None
            if isinstance(desktop, str) and desktop:
                token = desktop
            elif isinstance(desktop, list) and desktop:
                first = desktop[0]
                token = first.get("value") if isinstance(first, dict) else None
            if not token and isinstance(auth_info, dict):
                token = auth_info.get("token")
            if token:
                return {"token": token, "source": str(keyv) + ":keyv:authInfo", "expiration": None, "valid": True}

    path = panel_token_path(args)
    if not path.exists():
        raise QingLongError(
            "Missing QingLong panel token. Set QINGLONG_API_TOKEN or provide data/db/keyv.sqlite.",
            code="missing_panel_token",
        )
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise QingLongError(f"Cannot read QingLong panel token: {exc}", code="bad_panel_token") from exc
    token = payload.get("value")
    expiration = payload.get("expiration")
    if not token:
        raise QingLongError("QingLong panel token file does not include value.", code="bad_panel_token")
    valid = not isinstance(expiration, int) or expiration > int(time.time())
    if not valid:
        raise QingLongError("QingLong panel token is expired.", code="expired_panel_token")
    return {"token": token, "source": str(path), "expiration": expiration, "valid": valid}


def call_open(args: dict[str, Any], method: str, path: str, *, query: dict[str, Any] | None = None, body: Any = None) -> dict[str, Any]:
    token_info = get_token(args)
    url = make_url(base_url(args), "/open", path, query)
    http_status, payload = request_json(method, url, token=token_info["token"], body=body)
    return {
        "ok": isinstance(payload, dict) and payload.get("code") == 200,
        "http_status": http_status,
        "response": redact_tree(payload),
    }


def call_api(args: dict[str, Any], method: str, path: str, *, query: dict[str, Any] | None = None, body: Any = None) -> dict[str, Any]:
    token_info = get_panel_token(args)
    url = make_url(base_url(args), "/api", path, query)
    http_status, payload = request_json(method, url, token=token_info["token"], body=body)
    return {
        "ok": isinstance(payload, dict) and payload.get("code") == 200,
        "http_status": http_status,
        "response": redact_tree(payload),
    }


def ensure_ids(args: dict[str, Any]) -> list[int]:
    ids = args.get("ids")
    if not isinstance(ids, list) or not ids:
        raise QingLongError("ids must be a non-empty array.", code="bad_ids")
    out: list[int] = []
    for item in ids:
        if not isinstance(item, int):
            raise QingLongError("ids must contain integers.", code="bad_ids")
        out.append(item)
    return out


def require_confirmed(args: dict[str, Any], action: str) -> None:
    if args.get("confirmed") is not True:
        raise QingLongError(f"{action} requires confirmed=true.", code="confirmation_required")


def summarize_envs(data: Any, *, include_values: bool) -> Any:
    if include_values or not isinstance(data, list):
        return data
    result = []
    for item in data:
        if not isinstance(item, dict):
            result.append(item)
            continue
        clone = dict(item)
        if "value" in clone:
            clone["value"] = f"<redacted:{len(str(clone['value']))}>"
        result.append(clone)
    return result


def docker_compose_state(args: dict[str, Any]) -> dict[str, Any]:
    directory = compose_dir(args)
    compose_file = directory / "docker-compose.yml"
    if not compose_file.exists():
        return {"ok": False, "compose_dir": str(directory), "error": "docker-compose.yml not found"}
    proc = None
    attempted: list[str] = []
    for command in (
        ["docker", "compose", "ps", "--format", "json"],
        ["docker-compose", "ps", "--format", "json"],
        ["docker-compose", "ps"],
    ):
        attempted.append(" ".join(command))
        try:
            candidate = subprocess.run(
                command,
                cwd=str(directory),
                text=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                timeout=10,
                check=False,
            )
        except FileNotFoundError:
            continue
        except subprocess.TimeoutExpired:
            return {"ok": False, "compose_dir": str(directory), "error": f"{attempted[-1]} timed out"}
        if "--format" in command and candidate.returncode == 0 and not candidate.stdout.strip():
            proc = candidate
            continue
        if candidate.returncode == 0 or candidate.stdout.strip():
            proc = candidate
            break
        proc = candidate

    if proc is None:
        return {"ok": False, "compose_dir": str(directory), "error": "docker compose command not found"}

    services = []
    for line in proc.stdout.splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            services.append(json.loads(line))
        except json.JSONDecodeError:
            services.append({"raw": line})
    return {
        "ok": proc.returncode == 0,
        "compose_dir": str(directory),
        "command": attempted[-1],
        "services": services,
        "stderr": proc.stderr.strip()[:500] if proc.returncode != 0 else "",
    }


def op_status(args: dict[str, Any]) -> dict[str, Any]:
    root = base_url(args)
    health: dict[str, Any]
    try:
        http_status, payload = request_json("GET", root.rstrip("/") + "/api/health")
        health = {"ok": http_status == 200, "http_status": http_status, "response": redact_tree(payload)}
    except QingLongError as exc:
        health = {"ok": False, "error": str(exc), "code": exc.code}

    db = db_path(args)
    app = read_system_app(args)
    open_auth: dict[str, Any]
    if args.get("skip_auth"):
        open_auth = {"skipped": True}
    elif not health.get("ok"):
        open_auth = {"ok": False, "skipped": True, "reason": "health check failed"}
    else:
        try:
            token = get_token(args)
            open_auth = {
                "ok": True,
                "http_status": token["http_status"],
                "expiration": token["expiration"],
                "client_id": token["client_id_redacted"],
                "credential_source": token["credential_source"],
            }
        except QingLongError as exc:
            open_auth = {"ok": False, "error": str(exc), "code": exc.code, "details": exc.details}

    env_open_auth: dict[str, Any]
    if args.get("skip_auth"):
        env_open_auth = {"skipped": True}
    elif not health.get("ok"):
        env_open_auth = {"ok": False, "skipped": True, "reason": "health check failed"}
    else:
        try:
            probe = call_open(args, "GET", "/envs", query={"searchValue": "__agentdock_probe__"})
            env_open_auth = {
                "ok": bool(probe.get("ok")),
                "http_status": probe.get("http_status"),
            }
        except QingLongError as exc:
            env_open_auth = {"ok": False, "error": str(exc), "code": exc.code, "details": exc.details}

    return {
        "ok": bool(health.get("ok"))
        and (open_auth.get("ok") is True or open_auth.get("skipped") is True)
        and (env_open_auth.get("ok") is True or env_open_auth.get("skipped") is True),
        "base_url": root,
        "compose": docker_compose_state(args),
        "database": {"path": str(db), "exists": db.exists(), "system_app_available": bool(app)},
        "health": health,
        "open_auth": open_auth,
        "env_open_auth": env_open_auth,
    }


def op_envs(args: dict[str, Any]) -> dict[str, Any]:
    query = {}
    if args.get("search"):
        query["searchValue"] = str(args["search"])
    result = call_open(args, "GET", "/envs", query=query)
    response = result.get("response")
    if isinstance(response, dict) and "data" in response:
        response = dict(response)
        response["data"] = summarize_envs(response["data"], include_values=bool(args.get("include_values")))
        result["response"] = response
    return result


def op_env_create(args: dict[str, Any]) -> dict[str, Any]:
    envs = args.get("envs")
    if not isinstance(envs, list) or not envs:
        raise QingLongError("envs must be a non-empty array.", code="bad_envs")
    return call_open(args, "POST", "/envs", body=envs)


def op_env_update(args: dict[str, Any]) -> dict[str, Any]:
    body = {k: v for k, v in args.items() if k in {"id", "name", "value", "remarks", "labels"}}
    return call_open(args, "PUT", "/envs", body=body)


def op_env_delete(args: dict[str, Any]) -> dict[str, Any]:
    require_confirmed(args, "env_delete")
    return call_open(args, "DELETE", "/envs", body=ensure_ids(args))


def op_ids(args: dict[str, Any], path: str, method: str = "PUT") -> dict[str, Any]:
    return call_open(args, method, path, body=ensure_ids(args))


def op_env_ids(args: dict[str, Any], path: str, method: str = "PUT") -> dict[str, Any]:
    return call_open(args, method, path, body=ensure_ids(args))


def op_crons(args: dict[str, Any]) -> dict[str, Any]:
    query = {}
    if args.get("search"):
        query["searchValue"] = str(args["search"])
    return call_open(args, "GET", "/crons", query=query)


def op_cron_detail(args: dict[str, Any]) -> dict[str, Any]:
    cron_id = args.get("id")
    if not isinstance(cron_id, int):
        raise QingLongError("id must be an integer.", code="bad_id")
    return call_open(args, "GET", f"/crons/{cron_id}")


def op_cron_create(args: dict[str, Any]) -> dict[str, Any]:
    body = {k: v for k, v in args.items() if k in {"name", "command", "schedule", "labels", "sub_id"}}
    for key in ("name", "command", "schedule"):
        if not body.get(key):
            raise QingLongError(f"Missing {key}.", code="bad_cron")
    return call_open(args, "POST", "/crons", body=body)


def op_cron_update(args: dict[str, Any]) -> dict[str, Any]:
    body = {k: v for k, v in args.items() if k in {"id", "name", "command", "schedule", "labels"}}
    if not isinstance(body.get("id"), int):
        raise QingLongError("id must be an integer.", code="bad_id")
    return call_open(args, "PUT", "/crons", body=body)


def op_cron_delete(args: dict[str, Any]) -> dict[str, Any]:
    require_confirmed(args, "cron_delete")
    return call_open(args, "DELETE", "/crons", body=ensure_ids(args))


def op_cron_logs(args: dict[str, Any]) -> dict[str, Any]:
    cron_id = args.get("id")
    if not isinstance(cron_id, int):
        raise QingLongError("id must be an integer.", code="bad_id")
    return call_open(args, "GET", f"/crons/{cron_id}/logs")


def op_cron_log(args: dict[str, Any]) -> dict[str, Any]:
    cron_id = args.get("id")
    if not isinstance(cron_id, int):
        raise QingLongError("id must be an integer.", code="bad_id")
    return call_open(args, "GET", f"/crons/{cron_id}/log")


def op_api(args: dict[str, Any]) -> dict[str, Any]:
    method = str(args.get("method") or "").upper()
    if method not in {"GET", "POST", "PUT", "DELETE"}:
        raise QingLongError("method must be GET, POST, PUT, or DELETE.", code="bad_method")
    path = args.get("path")
    if not isinstance(path, str) or not path:
        raise QingLongError("path must be a non-empty string.", code="bad_path")
    prefix = args.get("api_prefix") or "/open"
    url = make_url(base_url(args), prefix, path, args.get("query") if isinstance(args.get("query"), dict) else None)
    token_info = get_panel_token(args) if prefix == "/api" else get_token(args)
    http_status, payload = request_json(method, url, token=token_info["token"], body=args.get("body"))
    return {
        "ok": isinstance(payload, dict) and payload.get("code") == 200,
        "http_status": http_status,
        "response": redact_tree(payload),
    }


OPERATIONS = {
    "status": op_status,
    "envs": op_envs,
    "env_create": op_env_create,
    "env_update": op_env_update,
    "env_delete": op_env_delete,
    "env_enable": lambda args: op_env_ids(args, "/envs/enable"),
    "env_disable": lambda args: op_env_ids(args, "/envs/disable"),
    "crons": op_crons,
    "cron_detail": op_cron_detail,
    "cron_create": op_cron_create,
    "cron_update": op_cron_update,
    "cron_delete": op_cron_delete,
    "cron_run": lambda args: op_ids(args, "/crons/run"),
    "cron_stop": lambda args: op_ids(args, "/crons/stop"),
    "cron_enable": lambda args: op_ids(args, "/crons/enable"),
    "cron_disable": lambda args: op_ids(args, "/crons/disable"),
    "cron_logs": op_cron_logs,
    "cron_log": op_cron_log,
    "api": op_api,
}


def main() -> int:
    args = load_input()
    operation = str(args.pop("skill_action", "status"))
    handler = OPERATIONS.get(operation)
    if not handler:
        raise QingLongError(f"Unsupported operation: {operation}", code="unsupported_operation")
    result = handler(args)
    if isinstance(result, dict) and "ok" not in result:
        result = {"ok": True, **result}
    return json_out(result)


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except QingLongError as exc:
        raise SystemExit(json_out({"ok": False, "error": str(exc), "code": exc.code, "details": exc.details}, exit_code=1))
    except Exception as exc:
        raise SystemExit(json_out({"ok": False, "error": str(exc), "code": "unexpected_error"}, exit_code=1))

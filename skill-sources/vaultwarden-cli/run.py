#!/usr/bin/env python3
import json
import os
import shutil
import subprocess
import sys
from urllib.parse import urlparse

VERSION = "1.0.3"
KEYCHAIN_SERVICE = "agentdock-vaultwarden-cli"
KEYCHAIN_ACCOUNT = "bw-session"
CLI_CANDIDATES = ("/opt/homebrew/bin/bw", "/usr/local/bin/bw")
SECURITY = "/usr/bin/security"
PBCOPY = "/usr/bin/pbcopy"
PBPASTE = "/usr/bin/pbpaste"


def emit(value):
    print(json.dumps(value, ensure_ascii=False, separators=(",", ":")))


def fail(code, message, details=None, exit_code=1):
    payload = {"ok": False, "error": {"code": code, "message": message}}
    if details is not None:
        payload["error"]["details"] = details
    emit(payload)
    raise SystemExit(exit_code)


def load_input():
    raw = sys.stdin.read().strip()
    if not raw:
        return {}
    try:
        value = json.loads(raw)
    except json.JSONDecodeError as exc:
        fail("INVALID_INPUT", "输入不是有效 JSON", {"reason": str(exc)})
    if not isinstance(value, dict):
        fail("INVALID_INPUT", "输入必须是 JSON 对象")
    return value


def find_bw():
    found = shutil.which("bw")
    if found:
        return found
    for candidate in CLI_CANDIDATES:
        if os.path.isfile(candidate) and os.access(candidate, os.X_OK):
            return candidate
    fail("BW_NOT_FOUND", "未找到 Bitwarden 官方 bw CLI")


def run_process(argv, timeout=45, input_text=None, sensitive=False, allow_failure=False):
    try:
        completed = subprocess.run(
            argv,
            input=input_text,
            text=True,
            capture_output=True,
            timeout=timeout,
            check=False,
            env=os.environ.copy(),
        )
    except subprocess.TimeoutExpired:
        fail("COMMAND_TIMEOUT", "命令执行超时")
    if completed.returncode != 0 and not allow_failure:
        details = {"exit_code": completed.returncode}
        if not sensitive:
            details["stderr"] = completed.stderr.strip()[:2000]
        fail("COMMAND_FAILED", "官方 bw CLI 命令执行失败", details)
    return completed


def run_bw(args, timeout=45, session=None, sensitive=False, allow_failure=False):
    argv = [find_bw(), *args]
    if session:
        argv.extend(["--session", session])
    return run_process(argv, timeout=timeout, sensitive=sensitive, allow_failure=allow_failure)


def keychain_session(required=True):
    if not os.path.exists(SECURITY):
        if required:
            fail("KEYCHAIN_UNAVAILABLE", "当前系统没有 macOS security 工具")
        return ""
    completed = run_process(
        [SECURITY, "find-generic-password", "-w", "-s", KEYCHAIN_SERVICE, "-a", KEYCHAIN_ACCOUNT],
        timeout=10,
        sensitive=True,
        allow_failure=True,
    )
    value = completed.stdout.rstrip("\r\n") if completed.returncode == 0 else ""
    if required and not value:
        fail(
            "VAULT_LOCKED",
            "密码库尚未建立本机安全会话；请运行 skills/vaultwarden-cli/setup.py 完成交互式登录与解锁",
        )
    return value


def delete_keychain_session():
    run_process(
        [SECURITY, "delete-generic-password", "-s", KEYCHAIN_SERVICE, "-a", KEYCHAIN_ACCOUNT],
        timeout=10,
        sensitive=True,
        allow_failure=True,
    )


def parse_json(text, code="INVALID_BW_RESPONSE"):
    try:
        return json.loads(text)
    except json.JSONDecodeError:
        fail(code, "官方 bw CLI 返回了无法解析的数据")


def validate_server(server):
    if not isinstance(server, str):
        fail("INVALID_SERVER", "server 必须是字符串")
    value = server.strip().rstrip("/")
    parsed = urlparse(value)
    local_hosts = {"localhost", "127.0.0.1", "::1"}
    if parsed.scheme not in {"https", "http"} or not parsed.hostname:
        fail("INVALID_SERVER", "server 必须是有效的 HTTP(S) URL")
    if parsed.scheme != "https" and parsed.hostname not in local_hosts:
        fail("INSECURE_SERVER", "非本机 Vaultwarden 必须使用 HTTPS")
    if parsed.username or parsed.password or parsed.query or parsed.fragment:
        fail("INVALID_SERVER", "server URL 不能包含账号、密码、查询参数或片段")
    return value


def safe_uri_hosts(login):
    hosts = []
    if not isinstance(login, dict):
        return hosts
    for entry in login.get("uris") or []:
        if not isinstance(entry, dict):
            continue
        uri = entry.get("uri")
        if not isinstance(uri, str):
            continue
        parsed = urlparse(uri if "://" in uri else "//" + uri)
        host = parsed.hostname
        if host and host not in hosts:
            hosts.append(host)
    return hosts[:10]


def sanitize_item(item):
    if not isinstance(item, dict):
        return {}
    fields = item.get("fields") or []
    custom_names = [
        field.get("name")
        for field in fields
        if isinstance(field, dict) and isinstance(field.get("name"), str) and field.get("name")
    ]
    return {
        "id": item.get("id"),
        "name": item.get("name"),
        "type": item.get("type"),
        "favorite": bool(item.get("favorite", False)),
        "folder_id": item.get("folderId"),
        "collection_ids": item.get("collectionIds") or [],
        "revision_date": item.get("revisionDate"),
        "login_hosts": safe_uri_hosts(item.get("login")),
        "has_username": bool((item.get("login") or {}).get("username")) if isinstance(item.get("login"), dict) else False,
        "has_password": bool((item.get("login") or {}).get("password")) if isinstance(item.get("login"), dict) else False,
        "has_totp": bool((item.get("login") or {}).get("totp")) if isinstance(item.get("login"), dict) else False,
        "custom_field_names": custom_names[:50],
    }


def clipboard_clear_worker(secret, ttl):
    code = r'''
import subprocess, sys, time
ttl = int(sys.argv[1])
secret = sys.stdin.read()
time.sleep(ttl)
current = subprocess.run(["/usr/bin/pbpaste"], text=True, capture_output=True, check=False).stdout
if current == secret:
    subprocess.run(["/usr/bin/pbcopy"], input="", text=True, check=False)
'''
    child = subprocess.Popen(
        [sys.executable, "-c", code, str(ttl)],
        stdin=subprocess.PIPE,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        text=True,
        start_new_session=True,
        close_fds=True,
    )
    try:
        child.stdin.write(secret)
        child.stdin.close()
    except Exception:
        child.kill()


def handle_status():
    version = run_bw(["--version"], timeout=10).stdout.strip()
    completed = run_bw(["status"], timeout=15, allow_failure=True)
    data = parse_json(completed.stdout) if completed.returncode == 0 and completed.stdout.strip() else {}
    session = keychain_session(required=False)
    emit({
        "ok": True,
        "skill_version": VERSION,
        "cli": find_bw(),
        "cli_version": version,
        "server_url": data.get("serverUrl"),
        "vault_status": data.get("status", "unknown"),
        "last_sync": data.get("lastSync"),
        "secure_session_available": bool(session),
    })


def handle_configure_server(args):
    server = validate_server(args.get("server"))
    run_bw(["config", "server", server], timeout=30)
    delete_keychain_session()
    emit({"ok": True, "server_url": server, "session_cleared": True})


def handle_sync():
    session = keychain_session()
    run_bw(["sync"], timeout=60, session=session, sensitive=True)
    emit({"ok": True, "synced": True})


def handle_search(args):
    query = args.get("query")
    if not isinstance(query, str) or not query.strip():
        fail("INVALID_QUERY", "query 必须是非空字符串")
    limit = args.get("limit", 20)
    if not isinstance(limit, int) or isinstance(limit, bool) or not 1 <= limit <= 50:
        fail("INVALID_LIMIT", "limit 必须是 1 到 50 的整数")
    session = keychain_session()
    completed = run_bw(["list", "items", "--search", query.strip()], timeout=60, session=session, sensitive=True)
    data = parse_json(completed.stdout)
    if not isinstance(data, list):
        fail("INVALID_BW_RESPONSE", "官方 bw CLI 未返回条目数组")
    items = [sanitize_item(item) for item in data[:limit]]
    emit({"ok": True, "query": query.strip(), "count": len(items), "items": items})


def load_item(item_id, session):
    if not isinstance(item_id, str) or not item_id.strip():
        fail("INVALID_ITEM_ID", "item_id 必须是非空字符串")
    completed = run_bw(["get", "item", item_id.strip()], timeout=60, session=session, sensitive=True)
    item = parse_json(completed.stdout)
    if not isinstance(item, dict):
        fail("INVALID_BW_RESPONSE", "官方 bw CLI 未返回条目对象")
    return item


def handle_item(args):
    session = keychain_session()
    item = load_item(args.get("item_id"), session)
    emit({"ok": True, "item": sanitize_item(item)})


def extract_secret(args, session):
    item_id = args.get("item_id")
    field = args.get("field")
    if field in {"password", "username", "totp"}:
        completed = run_bw(["get", field, item_id], timeout=60, session=session, sensitive=True)
        return completed.stdout.rstrip("\r\n")
    if field == "custom":
        field_name = args.get("field_name")
        if not isinstance(field_name, str) or not field_name.strip():
            fail("MISSING_FIELD_NAME", "field=custom 时必须提供 field_name")
        item = load_item(item_id, session)
        for entry in item.get("fields") or []:
            if isinstance(entry, dict) and entry.get("name") == field_name:
                value = entry.get("value")
                return value if isinstance(value, str) else ""
        fail("FIELD_NOT_FOUND", "未找到指定自定义字段")
    fail("INVALID_FIELD", "field 不在允许范围内")


def handle_copy_secret(args):
    session = keychain_session()
    secret = extract_secret(args, session)
    if not secret:
        fail("EMPTY_SECRET", "指定字段为空")
    ttl = args.get("clear_after_seconds", 45)
    if not isinstance(ttl, int) or isinstance(ttl, bool) or not 10 <= ttl <= 300:
        fail("INVALID_TTL", "clear_after_seconds 必须是 10 到 300 的整数")
    completed = run_process([PBCOPY], timeout=10, input_text=secret, sensitive=True, allow_failure=True)
    if completed.returncode != 0:
        fail("CLIPBOARD_FAILED", "无法写入本机剪贴板")
    clipboard_clear_worker(secret, ttl)
    emit({"ok": True, "copied": True, "field": args.get("field"), "clear_after_seconds": ttl})


def handle_lock():
    run_bw(["lock"], timeout=30, sensitive=True, allow_failure=True)
    delete_keychain_session()
    emit({"ok": True, "locked": True, "secure_session_deleted": True})


def main():
    args = load_input()
    operation = str(args.pop("skill_action", "status"))
    handlers = {
        "status": lambda: handle_status(),
        "configure-server": lambda: handle_configure_server(args),
        "sync": lambda: handle_sync(),
        "search": lambda: handle_search(args),
        "item": lambda: handle_item(args),
        "copy-secret": lambda: handle_copy_secret(args),
        "lock": lambda: handle_lock(),
    }
    handler = handlers.get(operation)
    if handler is None:
        fail("UNKNOWN_OPERATION", "未知操作", {"operation": operation})
    handler()


if __name__ == "__main__":
    main()

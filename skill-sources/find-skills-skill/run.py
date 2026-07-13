#!/usr/bin/env python3
import json
import os
import re
import shutil
import subprocess
import sys

CLI_CANDIDATES = (
    "/opt/homebrew/bin/clawhub",
    "/usr/local/bin/clawhub",
)
SEARCH_LINE = re.compile(r"^(\S+)\s{2,}(.+?)\s+\(([0-9.]+)\)$")


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


def find_cli():
    found = shutil.which("clawhub")
    if found:
        return found
    for candidate in CLI_CANDIDATES:
        if os.path.isfile(candidate) and os.access(candidate, os.X_OK):
            return candidate
    fail("CLI_NOT_FOUND", "未找到 clawhub CLI")


def run_cli(args, timeout=35):
    cli = find_cli()
    env = os.environ.copy()
    env["NO_COLOR"] = "1"
    try:
        completed = subprocess.run(
            [cli, *args],
            check=False,
            capture_output=True,
            text=True,
            timeout=timeout,
            env=env,
        )
    except subprocess.TimeoutExpired:
        fail("CLI_TIMEOUT", "clawhub 命令执行超时", {"args": args})
    if completed.returncode != 0:
        fail(
            "CLI_FAILED",
            "clawhub 命令执行失败",
            {
                "exit_code": completed.returncode,
                "stderr": completed.stderr.strip()[:4000],
                "stdout": completed.stdout.strip()[:4000],
            },
        )
    return completed.stdout.strip(), completed.stderr.strip(), cli


def handle_status():
    stdout, _, cli = run_cli(["--cli-version"], timeout=10)
    emit({"ok": True, "cli": cli, "version": stdout.strip(), "skill_version": "1.0.6"})


def handle_search(args):
    query = args.get("query")
    if not isinstance(query, str) or not query.strip():
        fail("INVALID_QUERY", "query 必须是非空字符串")
    limit = args.get("limit", 10)
    if not isinstance(limit, int) or isinstance(limit, bool) or not 1 <= limit <= 50:
        fail("INVALID_LIMIT", "limit 必须是 1 到 50 的整数")
    stdout, _, _ = run_cli(["search", query.strip(), "--limit", str(limit)])
    results = []
    for line in stdout.splitlines():
        text = line.strip()
        if not text:
            continue
        match = SEARCH_LINE.match(text)
        if match:
            results.append({"slug": match.group(1), "name": match.group(2), "score": float(match.group(3))})
        else:
            results.append({"raw": text})
    emit({"ok": True, "query": query.strip(), "count": len(results), "results": results})


def handle_explore(args):
    limit = args.get("limit", 25)
    sort = args.get("sort", "newest")
    allowed = {"newest", "downloads", "rating", "installs", "installsAllTime", "trending"}
    if not isinstance(limit, int) or isinstance(limit, bool) or not 1 <= limit <= 200:
        fail("INVALID_LIMIT", "limit 必须是 1 到 200 的整数")
    if sort not in allowed:
        fail("INVALID_SORT", "sort 不在允许范围内", {"allowed": sorted(allowed)})
    stdout, _, _ = run_cli(["explore", "--limit", str(limit), "--sort", sort, "--json"])
    try:
        data = json.loads(stdout)
    except json.JSONDecodeError as exc:
        fail("INVALID_RESPONSE", "clawhub 返回了无法解析的 JSON", {"reason": str(exc), "stdout": stdout[:4000]})
    results = data if isinstance(data, list) else data.get("items", data.get("skills", data.get("results", []))) if isinstance(data, dict) else []
    emit({"ok": True, "sort": sort, "count": len(results) if isinstance(results, list) else None, "results": results})


def handle_sources():
    emit({
        "ok": True,
        "sources": [
            {"name": "ClawHub", "url": "https://clawhub.ai", "primary": True},
            {"name": "OpenClaw Directory", "url": "https://www.openclawdirectory.dev/skills"},
            {"name": "LobeHub Skills Marketplace", "url": "https://lobehub.com/skills"},
            {"name": "GitHub", "url": "https://github.com/search?q=openclaw+skill&type=repositories"}
        ]
    })


def main():
    args = load_input()
    operation = str(args.pop("skill_action", "status"))
    if operation == "status":
        handle_status()
    elif operation == "search":
        handle_search(args)
    elif operation == "explore":
        handle_explore(args)
    elif operation == "sources":
        handle_sources()
    else:
        fail("UNKNOWN_OPERATION", "未知操作", {"operation": operation})


if __name__ == "__main__":
    main()

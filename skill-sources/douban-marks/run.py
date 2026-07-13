#!/usr/bin/env python3
from __future__ import annotations

import html
import json
import os
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path
from typing import Any


VERSION = "0.1.8"
PAGE_SIZE = 30
CATEGORIES = {"movie": "movie", "book": "book", "music": "music"}
STATUSES = {"collect": "已标记", "wish": "想看/想读/想听", "do": "在看/在读/在听"}
USER_AGENT = (
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) "
    "AppleWebKit/537.36 (KHTML, like Gecko) AgentDock-DoubanMarks/0.1.8 Safari/537.36"
)


class SkillError(RuntimeError):
    def __init__(self, code: str, message: str, details: Any = None) -> None:
        super().__init__(message)
        self.code = code
        self.details = details


def emit(value: dict[str, Any], exit_code: int = 0) -> None:
    print(json.dumps(value, ensure_ascii=False, separators=(",", ":")))
    raise SystemExit(exit_code)


def fail(code: str, message: str, details: Any = None, exit_code: int = 1) -> None:
    payload: dict[str, Any] = {"ok": False, "error": {"code": code, "message": message}}
    if details is not None:
        payload["error"]["details"] = details
    emit(payload, exit_code)


def load_input() -> dict[str, Any]:
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


def load_env(args: dict[str, Any]) -> None:
    candidates: list[Path] = []
    if args.get("env_file"):
        candidates.append(Path(str(args["env_file"])).expanduser())
    if os.environ.get("DOUBAN_ENV_FILE"):
        candidates.append(Path(os.environ["DOUBAN_ENV_FILE"]).expanduser())
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


def configured_cookie() -> str:
    return os.environ.get("DOUBAN_COOKIE", "").strip()


def resolve_user_id(args: dict[str, Any]) -> str:
    user_id = str(args.get("user_id") or os.environ.get("DOUBAN_USER_ID") or os.environ.get("DOUBAN_UID") or "").strip()
    if not user_id:
        fail("MISSING_USER_ID", "缺少 user_id；可在输入中提供，或配置 DOUBAN_USER_ID")
    if "/" in user_id or "?" in user_id or "#" in user_id:
        fail("BAD_USER_ID", "user_id 不能包含 URL 路径或查询字符")
    return user_id


def request_url(url: str, *, timeout: int = 20) -> tuple[int, str, str]:
    headers = {
        "User-Agent": USER_AGENT,
        "Accept": "text/html,application/json;q=0.9,*/*;q=0.8",
        "Accept-Language": "zh-CN,zh;q=0.9,en;q=0.7",
    }
    cookie = configured_cookie()
    if cookie:
        headers["Cookie"] = cookie
    req = urllib.request.Request(url, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as response:
            raw = response.read()
            final_url = response.geturl()
            charset = response.headers.get_content_charset() or "utf-8"
            return response.status, raw.decode(charset, errors="replace"), final_url
    except urllib.error.HTTPError as exc:
        raw = exc.read()
        charset = exc.headers.get_content_charset() or "utf-8"
        return exc.code, raw.decode(charset, errors="replace"), exc.geturl()
    except urllib.error.URLError as exc:
        fail("NETWORK_ERROR", "无法连接豆瓣", {"reason": str(exc.reason), "url": redact_url(url)})


def request_form(url: str, form: dict[str, str], *, timeout: int = 20) -> tuple[int, str, str]:
    headers = {
        "User-Agent": USER_AGENT,
        "Accept": "application/json,text/html;q=0.9,*/*;q=0.8",
        "Accept-Language": "zh-CN,zh;q=0.9,en;q=0.7",
        "Content-Type": "application/x-www-form-urlencoded; charset=UTF-8",
        "Referer": "https://movie.douban.com/subject/" + re.sub(r"\D+", "", url) + "/",
        "X-Requested-With": "XMLHttpRequest",
    }
    cookie = configured_cookie()
    if cookie:
        headers["Cookie"] = cookie
    body = urllib.parse.urlencode(form).encode("utf-8")
    req = urllib.request.Request(url, data=body, headers=headers, method="POST")
    try:
        with urllib.request.urlopen(req, timeout=timeout) as response:
            raw = response.read()
            charset = response.headers.get_content_charset() or "utf-8"
            return response.status, raw.decode(charset, errors="replace"), response.geturl()
    except urllib.error.HTTPError as exc:
        raw = exc.read()
        charset = exc.headers.get_content_charset() or "utf-8"
        return exc.code, raw.decode(charset, errors="replace"), exc.geturl()
    except urllib.error.URLError as exc:
        fail("NETWORK_ERROR", "无法连接豆瓣", {"reason": str(exc.reason), "url": redact_url(url)})


def redact_url(url: str) -> str:
    return url.replace(configured_cookie(), "<redacted>") if configured_cookie() else url


def validate_subject_id(args: dict[str, Any]) -> str:
    subject_id = str(args.get("subject_id") or "").strip()
    if not re.fullmatch(r"\d+", subject_id):
        fail("BAD_SUBJECT_ID", "subject_id 必须是数字")
    return subject_id


def cookie_ck() -> str | None:
    cookie = configured_cookie()
    match = re.search(r"(?:^|;)\s*ck=([^;]+)", cookie)
    return urllib.parse.unquote(match.group(1)) if match else None


def interest_url(subject_id: str) -> str:
    return f"https://movie.douban.com/j/subject/{subject_id}/interest"


def clean_text(value: str) -> str:
    value = re.sub(r"<script\b.*?</script>", "", value, flags=re.I | re.S)
    value = re.sub(r"<style\b.*?</style>", "", value, flags=re.I | re.S)
    value = re.sub(r"<[^>]+>", " ", value)
    value = html.unescape(value)
    return re.sub(r"\s+", " ", value).strip()


def parse_subject_count(text: str) -> dict[str, int | None]:
    match = re.search(r'<span\s+class="subject-num">\s*(\d+)\s*-\s*(\d+)\s*&nbsp;\s*/\s*&nbsp;\s*([\d,]+)', text, re.S)
    if not match:
        match = re.search(r'<span\s+class="subject-num">\s*(\d+)\s*-\s*(\d+)\s*/\s*([\d,]+)', text, re.S)
    if not match:
        return {"page_start": None, "page_end": None, "total": None}
    return {
        "page_start": int(match.group(1)),
        "page_end": int(match.group(2)),
        "total": int(match.group(3).replace(",", "")),
    }


def parse_rating(block: str) -> int | None:
    match = re.search(r'class="rating(\d)-t"', block)
    return int(match.group(1)) if match else None


def parse_tags(block: str) -> list[str]:
    match = re.search(r'<span\s+class="tags">\s*标签:\s*(.*?)</span>', block, re.S)
    if not match:
        return []
    return [part for part in clean_text(match.group(1)).split() if part]


def parse_date(block: str) -> str | None:
    match = re.search(r'<div\s+class="date">\s*(.*?)</div>', block, re.S)
    if not match:
        return None
    text = re.sub(r'<span\s+class="rating\d-t"></span>\s*(?:&nbsp;)*', "", match.group(1), flags=re.S)
    cleaned = clean_text(text)
    date_match = re.search(r"\d{4}-\d{2}-\d{2}", cleaned)
    return date_match.group(0) if date_match else (cleaned or None)


def parse_comment(block: str) -> str | None:
    match = re.search(r'<div\s+class="comment">\s*(.*?)</div>', block, re.S)
    if not match:
        return None
    text = re.sub(r'<span\s+class="pl">.*?</span>', "", match.group(1), flags=re.S)
    return clean_text(text) or None


def parse_items(text: str, category: str, status: str, include_comments: bool) -> list[dict[str, Any]]:
    items: list[dict[str, Any]] = []
    pattern = re.compile(r'<li\s+id="list([^"]+)"\s+class="item">(.*?)</li>', re.S)
    for match in pattern.finditer(text):
        block = match.group(2)
        link = re.search(r'<div\s+class="title">\s*<a\s+href="([^"]+)">\s*(.*?)\s*</a>', block, re.S)
        if not link:
            continue
        url = html.unescape(link.group(1)).strip()
        subject_match = re.search(r"/subject/(\d+)/", url)
        subject_id = subject_match.group(1) if subject_match else match.group(1)
        item: dict[str, Any] = {
            "id": subject_id,
            "title": clean_text(link.group(2)),
            "url": url,
            "category": category,
            "status": status,
            "marked_at": parse_date(block),
            "rating": parse_rating(block),
            "tags": parse_tags(block),
        }
        intro = re.search(r'<span\s+class="intro">(.*?)</span>', block, re.S)
        if intro:
            item["intro"] = clean_text(intro.group(1))
        if include_comments:
            item["comment"] = parse_comment(block)
        items.append(item)
    return items


def detect_blocked_or_private(status: int, text: str, final_url: str) -> tuple[str | None, str | None]:
    lowered = text.lower()
    if status in {401, 403}:
        return "FORBIDDEN", f"HTTP {status}"
    if "sec.douban.com" in final_url or "检测到有异常请求" in text or "captcha" in lowered:
        return "CAPTCHA_OR_RATE_LIMIT", "豆瓣要求验证码或触发了访问限制"
    if "没有权限" in text or "仅自己可见" in text or "这个页面不存在" in text:
        return "PRIVATE_OR_NOT_FOUND", "列表不可公开访问，或用户/列表不存在"
    if "登录豆瓣" in text and "dbcl2" not in configured_cookie():
        return "LOGIN_REQUIRED", "可能需要登录态 DOUBAN_COOKIE"
    return None, None


def marks_url(category: str, user_id: str, status: str, start: int) -> str:
    host = CATEGORIES[category] + ".douban.com"
    safe_user = urllib.parse.quote(user_id, safe="")
    query = urllib.parse.urlencode(
        {
            "start": start,
            "sort": "time",
            "rating": "all",
            "filter": "all",
            "mode": "list",
        }
    )
    return f"https://{host}/people/{safe_user}/{status}?{query}"


def op_marks(args: dict[str, Any], *, forced_status: str | None = None) -> None:
    category = str(args.get("category") or "movie")
    status = forced_status or str(args.get("status") or "collect")
    if category not in CATEGORIES:
        fail("BAD_CATEGORY", "category 必须是 movie/book/music")
    if status not in STATUSES:
        fail("BAD_STATUS", "status 必须是 collect/wish/do")

    user_id = resolve_user_id(args)
    start = max(0, int(args.get("start", 0)))
    count = min(100, max(1, int(args.get("count", 30))))
    max_pages = min(10, max(1, int(args.get("max_pages", 1))))
    include_comments = bool(args.get("include_comments", True))

    results: list[dict[str, Any]] = []
    total: int | None = None
    page_meta: dict[str, int | None] = {"page_start": None, "page_end": None, "total": None}
    fetched_urls: list[str] = []

    page_start = start
    for page_index in range(max_pages):
        url = marks_url(category, user_id, status, page_start)
        http_status, text, final_url = request_url(url)
        fetched_urls.append(redact_url(final_url))
        blocked_code, blocked_message = detect_blocked_or_private(http_status, text, final_url)
        if blocked_code:
            fail(blocked_code, blocked_message or "豆瓣列表不可读取", {"status": http_status, "url": redact_url(final_url)})
        page_meta = parse_subject_count(text)
        if page_meta["total"] is not None:
            total = int(page_meta["total"])
        results.extend(parse_items(text, category, status, include_comments))
        if len(results) >= count:
            break
        if not results:
            break
        page_start += PAGE_SIZE
        if total is not None and page_start >= total:
            break
        if page_index + 1 < max_pages:
            time.sleep(0.6)

    results = results[:count]
    next_start = start + len(results)
    if total is not None and next_start >= total:
        next_start = None
    emit(
        {
            "ok": True,
            "category": category,
            "status": status,
            "status_label": STATUSES[status],
            "user_id": user_id,
            "start": start,
            "count": len(results),
            "total": total,
            "next_start": next_start,
            "cookie_configured": bool(configured_cookie()),
            "items": results,
            "source_urls": fetched_urls,
            "page": page_meta,
        }
    )


def op_movie_suggest(args: dict[str, Any]) -> None:
    q = str(args.get("q") or "").strip()
    if not q:
        fail("MISSING_QUERY", "缺少 q")
    count = min(20, max(1, int(args.get("count", 10))))
    query = urllib.parse.urlencode({"q": q})
    url = f"https://movie.douban.com/j/subject_suggest?{query}"
    status, text, final_url = request_url(url)
    if status != 200:
        fail("HTTP_ERROR", "豆瓣电影联想接口返回 HTTP 错误", {"status": status, "url": redact_url(final_url)})
    try:
        data = json.loads(text)
    except json.JSONDecodeError as exc:
        fail("INVALID_RESPONSE", "豆瓣电影联想接口返回非 JSON", {"reason": str(exc)})
    if not isinstance(data, list):
        fail("INVALID_RESPONSE", "豆瓣电影联想接口返回结构异常")
    items = []
    for row in data[:count]:
        if not isinstance(row, dict):
            continue
        items.append(
            {
                "id": row.get("id"),
                "title": row.get("title"),
                "sub_title": row.get("sub_title"),
                "year": row.get("year"),
                "type": row.get("type"),
                "url": row.get("url"),
                "image": row.get("img"),
            }
        )
    emit({"ok": True, "q": q, "count": len(items), "items": items, "source_url": redact_url(final_url)})


def op_status(args: dict[str, Any]) -> None:
    status, text, final_url = request_url("https://movie.douban.com/j/subject_suggest?q=%E6%B5%8B%E8%AF%95", timeout=10)
    reachable = False
    try:
        reachable = status == 200 and isinstance(json.loads(text), list)
    except Exception:
        reachable = False
    emit(
        {
            "ok": reachable,
            "skill_version": VERSION,
            "douban_reachable": reachable,
            "http_status": status,
            "cookie_configured": bool(configured_cookie()),
            "user_id_configured": bool(os.environ.get("DOUBAN_USER_ID") or os.environ.get("DOUBAN_UID")),
            "source_url": redact_url(final_url),
        },
        0 if reachable else 1,
    )


def parse_json_or_text(text: str) -> Any:
    stripped = text.strip()
    if stripped[:1] in {"{", "["}:
        try:
            return json.loads(stripped)
        except json.JSONDecodeError:
            pass
    return stripped[:2000]


def normalize_tags(value: Any) -> str:
    if value is None:
        return ""
    if isinstance(value, list):
        return " ".join(str(item).strip() for item in value if str(item).strip())
    return str(value).strip()


def build_interest_form(args: dict[str, Any], interest: str, *, require_rating: bool = False) -> dict[str, str]:
    if interest not in {"wish", "do", "collect"}:
        fail("BAD_INTEREST", "interest 必须是 wish/do/collect")
    form = {
        "interest": interest,
        "foldcollect": "F",
        "tags": normalize_tags(args.get("tags")),
        "comment": str(args.get("comment") or "").strip(),
    }
    ck = cookie_ck()
    if ck:
        form["ck"] = ck
    if bool(args.get("private", True)):
        form["private"] = "on"
    rating = args.get("rating")
    if rating is not None:
        try:
            rating_int = int(rating)
        except (TypeError, ValueError):
            fail("BAD_RATING", "rating 必须是 1-5 的整数")
        if rating_int < 1 or rating_int > 5:
            fail("BAD_RATING", "rating 必须是 1-5 的整数")
        form["rating"] = str(rating_int)
    elif require_rating:
        fail("MISSING_RATING", "缺少 rating")
    return {key: value for key, value in form.items() if value != ""}


def redacted_form(form: dict[str, str]) -> dict[str, str]:
    result = dict(form)
    if "ck" in result:
        result["ck"] = "<redacted>"
    return result


def op_movie_interest_status(args: dict[str, Any]) -> None:
    subject_id = validate_subject_id(args)
    if not configured_cookie():
        fail("MISSING_COOKIE", "需要 DOUBAN_COOKIE 才能读取登录态标记弹窗", {"subject_id": subject_id})
    status, text, final_url = request_url(interest_url(subject_id), timeout=20)
    blocked_code, blocked_message = detect_blocked_or_private(status, text, final_url)
    parsed = parse_json_or_text(text)
    if status == 200 and isinstance(parsed, dict) and parsed.get("html"):
        blocked_code, blocked_message = None, None
    ok = status == 200 and not blocked_code
    emit(
        {
            "ok": ok,
            "subject_id": subject_id,
            "http_status": status,
            "cookie_configured": True,
            "ck_configured": bool(cookie_ck()),
            "source_url": redact_url(final_url),
            "response": parsed,
            "blocked_code": blocked_code,
            "message": blocked_message,
        },
        0 if ok else 1,
    )


def op_movie_interest_write(args: dict[str, Any], interest: str, *, require_rating: bool = False) -> None:
    subject_id = validate_subject_id(args)
    dry_run = bool(args.get("dry_run", True))
    confirm = bool(args.get("confirm", False))
    url = interest_url(subject_id)
    form = build_interest_form(args, interest, require_rating=require_rating)
    prepared = {
        "ok": True,
        "dry_run": dry_run,
        "subject_id": subject_id,
        "interest": interest,
        "endpoint": url,
        "cookie_configured": bool(configured_cookie()),
        "ck_configured": bool(cookie_ck()),
        "form": redacted_form(form),
    }
    if dry_run:
        emit(prepared)
    if not confirm:
        fail("CONFIRM_REQUIRED", "真实写入必须设置 dry_run=false 且 confirm=true", prepared)
    if not configured_cookie():
        fail("MISSING_COOKIE", "真实写入需要 DOUBAN_COOKIE", prepared)
    if "ck" not in form:
        fail("MISSING_CK", "DOUBAN_COOKIE 中缺少 ck，无法提交豆瓣写操作", prepared)

    status, text, final_url = request_form(url, form, timeout=20)
    parsed = parse_json_or_text(text)
    response_ok = False
    if isinstance(parsed, dict):
        response_ok = status == 200 and parsed.get("r") in (0, "0", None)
    emit(
        {
            "ok": response_ok,
            "subject_id": subject_id,
            "interest": interest,
            "http_status": status,
            "source_url": redact_url(final_url),
            "response": parsed,
        },
        0 if response_ok else 1,
    )


def main() -> None:
    args = load_input()
    load_env(args)
    operation = str(args.pop("skill_action", "status"))
    try:
        if operation == "status":
            op_status(args)
        if operation == "movie-suggest":
            op_movie_suggest(args)
        if operation == "marks":
            op_marks(args)
        if operation == "movie-watched":
            args = dict(args)
            args["category"] = "movie"
            op_marks(args, forced_status="collect")
        if operation == "movie-wish":
            args = dict(args)
            args["category"] = "movie"
            op_marks(args, forced_status="wish")
        if operation == "movie-doing":
            args = dict(args)
            args["category"] = "movie"
            op_marks(args, forced_status="do")
        if operation == "movie-interest-status":
            op_movie_interest_status(args)
        if operation == "movie-mark-wish":
            op_movie_interest_write(args, "wish")
        if operation == "movie-mark-doing":
            op_movie_interest_write(args, "do")
        if operation == "movie-mark-watched":
            op_movie_interest_write(args, "collect")
        if operation == "movie-rate":
            op_movie_interest_write(args, "collect", require_rating=True)
        fail("UNKNOWN_OPERATION", "未知操作", {"operation": operation})
    except SkillError as exc:
        fail(exc.code, str(exc), exc.details)


if __name__ == "__main__":
    main()

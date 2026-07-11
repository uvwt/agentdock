#!/usr/bin/env python3
import html
import json
import os
import re
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import xml.etree.ElementTree as ET

SKILL = os.environ.get("SKILL_NAME", "rsshub")
ACTION = sys.argv[1] if len(sys.argv) > 1 else "status"
BASE_URL = os.environ.get("RSSHUB_BASE_URL", "http://127.0.0.1:1200").rstrip("/")
SAFE_BASES = ("http://127.0.0.1:", "http://localhost:")
MAX_READ_BYTES = 2 * 1024 * 1024


def load_args():
    raw = os.environ.get("SKILL_ARGS_JSON") or "{}"
    data = json.loads(raw)
    if not isinstance(data, dict):
        raise ValueError("args must be a JSON object")
    return data


def ensure_base_url():
    if not BASE_URL.startswith(SAFE_BASES):
        raise ValueError("RSSHUB_BASE_URL must point to localhost or 127.0.0.1")


def normalize_route(route):
    if not isinstance(route, str) or not route.strip():
        raise ValueError("route must be a non-empty string starting with /")
    route = route.strip()
    if not route.startswith("/"):
        raise ValueError("route must start with /")
    if route.startswith("//") or "://" in route or "\\" in route or ".." in route:
        raise ValueError("unsafe route path")
    if any(ch in route for ch in ["\x00", "\r", "\n", "#"]):
        raise ValueError("unsafe route characters")
    return route


def normalize_query(query):
    if query is None:
        return {}
    if not isinstance(query, dict):
        raise ValueError("query must be an object")
    normalized = {}
    for key, value in query.items():
        if not isinstance(key, str) or not key:
            raise ValueError("query keys must be non-empty strings")
        if any(ch in key for ch in ["\x00", "\r", "\n"]):
            raise ValueError("unsafe query key")
        if value is None:
            continue
        if isinstance(value, (str, int, float, bool)):
            normalized[key] = str(value).lower() if isinstance(value, bool) else value
        elif isinstance(value, list):
            vals = []
            for item in value:
                if item is None:
                    continue
                if not isinstance(item, (str, int, float, bool)):
                    raise ValueError("query array values must be scalar")
                vals.append(str(item).lower() if isinstance(item, bool) else item)
            normalized[key] = vals
        else:
            raise ValueError("query values must be scalar or arrays of scalar values")
    return normalized


def build_url(route, query=None):
    ensure_base_url()
    route = normalize_route(route)
    query = normalize_query(query)
    url = BASE_URL + route
    if query:
        sep = "&" if "?" in url else "?"
        url += sep + urllib.parse.urlencode(query, doseq=True)
    return url


def http_request(route="/", query=None, method="GET", timeout=20):
    url = build_url(route, query)
    req = urllib.request.Request(
        url,
        method=method.upper(),
        headers={
            "Accept": "application/rss+xml, application/atom+xml, application/xml, text/xml, text/html, */*",
            "User-Agent": "AgentDock-RSSHub-Skill/0.1.2",
        },
    )
    started = time.time()
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            raw_bytes = resp.read(MAX_READ_BYTES + 1)
            truncated = len(raw_bytes) > MAX_READ_BYTES
            if truncated:
                raw_bytes = raw_bytes[:MAX_READ_BYTES]
            raw = raw_bytes.decode("utf-8", errors="replace")
            return {
                "ok": 200 <= resp.status < 300,
                "status": resp.status,
                "url": url,
                "content_type": resp.headers.get("Content-Type", ""),
                "duration_ms": int((time.time() - started) * 1000),
                "body": raw,
                "bytes_read": len(raw_bytes),
                "truncated_by_reader": truncated,
            }
    except urllib.error.HTTPError as e:
        raw = e.read(MAX_READ_BYTES).decode("utf-8", errors="replace")
        return {
            "ok": False,
            "status": e.code,
            "url": url,
            "content_type": e.headers.get("Content-Type", "") if e.headers else "",
            "duration_ms": int((time.time() - started) * 1000),
            "body": raw,
            "error": raw[:2000],
        }


def tag_name(elem):
    return elem.tag.split("}", 1)[-1].lower() if isinstance(elem.tag, str) else ""


def child_text(elem, names):
    wanted = {n.lower() for n in names}
    for child in list(elem):
        if tag_name(child) in wanted:
            return (child.text or "").strip()
    return ""


def atom_link(elem):
    for child in list(elem):
        if tag_name(child) == "link":
            href = child.attrib.get("href", "").strip()
            if href:
                return href
    return child_text(elem, ["link"])


def clean_text(text, max_chars=1000):
    text = text or ""
    text = re.sub(r"<[^>]+>", " ", text)
    text = html.unescape(text)
    text = re.sub(r"\s+", " ", text).strip()
    if max_chars and len(text) > max_chars:
        return text[:max_chars] + "..."
    return text


def parse_feed_xml(raw, limit=10, include_content=False, content_max_chars=1000):
    root = ET.fromstring(raw)
    root_name = tag_name(root)
    result = {"feed_type": root_name, "channel": {}, "items": []}

    if root_name == "rss" or root.find("channel") is not None:
        channel = root.find("channel")
        if channel is None:
            channel = root
        result["feed_type"] = "rss"
        result["channel"] = {
            "title": child_text(channel, ["title"]),
            "link": child_text(channel, ["link"]),
            "description": clean_text(child_text(channel, ["description"]), 1000),
        }
        entries = channel.findall("item")
        for item in entries[:limit]:
            description = child_text(item, ["description", "summary"])
            content = child_text(item, ["encoded", "content"])
            entry = {
                "title": child_text(item, ["title"]),
                "link": child_text(item, ["link"]),
                "guid": child_text(item, ["guid", "id"]),
                "published": child_text(item, ["pubDate", "published", "updated"]),
                "author": child_text(item, ["author", "creator"]),
                "summary": clean_text(description or content, content_max_chars),
            }
            if include_content:
                entry["content"] = clean_text(content or description, content_max_chars)
            result["items"].append(entry)
        result["item_count"] = len(entries)
        return result

    if root_name == "feed":
        result["feed_type"] = "atom"
        result["channel"] = {
            "title": child_text(root, ["title"]),
            "link": atom_link(root),
            "description": clean_text(child_text(root, ["subtitle", "description"]), 1000),
        }
        entries = [child for child in list(root) if tag_name(child) == "entry"]
        for item in entries[:limit]:
            summary = child_text(item, ["summary", "content"])
            entry = {
                "title": child_text(item, ["title"]),
                "link": atom_link(item),
                "guid": child_text(item, ["id"]),
                "published": child_text(item, ["published", "updated"]),
                "author": child_text(item, ["author", "name"]),
                "summary": clean_text(summary, content_max_chars),
            }
            if include_content:
                entry["content"] = clean_text(child_text(item, ["content", "summary"]), content_max_chars)
            result["items"].append(entry)
        result["item_count"] = len(entries)
        return result

    raise ValueError(f"unsupported feed XML root: {root_name}")


def docker_line(name):
    try:
        proc = subprocess.run(
            ["docker", "ps", "-a", "--filter", f"name={name}", "--format", "{{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}"],
            text=True,
            capture_output=True,
            timeout=10,
        )
        return proc.stdout.strip()
    except Exception as exc:
        return f"docker_status_error: {exc}"


def docker_health(name):
    try:
        proc = subprocess.run(
            ["docker", "inspect", "-f", "{{.State.Health.Status}}", name],
            text=True,
            capture_output=True,
            timeout=10,
        )
        if proc.returncode == 0:
            return proc.stdout.strip()
        return "unknown"
    except Exception:
        return "unknown"


def action_status(_args):
    root = http_request("/", method="GET", timeout=10)
    return {
        "ok": root.get("ok", False),
        "skill": SKILL,
        "action": "status",
        "base_url": BASE_URL,
        "http": {k: v for k, v in root.items() if k != "body"},
        "docker": {
            "rsshub": docker_line("rsshub"),
            "redis": docker_line("rsshub-redis"),
            "redis_health": docker_health("rsshub-redis"),
        },
    }


def action_build_url(args):
    return {"ok": True, "skill": SKILL, "action": "buildUrl", "url": build_url(args["route"], args.get("query"))}


def action_fetch_feed(args):
    max_chars = int(args.get("max_chars", 20000))
    max_chars = max(1, min(max_chars, 200000))
    resp = http_request(args["route"], args.get("query"), timeout=30)
    body = resp.pop("body", "")
    truncated = len(body) > max_chars
    return {
        "ok": resp.get("ok", False),
        "skill": SKILL,
        "action": "fetchFeed",
        **resp,
        "body": body[:max_chars],
        "body_chars": len(body),
        "truncated": truncated,
    }


def action_parse_feed(args):
    limit = max(1, min(int(args.get("limit", 10)), 100))
    include_content = bool(args.get("include_content", False))
    content_max_chars = max(0, min(int(args.get("content_max_chars", 1000)), 50000))
    resp = http_request(args["route"], args.get("query"), timeout=30)
    raw = resp.pop("body", "")
    parsed = parse_feed_xml(raw, limit=limit, include_content=include_content, content_max_chars=content_max_chars) if resp.get("ok") else None
    return {
        "ok": bool(resp.get("ok") and parsed),
        "skill": SKILL,
        "action": "parseFeed",
        **resp,
        "feed": parsed,
    }


def action_probe_route(args):
    sample_limit = max(1, min(int(args.get("sample_limit", 3)), 20))
    resp = http_request(args["route"], args.get("query"), timeout=30)
    raw = resp.pop("body", "")
    probe = None
    parse_error = None
    if resp.get("ok"):
        try:
            parsed = parse_feed_xml(raw, limit=sample_limit, include_content=False, content_max_chars=300)
            probe = {
                "feed_type": parsed.get("feed_type"),
                "title": parsed.get("channel", {}).get("title", ""),
                "link": parsed.get("channel", {}).get("link", ""),
                "item_count": parsed.get("item_count", 0),
                "sample_titles": [item.get("title", "") for item in parsed.get("items", [])],
            }
        except Exception as exc:
            parse_error = str(exc)
    return {
        "ok": bool(resp.get("ok") and probe),
        "skill": SKILL,
        "action": "probeRoute",
        **resp,
        "probe": probe,
        "parse_error": parse_error,
    }


ACTIONS = {
    "status": action_status,
    "buildUrl": action_build_url,
    "fetchFeed": action_fetch_feed,
    "parseFeed": action_parse_feed,
    "probeRoute": action_probe_route,
}


def main():
    try:
        args = load_args()
        if ACTION not in ACTIONS:
            raise ValueError(f"unknown action: {ACTION}")
        result = ACTIONS[ACTION](args)
    except Exception as exc:
        result = {"ok": False, "skill": SKILL, "action": ACTION, "error": str(exc)}
    print(json.dumps(result, ensure_ascii=False, separators=(",", ":")))


if __name__ == "__main__":
    main()

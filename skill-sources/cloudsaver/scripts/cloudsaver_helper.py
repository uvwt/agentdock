#!/usr/bin/env python3
import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

ACTION = sys.argv[1] if len(sys.argv) > 1 else "status"
SKILL = os.environ.get("SKILL_NAME", "cloudsaver")
BASE_URL = os.environ.get("CLOUDSAVER_BASE_URL", "http://127.0.0.1:8008").rstrip("/")
AGENTDOCK_HOME = os.environ.get("AGENTDOCK_HOME", os.path.expanduser("~/.agentdock"))
DATA_DIR = os.environ.get("CLOUDSAVER_DATA_DIR", os.path.join(AGENTDOCK_HOME, "skill-data", "cloudsaver"))
TOKEN_FILE = os.environ.get("CLOUDSAVER_TOKEN_FILE", os.path.join(DATA_DIR, "token"))
STATS_TOKEN_FILE = os.path.join(DATA_DIR, "stats-token")
ENV_FILE = os.environ.get("CLOUDSAVER_ENV_FILE", os.path.join(DATA_DIR, ".env"))

SAFE_BASES = ("http://127.0.0.1:", "http://localhost:")


def load_args():
    raw = os.environ.get("SKILL_ARGS_JSON") or "{}"
    data = json.loads(raw)
    if not isinstance(data, dict):
        raise ValueError("args must be object")
    return data


def redact_value(v):
    if isinstance(v, str) and len(v) > 16:
        return v[:6] + "..." + v[-4:]
    return "***"


def safe_output(obj):
    if isinstance(obj, dict):
        out = {}
        for k, v in obj.items():
            if k.lower() in {"password", "token", "authorization", "cloud115cookie", "quarkcookie"}:
                out[k] = redact_value(v)
            else:
                out[k] = safe_output(v)
        return out
    if isinstance(obj, list):
        return [safe_output(x) for x in obj]
    return obj


def ensure_base():
    if not BASE_URL.startswith(SAFE_BASES):
        raise ValueError("CLOUDSAVER_BASE_URL must be localhost/127.0.0.1 for this skill")


def read_env_file(path=ENV_FILE):
    env = {}
    try:
        if not path or not os.path.exists(path):
            return env
        with open(path, "r", encoding="utf-8") as f:
            for raw_line in f:
                line = raw_line.strip()
                if not line or line.startswith("#") or "=" not in line:
                    continue
                key, value = line.split("=", 1)
                key = key.strip()
                value = value.strip().strip('"').strip("'")
                if key:
                    env[key] = value
    except OSError:
        return {}
    return env


def env_credentials():
    file_env = read_env_file()
    username = os.environ.get("CLOUDSAVER_USERNAME") or file_env.get("CLOUDSAVER_USERNAME")
    password = os.environ.get("CLOUDSAVER_PASSWORD") or file_env.get("CLOUDSAVER_PASSWORD")
    if username and password:
        return {"username": username, "password": password}
    return None


def normalize_token(token):
    token = str(token or "").strip()
    if token.lower().startswith("bearer "):
        token = token.split(None, 1)[1].strip()
    return token


def write_saved_token(token):
    token = normalize_token(token)
    if not token:
        return False
    targets = [TOKEN_FILE, STATS_TOKEN_FILE]
    os.makedirs(DATA_DIR, exist_ok=True)
    for path in targets:
        if not path:
            continue
        directory = os.path.dirname(path)
        if directory:
            os.makedirs(directory, exist_ok=True)
        with open(path, "w", encoding="utf-8") as f:
            f.write(token + "\n")
        try:
            os.chmod(path, 0o600)
        except OSError:
            pass
    return True


def read_saved_token():
    for path in (TOKEN_FILE, STATS_TOKEN_FILE):
        try:
            if path and os.path.exists(path):
                token = normalize_token(open(path, "r", encoding="utf-8").read())
                if token:
                    return token
        except OSError:
            pass
    return None


def token_headers(args):
    headers = {"Content-Type": "application/json", "Accept": "application/json"}
    if args.get("_no_token"):
        return headers
    token = args.get("token") or os.environ.get("CLOUDSAVER_TOKEN") or read_saved_token()
    if token:
        headers["Authorization"] = "Bearer " + normalize_token(token)
    return headers


def request(method, path, *, params=None, body=None, args=None, timeout=20):
    ensure_base()
    args = args or {}
    params = {k: v for k, v in (params or {}).items() if v is not None and v != ""}
    url = BASE_URL + path
    if params:
        url += "?" + urllib.parse.urlencode(params, doseq=True)
    data = None
    if body is not None:
        data = json.dumps(body, ensure_ascii=False).encode("utf-8")
    req = urllib.request.Request(url, data=data, method=method.upper(), headers=token_headers(args))
    started = time.time()
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            raw = resp.read().decode("utf-8", errors="replace")
            ctype = resp.headers.get("Content-Type", "")
            parsed = None
            if "json" in ctype or raw.strip().startswith(("{", "[")):
                try:
                    parsed = json.loads(raw)
                except Exception:
                    parsed = None
            return {
                "ok": 200 <= resp.status < 300,
                "status": resp.status,
                "duration_ms": int((time.time() - started) * 1000),
                "data": parsed if parsed is not None else raw[:5000]
            }
    except urllib.error.HTTPError as e:
        raw = e.read().decode("utf-8", errors="replace")
        try:
            parsed = json.loads(raw)
        except Exception:
            parsed = raw[:5000]
        return {"ok": False, "status": e.code, "duration_ms": int((time.time() - started) * 1000), "error": parsed}
    except (urllib.error.URLError, TimeoutError, OSError) as e:
        return {"ok": False, "status": 0, "duration_ms": int((time.time() - started) * 1000), "error": {"type": type(e).__name__, "message": str(e)}}


def refresh_saved_token():
    creds = env_credentials()
    if not creds:
        return None, {"ok": False, "status": 401, "error": "missing CLOUDSAVER_USERNAME/CLOUDSAVER_PASSWORD"}
    login_resp = request("POST", "/api/user/login", body=creds, args={"_no_token": True}, timeout=15)
    if not login_resp.get("ok"):
        return None, login_resp
    token = extract_token(login_resp)
    if not token:
        return None, {"ok": False, "status": login_resp.get("status"), "error": "login succeeded but token was not found"}
    write_saved_token(token)
    return normalize_token(token), login_resp


def request_with_auto_login(method, path, *, params=None, body=None, args=None, timeout=20):
    result = request(method, path, params=params, body=body, args=args, timeout=timeout)
    if result.get("status") != 401:
        return result
    token, login_resp = refresh_saved_token()
    if not token:
        result["auto_login"] = {"attempted": True, "ok": False, "status": login_resp.get("status") if isinstance(login_resp, dict) else None}
        return result
    retry_args = dict(args or {})
    retry_args["token"] = token
    retry_result = request(method, path, params=params, body=body, args=retry_args, timeout=timeout)
    retry_result["auto_login"] = {"attempted": True, "ok": True, "token_saved": True}
    return retry_result


def pop_token(args):
    return {k: v for k, v in args.items() if k != "token"}


def status(_):
    curl = request("HEAD", "/", timeout=10)
    docker = subprocess.run(
        ["docker", "ps", "--filter", "name=cloud-saver", "--format", "{{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}"],
        text=True,
        capture_output=True,
        timeout=10,
    )
    return {"ok": curl.get("ok", False), "skill": SKILL, "action": "status", "base_url": BASE_URL, "http": curl, "docker_ps": docker.stdout.strip()}


def login(args):
    return request("POST", "/api/user/login", body={"username": args["username"], "password": args["password"]}, args={"_no_token": True}, timeout=15)


def register(args):
    return request("POST", "/api/user/register", body={"username": args["username"], "password": args["password"], "registerCode": args["registerCode"]}, args=args, timeout=15)


def search(args):
    keyword = str(args.get("keyword") or "").strip()
    if not keyword:
        return {"ok": False, "status": 400, "error": "keyword is required"}
    return request_with_auto_login("GET", "/api/search", params={"keyword": keyword, "channelId": args.get("channelId", ""), "lastMessageId": args.get("lastMessageId", "")}, args=args, timeout=55)


def extract_token(obj):
    token_keys = {"token", "accesstoken", "access_token", "jwt"}
    if isinstance(obj, dict):
        for k, v in obj.items():
            if k.lower() in token_keys and isinstance(v, str) and v:
                return v
        for v in obj.values():
            token = extract_token(v)
            if token:
                return token
    elif isinstance(obj, list):
        for item in obj:
            token = extract_token(item)
            if token:
                return token
    return None


def search_with_login(args):
    token, login_resp = refresh_saved_token()
    if not token:
        return login_resp
    search_args = dict(args)
    search_args["token"] = token
    search_args.pop("username", None)
    search_args.pop("password", None)
    return search(search_args)


def douban_hot(args):
    params = {
        "type": args.get("type", "全部"),
        "category": args.get("category", "热门"),
        "api": args.get("api", "movie"),
        "limit": args.get("limit", 50),
    }
    if args.get("start") is not None:
        params["start"] = args["start"]
    return request_with_auto_login("GET", "/api/douban/hot", params=params, args=args, timeout=30)


def get_setting(args):
    return request_with_auto_login("GET", "/api/setting/get", args=args, timeout=15)


def save_setting(args):
    body = {"userSettings": args.get("userSettings"), "globalSetting": args.get("globalSetting")}
    return request_with_auto_login("POST", "/api/setting/save", body=body, args=args, timeout=15)


def cloud_share_info(args, kind):
    return request_with_auto_login("GET", f"/api/{kind}/share-info", params={"shareCode": args["shareCode"], "receiveCode": args.get("receiveCode")}, args=args, timeout=30)


def cloud_folders(args, kind):
    return request_with_auto_login("GET", f"/api/{kind}/folders", params={"parentCid": args.get("parentCid", "0")}, args=args, timeout=30)


def cloud_save(args, kind):
    body = pop_token(args)
    return request_with_auto_login("POST", f"/api/{kind}/save", body=body, args=args, timeout=60)


def sponsors(args):
    return request("GET", "/api/sponsors", params={"timestamp": int(time.time() * 1000)}, args=args, timeout=15)


def tele_images(args):
    return request("GET", "/api/tele-images", args=args, timeout=15)


ACTIONS = {
    "status": status,
    "login": login,
    "register": register,
    "search": search,
    "searchWithLogin": search_with_login,
    "doubanHot": douban_hot,
    "getSetting": get_setting,
    "saveSetting": save_setting,
    "cloud115ShareInfo": lambda a: cloud_share_info(a, "cloud115"),
    "cloud115Folders": lambda a: cloud_folders(a, "cloud115"),
    "cloud115Save": lambda a: cloud_save(a, "cloud115"),
    "quarkShareInfo": lambda a: cloud_share_info(a, "quark"),
    "quarkFolders": lambda a: cloud_folders(a, "quark"),
    "quarkSave": lambda a: cloud_save(a, "quark"),
    "sponsors": sponsors,
    "teleImages": tele_images,
}

try:
    if ACTION not in ACTIONS:
        raise ValueError("unknown action: " + ACTION)
    result = ACTIONS[ACTION](load_args())
    result.setdefault("skill", SKILL)
    result.setdefault("action", ACTION)
    print(json.dumps(safe_output(result), ensure_ascii=False))
except Exception as e:
    print(json.dumps({"ok": False, "skill": SKILL, "action": ACTION, "error": {"type": type(e).__name__, "message": str(e)}}, ensure_ascii=False))

#!/usr/bin/env python3
import base64
import hashlib
import json
import os
import secrets
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from collections import Counter
from pathlib import Path

VERSION = "0.1.7"
AGENTDOCK_HOME = Path(os.environ.get("AGENTDOCK_HOME", Path.home() / ".agentdock"))
STATE_DIR = AGENTDOCK_HOME / "skill-data" / "spotify-web-api"
STATE_FILE = STATE_DIR / "state.json"
DEFAULT_REDIRECT_URI = "http://127.0.0.1:8765/callback"
DEFAULT_SCOPES = [
    "playlist-read-private",
    "playlist-read-collaborative",
    "playlist-modify-public",
    "playlist-modify-private",
    "user-read-currently-playing",
    "user-read-playback-state",
    "user-modify-playback-state",
    "user-read-private",
    "user-top-read",
    "user-read-recently-played",
    "user-library-read",
]
ACCOUNTS_BASE = "https://accounts.spotify.com"
API_BASE = "https://api.spotify.com/v1"


def parse_env_scopes(value):
    if not value:
        return None
    value = value.strip()
    if not value:
        return None
    try:
        parsed = json.loads(value)
        if isinstance(parsed, list) and all(isinstance(item, str) and item.strip() for item in parsed):
            return [item.strip() for item in parsed]
    except Exception:
        pass
    parts = [part.strip() for part in value.replace(",", " ").split()]
    return parts or None


def apply_env_state(state):
    state = dict(state)
    env_map = {
        "SPOTIFY_CLIENT_ID": "client_id",
        "SPOTIFY_REDIRECT_URI": "redirect_uri",
    }
    for env_name, state_key in env_map.items():
        value = os.environ.get(env_name)
        if value:
            state[state_key] = value
    scopes = parse_env_scopes(os.environ.get("SPOTIFY_SCOPES"))
    if scopes:
        state["scopes"] = scopes
    return state


def emit(value):
    print(json.dumps(value, ensure_ascii=False, separators=(",", ":")))


def fail(code, message, details=None, exit_code=0):
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
        fail("INVALID_INPUT", "Input is not valid JSON", {"reason": str(exc)})
    if not isinstance(value, dict):
        fail("INVALID_INPUT", "Input must be a JSON object")
    return value


def load_state():
    state = {}
    if not STATE_FILE.exists():
        return apply_env_state(state)
    try:
        state = json.loads(STATE_FILE.read_text(encoding="utf-8"))
    except Exception as exc:
        fail("STATE_READ_FAILED", "Could not read Spotify local state", {"reason": str(exc)})
    return apply_env_state(state)


def save_state(state):
    STATE_DIR.mkdir(parents=True, exist_ok=True)
    tmp = STATE_FILE.with_suffix(".json.tmp")
    tmp.write_text(json.dumps(state, ensure_ascii=False, indent=2), encoding="utf-8")
    os.chmod(tmp, 0o600)
    tmp.replace(STATE_FILE)


def b64url(raw):
    return base64.urlsafe_b64encode(raw).rstrip(b"=").decode("ascii")


def code_challenge(verifier):
    return b64url(hashlib.sha256(verifier.encode("ascii")).digest())


def http_json(method, url, headers=None, body=None, timeout=20):
    data = None
    req_headers = headers or {}
    if body is not None:
        if isinstance(body, bytes):
            data = body
        else:
            data = json.dumps(body).encode("utf-8")
            req_headers.setdefault("Content-Type", "application/json")
    req = urllib.request.Request(url, data=data, method=method, headers=req_headers)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            raw = resp.read().decode("utf-8")
            if not raw:
                return {"_status": resp.status}
            parsed = json.loads(raw)
            if isinstance(parsed, dict):
                parsed["_status"] = resp.status
            return parsed
    except urllib.error.HTTPError as exc:
        raw = exc.read().decode("utf-8", errors="replace")[:4000]
        details = {"status": exc.code, "body": raw}
        try:
            parsed = json.loads(raw)
            details["json"] = parsed
        except Exception:
            pass
        fail("SPOTIFY_HTTP_ERROR", "Spotify API request failed", details)
    except urllib.error.URLError as exc:
        fail("SPOTIFY_NETWORK_ERROR", "Spotify API request failed", {"reason": str(exc)})


def token_form(data):
    body = urllib.parse.urlencode(data).encode("utf-8")
    return http_json("POST", f"{ACCOUNTS_BASE}/api/token", {"Content-Type": "application/x-www-form-urlencoded"}, body, timeout=25)


def ensure_auth_state(state):
    if not state.get("client_id"):
        fail("NOT_CONFIGURED", "Run auth-url with a Spotify app client_id first")
    if not state.get("access_token") and not state.get("refresh_token"):
        fail("NOT_AUTHENTICATED", "Run auth-url, open the URL, then run finish-auth")


def refresh_if_needed(state, force=False):
    ensure_auth_state(state)
    expires_at = float(state.get("expires_at", 0))
    if not force and state.get("access_token") and expires_at - time.time() > 60:
        return state
    refresh_token = state.get("refresh_token")
    if not refresh_token:
        fail("TOKEN_EXPIRED", "Access token expired and no refresh token is available")
    token = token_form({
        "grant_type": "refresh_token",
        "refresh_token": refresh_token,
        "client_id": state["client_id"],
    })
    state["access_token"] = token.get("access_token")
    state["expires_at"] = time.time() + int(token.get("expires_in", 3600))
    if token.get("refresh_token"):
        state["refresh_token"] = token["refresh_token"]
    save_state(state)
    return state


def api(method, path, state, query=None, body=None, expected_empty=False):
    state = refresh_if_needed(state)
    url = f"{API_BASE}{path}"
    if query:
        url += "?" + urllib.parse.urlencode(query, doseq=True)
    headers = {"Authorization": f"Bearer {state['access_token']}"}
    if expected_empty:
        req = urllib.request.Request(url, data=(json.dumps(body).encode("utf-8") if body is not None else None), method=method, headers={**headers, "Content-Type": "application/json"})
        try:
            with urllib.request.urlopen(req, timeout=20) as resp:
                raw = resp.read().decode("utf-8")
                return json.loads(raw) if raw else {"_status": resp.status}
        except urllib.error.HTTPError as exc:
            raw = exc.read().decode("utf-8", errors="replace")[:4000]
            fail("SPOTIFY_HTTP_ERROR", "Spotify API request failed", {"status": exc.code, "body": raw})
    return http_json(method, url, headers, body)


def simplify_track(track):
    if not track:
        return None
    return {
        "id": track.get("id"),
        "uri": track.get("uri"),
        "name": track.get("name"),
        "artists": [a.get("name") for a in track.get("artists", []) if a.get("name")],
        "album": (track.get("album") or {}).get("name"),
        "duration_ms": track.get("duration_ms"),
        "explicit": track.get("explicit"),
        "url": ((track.get("external_urls") or {}).get("spotify")),
    }


def simplify_artist(artist):
    if not artist:
        return None
    return {
        "id": artist.get("id"),
        "uri": artist.get("uri"),
        "name": artist.get("name"),
        "genres": artist.get("genres") or [],
        "popularity": artist.get("popularity"),
        "followers_total": ((artist.get("followers") or {}).get("total")),
        "url": ((artist.get("external_urls") or {}).get("spotify")),
    }


def parse_limit(args, default=20, maximum=50):
    limit = args.get("limit", default)
    if isinstance(limit, bool) or not isinstance(limit, int) or not 1 <= limit <= maximum:
        fail("INVALID_LIMIT", f"limit must be an integer between 1 and {maximum}")
    return limit


def parse_time_range(args):
    value = args.get("time_range", "medium_term")
    allowed = {"short_term", "medium_term", "long_term"}
    if value not in allowed:
        fail("INVALID_TIME_RANGE", "time_range must be short_term, medium_term, or long_term")
    return value


def top_items(state, item_type, args):
    limit = parse_limit(args, default=20, maximum=50)
    time_range = parse_time_range(args)
    data = api("GET", f"/me/top/{item_type}", state, {"limit": limit, "time_range": time_range})
    return time_range, data.get("items") or []


def simplify_playlist(pl):
    return {
        "id": pl.get("id"),
        "uri": pl.get("uri"),
        "name": pl.get("name"),
        "owner": ((pl.get("owner") or {}).get("display_name") or (pl.get("owner") or {}).get("id")),
        "public": pl.get("public"),
        "collaborative": pl.get("collaborative"),
        "tracks_total": ((pl.get("tracks") or {}).get("total")),
        "url": ((pl.get("external_urls") or {}).get("spotify")),
    }


def playlist_id(value, state):
    text = value.strip()
    if text.startswith("spotify:playlist:"):
        return text.split(":")[-1]
    if "open.spotify.com/playlist/" in text:
        part = text.split("/playlist/", 1)[1]
        return part.split("?", 1)[0].split("/", 1)[0]
    if re_like_id(text):
        return text
    playlists = list_playlists_all(state)
    matches = [p for p in playlists if (p.get("name") or "").lower() == text.lower()]
    if not matches:
        fail("PLAYLIST_NOT_FOUND", "Could not resolve playlist by ID, URI, URL, or exact name", {"playlist": value})
    if len(matches) > 1:
        fail("PLAYLIST_AMBIGUOUS", "Playlist name matched multiple playlists; use playlist ID or URI", {"matches": [simplify_playlist(p) for p in matches[:10]]})
    return matches[0]["id"]


def re_like_id(text):
    return bool(text) and all(ch.isalnum() for ch in text) and 10 <= len(text) <= 80


def list_playlists_all(state, limit=50):
    items = []
    offset = 0
    while True:
        data = api("GET", "/me/playlists", state, {"limit": min(limit, 50), "offset": offset})
        batch = data.get("items") or []
        items.extend(batch)
        if not data.get("next") or len(batch) == 0:
            return items
        offset += len(batch)
        if len(items) >= 500:
            return items


def get_current(state):
    try:
        data = api("GET", "/me/player", state)
    except SystemExit:
        raise
    if not data or data.get("_status") == 204:
        return None
    return data


def current_playlist_id(state):
    current = get_current(state)
    if not current:
        fail("NO_ACTIVE_PLAYBACK", "Spotify returned no active playback context")
    context = current.get("context") or {}
    if context.get("type") != "playlist" or not context.get("uri"):
        fail("CURRENT_CONTEXT_NOT_PLAYLIST", "Current playback context is not a playlist", {"context": context})
    return context["uri"].split(":")[-1], current


def handle_status(args):
    state = load_state()
    token_present = bool(state.get("access_token") or state.get("refresh_token"))
    expires_at = float(state.get("expires_at", 0) or 0)
    current_scopes = state.get("scopes", [])
    payload = {
        "ok": True,
        "skill_version": VERSION,
        "configured": bool(state.get("client_id")),
        "authenticated": token_present,
        "token_expired": bool(token_present and expires_at <= time.time()),
        "redirect_uri": state.get("redirect_uri"),
        "scopes": current_scopes,
        "recommended_missing_scopes": [s for s in DEFAULT_SCOPES if s not in current_scopes],
        "state_file": str(STATE_FILE),
    }
    if args.get("validate"):
        state = refresh_if_needed(state)
        token_present = bool(state.get("access_token") or state.get("refresh_token"))
        expires_at = float(state.get("expires_at", 0) or 0)
        payload["token_expired"] = bool(token_present and expires_at <= time.time())
        me = api("GET", "/me", state)
        payload["spotify_user"] = {"id": me.get("id"), "display_name": me.get("display_name"), "product": me.get("product")}
    emit(payload)


def handle_auth_url(args):
    state = load_state()
    client_id = args.get("client_id") or state.get("client_id")
    if not isinstance(client_id, str) or not client_id.strip():
        fail("INVALID_CLIENT_ID", "client_id must be a non-empty string unless one is already configured")
    redirect_uri = args.get("redirect_uri") or DEFAULT_REDIRECT_URI
    scopes = args.get("scopes") or DEFAULT_SCOPES
    if not isinstance(scopes, list) or not all(isinstance(s, str) and s.strip() for s in scopes):
        fail("INVALID_SCOPES", "scopes must be an array of non-empty strings")
    verifier = b64url(secrets.token_bytes(64))[:96]
    state_value = secrets.token_urlsafe(24)
    state.update({
        "client_id": client_id.strip(),
        "redirect_uri": redirect_uri,
        "scopes": scopes,
        "pending_state": state_value,
        "code_verifier": verifier,
        "created_at": time.time(),
    })
    save_state(state)
    query = {
        "response_type": "code",
        "client_id": client_id.strip(),
        "scope": " ".join(scopes),
        "redirect_uri": redirect_uri,
        "state": state_value,
        "code_challenge_method": "S256",
        "code_challenge": code_challenge(verifier),
    }
    emit({
        "ok": True,
        "auth_url": f"{ACCOUNTS_BASE}/authorize?{urllib.parse.urlencode(query)}",
        "redirect_uri": redirect_uri,
        "scopes": scopes,
        "next_step": "Open auth_url, approve access, then run finish-auth with callback_url or code+state.",
    })


def handle_finish_auth(args):
    state = load_state()
    if not state.get("client_id") or not state.get("code_verifier"):
        fail("NO_PENDING_AUTH", "Run auth-url first")
    code = args.get("code")
    got_state = args.get("state")
    callback_url = args.get("callback_url")
    if callback_url:
        parsed = urllib.parse.urlparse(callback_url)
        qs = urllib.parse.parse_qs(parsed.query)
        code = (qs.get("code") or [None])[0]
        got_state = (qs.get("state") or [None])[0]
        err = (qs.get("error") or [None])[0]
        if err:
            fail("SPOTIFY_AUTH_ERROR", "Spotify returned an OAuth error", {"error": err})
    if not code:
        fail("MISSING_CODE", "Provide callback_url or code")
    if state.get("pending_state") and got_state != state.get("pending_state"):
        fail("STATE_MISMATCH", "OAuth state does not match the pending auth request")
    token = token_form({
        "client_id": state["client_id"],
        "grant_type": "authorization_code",
        "code": code,
        "redirect_uri": state["redirect_uri"],
        "code_verifier": state["code_verifier"],
    })
    state["access_token"] = token.get("access_token")
    state["refresh_token"] = token.get("refresh_token")
    state["expires_at"] = time.time() + int(token.get("expires_in", 3600))
    state.pop("pending_state", None)
    state.pop("code_verifier", None)
    save_state(state)
    emit({"ok": True, "authenticated": True, "expires_in": token.get("expires_in"), "scope": token.get("scope")})


def handle_current():
    state = refresh_if_needed(load_state())
    current = get_current(state)
    if not current:
        emit({"ok": True, "active": False})
        return
    emit({
        "ok": True,
        "active": True,
        "is_playing": current.get("is_playing"),
        "device": current.get("device"),
        "context": current.get("context"),
        "track": simplify_track(current.get("item")),
        "progress_ms": current.get("progress_ms"),
    })


def handle_search(args):
    query = args.get("query")
    if not isinstance(query, str) or not query.strip():
        fail("INVALID_QUERY", "query must be a non-empty string")
    limit = args.get("limit", 5)
    if isinstance(limit, bool) or not isinstance(limit, int) or not 1 <= limit <= 20:
        fail("INVALID_LIMIT", "limit must be an integer between 1 and 20")
    market = args.get("market", "from_token")
    state = refresh_if_needed(load_state())
    data = api("GET", "/search", state, {"q": query.strip(), "type": "track", "limit": limit, "market": market})
    tracks = [simplify_track(t) for t in ((data.get("tracks") or {}).get("items") or [])]
    emit({"ok": True, "query": query.strip(), "count": len(tracks), "tracks": tracks})


def handle_list_playlists(args):
    limit = args.get("limit", 20)
    if isinstance(limit, bool) or not isinstance(limit, int) or not 1 <= limit <= 50:
        fail("INVALID_LIMIT", "limit must be an integer between 1 and 50")
    state = refresh_if_needed(load_state())
    data = api("GET", "/me/playlists", state, {"limit": limit})
    playlists = [simplify_playlist(p) for p in data.get("items", [])]
    emit({"ok": True, "count": len(playlists), "playlists": playlists})


def handle_top_tracks(args):
    state = refresh_if_needed(load_state())
    time_range, items = top_items(state, "tracks", args)
    tracks = [simplify_track(t) for t in items]
    emit({"ok": True, "time_range": time_range, "count": len(tracks), "tracks": tracks})


def handle_top_artists(args):
    state = refresh_if_needed(load_state())
    time_range, items = top_items(state, "artists", args)
    artists = [simplify_artist(a) for a in items]
    emit({"ok": True, "time_range": time_range, "count": len(artists), "artists": artists})


def handle_recently_played(args):
    limit = parse_limit(args, default=20, maximum=50)
    query = {"limit": limit}
    for key in ("after", "before"):
        if key in args:
            value = args[key]
            if isinstance(value, bool) or not isinstance(value, int) or value < 0:
                fail("INVALID_TIMESTAMP", f"{key} must be a Unix timestamp in milliseconds")
            query[key] = value
    if "after" in query and "before" in query:
        fail("INVALID_TIME_CURSOR", "Use after or before, not both")
    state = refresh_if_needed(load_state())
    data = api("GET", "/me/player/recently-played", state, query)
    items = []
    for item in data.get("items", []) or []:
        items.append({
            "played_at": item.get("played_at"),
            "context": item.get("context"),
            "track": simplify_track(item.get("track")),
        })
    emit({"ok": True, "count": len(items), "items": items, "cursors": data.get("cursors")})


def handle_saved_tracks(args):
    limit = parse_limit(args, default=20, maximum=50)
    offset = args.get("offset", 0)
    if isinstance(offset, bool) or not isinstance(offset, int) or offset < 0:
        fail("INVALID_OFFSET", "offset must be a non-negative integer")
    state = refresh_if_needed(load_state())
    data = api("GET", "/me/tracks", state, {"limit": limit, "offset": offset})
    items = []
    for item in data.get("items", []) or []:
        items.append({"added_at": item.get("added_at"), "track": simplify_track(item.get("track"))})
    emit({"ok": True, "count": len(items), "total": data.get("total"), "offset": offset, "items": items})


def handle_taste_profile(args):
    state = refresh_if_needed(load_state())
    limit = parse_limit(args, default=20, maximum=50)
    time_range = parse_time_range(args)
    _, top_track_items = top_items(state, "tracks", {"limit": limit, "time_range": time_range})
    _, top_artist_items = top_items(state, "artists", {"limit": limit, "time_range": time_range})
    recent_data = api("GET", "/me/player/recently-played", state, {"limit": min(limit, 50)})
    saved_data = api("GET", "/me/tracks", state, {"limit": min(limit, 50), "offset": 0})

    top_tracks = [simplify_track(t) for t in top_track_items]
    top_artists = [simplify_artist(a) for a in top_artist_items]
    recent_tracks = [simplify_track(i.get("track")) for i in (recent_data.get("items") or [])]
    saved_tracks = [simplify_track(i.get("track")) for i in (saved_data.get("items") or [])]

    artist_counter = Counter()
    track_counter = Counter()
    genre_counter = Counter()
    album_counter = Counter()
    explicit_count = 0
    considered_tracks = [t for t in (top_tracks + recent_tracks + saved_tracks) if t]
    for track in considered_tracks:
        track_counter[track.get("name") or track.get("id")] += 1
        album_counter[track.get("album") or "unknown"] += 1
        if track.get("explicit"):
            explicit_count += 1
        for artist_name in track.get("artists") or []:
            artist_counter[artist_name] += 1
    for artist in top_artists:
        if artist and artist.get("name"):
            artist_counter[artist["name"]] += 3
        for genre in (artist or {}).get("genres") or []:
            genre_counter[genre] += 1

    emit({
        "ok": True,
        "time_range": time_range,
        "counts": {
            "top_tracks": len(top_tracks),
            "top_artists": len(top_artists),
            "recent_tracks": len(recent_tracks),
            "saved_tracks_sample": len(saved_tracks),
            "considered_tracks": len(considered_tracks),
        },
        "signals": {
            "artist_mentions": [{"name": name, "score": score} for name, score in artist_counter.most_common(15)],
            "genres": [{"name": name, "count": count} for name, count in genre_counter.most_common(15)],
            "albums": [{"name": name, "count": count} for name, count in album_counter.most_common(10)],
            "repeated_track_names": [{"name": name, "count": count} for name, count in track_counter.most_common(10) if count > 1],
            "explicit_tracks_seen": explicit_count,
        },
        "top_tracks": top_tracks[:10],
        "top_artists": top_artists[:10],
        "recent_tracks": recent_tracks[:10],
        "saved_tracks_sample": saved_tracks[:10],
    })


def add_tracks(state, playlist, uris, position=None):
    if not isinstance(uris, list) or not uris or not all(isinstance(u, str) and u.startswith("spotify:track:") for u in uris):
        fail("INVALID_TRACK_URIS", "track_uris must be Spotify track URIs like spotify:track:...")
    pid = playlist_id(playlist, state)
    body = {"uris": uris}
    if position is not None:
        body["position"] = position
    result = api("POST", f"/playlists/{pid}/tracks", state, body=body)
    return pid, result


def handle_add_track_to_playlist(args):
    state = refresh_if_needed(load_state())
    pid, result = add_tracks(state, args.get("playlist"), args.get("track_uris"), args.get("position"))
    emit({"ok": True, "playlist_id": pid, "snapshot_id": result.get("snapshot_id"), "added_count": len(args.get("track_uris", []))})


def handle_add_track_to_current_playlist(args):
    state = refresh_if_needed(load_state())
    pid, current = current_playlist_id(state)
    _, result = add_tracks(state, pid, args.get("track_uris"), args.get("position"))
    emit({"ok": True, "playlist_id": pid, "context": current.get("context"), "snapshot_id": result.get("snapshot_id"), "added_count": len(args.get("track_uris", []))})


def handle_search_and_add_current(args):
    query = args.get("query")
    if not isinstance(query, str) or not query.strip():
        fail("INVALID_QUERY", "query must be a non-empty string")
    limit = args.get("limit", 5)
    result_index = args.get("result_index", 0)
    if isinstance(limit, bool) or not isinstance(limit, int) or not 1 <= limit <= 20:
        fail("INVALID_LIMIT", "limit must be an integer between 1 and 20")
    if isinstance(result_index, bool) or not isinstance(result_index, int) or not 0 <= result_index < limit:
        fail("INVALID_INDEX", "result_index must be within the returned search range")
    state = refresh_if_needed(load_state())
    data = api("GET", "/search", state, {"q": query.strip(), "type": "track", "limit": limit, "market": "from_token"})
    tracks = ((data.get("tracks") or {}).get("items") or [])
    if result_index >= len(tracks):
        fail("NO_SEARCH_RESULT", "No track exists at result_index", {"count": len(tracks)})
    track = simplify_track(tracks[result_index])
    pid, current = current_playlist_id(state)
    _, result = add_tracks(state, pid, [track["uri"]], None)
    emit({"ok": True, "playlist_id": pid, "context": current.get("context"), "added_track": track, "snapshot_id": result.get("snapshot_id")})


def handle_add_to_queue(args):
    uri = args.get("track_uri")
    if not isinstance(uri, str) or not uri.startswith("spotify:track:"):
        fail("INVALID_TRACK_URI", "track_uri must be a Spotify track URI like spotify:track:...")
    state = refresh_if_needed(load_state())
    query = {"uri": uri}
    if args.get("device_id"):
        query["device_id"] = args["device_id"]
    result = api("POST", "/me/player/queue", state, query=query, expected_empty=True)
    emit({"ok": True, "status": result.get("_status"), "track_uri": uri})


def main():
    args = load_input()
    op = str(args.pop("skill_action", "status"))
    if op == "status":
        handle_status(args)
    elif op == "auth-url":
        handle_auth_url(args)
    elif op == "finish-auth":
        handle_finish_auth(args)
    elif op == "current-playback":
        handle_current()
    elif op == "search-track":
        handle_search(args)
    elif op == "list-playlists":
        handle_list_playlists(args)
    elif op == "top-tracks":
        handle_top_tracks(args)
    elif op == "top-artists":
        handle_top_artists(args)
    elif op == "recently-played":
        handle_recently_played(args)
    elif op == "saved-tracks":
        handle_saved_tracks(args)
    elif op == "taste-profile":
        handle_taste_profile(args)
    elif op == "add-track-to-playlist":
        handle_add_track_to_playlist(args)
    elif op == "add-track-to-current-playlist":
        handle_add_track_to_current_playlist(args)
    elif op == "search-and-add-to-current-playlist":
        handle_search_and_add_current(args)
    elif op == "add-to-queue":
        handle_add_to_queue(args)
    else:
        fail("UNKNOWN_OPERATION", "Unknown operation", {"operation": op})


if __name__ == "__main__":
    main()

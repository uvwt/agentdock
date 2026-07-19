#!/usr/bin/env python3
"""Portable Linkwarden HTTP API helper for the Linkwarden Skill."""
from __future__ import annotations

import json
import os
import ssl
import sys
from dataclasses import dataclass
from typing import Any, Callable
from urllib.error import HTTPError, URLError
from urllib.parse import urlencode, urlsplit, urlunsplit
from urllib.request import Request, urlopen

SKILL_VERSION = "1.1.0"
UPSTREAM_REPOSITORY = "https://github.com/linkwarden/linkwarden"


class SkillError(Exception):
    code = "SKILL_ERROR"


class SkillInputError(SkillError):
    code = "INPUT_ERROR"


class SkillConfigurationError(SkillError):
    code = "CONFIG_MISSING"


class SkillProtocolError(SkillError):
    code = "PROTOCOL_ERROR"


@dataclass(frozen=True)
class RuntimeConfig:
    base_url: str
    token: str
    insecure_tls: bool


def emit(value: Any) -> None:
    print(json.dumps(value, ensure_ascii=False, separators=(",", ":")))


def load_input() -> dict[str, Any]:
    raw = sys.stdin.read().strip()
    if not raw:
        return {}
    try:
        value = json.loads(raw)
    except json.JSONDecodeError as exc:
        raise SkillInputError(f"输入不是有效 JSON：{exc}") from exc
    if not isinstance(value, dict):
        raise SkillInputError("输入必须是 JSON 对象")
    return value


def normalize_base_url(value: str) -> str:
    parsed = urlsplit(value.strip())
    if parsed.scheme not in {"http", "https"}:
        raise SkillInputError("LINKWARDEN_URL 必须使用 http 或 https")
    if not parsed.hostname:
        raise SkillInputError("LINKWARDEN_URL 必须包含主机名")
    if parsed.username or parsed.password:
        raise SkillInputError("LINKWARDEN_URL 不能包含用户名或密码")
    if parsed.query or parsed.fragment:
        raise SkillInputError("LINKWARDEN_URL 不能包含查询参数或片段")
    path = parsed.path.rstrip("/")
    return urlunsplit((parsed.scheme, parsed.netloc, path, "", ""))


def load_config(*, require_token: bool) -> RuntimeConfig:
    raw_url = os.environ.get("LINKWARDEN_URL", "").strip()
    if not raw_url:
        raise SkillConfigurationError("未配置 LINKWARDEN_URL")
    token = os.environ.get("LINKWARDEN_TOKEN", "").strip()
    if require_token and not token:
        raise SkillConfigurationError("未配置 LINKWARDEN_TOKEN")
    return RuntimeConfig(
        base_url=normalize_base_url(raw_url),
        token=token,
        insecure_tls=os.environ.get("LINKWARDEN_INSECURE_TLS") == "1",
    )


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


def compact_query(query: dict[str, Any] | None) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in (query or {}).items():
        if value is None:
            continue
        if isinstance(value, bool):
            result[key] = "true" if value else "false"
        else:
            result[key] = value
    return result


def request_api(
    config: RuntimeConfig,
    endpoint: str,
    *,
    method: str = "GET",
    query: dict[str, Any] | None = None,
    body: Any = None,
    require_auth: bool = True,
    timeout: int = 30,
) -> dict[str, Any]:
    if require_auth and not config.token:
        raise SkillConfigurationError("未配置 LINKWARDEN_TOKEN")
    if not endpoint.startswith("/"):
        endpoint = "/" + endpoint

    url = config.base_url.rstrip("/") + endpoint
    normalized_query = compact_query(query)
    if normalized_query:
        url += "?" + urlencode(normalized_query, doseq=True)

    headers = {
        "Accept": "application/json",
        "User-Agent": f"AgentDock-Linkwarden-Skill/{SKILL_VERSION}",
    }
    if require_auth:
        headers["Authorization"] = f"Bearer {config.token}"

    data = None
    if body is not None:
        data = json.dumps(body, ensure_ascii=False).encode("utf-8")
        headers["Content-Type"] = "application/json"

    request = Request(url, data=data, headers=headers, method=method.upper())
    context = ssl._create_unverified_context() if config.insecure_tls else None
    try:
        with urlopen(request, timeout=timeout, context=context) as response:
            raw = response.read()
            status = response.status
            content_type = response.headers.get("Content-Type", "")
    except HTTPError as exc:
        raw = exc.read()
        status = exc.code
        content_type = exc.headers.get("Content-Type", "") if exc.headers else ""
    except (URLError, TimeoutError, OSError) as exc:
        reason = getattr(exc, "reason", exc)
        return {
            "request_succeeded": False,
            "code": "NETWORK_ERROR",
            "message": f"连接 Linkwarden 失败：{reason}",
            "http_status": None,
            "endpoint": endpoint,
        }

    parsed = parse_response_body(raw, content_type)
    return {
        "request_succeeded": 200 <= status < 300,
        "http_status": status,
        "endpoint": endpoint,
        "response": parsed,
    }


def require_int(payload: dict[str, Any], key: str) -> int:
    value = payload.get(key)
    if isinstance(value, bool) or not isinstance(value, int):
        raise SkillInputError(f"{key} 必须是整数")
    return value


def optional_int(payload: dict[str, Any], key: str) -> int | None:
    if key not in payload or payload[key] is None:
        return None
    return require_int(payload, key)


def optional_bool(payload: dict[str, Any], key: str) -> bool | None:
    if key not in payload or payload[key] is None:
        return None
    value = payload[key]
    if not isinstance(value, bool):
        raise SkillInputError(f"{key} 必须是布尔值")
    return value


def optional_string(payload: dict[str, Any], key: str) -> str | None:
    if key not in payload or payload[key] is None:
        return None
    value = payload[key]
    if not isinstance(value, str):
        raise SkillInputError(f"{key} 必须是字符串")
    return value.strip()


def require_nonempty_string(payload: dict[str, Any], key: str) -> str:
    value = optional_string(payload, key)
    if not value:
        raise SkillInputError(f"{key} 不能为空")
    return value


def validate_link_url(value: str) -> str:
    if len(value) > 2048:
        raise SkillInputError("url 不能超过 2048 个字符")
    parsed = urlsplit(value)
    if parsed.scheme not in {"http", "https"} or not parsed.hostname:
        raise SkillInputError("url 必须是有效的 http 或 https 地址")
    if parsed.username or parsed.password:
        raise SkillInputError("url 不能包含用户名或密码")
    return value


def require_confirmation(payload: dict[str, Any], action: str) -> None:
    if payload.get("confirm") is not True:
        raise SkillInputError(f"{action} 属于破坏性操作，必须显式传入 confirm: true")


def normalize_tags(value: Any) -> list[dict[str, Any]]:
    if value is None:
        return []
    if not isinstance(value, list):
        raise SkillInputError("tags 必须是字符串或标签对象数组")

    tags: list[dict[str, Any]] = []
    seen: set[str] = set()
    for item in value:
        tag_id: int | None = None
        if isinstance(item, str):
            name = item.strip()
        elif isinstance(item, dict):
            raw_name = item.get("name")
            if not isinstance(raw_name, str):
                raise SkillInputError("标签对象必须包含字符串 name")
            name = raw_name.strip()
            raw_id = item.get("id")
            if raw_id is not None:
                if isinstance(raw_id, bool) or not isinstance(raw_id, int):
                    raise SkillInputError("标签 id 必须是整数")
                tag_id = raw_id
        else:
            raise SkillInputError("tags 只允许字符串或标签对象")

        if not name:
            raise SkillInputError("标签名称不能为空")
        if len(name) > 50:
            raise SkillInputError("标签名称不能超过 50 个字符")
        if name in seen:
            continue
        seen.add(name)
        tag: dict[str, Any] = {"name": name}
        if tag_id is not None:
            tag["id"] = tag_id
        tags.append(tag)
    return tags


def normalize_id_list(value: Any, field: str) -> list[int]:
    if not isinstance(value, list):
        raise SkillInputError(f"{field} 必须是整数数组")
    result: list[int] = []
    seen: set[int] = set()
    for item in value:
        if isinstance(item, bool) or not isinstance(item, int):
            raise SkillInputError(f"{field} 必须是整数数组")
        if item not in seen:
            seen.add(item)
            result.append(item)
    return result


def extract_envelope(result: dict[str, Any], label: str) -> Any:
    body = result.get("response")
    if not isinstance(body, dict) or "response" not in body:
        raise SkillProtocolError(f"Linkwarden 返回的 {label} 响应缺少 response 字段")
    return body["response"]


def extract_object(result: dict[str, Any], label: str) -> dict[str, Any]:
    value = extract_envelope(result, label)
    if not isinstance(value, dict):
        raise SkillProtocolError(f"Linkwarden 返回的 {label} 不是对象")
    return value


def get_object(config: RuntimeConfig, endpoint: str, label: str) -> tuple[dict[str, Any], dict[str, Any] | None]:
    result = request_api(config, endpoint)
    if not result.get("request_succeeded"):
        return result, None
    return result, extract_object(result, label)


def collection_identity(config: RuntimeConfig, existing: dict[str, Any], collection_id: int) -> tuple[dict[str, Any], dict[str, int] | None]:
    current = existing.get("collection")
    if isinstance(current, dict):
        current_id = current.get("id")
        owner_id = current.get("ownerId")
        if current_id == collection_id and isinstance(owner_id, int) and not isinstance(owner_id, bool):
            return {"request_succeeded": True}, {"id": collection_id, "ownerId": owner_id}

    result, target = get_object(config, f"/api/v1/collections/{collection_id}", "集合")
    if target is None:
        return result, None
    owner_id = target.get("ownerId")
    if isinstance(owner_id, bool) or not isinstance(owner_id, int):
        raise SkillProtocolError("目标集合响应缺少有效 ownerId")
    return result, {"id": collection_id, "ownerId": owner_id}


def preserve_pinned_by(value: Any) -> list[dict[str, int]]:
    if not isinstance(value, list):
        return []
    result: list[dict[str, int]] = []
    for item in value:
        if isinstance(item, dict):
            user_id = item.get("id")
            if isinstance(user_id, int) and not isinstance(user_id, bool):
                result.append({"id": user_id})
    return result


def preserve_members(value: Any) -> list[dict[str, Any]]:
    if not isinstance(value, list):
        return []
    members: list[dict[str, Any]] = []
    for item in value:
        if not isinstance(item, dict):
            continue
        user_id = item.get("userId")
        if not isinstance(user_id, int) or isinstance(user_id, bool):
            user = item.get("user")
            user_id = user.get("id") if isinstance(user, dict) else None
        if not isinstance(user_id, int) or isinstance(user_id, bool):
            continue
        members.append(
            {
                "userId": user_id,
                "canCreate": bool(item.get("canCreate", False)),
                "canUpdate": bool(item.get("canUpdate", False)),
                "canDelete": bool(item.get("canDelete", False)),
            }
        )
    return members


def op_status(_: dict[str, Any]) -> dict[str, Any]:
    raw_url = os.environ.get("LINKWARDEN_URL", "").strip()
    token_configured = bool(os.environ.get("LINKWARDEN_TOKEN", "").strip())
    if not raw_url:
        return {
            "service": "linkwarden",
            "skill_version": SKILL_VERSION,
            "upstream": UPSTREAM_REPOSITORY,
            "configured": False,
            "token_configured": token_configured,
            "service_reachable": False,
            "message": "未配置 LINKWARDEN_URL",
        }

    config = load_config(require_token=False)
    result = request_api(config, "/api/v1/config", require_auth=False, timeout=15)
    result.update(
        {
            "service": "linkwarden",
            "skill_version": SKILL_VERSION,
            "upstream": UPSTREAM_REPOSITORY,
            "base_url": config.base_url,
            "configured": True,
            "token_configured": token_configured,
            "service_reachable": bool(result.get("request_succeeded")),
        }
    )
    return result


def op_search(payload: dict[str, Any]) -> dict[str, Any]:
    config = load_config(require_token=True)
    query = {
        "searchQueryString": optional_string(payload, "query"),
        "sort": optional_int(payload, "sort") if "sort" in payload else 0,
        "cursor": optional_int(payload, "cursor"),
        "collectionId": optional_int(payload, "collection_id"),
        "tagId": optional_int(payload, "tag_id"),
        "pinnedOnly": optional_bool(payload, "pinned_only"),
    }
    return request_api(config, "/api/v1/search", query=query)


def op_get_link(payload: dict[str, Any]) -> dict[str, Any]:
    config = load_config(require_token=True)
    link_id = require_int(payload, "id")
    return request_api(config, f"/api/v1/links/{link_id}")


def op_get_highlights(payload: dict[str, Any]) -> dict[str, Any]:
    config = load_config(require_token=True)
    link_id = require_int(payload, "id")
    return request_api(config, f"/api/v1/links/{link_id}/highlights")


def op_create_link(payload: dict[str, Any]) -> dict[str, Any]:
    config = load_config(require_token=True)
    url = validate_link_url(require_nonempty_string(payload, "url"))
    body: dict[str, Any] = {"url": url}

    for input_key, api_key in (("name", "name"), ("description", "description")):
        if input_key in payload:
            value = optional_string(payload, input_key)
            if value is not None:
                body[api_key] = value

    collection_id = optional_int(payload, "collection_id")
    collection_name = optional_string(payload, "collection_name")
    if collection_id is not None and collection_name:
        raise SkillInputError("collection_id 和 collection_name 只能提供一个")
    if collection_id is not None:
        body["collection"] = {"id": collection_id}
    elif collection_name:
        body["collection"] = {"name": collection_name}

    if "tags" in payload:
        body["tags"] = normalize_tags(payload["tags"])
    return request_api(config, "/api/v1/links", method="POST", body=body)


def op_update_link(payload: dict[str, Any]) -> dict[str, Any]:
    config = load_config(require_token=True)
    link_id = require_int(payload, "id")
    current_result, existing = get_object(config, f"/api/v1/links/{link_id}", "书签")
    if existing is None:
        return current_result

    current_collection = existing.get("collection")
    current_collection_id = None
    if isinstance(current_collection, dict):
        current_collection_id = current_collection.get("id")
    if not isinstance(current_collection_id, int) or isinstance(current_collection_id, bool):
        current_collection_id = existing.get("collectionId")
    if not isinstance(current_collection_id, int) or isinstance(current_collection_id, bool):
        raise SkillProtocolError("书签响应缺少有效 collectionId")

    target_collection_id = optional_int(payload, "collection_id") or current_collection_id
    collection_result, collection = collection_identity(config, existing, target_collection_id)
    if collection is None:
        return collection_result

    body: dict[str, Any] = {
        "id": link_id,
        "name": existing.get("name"),
        "url": existing.get("url"),
        "description": existing.get("description"),
        "icon": existing.get("icon"),
        "iconWeight": existing.get("iconWeight"),
        "color": existing.get("color"),
        "collection": collection,
        "tags": normalize_tags(existing.get("tags", [])),
        "pinnedBy": preserve_pinned_by(existing.get("pinnedBy")),
    }

    field_map = {
        "name": "name",
        "description": "description",
        "icon": "icon",
        "icon_weight": "iconWeight",
        "color": "color",
    }
    for input_key, api_key in field_map.items():
        if input_key in payload:
            body[api_key] = optional_string(payload, input_key)

    if "url" in payload:
        value = optional_string(payload, "url")
        body["url"] = validate_link_url(value) if value else value
    if "tags" in payload:
        body["tags"] = normalize_tags(payload["tags"])
    if "pinned_by_user_ids" in payload:
        body["pinnedBy"] = [
            {"id": user_id}
            for user_id in normalize_id_list(payload["pinned_by_user_ids"], "pinned_by_user_ids")
        ]

    return request_api(config, f"/api/v1/links/{link_id}", method="PUT", body=body)


def op_rearchive_link(payload: dict[str, Any]) -> dict[str, Any]:
    require_confirmation(payload, "rearchive-link")
    config = load_config(require_token=True)
    link_id = require_int(payload, "id")
    return request_api(config, f"/api/v1/links/{link_id}/archive", method="PUT")


def op_delete_link(payload: dict[str, Any]) -> dict[str, Any]:
    require_confirmation(payload, "delete-link")
    config = load_config(require_token=True)
    link_id = require_int(payload, "id")
    return request_api(config, f"/api/v1/links/{link_id}", method="DELETE")


def op_list_collections(_: dict[str, Any]) -> dict[str, Any]:
    config = load_config(require_token=True)
    return request_api(config, "/api/v1/collections")


def op_get_collection(payload: dict[str, Any]) -> dict[str, Any]:
    config = load_config(require_token=True)
    collection_id = require_int(payload, "id")
    return request_api(config, f"/api/v1/collections/{collection_id}")


def op_create_collection(payload: dict[str, Any]) -> dict[str, Any]:
    config = load_config(require_token=True)
    body: dict[str, Any] = {"name": require_nonempty_string(payload, "name")}
    field_map = {
        "description": "description",
        "color": "color",
        "icon": "icon",
        "icon_weight": "iconWeight",
    }
    for input_key, api_key in field_map.items():
        if input_key in payload:
            value = optional_string(payload, input_key)
            if value is not None:
                body[api_key] = value
    parent_id = optional_int(payload, "parent_id")
    if parent_id is not None:
        body["parentId"] = parent_id
    return request_api(config, "/api/v1/collections", method="POST", body=body)


def op_update_collection(payload: dict[str, Any]) -> dict[str, Any]:
    config = load_config(require_token=True)
    collection_id = require_int(payload, "id")
    current_result, existing = get_object(config, f"/api/v1/collections/{collection_id}", "集合")
    if existing is None:
        return current_result

    name = existing.get("name")
    if not isinstance(name, str) or not name.strip():
        raise SkillProtocolError("集合响应缺少有效 name")

    current_parent_id = existing.get("parentId")
    if current_parent_id is None and isinstance(existing.get("parent"), dict):
        current_parent_id = existing["parent"].get("id")

    body: dict[str, Any] = {
        "id": collection_id,
        "name": name,
        "parentId": current_parent_id,
        "members": preserve_members(existing.get("members")),
    }
    for api_key in ("description", "color", "icon", "iconWeight"):
        value = existing.get(api_key)
        if isinstance(value, str):
            body[api_key] = value
    if isinstance(existing.get("isPublic"), bool):
        body["isPublic"] = existing["isPublic"]

    field_map = {
        "name": "name",
        "description": "description",
        "color": "color",
        "icon": "icon",
        "icon_weight": "iconWeight",
    }
    for input_key, api_key in field_map.items():
        if input_key in payload:
            value = optional_string(payload, input_key)
            if input_key == "name" and not value:
                raise SkillInputError("name 不能为空")
            if value is not None:
                body[api_key] = value

    if "is_public" in payload:
        is_public = optional_bool(payload, "is_public")
        if is_public is not None:
            body["isPublic"] = is_public
    if "parent_id" in payload:
        parent_id = payload["parent_id"]
        if parent_id is not None and parent_id != "root" and (
            isinstance(parent_id, bool) or not isinstance(parent_id, int)
        ):
            raise SkillInputError("parent_id 必须是整数、root 或 null")
        body["parentId"] = parent_id
    if "propagate_to_subcollections" in payload:
        body["propagateToSubcollections"] = optional_bool(
            payload, "propagate_to_subcollections"
        )

    return request_api(
        config,
        f"/api/v1/collections/{collection_id}",
        method="PUT",
        body=body,
    )


def op_delete_collection(payload: dict[str, Any]) -> dict[str, Any]:
    require_confirmation(payload, "delete-collection")
    config = load_config(require_token=True)
    collection_id = require_int(payload, "id")
    current_result, existing = get_object(config, f"/api/v1/collections/{collection_id}", "集合")
    if existing is None:
        return current_result

    current_name = existing.get("name")
    confirm_name = optional_string(payload, "confirm_name")
    if not isinstance(current_name, str) or confirm_name != current_name:
        raise SkillInputError("confirm_name 必须与当前集合名称完全一致")
    return request_api(config, f"/api/v1/collections/{collection_id}", method="DELETE")


def op_list_tags(payload: dict[str, Any]) -> dict[str, Any]:
    config = load_config(require_token=True)
    query = {
        "search": optional_string(payload, "search"),
        "sort": optional_int(payload, "sort") if "sort" in payload else 2,
        "cursor": optional_int(payload, "cursor"),
    }
    return request_api(config, "/api/v1/tags", query=query)


def op_delete_tag(payload: dict[str, Any]) -> dict[str, Any]:
    require_confirmation(payload, "delete-tag")
    config = load_config(require_token=True)
    tag_id = require_int(payload, "id")
    current_result, existing = get_object(config, f"/api/v1/tags/{tag_id}", "标签")
    if existing is None:
        return current_result

    current_name = existing.get("name")
    confirm_name = optional_string(payload, "confirm_name")
    if not isinstance(current_name, str) or confirm_name != current_name:
        raise SkillInputError("confirm_name 必须与当前标签名称完全一致")

    count = existing.get("_count")
    current_link_count = count.get("links") if isinstance(count, dict) else None
    if (
        isinstance(current_link_count, bool)
        or not isinstance(current_link_count, int)
        or current_link_count < 0
    ):
        raise SkillProtocolError("标签响应缺少有效的关联书签数量")

    confirm_link_count = require_int(payload, "confirm_link_count")
    if confirm_link_count != current_link_count:
        raise SkillInputError("confirm_link_count 必须与当前关联书签数量完全一致")

    return request_api(config, f"/api/v1/tags/{tag_id}", method="DELETE")


Action = Callable[[dict[str, Any]], dict[str, Any]]
ACTIONS: dict[str, Action] = {
    "status": op_status,
    "search": op_search,
    "get-link": op_get_link,
    "get-highlights": op_get_highlights,
    "create-link": op_create_link,
    "update-link": op_update_link,
    "rearchive-link": op_rearchive_link,
    "delete-link": op_delete_link,
    "list-collections": op_list_collections,
    "get-collection": op_get_collection,
    "create-collection": op_create_collection,
    "update-collection": op_update_collection,
    "delete-collection": op_delete_collection,
    "list-tags": op_list_tags,
    "delete-tag": op_delete_tag,
}


def run_action(payload: dict[str, Any]) -> dict[str, Any]:
    action = payload.get("skill_action", "status")
    if not isinstance(action, str):
        raise SkillInputError("skill_action 必须是字符串")
    handler = ACTIONS.get(action)
    if handler is None:
        raise SkillInputError(
            f"不支持的 skill_action：{action}；可用动作：{', '.join(sorted(ACTIONS))}"
        )
    return handler(payload)


def main() -> int:
    try:
        result = run_action(load_input())
    except SkillError as exc:
        emit({"request_succeeded": False, "code": exc.code, "message": str(exc)})
        return 2
    except Exception:
        emit(
            {
                "request_succeeded": False,
                "code": "INTERNAL_ERROR",
                "message": "Linkwarden Skill 执行出现未预期错误",
            }
        )
        return 1
    emit(result)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

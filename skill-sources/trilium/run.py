#!/usr/bin/env python3
"""Portable Trilium ETAPI helper for the AgentDock Trilium Skill."""
from __future__ import annotations

import json
import os
import re
import secrets
import ssl
import string
import sys
from typing import Any, Callable
from urllib.error import HTTPError, URLError
from urllib.parse import quote, urlencode, urlsplit, urlunsplit
from urllib.request import Request, urlopen

SKILL_VERSION = "1.0.0"
UPSTREAM_REPOSITORY = "https://github.com/TriliumNext/Trilium"
DEFAULT_TIMEOUT = 20
MAX_RESPONSE_BYTES = 8 * 1024 * 1024
ENTITY_ID_ALPHABET = string.ascii_letters + string.digits
BACKUP_NAME_PATTERN = re.compile(r"^[A-Za-z0-9._-]{1,64}$")


class TriliumError(RuntimeError):
    def __init__(self, message: str, *, code: str = "trilium_error", details: Any = None) -> None:
        super().__init__(message)
        self.code = code
        self.details = details


def emit(payload: dict[str, Any], *, exit_code: int = 0) -> int:
    print(json.dumps(payload, ensure_ascii=False, indent=2, sort_keys=True))
    return exit_code


def load_input() -> dict[str, Any]:
    raw = sys.stdin.read().strip()
    if not raw:
        return {}
    try:
        payload = json.loads(raw)
    except json.JSONDecodeError as exc:
        raise TriliumError(f"Invalid JSON input: {exc}", code="bad_json") from exc
    if not isinstance(payload, dict):
        raise TriliumError("Skill input must be a JSON object.", code="bad_json")
    return payload


def normalize_api_base_url(value: str) -> str:
    parsed = urlsplit(value.strip())
    if parsed.scheme not in {"http", "https"}:
        raise TriliumError("base_url must use http or https.", code="bad_base_url")
    if not parsed.hostname:
        raise TriliumError("base_url must include a host.", code="bad_base_url")
    if parsed.username or parsed.password:
        raise TriliumError("base_url must not contain credentials.", code="bad_base_url")
    if parsed.query or parsed.fragment:
        raise TriliumError("base_url must not contain query or fragment.", code="bad_base_url")

    path = parsed.path.rstrip("/")
    if not path.endswith("/etapi"):
        path += "/etapi"
    return urlunsplit((parsed.scheme, parsed.netloc, path, "", ""))


def resolve_api_base_url(payload: dict[str, Any]) -> str:
    value = payload.get("base_url") or os.environ.get("TRILIUM_URL")
    if not value:
        raise TriliumError("TRILIUM_URL is not configured.", code="missing_base_url")
    if not isinstance(value, str):
        raise TriliumError("base_url must be a string.", code="bad_base_url")
    return normalize_api_base_url(value)


def resolve_token() -> str:
    token = os.environ.get("TRILIUM_ETAPI_TOKEN", "")
    if not token:
        raise TriliumError("TRILIUM_ETAPI_TOKEN is not configured.", code="missing_token")
    return token


def timeout_from(payload: dict[str, Any]) -> int:
    value = payload.get("timeout", DEFAULT_TIMEOUT)
    if isinstance(value, bool) or not isinstance(value, int) or not 1 <= value <= 120:
        raise TriliumError("timeout must be an integer between 1 and 120.", code="bad_timeout")
    return value


def redact(value: Any, secret: str) -> Any:
    if isinstance(value, dict):
        result: dict[str, Any] = {}
        for key, child in value.items():
            if key.lower() in {"authorization", "token", "password", "auth_token"}:
                result[key] = "<redacted>"
            else:
                result[key] = redact(child, secret)
        return result
    if isinstance(value, list):
        return [redact(item, secret) for item in value]
    if isinstance(value, str) and secret:
        return value.replace(secret, "<redacted>")
    return value


def is_textual_content_type(content_type: str) -> bool:
    media_type = content_type.split(";", 1)[0].strip().lower()
    return (
        media_type.startswith("text/")
        or media_type in {
            "application/json",
            "application/javascript",
            "application/xml",
            "application/xhtml+xml",
            "application/svg+xml",
        }
        or media_type.endswith("+json")
        or media_type.endswith("+xml")
    )


def parse_response(raw: bytes, content_type: str) -> Any:
    if not raw:
        return None
    if not is_textual_content_type(content_type):
        return {
            "binary": True,
            "content_type": content_type or "application/octet-stream",
            "size_bytes": len(raw),
        }

    text = raw.decode("utf-8", errors="replace")
    media_type = content_type.split(";", 1)[0].strip().lower()
    if media_type == "application/json" or media_type.endswith("+json") or text[:1] in {"{", "["}:
        try:
            return json.loads(text)
        except json.JSONDecodeError:
            pass
    return text


def request_trilium(
    payload: dict[str, Any],
    endpoint: str,
    *,
    method: str = "GET",
    query: dict[str, Any] | None = None,
    json_body: dict[str, Any] | None = None,
    text_body: str | None = None,
) -> dict[str, Any]:
    if not endpoint.startswith("/") or ".." in endpoint:
        raise TriliumError("Invalid ETAPI endpoint.", code="bad_endpoint")
    if json_body is not None and text_body is not None:
        raise TriliumError("A request cannot use both JSON and text bodies.", code="bad_request")

    api_base_url = resolve_api_base_url(payload)
    token = resolve_token()
    url = api_base_url + endpoint
    # Trilium 的布尔查询参数严格只接受小写 true/false，不能直接使用
    # urllib 对 Python bool 的默认字符串化结果 True/False。
    compact_query = {
        key: ("true" if value else "false") if isinstance(value, bool) else value
        for key, value in (query or {}).items()
        if value is not None
    }
    if compact_query:
        url += "?" + urlencode(compact_query, doseq=True)

    headers = {
        "Accept": "application/json, text/plain;q=0.9, */*;q=0.5",
        "Authorization": token,
        "User-Agent": f"AgentDock-Trilium-Skill/{SKILL_VERSION}",
    }
    data: bytes | None = None
    if json_body is not None:
        data = json.dumps(json_body, ensure_ascii=False).encode("utf-8")
        headers["Content-Type"] = "application/json"
    elif text_body is not None:
        data = text_body.encode("utf-8")
        headers["Content-Type"] = "text/plain; charset=utf-8"

    request = Request(url, data=data, headers=headers, method=method.upper())
    context = ssl._create_unverified_context() if os.environ.get("TRILIUM_INSECURE_TLS") == "1" else None

    try:
        with urlopen(request, timeout=timeout_from(payload), context=context) as response:
            raw = response.read(MAX_RESPONSE_BYTES + 1)
            status = response.status
            content_type = response.headers.get("Content-Type", "")
    except HTTPError as exc:
        raw = exc.read(MAX_RESPONSE_BYTES + 1)
        status = exc.code
        content_type = exc.headers.get("Content-Type", "") if exc.headers else ""
    except URLError as exc:
        raise TriliumError(f"Cannot reach Trilium ETAPI: {exc.reason}", code="network_error") from exc
    except TimeoutError as exc:
        raise TriliumError("Trilium ETAPI request timed out.", code="network_timeout") from exc

    if len(raw) > MAX_RESPONSE_BYTES:
        raise TriliumError("Trilium ETAPI response exceeded 8 MiB.", code="response_too_large")

    response_payload = redact(parse_response(raw, content_type), token)
    return {
        "success": 200 <= status < 300,
        "service": "trilium",
        "api_base_url": api_base_url,
        "endpoint": endpoint,
        "http_status": status,
        "response": response_payload,
    }


def require(payload: dict[str, Any], key: str) -> Any:
    if key not in payload:
        raise TriliumError(f"Missing required field: {key}.", code="missing_field", details={"field": key})
    return payload[key]


def require_string(payload: dict[str, Any], key: str, *, allow_empty: bool = False) -> str:
    value = require(payload, key)
    if not isinstance(value, str) or (not allow_empty and not value.strip()):
        qualifier = "a string" if allow_empty else "a non-empty string"
        raise TriliumError(f"{key} must be {qualifier}.", code="bad_field", details={"field": key})
    return value if allow_empty else value.strip()


def optional_string(payload: dict[str, Any], key: str) -> str | None:
    if key not in payload:
        return None
    value = payload[key]
    if not isinstance(value, str):
        raise TriliumError(f"{key} must be a string.", code="bad_field", details={"field": key})
    return value


def optional_int(payload: dict[str, Any], key: str, *, minimum: int | None = None, maximum: int | None = None) -> int | None:
    if key not in payload:
        return None
    value = payload[key]
    if isinstance(value, bool) or not isinstance(value, int):
        raise TriliumError(f"{key} must be an integer.", code="bad_field", details={"field": key})
    if minimum is not None and value < minimum:
        raise TriliumError(f"{key} must be at least {minimum}.", code="bad_field", details={"field": key})
    if maximum is not None and value > maximum:
        raise TriliumError(f"{key} must be at most {maximum}.", code="bad_field", details={"field": key})
    return value


def path_id(payload: dict[str, Any], key: str) -> str:
    return quote(require_string(payload, key), safe="")


def copy_fields(payload: dict[str, Any], mapping: dict[str, str]) -> dict[str, Any]:
    body: dict[str, Any] = {}
    for source, target in mapping.items():
        if source in payload:
            body[target] = payload[source]
    return body


def require_confirmed(payload: dict[str, Any], action: str) -> None:
    if payload.get("confirm") is not True:
        raise TriliumError(
            f"{action} requires confirm: true.",
            code="confirmation_required",
            details={"action": action},
        )


def generated_entity_id() -> str:
    return "".join(secrets.choice(ENTITY_ID_ALPHABET) for _ in range(12))


def op_status(payload: dict[str, Any]) -> dict[str, Any]:
    base_value = payload.get("base_url") or os.environ.get("TRILIUM_URL")
    token_present = bool(os.environ.get("TRILIUM_ETAPI_TOKEN"))
    missing = []
    if not base_value:
        missing.append("TRILIUM_URL")
    if not token_present:
        missing.append("TRILIUM_ETAPI_TOKEN")

    result: dict[str, Any] = {
        "success": True,
        "service": "trilium",
        "skill_version": SKILL_VERSION,
        "upstream": UPSTREAM_REPOSITORY,
        "configured": not missing,
        "ready": False,
        "missing_environment": missing,
    }
    if missing:
        return result

    api_result = request_trilium(payload, "/app-info")
    result.update(
        {
            "success": api_result["success"],
            "ready": api_result["success"],
            "api_base_url": api_result["api_base_url"],
            "http_status": api_result["http_status"],
            "app_info": api_result["response"],
        }
    )
    return result


def op_search_notes(payload: dict[str, Any]) -> dict[str, Any]:
    query_text = require_string(payload, "query")
    query: dict[str, Any] = {"search": query_text}
    for source, target in {
        "fast_search": "fastSearch",
        "include_archived_notes": "includeArchivedNotes",
        "ancestor_note_id": "ancestorNoteId",
        "ancestor_depth": "ancestorDepth",
        "order_by": "orderBy",
        "order_direction": "orderDirection",
        "debug": "debug",
    }.items():
        if source in payload:
            value = payload[source]
            if source in {"fast_search", "include_archived_notes", "debug"} and not isinstance(value, bool):
                raise TriliumError(f"{source} must be a boolean.", code="bad_field", details={"field": source})
            query[target] = value
    limit = optional_int(payload, "limit", minimum=1, maximum=1000)
    if limit is not None:
        query["limit"] = limit
    return request_trilium(payload, "/notes", query=query)


def op_get_note(payload: dict[str, Any]) -> dict[str, Any]:
    return request_trilium(payload, f"/notes/{path_id(payload, 'note_id')}")


def op_get_note_content(payload: dict[str, Any]) -> dict[str, Any]:
    return request_trilium(payload, f"/notes/{path_id(payload, 'note_id')}/content")


def op_create_note(payload: dict[str, Any]) -> dict[str, Any]:
    body = {
        "parentNoteId": require_string(payload, "parent_note_id"),
        "title": require_string(payload, "title"),
        "type": require_string(payload, "type"),
        "content": require_string(payload, "content", allow_empty=True) if "content" in payload else "",
    }
    body.update(
        copy_fields(
            payload,
            {
                "mime": "mime",
                "note_position": "notePosition",
                "prefix": "prefix",
                "is_expanded": "isExpanded",
                "note_id": "noteId",
                "date_created": "dateCreated",
                "utc_date_created": "utcDateCreated",
            },
        )
    )
    return request_trilium(payload, "/create-note", method="POST", json_body=body)


def op_update_note(payload: dict[str, Any]) -> dict[str, Any]:
    note_id = path_id(payload, "note_id")
    body = copy_fields(
        payload,
        {
            "title": "title",
            "type": "type",
            "mime": "mime",
            "date_created": "dateCreated",
            "utc_date_created": "utcDateCreated",
        },
    )
    if not body:
        raise TriliumError("update-note requires at least one editable field.", code="missing_changes")
    return request_trilium(payload, f"/notes/{note_id}", method="PATCH", json_body=body)


def op_set_note_content(payload: dict[str, Any]) -> dict[str, Any]:
    note_id = path_id(payload, "note_id")
    content = require_string(payload, "content", allow_empty=True)
    return request_trilium(payload, f"/notes/{note_id}/content", method="PUT", text_body=content)


def op_delete_note(payload: dict[str, Any]) -> dict[str, Any]:
    require_confirmed(payload, "delete-note")
    note_id = path_id(payload, "note_id")
    confirm_title = require_string(payload, "confirm_title")

    target = request_trilium(payload, f"/notes/{note_id}")
    if not target["success"]:
        return target
    target_payload = target["response"]
    current_title = target_payload.get("title") if isinstance(target_payload, dict) else None
    if current_title != confirm_title:
        raise TriliumError(
            "confirm_title does not match the current note title.",
            code="confirmation_mismatch",
            details={"expected_title": current_title},
        )

    deleted = request_trilium(payload, f"/notes/{note_id}", method="DELETE")
    return {
        "success": deleted["success"],
        "service": "trilium",
        "action": "delete-note",
        "target": {"note_id": require_string(payload, "note_id"), "title": current_title},
        "http_status": deleted["http_status"],
        "response": deleted["response"],
    }


def op_note_history(payload: dict[str, Any]) -> dict[str, Any]:
    query = {"ancestorNoteId": payload.get("ancestor_note_id")}
    return request_trilium(payload, "/notes/history", query=query)


def op_list_note_revisions(payload: dict[str, Any]) -> dict[str, Any]:
    return request_trilium(payload, f"/notes/{path_id(payload, 'note_id')}/revisions")


def op_get_revision(payload: dict[str, Any]) -> dict[str, Any]:
    return request_trilium(payload, f"/revisions/{path_id(payload, 'revision_id')}")


def op_get_revision_content(payload: dict[str, Any]) -> dict[str, Any]:
    return request_trilium(payload, f"/revisions/{path_id(payload, 'revision_id')}/content")


def op_create_revision(payload: dict[str, Any]) -> dict[str, Any]:
    body = {"description": optional_string(payload, "description") or ""}
    return request_trilium(payload, f"/notes/{path_id(payload, 'note_id')}/revision", method="POST", json_body=body)


def op_undelete_note(payload: dict[str, Any]) -> dict[str, Any]:
    require_confirmed(payload, "undelete-note")
    note_id = require_string(payload, "note_id")
    if require_string(payload, "confirm_note_id") != note_id:
        raise TriliumError("confirm_note_id does not match note_id.", code="confirmation_mismatch")
    return request_trilium(payload, f"/notes/{quote(note_id, safe='')}/undelete", method="POST", json_body={})


def op_get_branch(payload: dict[str, Any]) -> dict[str, Any]:
    return request_trilium(payload, f"/branches/{path_id(payload, 'branch_id')}")


def op_create_branch(payload: dict[str, Any]) -> dict[str, Any]:
    body = {
        "noteId": require_string(payload, "note_id"),
        "parentNoteId": require_string(payload, "parent_note_id"),
    }
    body.update(copy_fields(payload, {"note_position": "notePosition", "prefix": "prefix", "is_expanded": "isExpanded"}))
    return request_trilium(payload, "/branches", method="POST", json_body=body)


def op_update_branch(payload: dict[str, Any]) -> dict[str, Any]:
    body = copy_fields(payload, {"note_position": "notePosition", "prefix": "prefix", "is_expanded": "isExpanded"})
    if not body:
        raise TriliumError("update-branch requires at least one editable field.", code="missing_changes")
    return request_trilium(payload, f"/branches/{path_id(payload, 'branch_id')}", method="PATCH", json_body=body)


def op_delete_branch(payload: dict[str, Any]) -> dict[str, Any]:
    require_confirmed(payload, "delete-branch")
    branch_id = require_string(payload, "branch_id")
    if require_string(payload, "confirm_branch_id") != branch_id:
        raise TriliumError("confirm_branch_id does not match branch_id.", code="confirmation_mismatch")
    return request_trilium(payload, f"/branches/{quote(branch_id, safe='')}", method="DELETE")


def op_refresh_note_ordering(payload: dict[str, Any]) -> dict[str, Any]:
    return request_trilium(payload, f"/refresh-note-ordering/{path_id(payload, 'parent_note_id')}", method="POST", json_body={})


def op_get_attribute(payload: dict[str, Any]) -> dict[str, Any]:
    return request_trilium(payload, f"/attributes/{path_id(payload, 'attribute_id')}")


def op_create_attribute(payload: dict[str, Any]) -> dict[str, Any]:
    attribute_type = require_string(payload, "type")
    if attribute_type not in {"label", "relation"}:
        raise TriliumError("type must be label or relation.", code="bad_field", details={"field": "type"})
    body = {
        "attributeId": optional_string(payload, "attribute_id") or generated_entity_id(),
        "noteId": require_string(payload, "note_id"),
        "type": attribute_type,
        "name": require_string(payload, "name"),
        "value": optional_string(payload, "value") or "",
    }
    body.update(copy_fields(payload, {"is_inheritable": "isInheritable", "position": "position"}))
    return request_trilium(payload, "/attributes", method="POST", json_body=body)


def op_update_attribute(payload: dict[str, Any]) -> dict[str, Any]:
    body = copy_fields(payload, {"value": "value", "position": "position"})
    if not body:
        raise TriliumError("update-attribute requires value or position.", code="missing_changes")
    return request_trilium(payload, f"/attributes/{path_id(payload, 'attribute_id')}", method="PATCH", json_body=body)


def op_delete_attribute(payload: dict[str, Any]) -> dict[str, Any]:
    require_confirmed(payload, "delete-attribute")
    attribute_id = require_string(payload, "attribute_id")
    if require_string(payload, "confirm_attribute_id") != attribute_id:
        raise TriliumError("confirm_attribute_id does not match attribute_id.", code="confirmation_mismatch")
    return request_trilium(payload, f"/attributes/{quote(attribute_id, safe='')}", method="DELETE")


def op_list_note_attachments(payload: dict[str, Any]) -> dict[str, Any]:
    return request_trilium(payload, f"/notes/{path_id(payload, 'note_id')}/attachments")


def op_get_attachment(payload: dict[str, Any]) -> dict[str, Any]:
    return request_trilium(payload, f"/attachments/{path_id(payload, 'attachment_id')}")


def op_get_attachment_content(payload: dict[str, Any]) -> dict[str, Any]:
    return request_trilium(payload, f"/attachments/{path_id(payload, 'attachment_id')}/content")


def calendar_action(endpoint_template: str, input_key: str) -> Callable[[dict[str, Any]], dict[str, Any]]:
    def execute(payload: dict[str, Any]) -> dict[str, Any]:
        return request_trilium(payload, endpoint_template.format(value=path_id(payload, input_key)))

    return execute


def op_create_backup(payload: dict[str, Any]) -> dict[str, Any]:
    require_confirmed(payload, "create-backup")
    backup_name = require_string(payload, "backup_name")
    if not BACKUP_NAME_PATTERN.fullmatch(backup_name):
        raise TriliumError(
            "backup_name may contain only letters, digits, dot, underscore, and hyphen (1-64 characters).",
            code="bad_backup_name",
        )
    if require_string(payload, "confirm_backup_name") != backup_name:
        raise TriliumError("confirm_backup_name does not match backup_name.", code="confirmation_mismatch")
    return request_trilium(payload, f"/backup/{quote(backup_name, safe='')}", method="PUT", json_body={})


ACTIONS: dict[str, Callable[[dict[str, Any]], dict[str, Any]]] = {
    "status": op_status,
    "search-notes": op_search_notes,
    "get-note": op_get_note,
    "get-note-content": op_get_note_content,
    "create-note": op_create_note,
    "update-note": op_update_note,
    "set-note-content": op_set_note_content,
    "delete-note": op_delete_note,
    "note-history": op_note_history,
    "list-note-revisions": op_list_note_revisions,
    "get-revision": op_get_revision,
    "get-revision-content": op_get_revision_content,
    "create-revision": op_create_revision,
    "undelete-note": op_undelete_note,
    "get-branch": op_get_branch,
    "create-branch": op_create_branch,
    "update-branch": op_update_branch,
    "delete-branch": op_delete_branch,
    "refresh-note-ordering": op_refresh_note_ordering,
    "get-attribute": op_get_attribute,
    "create-attribute": op_create_attribute,
    "update-attribute": op_update_attribute,
    "delete-attribute": op_delete_attribute,
    "list-note-attachments": op_list_note_attachments,
    "get-attachment": op_get_attachment,
    "get-attachment-content": op_get_attachment_content,
    "get-inbox-note": calendar_action("/inbox/{value}", "date"),
    "get-day-note": calendar_action("/calendar/days/{value}", "date"),
    "get-week-note": calendar_action("/calendar/weeks/{value}", "week"),
    "get-month-note": calendar_action("/calendar/months/{value}", "month"),
    "get-year-note": calendar_action("/calendar/years/{value}", "year"),
    "create-backup": op_create_backup,
}


def main() -> int:
    try:
        payload = load_input()
        action = payload.get("skill_action", "status")
        if not isinstance(action, str) or action not in ACTIONS:
            raise TriliumError(
                f"Unsupported skill_action: {action!r}.",
                code="unsupported_action",
                details={"supported_actions": sorted(ACTIONS)},
            )
        result = ACTIONS[action](payload)
        return emit(result, exit_code=0 if result.get("success") else 1)
    except TriliumError as exc:
        result: dict[str, Any] = {
            "success": False,
            "service": "trilium",
            "code": exc.code,
            "message": str(exc),
        }
        if exc.details is not None:
            result["details"] = exc.details
        return emit(result, exit_code=1)
    except Exception as exc:  # pragma: no cover - unexpected boundary failure
        return emit(
            {
                "success": False,
                "service": "trilium",
                "code": "internal_error",
                "message": f"Unexpected Trilium Skill failure: {exc}",
            },
            exit_code=1,
        )


if __name__ == "__main__":
    raise SystemExit(main())

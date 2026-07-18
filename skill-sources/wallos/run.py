#!/usr/bin/env python3
"""Portable Wallos HTTP API helper for the Wallos AgentDock Skill."""
from __future__ import annotations

import json
import os
import sys
from datetime import date
from typing import Any, Callable
from urllib.error import HTTPError, URLError
from urllib.parse import urlencode, urlsplit, urlunsplit
from urllib.request import Request, urlopen

SKILL_VERSION = "0.1.1"
UPSTREAM_REPOSITORY = "https://github.com/ellite/Wallos"
DEFAULT_TIMEOUT = 20
MAX_RESPONSE_BYTES = 2 * 1024 * 1024

SUBSCRIPTION_FIELDS = {
    "name",
    "price",
    "currency_id",
    "frequency",
    "cycle",
    "next_payment",
    "start_date",
    "auto_renew",
    "payment_method_id",
    "payer_user_id",
    "category_id",
    "notes",
    "url",
    "logo_url",
    "notify",
    "notify_days_before",
    "inactive",
    "cancellation_date",
    "replacement_subscription_id",
}
BOOLEAN_SUBSCRIPTION_FIELDS = {"auto_renew", "notify", "inactive"}
DATE_FIELDS = {"next_payment", "start_date", "cancellation_date"}
INTEGER_FIELDS = {
    "currency_id",
    "frequency",
    "cycle",
    "payment_method_id",
    "payer_user_id",
    "category_id",
    "notify_days_before",
    "replacement_subscription_id",
}
ALLOWED_SORTS = {
    "name",
    "id",
    "next_payment",
    "price",
    "payer_user_id",
    "category_id",
    "payment_method_id",
    "inactive",
    "alphanumeric",
}
REQUIRED_ADD_FIELDS = (
    "name",
    "price",
    "currency_id",
    "frequency",
    "cycle",
    "next_payment",
    "payer_user_id",
    "payment_method_id",
    "category_id",
)


class WallosError(RuntimeError):
    def __init__(self, message: str, *, code: str = "wallos_error", details: Any = None) -> None:
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
        raise WallosError(f"Invalid JSON input: {exc}", code="bad_json") from exc
    if not isinstance(payload, dict):
        raise WallosError("Skill input must be a JSON object.", code="bad_json")
    return payload


def normalize_base_url(value: str) -> str:
    parsed = urlsplit(value.strip())
    if parsed.scheme not in {"http", "https"}:
        raise WallosError("base_url must use http or https.", code="bad_base_url")
    if not parsed.hostname:
        raise WallosError("base_url must include a host.", code="bad_base_url")
    if parsed.username or parsed.password:
        raise WallosError("base_url must not contain credentials.", code="bad_base_url")
    if parsed.query or parsed.fragment:
        raise WallosError("base_url must not contain query or fragment.", code="bad_base_url")
    return urlunsplit((parsed.scheme, parsed.netloc, parsed.path.rstrip("/"), "", ""))


def resolve_base_url(payload: dict[str, Any]) -> str:
    value = payload.get("base_url") or os.environ.get("WALLOS_BASE_URL")
    if not value:
        raise WallosError("WALLOS_BASE_URL is not configured.", code="missing_base_url")
    if not isinstance(value, str):
        raise WallosError("base_url must be a string.", code="bad_base_url")
    return normalize_base_url(value)


def resolve_api_key() -> str:
    api_key = os.environ.get("WALLOS_API_KEY", "")
    if not api_key:
        raise WallosError("WALLOS_API_KEY is not configured.", code="missing_api_key")
    return api_key


def timeout_from(payload: dict[str, Any]) -> int:
    value = payload.get("timeout", DEFAULT_TIMEOUT)
    if isinstance(value, bool) or not isinstance(value, int) or not 1 <= value <= 120:
        raise WallosError("timeout must be an integer between 1 and 120.", code="bad_timeout")
    return value


def redact(value: Any, secret: str) -> Any:
    if isinstance(value, dict):
        result: dict[str, Any] = {}
        for key, child in value.items():
            if key.lower() in {"api_key", "apikey", "authorization", "token", "password"}:
                result[key] = "<redacted>"
            else:
                result[key] = redact(child, secret)
        return result
    if isinstance(value, list):
        return [redact(item, secret) for item in value]
    if isinstance(value, str) and secret:
        return value.replace(secret, "<redacted>")
    return value


def form_value(value: Any) -> str:
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, (list, tuple, set)):
        return ",".join(str(item) for item in value)
    return str(value)


def parse_response(raw: bytes) -> Any:
    text = raw.decode("utf-8", errors="replace")
    if not text:
        return None
    try:
        return json.loads(text)
    except json.JSONDecodeError:
        return text


def request_wallos(
    payload: dict[str, Any],
    endpoint: str,
    params: dict[str, Any] | None = None,
) -> dict[str, Any]:
    base_url = resolve_base_url(payload)
    api_key = resolve_api_key()
    form = {"api_key": api_key}
    for key, value in (params or {}).items():
        if value is not None:
            form[key] = form_value(value)

    request = Request(
        base_url + endpoint,
        data=urlencode(form).encode("utf-8"),
        headers={
            "Accept": "application/json",
            "Content-Type": "application/x-www-form-urlencoded",
            "User-Agent": f"AgentDock-Wallos-Skill/{SKILL_VERSION}",
        },
        method="POST",
    )

    try:
        with urlopen(request, timeout=timeout_from(payload)) as response:
            raw = response.read(MAX_RESPONSE_BYTES + 1)
            http_status = response.status
    except HTTPError as exc:
        raw = exc.read(MAX_RESPONSE_BYTES + 1)
        http_status = exc.code
    except URLError as exc:
        raise WallosError(f"Cannot reach Wallos API: {exc.reason}", code="network_error") from exc
    except TimeoutError as exc:
        raise WallosError("Wallos API request timed out.", code="network_timeout") from exc

    if len(raw) > MAX_RESPONSE_BYTES:
        raise WallosError("Wallos API response exceeded 2 MiB.", code="response_too_large")

    response_payload = redact(parse_response(raw), api_key)
    upstream_success = isinstance(response_payload, dict) and response_payload.get("success") is True
    return {
        "success": 200 <= http_status < 300 and upstream_success,
        "service": "wallos",
        "base_url": base_url,
        "endpoint": endpoint,
        "http_status": http_status,
        "response": response_payload,
    }


def require(payload: dict[str, Any], key: str) -> Any:
    if key not in payload:
        raise WallosError(f"Missing required field: {key}.", code="missing_field", details={"field": key})
    return payload[key]


def require_non_empty_string(payload: dict[str, Any], key: str) -> str:
    value = require(payload, key)
    if not isinstance(value, str) or not value.strip():
        raise WallosError(f"{key} must be a non-empty string.", code="bad_field", details={"field": key})
    return value.strip()


def positive_int(value: Any, key: str, *, allow_zero: bool = False) -> int:
    if isinstance(value, bool) or not isinstance(value, int):
        raise WallosError(f"{key} must be an integer.", code="bad_field", details={"field": key})
    minimum = 0 if allow_zero else 1
    if value < minimum:
        raise WallosError(f"{key} must be at least {minimum}.", code="bad_field", details={"field": key})
    return value


def normalize_boolean(value: Any, key: str) -> int:
    if isinstance(value, bool):
        return 1 if value else 0
    if value == 0 or value == 1:
        return int(value)
    raise WallosError(f"{key} must be true, false, 0, or 1.", code="bad_field", details={"field": key})


def normalize_flag(value: Any, key: str) -> bool:
    return bool(normalize_boolean(value, key))


def validate_date(value: Any, key: str, *, allow_empty: bool = False) -> str:
    if allow_empty and (value == "" or value is None):
        return ""
    if not isinstance(value, str):
        raise WallosError(f"{key} must use YYYY-MM-DD.", code="bad_date", details={"field": key})
    try:
        parsed = date.fromisoformat(value)
    except ValueError as exc:
        raise WallosError(f"{key} must be a valid YYYY-MM-DD date.", code="bad_date", details={"field": key}) from exc
    if parsed.isoformat() != value:
        raise WallosError(f"{key} must use YYYY-MM-DD.", code="bad_date", details={"field": key})
    return value


def validate_logo_url(value: Any) -> str:
    if not isinstance(value, str):
        raise WallosError("logo_url must be a string.", code="bad_logo_url")
    parsed = urlsplit(value)
    if parsed.scheme not in {"http", "https"} or not parsed.hostname or parsed.username or parsed.password:
        raise WallosError("logo_url must be an http or https URL without embedded credentials.", code="bad_logo_url")
    return value


def validate_subscription_fields(payload: dict[str, Any], *, require_add_fields: bool) -> dict[str, Any]:
    if require_add_fields:
        for key in REQUIRED_ADD_FIELDS:
            require(payload, key)

    fields = {key: payload[key] for key in SUBSCRIPTION_FIELDS if key in payload}
    if require_add_fields or "name" in fields:
        fields["name"] = require_non_empty_string(fields, "name")

    if "price" in fields:
        price = fields["price"]
        if isinstance(price, bool) or not isinstance(price, (int, float)) or price < 0:
            raise WallosError("price must be a non-negative number.", code="bad_field", details={"field": "price"})

    for key in INTEGER_FIELDS:
        if key not in fields or fields[key] is None or fields[key] == "":
            continue
        fields[key] = positive_int(fields[key], key, allow_zero=key == "notify_days_before")

    if "cycle" in fields and fields["cycle"] not in {1, 2, 3, 4}:
        raise WallosError("cycle must be 1, 2, 3, or 4.", code="bad_cycle")

    for key in BOOLEAN_SUBSCRIPTION_FIELDS:
        if key in fields:
            fields[key] = normalize_boolean(fields[key], key)

    for key in DATE_FIELDS:
        if key in fields:
            fields[key] = validate_date(fields[key], key, allow_empty=key == "cancellation_date")

    if "logo_url" in fields:
        fields["logo_url"] = validate_logo_url(fields["logo_url"])

    return fields


def op_status(payload: dict[str, Any]) -> dict[str, Any]:
    base_value = payload.get("base_url") or os.environ.get("WALLOS_BASE_URL")
    api_key_present = bool(os.environ.get("WALLOS_API_KEY"))
    missing = []
    if not base_value:
        missing.append("WALLOS_BASE_URL")
    if not api_key_present:
        missing.append("WALLOS_API_KEY")

    result: dict[str, Any] = {
        "success": True,
        "service": "wallos",
        "skill_version": SKILL_VERSION,
        "upstream": UPSTREAM_REPOSITORY,
        "configured": not missing,
        "ready": False,
        "missing_environment": missing,
    }
    if missing:
        return result

    api_result = request_wallos(payload, "/api/status/version.php")
    result.update(
        {
            "success": api_result["success"],
            "ready": api_result["success"],
            "base_url": api_result["base_url"],
            "http_status": api_result["http_status"],
            "version": api_result["response"],
        }
    )
    return result


def op_current_user(payload: dict[str, Any]) -> dict[str, Any]:
    return request_wallos(payload, "/api/users/get_user.php")


def op_list_subscriptions(payload: dict[str, Any]) -> dict[str, Any]:
    params: dict[str, Any] = {}
    for source, target in (
        ("member", "member"),
        ("category", "category"),
        ("payment_method", "payment"),
    ):
        if source in payload:
            value = payload[source]
            if isinstance(value, bool) or not isinstance(value, (int, str, list, tuple, set)):
                raise WallosError(f"{source} must be an id or an array of ids.", code="bad_filter")
            params[target] = value

    if "state" in payload:
        params["state"] = normalize_boolean(payload["state"], "state")
    if "disabled_to_bottom" in payload:
        params["disabled_to_bottom"] = normalize_flag(payload["disabled_to_bottom"], "disabled_to_bottom")
    if "convert_currency" in payload:
        params["convert_currency"] = normalize_flag(payload["convert_currency"], "convert_currency")
    if "sort" in payload:
        if payload["sort"] not in ALLOWED_SORTS:
            raise WallosError("sort is not supported by Wallos.", code="bad_sort")
        params["sort"] = payload["sort"]

    return request_wallos(payload, "/api/subscriptions/get_subscriptions.php", params)


def op_get_subscription(payload: dict[str, Any]) -> dict[str, Any]:
    params = {"id": positive_int(require(payload, "id"), "id")}
    if "convert_currency" in payload:
        params["convert_currency"] = normalize_flag(payload["convert_currency"], "convert_currency")
    return request_wallos(payload, "/api/subscriptions/get_subscription.php", params)


def op_monthly_cost(payload: dict[str, Any]) -> dict[str, Any]:
    month = positive_int(require(payload, "month"), "month")
    if month > 12:
        raise WallosError("month must be between 1 and 12.", code="bad_month")
    year = positive_int(require(payload, "year"), "year")
    return request_wallos(payload, "/api/subscriptions/get_monthly_cost.php", {"month": month, "year": year})


def simple_read(endpoint: str) -> Callable[[dict[str, Any]], dict[str, Any]]:
    return lambda payload: request_wallos(payload, endpoint)


def op_add_subscription(payload: dict[str, Any]) -> dict[str, Any]:
    params = {"action": "add", **validate_subscription_fields(payload, require_add_fields=True)}
    return request_wallos(payload, "/api/subscriptions/set_subscriptions.php", params)


def op_edit_subscription(payload: dict[str, Any]) -> dict[str, Any]:
    subscription_id = positive_int(require(payload, "id"), "id")
    fields = validate_subscription_fields(payload, require_add_fields=False)
    if not fields:
        raise WallosError("edit_subscription requires at least one field to change.", code="no_changes")
    params = {"action": "edit", "id": subscription_id, **fields}
    return request_wallos(payload, "/api/subscriptions/set_subscriptions.php", params)


def require_confirmation(payload: dict[str, Any], action: str) -> None:
    if payload.get("confirmed") is not True:
        raise WallosError(f"{action} requires confirmed=true.", code="confirmation_required")


def op_delete_subscription(payload: dict[str, Any]) -> dict[str, Any]:
    require_confirmation(payload, "delete_subscription")
    params = {"action": "delete", "id": positive_int(require(payload, "id"), "id")}
    return request_wallos(payload, "/api/subscriptions/set_subscriptions.php", params)


def op_add_category(payload: dict[str, Any]) -> dict[str, Any]:
    params = {"action": "add", "name": require_non_empty_string(payload, "name")}
    return request_wallos(payload, "/api/categories/set_categories.php", params)


def op_edit_category(payload: dict[str, Any]) -> dict[str, Any]:
    params = {
        "action": "edit",
        "id": positive_int(require(payload, "id"), "id"),
        "name": require_non_empty_string(payload, "name"),
    }
    return request_wallos(payload, "/api/categories/set_categories.php", params)


def op_delete_category(payload: dict[str, Any]) -> dict[str, Any]:
    require_confirmation(payload, "delete_category")
    category_id = positive_int(require(payload, "id"), "id")
    if category_id == 1:
        raise WallosError("Wallos default category 1 cannot be deleted.", code="default_category")
    return request_wallos(
        payload,
        "/api/categories/set_categories.php",
        {"action": "delete", "id": category_id},
    )


ACTIONS: dict[str, Callable[[dict[str, Any]], dict[str, Any]]] = {
    "status": op_status,
    "current_user": op_current_user,
    "list_subscriptions": op_list_subscriptions,
    "get_subscription": op_get_subscription,
    "monthly_cost": op_monthly_cost,
    "list_categories": simple_read("/api/categories/get_categories.php"),
    "list_currencies": simple_read("/api/currencies/get_currencies.php"),
    "list_payment_methods": simple_read("/api/payment_methods/get_payment_methods.php"),
    "list_household": simple_read("/api/household/get_household.php"),
    "add_subscription": op_add_subscription,
    "edit_subscription": op_edit_subscription,
    "delete_subscription": op_delete_subscription,
    "add_category": op_add_category,
    "edit_category": op_edit_category,
    "delete_category": op_delete_category,
}


def main() -> int:
    try:
        payload = load_input()
        action = payload.get("skill_action", "status")
        if not isinstance(action, str) or action not in ACTIONS:
            raise WallosError(
                f"Unsupported skill_action: {action!r}.",
                code="unsupported_action",
                details={"supported_actions": sorted(ACTIONS)},
            )
        return emit(ACTIONS[action](payload))
    except WallosError as exc:
        result: dict[str, Any] = {
            "success": False,
            "service": "wallos",
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
                "service": "wallos",
                "code": "internal_error",
                "message": f"Unexpected Wallos Skill failure: {exc}",
            },
            exit_code=1,
        )


if __name__ == "__main__":
    raise SystemExit(main())

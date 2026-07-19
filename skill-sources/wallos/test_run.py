#!/usr/bin/env python3
from __future__ import annotations

import importlib.util
import io
import json
import os
import unittest
from pathlib import Path
from typing import Any
from unittest import mock
from urllib.error import HTTPError
from urllib.parse import parse_qs

MODULE_PATH = Path(__file__).with_name("run.py")
spec = importlib.util.spec_from_file_location("wallos_skill", MODULE_PATH)
assert spec and spec.loader
wallos = importlib.util.module_from_spec(spec)
spec.loader.exec_module(wallos)


class FakeResponse:
    def __init__(self, status: int, body: Any) -> None:
        self.status = status
        self._raw = json.dumps(body).encode("utf-8")

    def __enter__(self) -> "FakeResponse":
        return self

    def __exit__(self, exc_type: Any, exc: Any, tb: Any) -> None:
        return None

    def read(self, size: int = -1) -> bytes:
        return self._raw if size < 0 else self._raw[:size]


class WallosSkillTests(unittest.TestCase):
    def setUp(self) -> None:
        self.environment = mock.patch.dict(os.environ, {}, clear=True)
        self.environment.start()

    def tearDown(self) -> None:
        self.environment.stop()

    def configure(self) -> None:
        os.environ["WALLOS_BASE_URL"] = "https://wallos.example.com/"
        os.environ["WALLOS_API_KEY"] = "unit-secret-key"

    def test_status_without_configuration_does_not_call_network(self) -> None:
        with mock.patch.object(wallos, "urlopen") as opener:
            result = wallos.op_status({})
        self.assertTrue(result["success"])
        self.assertFalse(result["configured"])
        self.assertFalse(result["ready"])
        self.assertEqual(result["missing_environment"], ["WALLOS_BASE_URL", "WALLOS_API_KEY"])
        opener.assert_not_called()

    def test_normalize_base_url_rejects_credentials_and_query(self) -> None:
        with self.assertRaises(wallos.WallosError) as credentials:
            wallos.normalize_base_url("https://user:pass@example.com")
        self.assertEqual(credentials.exception.code, "bad_base_url")

        with self.assertRaises(wallos.WallosError) as query:
            wallos.normalize_base_url("https://example.com?api_key=bad")
        self.assertEqual(query.exception.code, "bad_base_url")

    def test_request_uses_post_form_and_never_places_key_in_url_or_result(self) -> None:
        self.configure()
        captured: dict[str, Any] = {}

        def fake_urlopen(request: Any, timeout: int) -> FakeResponse:
            captured["url"] = request.full_url
            captured["method"] = request.get_method()
            captured["content_type"] = request.get_header("Content-type")
            captured["form"] = parse_qs(request.data.decode("utf-8"))
            captured["timeout"] = timeout
            return FakeResponse(200, {"success": True, "title": "version", "version": "v5.2.0"})

        with mock.patch.object(wallos, "urlopen", side_effect=fake_urlopen):
            result = wallos.request_wallos({}, "/api/status/version.php")

        rendered = json.dumps(result)
        self.assertTrue(result["success"])
        self.assertEqual(captured["method"], "POST")
        self.assertEqual(captured["content_type"], "application/x-www-form-urlencoded")
        self.assertEqual(captured["form"]["api_key"], ["unit-secret-key"])
        self.assertNotIn("unit-secret-key", captured["url"])
        self.assertNotIn("unit-secret-key", rendered)

    def test_list_subscriptions_maps_filters_to_wallos_parameters(self) -> None:
        with mock.patch.object(wallos, "request_wallos", return_value={"success": True}) as request:
            wallos.op_list_subscriptions(
                {
                    "member": [1, 2],
                    "category": 3,
                    "payment_method": [4, 5],
                    "state": 0,
                    "convert_currency": True,
                    "disabled_to_bottom": True,
                    "sort": "next_payment",
                }
            )

        request.assert_called_once_with(
            mock.ANY,
            "/api/subscriptions/get_subscriptions.php",
            {
                "member": [1, 2],
                "category": 3,
                "payment": [4, 5],
                "state": 0,
                "convert_currency": True,
                "disabled_to_bottom": True,
                "sort": "next_payment",
            },
        )

    def test_add_subscription_validates_and_normalizes_fields(self) -> None:
        payload = {
            "name": "Example",
            "price": 9.99,
            "currency_id": 1,
            "frequency": 1,
            "cycle": 3,
            "next_payment": "2026-08-01",
            "payer_user_id": 1,
            "payment_method_id": 2,
            "category_id": 3,
            "auto_renew": False,
            "notify": True,
            "inactive": False,
        }
        with mock.patch.object(wallos, "request_wallos", return_value={"success": True}) as request:
            wallos.op_add_subscription(payload)

        params = request.call_args.args[2]
        self.assertEqual(params["action"], "add")
        self.assertEqual(params["auto_renew"], 0)
        self.assertEqual(params["notify"], 1)
        self.assertEqual(params["inactive"], 0)

    def test_add_subscription_requires_lookup_relationships(self) -> None:
        payload = {
            "name": "Example",
            "price": 9.99,
            "currency_id": 1,
            "frequency": 1,
            "cycle": 3,
            "next_payment": "2026-08-01",
            "payer_user_id": 1,
            "payment_method_id": 2,
            "category_id": 3,
        }

        for field in ("payer_user_id", "payment_method_id", "category_id"):
            with self.subTest(field=field):
                incomplete = dict(payload)
                incomplete.pop(field)
                with self.assertRaises(wallos.WallosError) as raised:
                    wallos.op_add_subscription(incomplete)
                self.assertEqual(raised.exception.code, "missing_field")
                self.assertEqual(raised.exception.details, {"field": field})

    def test_invalid_cycle_and_date_are_rejected_before_network(self) -> None:
        base = {
            "name": "Example",
            "price": 9.99,
            "currency_id": 1,
            "frequency": 1,
            "cycle": 5,
            "next_payment": "2026-08-01",
            "payer_user_id": 1,
            "payment_method_id": 2,
            "category_id": 3,
        }
        with self.assertRaises(wallos.WallosError) as cycle:
            wallos.op_add_subscription(base)
        self.assertEqual(cycle.exception.code, "bad_cycle")

        base["cycle"] = 3
        base["next_payment"] = "2026-02-30"
        with self.assertRaises(wallos.WallosError) as bad_date:
            wallos.op_add_subscription(base)
        self.assertEqual(bad_date.exception.code, "bad_date")

    def test_edit_requires_a_real_change(self) -> None:
        with self.assertRaises(wallos.WallosError) as raised:
            wallos.op_edit_subscription({"id": 9})
        self.assertEqual(raised.exception.code, "no_changes")

    def test_delete_requires_confirmation(self) -> None:
        with self.assertRaises(wallos.WallosError) as subscription:
            wallos.op_delete_subscription({"id": 9})
        self.assertEqual(subscription.exception.code, "confirmation_required")

        with self.assertRaises(wallos.WallosError) as category:
            wallos.op_delete_category({"id": 2})
        self.assertEqual(category.exception.code, "confirmation_required")

    def test_default_category_is_rejected_locally(self) -> None:
        with self.assertRaises(wallos.WallosError) as raised:
            wallos.op_delete_category({"id": 1, "confirmed": True})
        self.assertEqual(raised.exception.code, "default_category")

    def test_upstream_failure_is_preserved_without_leaking_key(self) -> None:
        self.configure()
        body = {
            "success": False,
            "title": "Invalid API key",
            "message": "unit-secret-key is invalid",
            "api_key": "unit-secret-key",
        }
        with mock.patch.object(wallos, "urlopen", return_value=FakeResponse(200, body)):
            result = wallos.request_wallos({}, "/api/status/version.php")

        rendered = json.dumps(result)
        self.assertFalse(result["success"])
        self.assertNotIn("unit-secret-key", rendered)
        self.assertIn("<redacted>", rendered)

    def test_http_error_body_is_structured_and_redacted(self) -> None:
        self.configure()
        body = json.dumps({"success": False, "message": "unit-secret-key rejected"}).encode("utf-8")
        error = HTTPError(
            "https://wallos.example.com/api/status/version.php",
            401,
            "Unauthorized",
            {},
            io.BytesIO(body),
        )
        with mock.patch.object(wallos, "urlopen", side_effect=error):
            result = wallos.request_wallos({}, "/api/status/version.php")

        self.assertFalse(result["success"])
        self.assertEqual(result["http_status"], 401)
        self.assertNotIn("unit-secret-key", json.dumps(result))


if __name__ == "__main__":
    unittest.main(verbosity=2)

#!/usr/bin/env python3
from __future__ import annotations

import importlib.util
import io
import json
import os
import unittest
import urllib.error
from pathlib import Path
from typing import Any
from unittest import mock

MODULE_PATH = Path(__file__).with_name("run.py")
spec = importlib.util.spec_from_file_location("poke_skill", MODULE_PATH)
assert spec and spec.loader
poke = importlib.util.module_from_spec(spec)
spec.loader.exec_module(poke)


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


class PokeSkillTests(unittest.TestCase):
    def setUp(self) -> None:
        self.old_key = os.environ.pop("POKE_API_KEY", None)

    def tearDown(self) -> None:
        os.environ.pop("POKE_API_KEY", None)
        if self.old_key is not None:
            os.environ["POKE_API_KEY"] = self.old_key

    def test_status_without_key(self) -> None:
        result = poke.status_operation()
        self.assertTrue(result["ok"])
        self.assertFalse(result["configured"])
        self.assertFalse(result["ready"])
        self.assertFalse(result["legacy_endpoint_supported"])

    def test_dry_run_without_key_flattens_context(self) -> None:
        result = poke.send_operation(
            {
                "message": "Summarize this",
                "context": {"url": "https://example.com", "event_id": 7},
                "dry_run": True,
            }
        )
        self.assertTrue(result["ok"])
        self.assertTrue(result["dry_run"])
        self.assertEqual(result["request"]["message"], "Summarize this")
        self.assertEqual(result["request"]["event_id"], 7)

    def test_context_can_supply_message(self) -> None:
        payload, _ = poke.build_payload({"context": {"message": "test", "source": "unit"}})
        self.assertEqual(payload["message"], "test")
        self.assertEqual(payload["source"], "unit")

    def test_parse_response_accepts_plain_text(self) -> None:
        self.assertEqual(poke.parse_response(b"Message sent successfully"), "Message sent successfully")

    def test_parse_response_accepts_empty_body(self) -> None:
        self.assertIsNone(poke.parse_response(b""))

    def test_plain_text_response_is_wrapped_as_object(self) -> None:
        with mock.patch.object(
            poke.urllib.request,
            "urlopen",
            return_value=FakeResponse(200, "Message sent successfully"),
        ):
            result = poke.post_message(
                json.dumps({"message": "hello"}).encode(),
                "unit-key",
                5,
                endpoint="https://poke.test/api/v1/inbound/api-message",
            )
        self.assertTrue(result["ok"])
        self.assertEqual(result["response"], {"body": "Message sent successfully"})

    def test_live_success_uses_bearer_header(self) -> None:
        captured: dict[str, Any] = {}

        def fake_urlopen(request: Any, timeout: int) -> FakeResponse:
            captured["authorization"] = request.get_header("Authorization")
            captured["content_type"] = request.get_header("Content-type")
            captured["body"] = json.loads(request.data.decode("utf-8"))
            captured["timeout"] = timeout
            return FakeResponse(200, {"success": True, "message": "Message sent successfully"})

        with mock.patch.object(poke.urllib.request, "urlopen", side_effect=fake_urlopen):
            result = poke.post_message(
                json.dumps({"message": "hello"}).encode(),
                "unit-key",
                5,
                endpoint="https://poke.test/api/v1/inbound/api-message",
            )

        self.assertTrue(result["ok"])
        self.assertEqual(result["http_status"], 200)
        self.assertEqual(captured["body"], {"message": "hello"})
        self.assertEqual(captured["authorization"], "Bearer unit-key")
        self.assertEqual(captured["content_type"], "application/json")
        self.assertNotIn("unit-key", json.dumps(result))

    def test_unauthorized_response_is_structured_and_redacted(self) -> None:
        response = {
            "success": False,
            "message": "Bearer unit-key is invalid",
            "authorization": "Bearer unit-key",
        }
        error = urllib.error.HTTPError(
            "https://poke.test/api/v1/inbound/api-message",
            401,
            "Unauthorized",
            {},
            io.BytesIO(json.dumps(response).encode("utf-8")),
        )
        with mock.patch.object(poke.urllib.request, "urlopen", side_effect=error):
            result = poke.post_message(
                json.dumps({"message": "hello"}).encode(),
                "unit-key",
                5,
                endpoint="https://poke.test/api/v1/inbound/api-message",
            )

        rendered = json.dumps(result)
        self.assertFalse(result["ok"])
        self.assertEqual(result["code"], "unauthorized")
        self.assertNotIn("unit-key", rendered)
        self.assertIn("<redacted>", rendered)

    def test_network_error_is_structured(self) -> None:
        with mock.patch.object(
            poke.urllib.request,
            "urlopen",
            side_effect=urllib.error.URLError("offline"),
        ):
            with self.assertRaises(poke.PokeError) as caught:
                poke.post_message(
                    json.dumps({"message": "hello"}).encode(),
                    "unit-key",
                    5,
                    endpoint="https://poke.test/api/v1/inbound/api-message",
                )
        self.assertEqual(caught.exception.code, "network_error")

    def test_rejects_missing_message(self) -> None:
        with self.assertRaises(poke.PokeError) as caught:
            poke.build_payload({"context": {"url": "https://example.com"}})
        self.assertEqual(caught.exception.code, "missing_message")

    def test_rejects_oversized_request(self) -> None:
        with self.assertRaises(poke.PokeError) as caught:
            poke.build_payload({"message": "x" * (poke.MAX_REQUEST_BYTES + 1)})
        self.assertEqual(caught.exception.code, "request_too_large")


if __name__ == "__main__":
    unittest.main(verbosity=2)

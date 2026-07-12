#!/usr/bin/env python3
from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import run


class GrokQuotaTests(unittest.TestCase):
    def test_parse_exhausted_quota_with_actual_and_limit(self) -> None:
        body = json.dumps(
            {
                "code": "subscription:free-usage-exhausted",
                "error": (
                    "You've used all the included free usage for model grok-4.5-build-free for now. "
                    "Usage resets over a rolling 24-hour window — "
                    "tokens (actual/limit): 1,065,387/1,000,000."
                ),
            }
        ).encode()

        quota = run.parse_exhausted_quota(429, body)

        self.assertIsNotNone(quota)
        assert quota is not None
        self.assertEqual(quota["actual_tokens"], 1_065_387)
        self.assertEqual(quota["limit_tokens"], 1_000_000)
        self.assertEqual(quota["remaining_tokens"], 0)
        self.assertEqual(quota["overage_tokens"], 65_387)
        self.assertEqual(quota["used_percent"], 106.54)
        self.assertEqual(quota["reset_policy"], "rolling_24_hours")
        self.assertIsNone(quota["reset_at"])

    def test_generic_429_is_not_reported_as_exhausted(self) -> None:
        body = b'{"code":"rate_limit","error":"too many requests"}'
        self.assertIsNone(run.parse_exhausted_quota(429, body))

    def test_nested_provider_error_is_supported(self) -> None:
        body = json.dumps(
            {
                "status": 429,
                "error": {
                    "code": "subscription:free-usage-exhausted",
                    "message": "You've used all the included free usage for now.",
                },
            }
        ).encode()
        quota = run.parse_exhausted_quota(429, body)
        self.assertIsNotNone(quota)
        assert quota is not None
        self.assertTrue(quota["exhausted"])
        self.assertIsNone(quota["actual_tokens"])

    def test_parse_sse_completed_usage(self) -> None:
        body = (
            b'event: response.created\n'
            b'data: {"type":"response.created","response":{"id":"resp_1"}}\n\n'
            b'event: response.completed\n'
            b'data: {"type":"response.completed","response":{"usage":'
            b'{"input_tokens":3,"output_tokens":1,"total_tokens":4}}}\n\n'
        )
        self.assertEqual(
            run.parse_sse_usage(body),
            {"input_tokens": 3, "output_tokens": 1, "total_tokens": 4},
        )

    def test_status_redacts_identity_and_tokens(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            auth_path = Path(temp_dir) / "account.json"
            auth_path.write_text(
                json.dumps(
                    {
                        "type": "xai",
                        "access_token": "secret-access-token",
                        "refresh_token": "secret-refresh-token",
                        "email": "person@example.com",
                        "sub": "subject-123",
                        "expired": "2030-01-01T00:00:00Z",
                    }
                )
            )
            result = run.status({"auth_dir": temp_dir})
            encoded = json.dumps(result)

        self.assertTrue(result["ok"])
        self.assertEqual(len(result["accounts"]), 1)
        self.assertNotIn("secret-access-token", encoded)
        self.assertNotIn("secret-refresh-token", encoded)
        self.assertNotIn("person@example.com", encoded)
        self.assertNotIn("subject-123", encoded)
        self.assertNotIn("account.json", encoded)

    def test_grok_build_cli_auth_file_is_detected_and_redacted(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            auth_path = Path(temp_dir) / "auth.json"
            auth_path.write_text(
                json.dumps(
                    {
                        f"https://auth.x.ai::{run.XAI_CLIENT_ID}": {
                            "key": "cli-access-token",
                            "refresh_token": "cli-refresh-token",
                            "expires_at": "2030-01-01T00:00:00Z",
                            "oidc_issuer": "https://auth.x.ai",
                            "oidc_client_id": run.XAI_CLIENT_ID,
                            "user_id": "user-123",
                            "email": "person@example.com",
                        }
                    }
                )
            )
            result = run.status({"auth_file": str(auth_path)})
            encoded = json.dumps(result)

        self.assertTrue(result["ok"])
        self.assertEqual(len(result["accounts"]), 1)
        self.assertEqual(result["accounts"][0]["credential_source"], "grok_build_cli")
        self.assertTrue(result["accounts"][0]["access_token_present"])
        self.assertTrue(result["accounts"][0]["refresh_token_present"])
        self.assertNotIn("cli-access-token", encoded)
        self.assertNotIn("cli-refresh-token", encoded)
        self.assertNotIn("person@example.com", encoded)
        self.assertNotIn("user-123", encoded)
        self.assertNotIn("auth.json", encoded)

    def test_default_grok_auth_is_kept_with_configured_cpa_directory(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            fake_home = Path(temp_dir) / "home"
            cpa_dir = Path(temp_dir) / "cpa"
            with mock.patch.object(run, "system_home", return_value=fake_home), mock.patch.dict(
                run.os.environ,
                {"GROK_QUOTA_AUTH_DIR": str(cpa_dir)},
                clear=True,
            ):
                files, directories = run.credential_sources({})

        self.assertIn(fake_home / ".grok" / "auth.json", files)
        self.assertIn(cpa_dir, directories)

    def test_query_without_credentials_has_explicit_error(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            with self.assertRaises(run.SkillError) as raised:
                run.query({"auth_dir": temp_dir})
        self.assertEqual(raised.exception.code, "credentials_not_found")

    def test_rejects_non_xai_token_endpoint(self) -> None:
        with self.assertRaises(run.SkillError) as raised:
            run.validate_xai_endpoint("https://evil.example/token", "token")
        self.assertEqual(raised.exception.code, "unsafe_endpoint")

    def test_parse_and_merge_detailed_billing_quota(self) -> None:
        credits_body = json.dumps(
            {
                "config": {
                    "currentPeriod": {
                        "type": "WEEKLY",
                        "start": "2026-07-12T01:36:00Z",
                        "end": "2026-07-19T01:36:00Z",
                    },
                    "creditUsagePercent": 20,
                    "productUsage": [
                        {"product": "GrokChat", "usagePercent": 18},
                        {"product": "GrokBuild", "usagePercent": 2},
                    ],
                }
            }
        ).encode()
        billing_body = json.dumps(
            {
                "config": {
                    "monthlyLimit": {"val": 15_000},
                    "used": {"val": 776},
                    "onDemandCap": {"val": 0},
                    "billingPeriodStart": "2026-07-01T00:00:00Z",
                    "billingPeriodEnd": "2026-08-01T00:00:00Z",
                }
            }
        ).encode()

        merged = run.merge_billing_records(
            run.parse_billing_body(credits_body),
            run.parse_billing_body(billing_body),
        )
        self.assertIsNotNone(merged)
        assert merged is not None
        quota = run.render_billing_quota(merged)

        self.assertEqual(quota["plan"]["name"], "SuperGrok")
        self.assertEqual(quota["weekly_limit"]["used_percent"], 20)
        self.assertEqual(quota["weekly_limit"]["remaining_percent"], 80)
        self.assertEqual(quota["weekly_limit"]["reset_at"], "2026-07-19T01:36:00Z")
        self.assertEqual(quota["product_usage"][0]["product"], "GrokChat")
        self.assertEqual(quota["product_usage"][0]["used_percent"], 18)
        self.assertEqual(quota["product_usage"][1]["product"], "GrokBuild")
        self.assertEqual(quota["product_usage"][1]["used_percent"], 2)
        self.assertEqual(quota["monthly_credits"]["used_cents"], 776)
        self.assertEqual(quota["monthly_credits"]["remaining_cents"], 14_224)
        self.assertEqual(quota["monthly_credits"]["used_usd"], 7.76)
        self.assertEqual(quota["monthly_credits"]["remaining_usd"], 142.24)
        self.assertEqual(quota["monthly_credits"]["limit_usd"], 150.0)
        self.assertEqual(quota["monthly_credits"]["used_percent"], 5.17)
        self.assertFalse(quota["pay_as_you_go"]["enabled"])

    def test_query_prefers_billing_details_without_model_probe(self) -> None:
        credits_body = json.dumps(
            {
                "config": {
                    "currentPeriod": {
                        "type": "WEEKLY",
                        "end": "2026-07-19T01:36:00Z",
                    },
                    "creditUsagePercent": 20,
                    "productUsage": [
                        {"product": "GrokChat", "usagePercent": 18},
                        {"product": "GrokBuild", "usagePercent": 2},
                    ],
                }
            }
        ).encode()
        billing_body = json.dumps(
            {
                "config": {
                    "monthlyLimit": {"val": 15_000},
                    "used": {"val": 776},
                    "onDemandCap": {"val": 0},
                    "billingPeriodEnd": "2026-08-01T00:00:00Z",
                }
            }
        ).encode()

        with tempfile.TemporaryDirectory() as temp_dir:
            auth_path = Path(temp_dir) / "account.json"
            auth_path.write_text(
                json.dumps(
                    {
                        "type": "xai",
                        "access_token": "opaque-access-token",
                        "refresh_token": "opaque-refresh-token",
                        "sub": "user-123",
                        "expired": "2030-01-01T00:00:00Z",
                    }
                )
            )

            def billing_side_effect(
                access_token: str,
                user_id: str,
                url: str,
                timeout: int,
            ) -> tuple[int, bytes, str]:
                self.assertEqual(access_token, "opaque-access-token")
                self.assertEqual(user_id, "user-123")
                self.assertEqual(timeout, 20)
                body = credits_body if url == run.BILLING_CREDITS_URL else billing_body
                return 200, body, "application/json"

            with mock.patch.object(run, "billing_request", side_effect=billing_side_effect), mock.patch.object(
                run,
                "probe_request",
            ) as probe:
                result = run.query({"auth_file": str(auth_path)})

        encoded = json.dumps(result)
        self.assertTrue(result["ok"])
        self.assertTrue(result["available"])
        self.assertEqual(result["source"], "grok_billing_endpoints")
        self.assertFalse(result["probe_may_consume_tokens"])
        self.assertEqual(result["quota"]["weekly_limit"]["used_percent"], 20)
        self.assertEqual(result["quota"]["monthly_credits"]["used_usd"], 7.76)
        self.assertEqual(result["quota"]["monthly_credits"]["remaining_usd"], 142.24)
        self.assertNotIn("opaque-access-token", encoded)
        self.assertNotIn("opaque-refresh-token", encoded)
        self.assertNotIn("user-123", encoded)
        probe.assert_not_called()

    def test_billing_request_uses_fixed_hosts_and_user_id(self) -> None:
        response = mock.MagicMock()
        response.status = 200
        response.headers = {"Content-Type": "application/json"}
        response.read.side_effect = [b'{"config":{}}', b""]
        response.__enter__.return_value = response
        response.__exit__.return_value = False
        with mock.patch.object(run.HTTP_OPENER, "open", return_value=response) as opener:
            status, _, _ = run.billing_request(
                "opaque-token",
                "user-123",
                run.BILLING_CREDITS_URL,
                10,
            )
        request = opener.call_args.args[0]
        headers = {key.lower(): value for key, value in request.header_items()}
        self.assertEqual(status, 200)
        self.assertEqual(request.full_url, run.BILLING_CREDITS_URL)
        self.assertEqual(headers["x-xai-token-auth"], "xai-grok-cli")
        self.assertEqual(headers["x-userid"], "user-123")
        self.assertIsNone(request.data)

    def test_billing_request_rejects_unknown_host(self) -> None:
        with self.assertRaises(run.SkillError) as raised:
            run.billing_request("opaque-token", "", "https://evil.example/billing", 10)
        self.assertEqual(raised.exception.code, "unsafe_endpoint")

    def test_probe_request_uses_fixed_cli_proxy_host(self) -> None:
        response = mock.MagicMock()
        response.status = 200
        response.headers = {"Content-Type": "text/event-stream"}
        response.read.side_effect = [b"", b""]
        response.__enter__.return_value = response
        response.__exit__.return_value = False
        with mock.patch.object(run.HTTP_OPENER, "open", return_value=response) as opener:
            status, _, _ = run.probe_request("opaque-token", run.DEFAULT_MODEL, 10)
        request = opener.call_args.args[0]
        self.assertEqual(status, 200)
        self.assertEqual(request.full_url, run.CLI_RESPONSES_URL)
        self.assertEqual(request.get_header("X-xai-token-auth"), "xai-grok-cli")
        self.assertNotIn("opaque-token", str(request.data))

    def test_sanitize_text_redacts_secret_fields(self) -> None:
        text = '{"access_token":"plain-secret","authorization":"token-value"}'
        sanitized = run.sanitize_text(text)
        self.assertNotIn("plain-secret", sanitized)
        self.assertNotIn("token-value", sanitized)
        self.assertEqual(sanitized.count("[REDACTED]"), 2)

    def test_redirect_handler_refuses_redirects(self) -> None:
        handler = run.NoRedirectHandler()
        self.assertIsNone(handler.redirect_request(None, None, 302, "Found", {}, "https://evil.example"))


if __name__ == "__main__":
    unittest.main()

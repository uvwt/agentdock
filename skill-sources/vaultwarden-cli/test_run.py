#!/usr/bin/env python3
import importlib.util
import pathlib
import unittest


MODULE_PATH = pathlib.Path(__file__).with_name("run.py")
SPEC = importlib.util.spec_from_file_location("vaultwarden_skill", MODULE_PATH)
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class VaultwardenSkillTests(unittest.TestCase):
    def test_https_server(self):
        self.assertEqual(MODULE.validate_server("https://vault.example.com/"), "https://vault.example.com")

    def test_remote_http_rejected(self):
        with self.assertRaises(SystemExit):
            MODULE.validate_server("http://vault.example.com")

    def test_local_http_allowed(self):
        self.assertEqual(MODULE.validate_server("http://127.0.0.1:8080/"), "http://127.0.0.1:8080")

    def test_sanitize_drops_secret_values(self):
        item = {
            "id": "id-1",
            "name": "Example",
            "type": 1,
            "login": {"username": "alice", "password": "secret", "totp": "seed", "uris": [{"uri": "https://example.com/path?q=1"}]},
            "notes": "private",
            "fields": [{"name": "API Key", "value": "private-key"}],
        }
        result = MODULE.sanitize_item(item)
        encoded = str(result)
        self.assertNotIn("alice", encoded)
        self.assertNotIn("secret", encoded)
        self.assertNotIn("private-key", encoded)
        self.assertNotIn("private", encoded)
        self.assertEqual(result["login_hosts"], ["example.com"])
        self.assertEqual(result["custom_field_names"], ["API Key"])


if __name__ == "__main__":
    unittest.main()

from __future__ import annotations

import importlib.util
import json
import os
import sys
import threading
import unittest
from contextlib import contextmanager
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any, Iterator
from urllib.parse import parse_qs, urlsplit
from unittest.mock import patch

MODULE_PATH = Path(__file__).resolve().parents[1] / "run.py"
SPEC = importlib.util.spec_from_file_location("linkwarden_skill_run", MODULE_PATH)
assert SPEC and SPEC.loader
skill = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = skill
SPEC.loader.exec_module(skill)


class RecordingHandler(BaseHTTPRequestHandler):
    def log_message(self, format: str, *args: Any) -> None:
        return

    def do_GET(self) -> None:
        self._handle("GET")

    def do_POST(self) -> None:
        self._handle("POST")

    def do_PUT(self) -> None:
        self._handle("PUT")

    def do_DELETE(self) -> None:
        self._handle("DELETE")

    def _handle(self, method: str) -> None:
        length = int(self.headers.get("Content-Length", "0"))
        raw = self.rfile.read(length) if length else b""
        body = json.loads(raw) if raw else None
        parsed = urlsplit(self.path)
        record = {
            "method": method,
            "path": parsed.path,
            "query": parse_qs(parsed.query),
            "authorization": self.headers.get("Authorization"),
            "body": body,
        }
        self.server.records.append(record)  # type: ignore[attr-defined]

        status, response = self.server.responses.get(  # type: ignore[attr-defined]
            (method, parsed.path),
            (404, {"response": "not found"}),
        )
        encoded = json.dumps(response).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(encoded)))
        self.end_headers()
        self.wfile.write(encoded)


@contextmanager
def api_server(
    responses: dict[tuple[str, str], tuple[int, Any]],
) -> Iterator[tuple[str, list[dict[str, Any]]]]:
    server = ThreadingHTTPServer(("127.0.0.1", 0), RecordingHandler)
    server.responses = responses  # type: ignore[attr-defined]
    server.records = []  # type: ignore[attr-defined]
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        host, port = server.server_address
        yield f"http://{host}:{port}", server.records  # type: ignore[attr-defined]
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=2)


@contextmanager
def skill_environment(base_url: str = "", token: str = "") -> Iterator[None]:
    clean = {
        key: value
        for key, value in os.environ.items()
        if not key.startswith("LINKWARDEN_")
    }
    if base_url:
        clean["LINKWARDEN_URL"] = base_url
    if token:
        clean["LINKWARDEN_TOKEN"] = token
    with patch.dict(os.environ, clean, clear=True):
        yield


class LinkwardenSkillTests(unittest.TestCase):
    def test_status_without_configuration_is_safe(self) -> None:
        with skill_environment():
            result = skill.run_action({"skill_action": "status"})

        self.assertFalse(result["configured"])
        self.assertFalse(result["token_configured"])
        self.assertEqual(result["skill_version"], "1.1.0")

    def test_base_url_rejects_credentials_and_query(self) -> None:
        with self.assertRaises(skill.SkillInputError):
            skill.normalize_base_url("https://user:pass@example.com")
        with self.assertRaises(skill.SkillInputError):
            skill.normalize_base_url("https://example.com?token=secret")

    def test_search_uses_new_endpoint_and_bearer_auth(self) -> None:
        responses = {
            ("GET", "/api/v1/search"): (200, {"data": {"links": []}}),
        }
        with api_server(responses) as (base_url, records), skill_environment(
            base_url, "test-token"
        ):
            result = skill.run_action(
                {
                    "skill_action": "search",
                    "query": "agentdock",
                    "collection_id": 42,
                    "pinned_only": True,
                }
            )

        self.assertTrue(result["request_succeeded"])
        self.assertEqual(len(records), 1)
        request = records[0]
        self.assertEqual(request["path"], "/api/v1/search")
        self.assertEqual(request["authorization"], "Bearer test-token")
        self.assertEqual(request["query"]["searchQueryString"], ["agentdock"])
        self.assertEqual(request["query"]["collectionId"], ["42"])
        self.assertEqual(request["query"]["pinnedOnly"], ["true"])

    def test_create_link_maps_collection_and_tags(self) -> None:
        responses = {
            ("POST", "/api/v1/links"): (200, {"response": {"id": 101}}),
        }
        with api_server(responses) as (base_url, records), skill_environment(
            base_url, "test-token"
        ):
            result = skill.run_action(
                {
                    "skill_action": "create-link",
                    "url": "https://example.com",
                    "name": "Example",
                    "collection_id": 42,
                    "tags": ["reference", "reference", "ai"],
                }
            )

        self.assertTrue(result["request_succeeded"])
        body = records[0]["body"]
        self.assertEqual(body["collection"], {"id": 42})
        self.assertEqual(
            body["tags"],
            [{"name": "reference"}, {"name": "ai"}],
        )

    def test_update_link_reads_then_merges_complete_object(self) -> None:
        existing = {
            "id": 101,
            "name": "Old",
            "url": "https://example.com",
            "description": "Keep this",
            "icon": None,
            "iconWeight": None,
            "color": None,
            "collectionId": 42,
            "collection": {"id": 42, "ownerId": 1},
            "tags": [{"id": 7, "name": "old"}],
            "pinnedBy": [{"id": 9}],
        }
        responses = {
            ("GET", "/api/v1/links/101"): (200, {"response": existing}),
            ("PUT", "/api/v1/links/101"): (200, {"response": {"id": 101}}),
        }
        with api_server(responses) as (base_url, records), skill_environment(
            base_url, "test-token"
        ):
            result = skill.run_action(
                {
                    "skill_action": "update-link",
                    "id": 101,
                    "name": "New",
                    "tags": ["updated"],
                }
            )

        self.assertTrue(result["request_succeeded"])
        self.assertEqual([record["method"] for record in records], ["GET", "PUT"])
        body = records[1]["body"]
        self.assertEqual(body["id"], 101)
        self.assertEqual(body["name"], "New")
        self.assertEqual(body["url"], "https://example.com")
        self.assertEqual(body["description"], "Keep this")
        self.assertEqual(body["collection"], {"id": 42, "ownerId": 1})
        self.assertEqual(body["tags"], [{"name": "updated"}])
        self.assertEqual(body["pinnedBy"], [{"id": 9}])

    def test_update_collection_preserves_members(self) -> None:
        existing = {
            "id": 42,
            "name": "Old Collection",
            "description": "Keep",
            "color": "#123456",
            "isPublic": False,
            "icon": "folder",
            "iconWeight": "regular",
            "parentId": None,
            "members": [
                {
                    "userId": 8,
                    "canCreate": True,
                    "canUpdate": True,
                    "canDelete": False,
                }
            ],
        }
        responses = {
            ("GET", "/api/v1/collections/42"): (200, {"response": existing}),
            ("PUT", "/api/v1/collections/42"): (
                200,
                {"response": {"id": 42, "name": "New Collection"}},
            ),
        }
        with api_server(responses) as (base_url, records), skill_environment(
            base_url, "test-token"
        ):
            result = skill.run_action(
                {
                    "skill_action": "update-collection",
                    "id": 42,
                    "name": "New Collection",
                }
            )

        self.assertTrue(result["request_succeeded"])
        body = records[1]["body"]
        self.assertEqual(body["name"], "New Collection")
        self.assertEqual(body["description"], "Keep")
        self.assertEqual(
            body["members"],
            [
                {
                    "userId": 8,
                    "canCreate": True,
                    "canUpdate": True,
                    "canDelete": False,
                }
            ],
        )

    def test_destructive_link_actions_require_confirmation(self) -> None:
        with skill_environment("https://links.example.com", "test-token"):
            for action in ("delete-link", "rearchive-link"):
                with self.subTest(action=action):
                    with self.assertRaises(skill.SkillInputError):
                        skill.run_action({"skill_action": action, "id": 101})

    def test_delete_collection_requires_exact_name(self) -> None:
        responses = {
            ("GET", "/api/v1/collections/42"): (
                200,
                {"response": {"id": 42, "name": "Development"}},
            ),
            ("DELETE", "/api/v1/collections/42"): (
                200,
                {"response": "Collection deleted."},
            ),
        }
        with api_server(responses) as (base_url, records), skill_environment(
            base_url, "test-token"
        ):
            with self.assertRaises(skill.SkillInputError):
                skill.run_action(
                    {
                        "skill_action": "delete-collection",
                        "id": 42,
                        "confirm": True,
                        "confirm_name": "Wrong",
                    }
                )
            result = skill.run_action(
                {
                    "skill_action": "delete-collection",
                    "id": 42,
                    "confirm": True,
                    "confirm_name": "Development",
                }
            )

        self.assertTrue(result["request_succeeded"])
        self.assertEqual(
            [record["method"] for record in records],
            ["GET", "GET", "DELETE"],
        )

    def test_delete_tag_requires_exact_name_and_link_count(self) -> None:
        responses = {
            ("GET", "/api/v1/tags/77"): (
                200,
                {
                    "response": {
                        "id": 77,
                        "name": "legacy-tag",
                        "_count": {"links": 3},
                    }
                },
            ),
            ("DELETE", "/api/v1/tags/77"): (
                200,
                {"response": "Tag deleted."},
            ),
        }
        with api_server(responses) as (base_url, records), skill_environment(
            base_url, "test-token"
        ):
            with self.assertRaises(skill.SkillInputError):
                skill.run_action(
                    {
                        "skill_action": "delete-tag",
                        "id": 77,
                        "confirm_name": "legacy-tag",
                        "confirm_link_count": 3,
                    }
                )
            with self.assertRaises(skill.SkillInputError):
                skill.run_action(
                    {
                        "skill_action": "delete-tag",
                        "id": 77,
                        "confirm": True,
                        "confirm_name": "wrong-tag",
                        "confirm_link_count": 3,
                    }
                )
            with self.assertRaises(skill.SkillInputError):
                skill.run_action(
                    {
                        "skill_action": "delete-tag",
                        "id": 77,
                        "confirm": True,
                        "confirm_name": "legacy-tag",
                        "confirm_link_count": 2,
                    }
                )
            result = skill.run_action(
                {
                    "skill_action": "delete-tag",
                    "id": 77,
                    "confirm": True,
                    "confirm_name": "legacy-tag",
                    "confirm_link_count": 3,
                }
            )

        self.assertTrue(result["request_succeeded"])
        self.assertEqual(
            [record["method"] for record in records],
            ["GET", "GET", "GET", "DELETE"],
        )


if __name__ == "__main__":
    unittest.main()

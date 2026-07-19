#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import subprocess
import sys
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlsplit

ROOT = Path(__file__).resolve().parents[1]
RUNNER = ROOT / "run.py"
TOKEN = "test-etapi-secret-token"


class FakeTriliumHandler(BaseHTTPRequestHandler):
    requests: list[dict[str, object]] = []

    def log_message(self, format: str, *args: object) -> None:
        return

    def record_request(self) -> tuple[str, bytes]:
        parsed = urlsplit(self.path)
        length = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(length) if length else b""
        self.__class__.requests.append(
            {
                "method": self.command,
                "path": parsed.path,
                "query": parse_qs(parsed.query),
                "body": body,
                "authorization": self.headers.get("Authorization"),
                "content_type": self.headers.get("Content-Type"),
            }
        )
        return parsed.path, body

    def send_json(self, status: int, payload: object) -> None:
        raw = json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def send_empty(self, status: int = 204) -> None:
        self.send_response(status)
        self.end_headers()

    def do_GET(self) -> None:
        path, _ = self.record_request()
        if path == "/etapi/app-info":
            self.send_json(200, {"appVersion": "0.103.0", "dbVersion": 234})
        elif path == "/etapi/notes":
            self.send_json(200, {"results": [{"noteId": "n1", "title": "Target"}]})
        elif path == "/etapi/notes/n1":
            self.send_json(200, {"noteId": "n1", "title": "Target", "type": "text"})
        elif path == "/etapi/notes/leak":
            self.send_json(500, {"message": f"upstream echoed {TOKEN}", "token": TOKEN})
        elif path == "/etapi/attachments/bin/content":
            raw = b"\x00\x01\x02\xff"
            self.send_response(200)
            self.send_header("Content-Type", "application/octet-stream")
            self.send_header("Content-Length", str(len(raw)))
            self.end_headers()
            self.wfile.write(raw)
        else:
            self.send_json(404, {"code": "NOT_FOUND"})

    def do_POST(self) -> None:
        path, body = self.record_request()
        if path == "/etapi/create-note":
            request_payload = json.loads(body)
            self.send_json(
                201,
                {
                    "note": {
                        "noteId": "new-note",
                        "title": request_payload["title"],
                        "type": request_payload["type"],
                    },
                    "branch": {"branchId": "new-branch"},
                },
            )
        elif path == "/etapi/attributes":
            self.send_json(201, json.loads(body))
        else:
            self.send_empty()

    def do_PATCH(self) -> None:
        self.record_request()
        self.send_json(200, {"success": True})

    def do_PUT(self) -> None:
        self.record_request()
        self.send_empty()

    def do_DELETE(self) -> None:
        path, _ = self.record_request()
        if path == "/etapi/notes/n1":
            self.send_empty()
        else:
            self.send_json(404, {"code": "NOT_FOUND"})


class TriliumSkillTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.server = ThreadingHTTPServer(("127.0.0.1", 0), FakeTriliumHandler)
        cls.thread = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.thread.start()
        host, port = cls.server.server_address
        cls.base_url = f"http://{host}:{port}"

    @classmethod
    def tearDownClass(cls) -> None:
        cls.server.shutdown()
        cls.server.server_close()
        cls.thread.join(timeout=5)

    def setUp(self) -> None:
        FakeTriliumHandler.requests.clear()

    def run_skill(
        self,
        payload: object,
        *,
        configured: bool = True,
    ) -> tuple[subprocess.CompletedProcess[str], dict[str, object]]:
        env = os.environ.copy()
        env.pop("TRILIUM_URL", None)
        env.pop("TRILIUM_ETAPI_TOKEN", None)
        env.pop("TRILIUM_INSECURE_TLS", None)
        if configured:
            env["TRILIUM_URL"] = self.base_url
            env["TRILIUM_ETAPI_TOKEN"] = TOKEN
        proc = subprocess.run(
            [sys.executable, str(RUNNER)],
            input=json.dumps(payload),
            text=True,
            capture_output=True,
            env=env,
            check=False,
        )
        self.assertTrue(proc.stdout.strip(), proc.stderr)
        return proc, json.loads(proc.stdout)

    def test_status_without_environment_is_read_only(self) -> None:
        proc, result = self.run_skill({"skill_action": "status"}, configured=False)
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertFalse(result["configured"])
        self.assertEqual(
            result["missing_environment"],
            ["TRILIUM_URL", "TRILIUM_ETAPI_TOKEN"],
        )
        self.assertEqual(FakeTriliumHandler.requests, [])

    def test_status_calls_app_info_with_token(self) -> None:
        proc, result = self.run_skill({"skill_action": "status"})
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertTrue(result["ready"])
        self.assertEqual(result["app_info"]["appVersion"], "0.103.0")
        request = FakeTriliumHandler.requests[0]
        self.assertEqual(request["path"], "/etapi/app-info")
        self.assertEqual(request["authorization"], TOKEN)

    def test_search_maps_query_fields(self) -> None:
        proc, result = self.run_skill(
            {
                "skill_action": "search-notes",
                "query": "tolkien #book",
                "fast_search": True,
                "ancestor_note_id": "books",
                "limit": 7,
            }
        )
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertTrue(result["success"])
        query = FakeTriliumHandler.requests[0]["query"]
        self.assertEqual(query["search"], ["tolkien #book"])
        self.assertEqual(query["fastSearch"], ["true"])
        self.assertEqual(query["ancestorNoteId"], ["books"])
        self.assertEqual(query["limit"], ["7"])

    def test_set_content_sends_raw_text(self) -> None:
        content = "<p>新的正文</p>"
        proc, result = self.run_skill(
            {"skill_action": "set-note-content", "note_id": "n1", "content": content}
        )
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertTrue(result["success"])
        request = FakeTriliumHandler.requests[0]
        self.assertEqual(request["method"], "PUT")
        self.assertEqual(request["body"].decode("utf-8"), content)
        self.assertTrue(str(request["content_type"]).startswith("text/plain"))

    def test_create_note_maps_snake_case_fields(self) -> None:
        proc, result = self.run_skill(
            {
                "skill_action": "create-note",
                "parent_note_id": "root",
                "title": "Created",
                "type": "code",
                "content": "print('ok')",
                "mime": "text/x-python",
                "note_position": 30,
            }
        )
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertEqual(result["response"]["note"]["noteId"], "new-note")
        request_body = json.loads(FakeTriliumHandler.requests[0]["body"])
        self.assertEqual(request_body["parentNoteId"], "root")
        self.assertEqual(request_body["notePosition"], 30)
        self.assertNotIn("parent_note_id", request_body)

    def test_delete_requires_confirmation_without_network_call(self) -> None:
        proc, result = self.run_skill(
            {"skill_action": "delete-note", "note_id": "n1", "confirm_title": "Target"}
        )
        self.assertEqual(proc.returncode, 1)
        self.assertEqual(result["code"], "confirmation_required")
        self.assertEqual(FakeTriliumHandler.requests, [])

    def test_delete_title_mismatch_does_not_delete(self) -> None:
        proc, result = self.run_skill(
            {
                "skill_action": "delete-note",
                "note_id": "n1",
                "confirm": True,
                "confirm_title": "Wrong",
            }
        )
        self.assertEqual(proc.returncode, 1)
        self.assertEqual(result["code"], "confirmation_mismatch")
        self.assertEqual([item["method"] for item in FakeTriliumHandler.requests], ["GET"])

    def test_delete_reads_target_then_deletes(self) -> None:
        proc, result = self.run_skill(
            {
                "skill_action": "delete-note",
                "note_id": "n1",
                "confirm": True,
                "confirm_title": "Target",
            }
        )
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertTrue(result["success"])
        self.assertEqual(result["target"], {"note_id": "n1", "title": "Target"})
        self.assertEqual([item["method"] for item in FakeTriliumHandler.requests], ["GET", "DELETE"])

    def test_upstream_error_redacts_token(self) -> None:
        proc, result = self.run_skill({"skill_action": "get-note", "note_id": "leak"})
        self.assertEqual(proc.returncode, 1)
        rendered = json.dumps(result)
        self.assertNotIn(TOKEN, rendered)
        self.assertIn("<redacted>", rendered)

    def test_binary_content_returns_only_metadata(self) -> None:
        proc, result = self.run_skill(
            {"skill_action": "get-attachment-content", "attachment_id": "bin"}
        )
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertEqual(
            result["response"],
            {
                "binary": True,
                "content_type": "application/octet-stream",
                "size_bytes": 4,
            },
        )

    def test_create_attribute_generates_entity_id(self) -> None:
        proc, result = self.run_skill(
            {
                "skill_action": "create-attribute",
                "note_id": "n1",
                "type": "label",
                "name": "status",
                "value": "done",
            }
        )
        self.assertEqual(proc.returncode, 0, proc.stderr)
        attribute_id = result["response"]["attributeId"]
        self.assertEqual(len(attribute_id), 12)
        self.assertTrue(attribute_id.isalnum())

    def test_unknown_action_fails_with_supported_actions(self) -> None:
        proc, result = self.run_skill({"skill_action": "raw-request"})
        self.assertEqual(proc.returncode, 1)
        self.assertEqual(result["code"], "unsupported_action")
        self.assertNotIn("raw-request", result["details"]["supported_actions"])

    def test_non_object_input_fails(self) -> None:
        proc, result = self.run_skill(["status"])
        self.assertEqual(proc.returncode, 1)
        self.assertEqual(result["code"], "bad_json")


if __name__ == "__main__":
    unittest.main()

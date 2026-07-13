from __future__ import annotations

import json
import os
import pathlib
import subprocess
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

ROOT = pathlib.Path(__file__).resolve().parents[1]
TOKEN = "test-api-token-never-print"


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_args):
        return

    def send_json(self, payload, status=200):
        body = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def authorized(self):
        return self.headers.get("Authorization") == "Bearer " + TOKEN

    def do_POST(self):
        if not self.authorized():
            self.send_json({"error": "authorization required"}, 401)
            return
        if self.path == "/api/v1/sync-requests":
            self.send_json({
                "id": "sync_1", "device_id": "device_1", "status": "pending",
                "requested_at": "2026-06-16T10:00:00Z", "expires_at": "2026-06-17T10:00:00Z",
                "created_at": "2026-06-16T10:00:00Z", "updated_at": "2026-06-16T10:00:00Z",
            }, 202)
            return
        self.send_json({"error": "not found"}, 404)

    def do_GET(self):
        path = self.path.split("?", 1)[0]
        if path == "/healthz":
            self.send_json({"status": "ok", "api_version": "v1"})
            return
        if path == "/api/v1/capabilities":
            self.send_json({"api_version": "v1", "supported_features": ["normalized_samples", "daily_summaries", "data_freshness", "async_sync_requests"]})
            return
        if not self.authorized():
            self.send_json({"error": "authorization required"}, 401)
            return
        if path == "/api/v1/health/freshness":
            self.send_json({"status": "healthy", "devices": [], "types": [], "server_time": "2026-06-16T10:00:00Z"})
        elif path == "/api/v1/health/daily":
            self.send_json({"items": [{"date": "2026-06-16", "type": "step_count", "healthkit_identifier": "HKQuantityTypeIdentifierStepCount", "sample_count": 1, "numeric_count": 1, "sum": 1200, "timezone": "Asia/Shanghai", "source_count": 1, "updated_at": "2026-06-16T10:00:00Z"}], "count": 1})
        elif path == "/api/v1/health/latest":
            self.send_json({"items": [{"id": "s1", "type": "step_count", "healthkit_identifier": "HKQuantityTypeIdentifierStepCount", "category": "quantity", "value": 1200, "unit": "count", "start_at": "2026-06-16T09:00:00Z", "end_at": "2026-06-16T10:00:00Z", "device_id": "device_1", "sensitive": False, "deleted": False}], "count": 1})
        elif path == "/api/v1/health/trends":
            self.send_json({"items": [{"date": "2026-06-16", "type": "step_count", "healthkit_identifier": "HKQuantityTypeIdentifierStepCount", "sample_count": 1, "numeric_count": 1, "sum": 1200, "timezone": "Asia/Shanghai", "source_count": 1, "updated_at": "2026-06-16T10:00:00Z"}], "count": 1})
        elif path == "/api/v1/health/samples":
            self.send_json({"items": [{"id": "s1", "type": "step_count", "healthkit_identifier": "HKQuantityTypeIdentifierStepCount", "category": "quantity", "value": 1200, "unit": "count", "start_at": "2026-06-16T09:00:00Z", "end_at": "2026-06-16T10:00:00Z", "device_id": "device_1", "sensitive": False, "deleted": False}], "count": 1})
        elif path == "/api/v1/health/clinical":
            self.send_json({"items": [{"stable_record_id": "c1", "resource_type": "AllergyIntolerance"}], "count": 1})
        elif path == "/api/v1/sync-requests/sync_1":
            self.send_json({"id": "sync_1", "device_id": "device_1", "status": "completed", "requested_at": "2026-06-16T10:00:00Z", "completed_at": "2026-06-16T10:01:00Z", "expires_at": "2026-06-17T10:00:00Z", "created_at": "2026-06-16T10:00:00Z", "updated_at": "2026-06-16T10:01:00Z"})
        else:
            self.send_json({"error": "not found"}, 404)


class SkillTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        cls.thread = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.thread.start()

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()
        cls.server.server_close()
        cls.thread.join(timeout=2)

    def run_skill(self, operation, payload=None):
        env = os.environ.copy()
        env.update({
            "AGENTDOCK_SKILL_VERSION": "0.2.4",
            "VITAPULSE_BASE_URL": f"http://127.0.0.1:{self.server.server_port}",
            "VITAPULSE_API_TOKEN": TOKEN,
            "PYTHONDONTWRITEBYTECODE": "1",
        })
        proc = subprocess.run(
            [str(ROOT / "run.py")],
            input=json.dumps({"skill_action": operation, **(payload or {})}),
            text=True,
            capture_output=True,
            env=env,
            check=False,
            timeout=5,
        )
        self.assertEqual(proc.returncode, 0, proc.stderr + proc.stdout)
        self.assertNotIn(TOKEN, proc.stdout + proc.stderr)
        return json.loads(proc.stdout)

    def test_status(self):
        result = self.run_skill("status")
        self.assertTrue(result["ready"])
        self.assertTrue(result["authenticated"])
        self.assertEqual(result["freshness"]["status"], "healthy")

    def test_summary_latest_trend_and_samples(self):
        self.assertEqual(self.run_skill("today_summary")["count"], 1)
        self.assertEqual(self.run_skill("latest_metric", {"type_identifier": "HKQuantityTypeIdentifierStepCount"})["count"], 1)
        self.assertEqual(self.run_skill("trend", {"type_identifier": "HKQuantityTypeIdentifierStepCount", "days": 7})["count"], 1)
        self.assertEqual(self.run_skill("query_samples", {"limit": 10})["items"][0]["type"], "step_count")

    def test_sync_request_and_status(self):
        created = self.run_skill("sync_request")
        self.assertFalse(created["immediate_execution_guaranteed"])
        self.assertEqual(created["request"]["status"], "pending")
        status = self.run_skill("sync_request_status", {"request_id": "sync_1"})
        self.assertEqual(status["request"]["status"], "completed")

    def test_clinical_summary(self):
        result = self.run_skill("clinical_summary")
        self.assertEqual(result["count"], 1)


if __name__ == "__main__":
    unittest.main()

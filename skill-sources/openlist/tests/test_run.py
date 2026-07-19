#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
RUNNER = ROOT / "run.py"


class SessionPathTests(unittest.TestCase):
    def run_session_status(self, env_overrides: dict[str, str], unset: tuple[str, ...] = ()) -> dict[str, object]:
        env = os.environ.copy()
        for key in unset:
            env.pop(key, None)
        env.update(env_overrides)
        proc = subprocess.run(
            [sys.executable, str(RUNNER)],
            input=json.dumps({"skill_action": "session-status"}),
            text=True,
            capture_output=True,
            env=env,
            check=False,
        )
        self.assertEqual(proc.returncode, 0, proc.stderr)
        return json.loads(proc.stdout)

    def test_explicit_session_file_takes_precedence(self) -> None:
        with tempfile.TemporaryDirectory() as temp:
            expected = Path(temp) / "custom" / "session.json"
            result = self.run_session_status({"OPENLIST_SESSION_FILE": str(expected)})
            self.assertEqual(result["session_file"], str(expected))

    def test_xdg_state_home_is_used_for_portable_default(self) -> None:
        with tempfile.TemporaryDirectory() as temp:
            result = self.run_session_status(
                {"XDG_STATE_HOME": temp},
                unset=("OPENLIST_SESSION_FILE",),
            )
            self.assertEqual(result["session_file"], str(Path(temp) / "openlist-skill" / "session.json"))

    def test_home_state_directory_is_used_without_xdg(self) -> None:
        with tempfile.TemporaryDirectory() as temp:
            result = self.run_session_status(
                {"HOME": temp},
                unset=("OPENLIST_SESSION_FILE", "XDG_STATE_HOME"),
            )
            expected = Path(temp) / ".local" / "state" / "openlist-skill" / "session.json"
            self.assertEqual(result["session_file"], str(expected))


if __name__ == "__main__":
    unittest.main()

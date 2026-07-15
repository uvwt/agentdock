#!/usr/bin/env python3
from __future__ import annotations

import importlib.util
from pathlib import Path
import tempfile
import unittest

MODULE_PATH = Path(__file__).with_name("recall_backup_export.py")
SPEC = importlib.util.spec_from_file_location("recall_backup_export", MODULE_PATH)
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class FakeClient:
    def __init__(self, empty: bool = False) -> None:
        self.empty = empty

    def call(self, name: str, args: dict[str, object], request_id: int) -> dict[str, object]:
        if name == "recall_maintain":
            return {"entries": [{"path": "recall/docs/current.md"}]}
        if name == "recall_read":
            content = "" if self.empty else "# Current\n"
            return {"recall": {"raw_content": content, "size_bytes": len(content)}}
        raise AssertionError(name)


class RecallBackupExportTest(unittest.TestCase):
    def make_repo(self, root: Path) -> Path:
        repo = root / "repo"
        (repo / ".git").mkdir(parents=True)
        (repo / "recall/docs").mkdir(parents=True)
        (repo / "recall/docs/obsolete.md").write_text("obsolete", encoding="utf-8")
        (repo / "assets").mkdir()
        (repo / "assets/index.bin").write_bytes(b"binary")
        (repo / "private-notes/encrypted/health").mkdir(parents=True)
        (repo / "private-notes/encrypted/health/note.age").write_bytes(b"ciphertext")
        (repo / "private-notes/notes/health").mkdir(parents=True)
        (repo / "private-notes/notes/health/note.md").write_text("plaintext", encoding="utf-8")
        (repo / "private-notes/README.md").write_text("policy", encoding="utf-8")
        return repo

    def test_export_replaces_recall_markdown_and_preserves_opaque_assets(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            repo = self.make_repo(Path(tmp))
            count = MODULE.export_recall_to_repo(repo, FakeClient())

            self.assertEqual(count, 1)
            self.assertEqual((repo / "recall/docs/current.md").read_text(), "# Current\n")
            self.assertFalse((repo / "recall/docs/obsolete.md").exists())
            self.assertEqual((repo / "assets/index.bin").read_bytes(), b"binary")
            self.assertEqual(
                (repo / "private-notes/encrypted/health/note.age").read_bytes(), b"ciphertext"
            )
            self.assertEqual((repo / "private-notes/README.md").read_text(), "policy")
            self.assertFalse((repo / "private-notes/notes/health/note.md").exists())

    def test_empty_recall_read_does_not_mutate_repository(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            repo = self.make_repo(Path(tmp))
            before = (repo / "recall/docs/obsolete.md").read_text()

            with self.assertRaisesRegex(RuntimeError, "empty markdown content"):
                MODULE.export_recall_to_repo(repo, FakeClient(empty=True))

            self.assertEqual((repo / "recall/docs/obsolete.md").read_text(), before)
            self.assertTrue((repo / "private-notes/encrypted/health/note.age").exists())


if __name__ == "__main__":
    unittest.main()

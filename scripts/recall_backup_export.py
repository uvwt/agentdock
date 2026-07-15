#!/usr/bin/env python3
"""Export Recall Markdown through MCP without deleting opaque backup assets."""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import shutil
import tempfile
from typing import Protocol
import urllib.request


class RecallClient(Protocol):
    def call(self, name: str, args: dict[str, object], request_id: int) -> dict[str, object]: ...


class MCPRecallClient:
    def __init__(self, endpoint: str, token: str = "") -> None:
        self.endpoint = endpoint
        self.token = token

    def call(self, name: str, args: dict[str, object], request_id: int) -> dict[str, object]:
        payload = {
            "jsonrpc": "2.0",
            "id": request_id,
            "method": "tools/call",
            "params": {"name": name, "arguments": args},
        }
        request = urllib.request.Request(
            self.endpoint,
            data=json.dumps(payload).encode(),
            headers={"Content-Type": "application/json"},
        )
        if self.token:
            request.add_header("Authorization", "Bearer " + self.token)
        with urllib.request.urlopen(request, timeout=60) as response:
            outer = json.loads(response.read())
        if "error" in outer:
            raise RuntimeError(outer["error"])
        text = outer["result"]["content"][0]["text"]
        return json.loads(text)


def should_preserve(relative_path: Path) -> bool:
    """Keep files the Markdown API cannot reproduce.

    Private-note plaintext stays excluded. Only encrypted ciphertext and the
    repository-level private-note policy files survive an API fallback export.
    """
    parts = relative_path.parts
    if len(parts) >= 2 and parts[0] == "private-notes" and parts[1] == "encrypted":
        return True
    if relative_path.as_posix() in {
        "private-notes/.gitignore",
        "private-notes/.gitkeep",
        "private-notes/README.md",
        "private-notes/RULES.md",
    }:
        return True
    return relative_path.suffix.lower() != ".md"


def collect_preserved_files(repo: Path) -> dict[str, bytes]:
    preserved: dict[str, bytes] = {}
    for file_path in repo.rglob("*"):
        if not file_path.is_file() or ".git" in file_path.parts:
            continue
        relative_path = file_path.relative_to(repo)
        if should_preserve(relative_path):
            preserved[relative_path.as_posix()] = file_path.read_bytes()
    return preserved


def recall_markdown_paths(client: RecallClient) -> list[str]:
    listed = client.call("recall_maintain", {"action": "list", "max_entries": 20000}, 1)
    entries = listed.get("entries") or []
    paths: list[str] = []
    for entry in entries:
        path = entry.get("path") if isinstance(entry, dict) else str(entry)
        if path and path.endswith(".md"):
            paths.append(path)
    paths = sorted(set(paths))
    if not paths:
        raise RuntimeError("NexusDock Recall export returned no markdown paths")
    return paths


def export_recall_to_repo(repo: Path, client: RecallClient) -> int:
    paths = recall_markdown_paths(client)
    preserved = collect_preserved_files(repo)

    with tempfile.TemporaryDirectory(prefix="recalldock-export-") as tmp:
        staging = Path(tmp) / "export"
        staging.mkdir()
        for relative, content in preserved.items():
            target = staging / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(content)

        empty_reads: list[tuple[str, object]] = []
        for index, path in enumerate(paths, 1):
            data = client.call("recall_read", {"path": path, "include_raw": True}, 1000 + index)
            recall = data.get("recall") or {}
            content = recall.get("raw_content") or recall.get("content") or recall.get("body") or ""
            size = recall.get("size_bytes")
            if not content:
                empty_reads.append((path, size))
                continue
            target = staging / path
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text(str(content), encoding="utf-8")

        if empty_reads:
            preview = "\n".join(f"{path} size_bytes={size}" for path, size in empty_reads[:80])
            raise RuntimeError(
                "NexusDock Recall export produced empty markdown content; "
                f"refusing to replace repo.\n{preview}"
            )

        for child in list(repo.iterdir()):
            if child.name == ".git":
                continue
            if child.is_dir():
                shutil.rmtree(child)
            else:
                child.unlink()

        for child in staging.iterdir():
            target = repo / child.name
            if child.is_dir():
                shutil.copytree(child, target)
            else:
                shutil.copy2(child, target)
    return len(paths)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", required=True, type=Path)
    parser.add_argument("--endpoint", required=True)
    args = parser.parse_args()

    count = export_recall_to_repo(
        args.repo,
        MCPRecallClient(args.endpoint, os.environ.get("AGENTDOCK_AUTH_TOKEN", "")),
    )
    print(f"exported NexusDock Recall markdown files: {count}")


if __name__ == "__main__":
    main()

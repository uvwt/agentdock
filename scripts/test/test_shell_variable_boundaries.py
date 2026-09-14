#!/usr/bin/env python3
from __future__ import annotations

from pathlib import Path
import re
import subprocess
import unittest

ROOT = Path(__file__).resolve().parent.parent.parent
UNBRACED_VARIABLE_BEFORE_NON_ASCII = re.compile(r"\$([A-Za-z_][A-Za-z0-9_]*)(?=[^\x00-\x7f])")


def tracked_shell_scripts() -> list[Path]:
    output = subprocess.check_output(
        ["git", "-C", str(ROOT), "ls-files", "*.sh"],
        text=True,
    )
    return [
        ROOT / relative
        for relative in output.splitlines()
        if relative and (ROOT / relative).is_file()
    ]


def unsafe_boundaries(path: Path) -> list[str]:
    findings: list[str] = []
    for line_number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        for match in UNBRACED_VARIABLE_BEFORE_NON_ASCII.finditer(line):
            column = match.start() + 1
            relative = path.relative_to(ROOT)
            findings.append(f"{relative}:{line_number}:{column}: {line.strip()}")
    return findings


class ShellVariableBoundaryTest(unittest.TestCase):
    def test_detector_catches_unbraced_variable_before_non_ascii(self) -> None:
        self.assertIsNotNone(
            UNBRACED_VARIABLE_BEFORE_NON_ASCII.search('echo "公网模式：$mode）"')
        )
        self.assertIsNone(
            UNBRACED_VARIABLE_BEFORE_NON_ASCII.search('echo "公网模式：${mode}）"')
        )

    def test_tracked_shell_scripts_use_explicit_variable_boundaries(self) -> None:
        findings = [
            finding
            for path in tracked_shell_scripts()
            for finding in unsafe_boundaries(path)
        ]
        self.assertEqual(
            findings,
            [],
            "shell 变量后紧跟非 ASCII 字符时必须使用 ${var} 明确变量边界：\n"
            + "\n".join(findings),
        )


if __name__ == "__main__":
    unittest.main()

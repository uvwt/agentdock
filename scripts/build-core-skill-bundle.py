#!/usr/bin/env python3

from __future__ import annotations

import argparse
import hashlib
import json
import re
import shutil
import stat
import zipfile
from pathlib import Path

CORE_SKILLS = (
    "skill-authoring",
    "skill-installation",
    "skill-vetter-runtime",
)
FIXED_ZIP_TIME = (1980, 1, 1, 0, 0, 0)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Build AgentDock's official core Skill Bundle.")
    parser.add_argument("--repo-root", type=Path, default=Path(__file__).resolve().parent.parent)
    parser.add_argument("--output", type=Path, required=True)
    return parser.parse_args()


def read_identity(skill_root: Path, expected_name: str) -> tuple[str, str]:
    document = (skill_root / "SKILL.md").read_text(encoding="utf-8")
    match = re.match(r"^---\s*\n(.*?)\n---\s*\n", document, re.DOTALL)
    if match is None:
        raise ValueError(f"{skill_root}/SKILL.md has invalid frontmatter")

    fields: dict[str, str] = {}
    for line in match.group(1).splitlines():
        key, separator, value = line.partition(":")
        if separator:
            fields[key.strip()] = value.strip()
    name = fields.get("name", "")
    version = fields.get("version", "")
    if name != expected_name:
        raise ValueError(f"expected Skill name {expected_name!r}, got {name!r}")
    if not version:
        raise ValueError(f"{expected_name} has no version")
    return name, version


def package_skill(skill_root: Path, archive_path: Path) -> str:
    files = sorted(path for path in skill_root.rglob("*") if path.is_file() or path.is_symlink())
    if not files:
        raise ValueError(f"{skill_root} has no files")

    with zipfile.ZipFile(archive_path, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
        for path in files:
            if path.is_symlink():
                raise ValueError(f"symlink is not allowed in core Skill: {path}")
            relative = path.relative_to(skill_root).as_posix()
            data = path.read_bytes()
            mode = stat.S_IMODE(path.stat().st_mode)
            info = zipfile.ZipInfo(relative, FIXED_ZIP_TIME)
            info.create_system = 3
            info.compress_type = zipfile.ZIP_DEFLATED
            info.external_attr = (stat.S_IFREG | mode) << 16
            archive.writestr(info, data, compress_type=zipfile.ZIP_DEFLATED, compresslevel=9)

    digest = hashlib.sha256(archive_path.read_bytes()).hexdigest()
    return f"sha256:{digest}"


def build_bundle(repo_root: Path, output: Path) -> None:
    repo_root = repo_root.resolve()
    output = output.resolve()
    if output.exists():
        shutil.rmtree(output)
    packages = output / "packages"
    packages.mkdir(parents=True, mode=0o755)

    entries: list[dict[str, str]] = []
    for expected_name in CORE_SKILLS:
        skill_root = repo_root / "skill-sources" / expected_name
        name, version = read_identity(skill_root, expected_name)
        relative_archive = Path("packages") / f"{name}.zip"
        digest = package_skill(skill_root, output / relative_archive)
        entries.append(
            {
                "name": name,
                "version": version,
                "path": relative_archive.as_posix(),
                "digest": digest,
            }
        )

    manifest = {"skills": entries}
    (output / "manifest.json").write_text(
        json.dumps(manifest, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )


def main() -> None:
    args = parse_args()
    build_bundle(args.repo_root, args.output)
    print(f"built core Skill Bundle: {args.output.resolve()}")


if __name__ == "__main__":
    main()

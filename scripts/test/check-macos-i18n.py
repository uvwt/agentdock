#!/usr/bin/env python3
from __future__ import annotations

from collections import Counter
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[2]
SOURCE_DIR = ROOT / "desktop/macos/AgentDockApp/Sources"
RESOURCE_DIR = ROOT / "desktop/macos/AgentDockApp/Resources"
LOCALES = ("en", "zh-Hans")
CJK = re.compile(r"[\u3400-\u4dbf\u4e00-\u9fff]")
L10N_CALL = re.compile(r'L10n\.(?:text|format)\(\s*"((?:\\.|[^"\\])*)"')
STRINGS_KEY = re.compile(r'^\s*"((?:\\.|[^"\\])*)"\s*=', re.MULTILINE)
STRINGS_ENTRY = re.compile(
    r'^\s*"((?:\\.|[^"\\])*)"\s*=\s*"((?:\\.|[^"\\])*)"\s*;',
    re.MULTILINE,
)
SWIFT_STRING = re.compile(r'"((?:\\.|[^"\\])*)"')
FORMAT_SPECIFIER = re.compile(r'%(?:\d+\$)?(?:@|d)')


def strip_line_comment(line: str) -> str:
    escaped = False
    in_string = False
    index = 0
    while index < len(line):
        char = line[index]
        if in_string:
            if escaped:
                escaped = False
            elif char == "\\":
                escaped = True
            elif char == '"':
                in_string = False
        else:
            if char == '"':
                in_string = True
            elif char == "/" and index + 1 < len(line) and line[index + 1] == "/":
                return line[:index]
        index += 1
    return line


def load_strings(path: Path) -> list[str]:
    text = path.read_text(encoding="utf-8")
    return STRINGS_KEY.findall(text)


def load_entries(path: Path) -> list[tuple[str, str]]:
    return STRINGS_ENTRY.findall(path.read_text(encoding="utf-8"))


def main() -> int:
    errors: list[str] = []
    source_keys: set[str] = set()

    for path in sorted(SOURCE_DIR.glob("*.swift")):
        text = path.read_text(encoding="utf-8")
        source_keys.update(L10N_CALL.findall(text))
        for line_number, line in enumerate(text.splitlines(), 1):
            executable = strip_line_comment(line)
            if "NSLog(" in executable:
                continue
            for literal in SWIFT_STRING.findall(executable):
                if CJK.search(literal):
                    errors.append(f"{path.relative_to(ROOT)}:{line_number}: hard-coded CJK Swift string: {literal}")

    locale_keys: dict[str, set[str]] = {}
    for locale in LOCALES:
        strings_path = RESOURCE_DIR / f"{locale}.lproj" / "Localizable.strings"
        if not strings_path.is_file():
            errors.append(f"missing localization file: {strings_path.relative_to(ROOT)}")
            continue
        keys = load_strings(strings_path)
        duplicates = sorted(key for key, count in Counter(keys).items() if count > 1)
        if duplicates:
            errors.extend(f"{strings_path.relative_to(ROOT)}: duplicate key: {key}" for key in duplicates)
        for key, value in load_entries(strings_path):
            key_formats = FORMAT_SPECIFIER.findall(key)
            value_formats = FORMAT_SPECIFIER.findall(value)
            if key_formats != value_formats:
                errors.append(
                    f"{strings_path.relative_to(ROOT)}: format specifiers differ for {key}: "
                    f"{key_formats} != {value_formats}"
                )
        locale_keys[locale] = set(keys)
        missing = sorted(source_keys - set(keys))
        errors.extend(f"{strings_path.relative_to(ROOT)}: missing key: {key}" for key in missing)

        info_path = RESOURCE_DIR / f"{locale}.lproj" / "InfoPlist.strings"
        if not info_path.is_file():
            errors.append(f"missing localization file: {info_path.relative_to(ROOT)}")
        elif "NSAppleEventsUsageDescription" not in set(load_strings(info_path)):
            errors.append(f"{info_path.relative_to(ROOT)}: missing NSAppleEventsUsageDescription")

    if len(locale_keys) == len(LOCALES):
        baseline = locale_keys[LOCALES[0]]
        for locale in LOCALES[1:]:
            extra = sorted(locale_keys[locale] - baseline)
            missing = sorted(baseline - locale_keys[locale])
            errors.extend(f"{locale}: extra localization key: {key}" for key in extra)
            errors.extend(f"{locale}: missing localization key present in en: {key}" for key in missing)

    if errors:
        print(f"macOS i18n check failed with {len(errors)} issue(s):", file=sys.stderr)
        for error in errors:
            print(f"- {error}", file=sys.stderr)
        return 1

    count = len(locale_keys.get("en", set()))
    print(f"macOS i18n check passed: {len(source_keys)} referenced keys, {count} localized entries per locale.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

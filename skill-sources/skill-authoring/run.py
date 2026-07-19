#!/usr/bin/env python3
from __future__ import annotations

import json
import re
import sys
from dataclasses import dataclass
from pathlib import Path

VERSION = "1.1.3"
MAX_FILES = 500
MAX_TEXT_BYTES = 1 << 20
HOST_METADATA_FILES = {".agentdock-install.json"}
TEXT_SUFFIXES = {
    ".bash", ".css", ".go", ".html", ".ini", ".js", ".json", ".jsx", ".md",
    ".py", ".rs", ".sh", ".toml", ".ts", ".tsx", ".txt", ".yaml", ".yml", ".zsh",
}


@dataclass(frozen=True)
class Rule:
    code: str
    severity: str
    pattern: re.Pattern[str]
    message: str
    suggestion: str
    skill_md_only: bool = False


RULES = (
    Rule(
        "HARDCODED_AGENTDOCK_INSTALL_PATH",
        "error",
        re.compile(
            r"(?:"
            r"(?:~|\$HOME|\$\{HOME\}|/[A-Za-z0-9._-]+)?/?\.agentdock"
            r"|\$AGENTDOCK_HOME|\$\{AGENTDOCK_HOME\}"
            r")/skill-store/installed/[a-z][a-z0-9-]*/v?\d+\.\d+\.\d+",
            re.I,
        ),
        "不应硬编码 AgentDock 已安装版本目录。",
        "改为从 Skill 包根目录执行相对命令，例如 `python3 run.py`。",
    ),
    Rule(
        "AGENTDOCK_SKILL_DIR_DEPENDENCY",
        "error",
        re.compile(r"\bAGENTDOCK_SKILL_DIR\b"),
        "Skill 核心运行流程不应依赖 AgentDock 专属目录变量。",
        "让运行宿主切换到 Skill 包根目录，包内只使用相对路径。",
    ),
    Rule(
        "AGENTDOCK_PRIVATE_DIR_DEPENDENCY",
        "error",
        re.compile(r"\bAGENTDOCK_(?:DIR|HOME)\b"),
        "Skill 核心运行流程不应依赖 AgentDock 私有目录变量。",
        "改用业务专属环境变量、XDG 目录或包内相对路径。",
    ),
    Rule(
        "AGENTDOCK_ENV_FILE_ACCESS",
        "error",
        re.compile(r"(?:source|open|read_text|read_bytes|Path\s*\().{0,120}\.agentdock/env/skill/|\.agentdock/env/skill/.{0,120}(?:source|open|read_text|read_bytes)", re.I),
        "Skill 脚本不得主动读取或 source AgentDock 私有环境文件。",
        "只从当前进程环境读取变量，由运行宿主负责注入。",
    ),
    Rule(
        "FIXED_USER_ABSOLUTE_PATH",
        "error",
        re.compile(r"(?:/Users/[A-Za-z0-9._-]+/|/home/[A-Za-z0-9._-]+/|[A-Za-z]:\\Users\\[A-Za-z0-9._-]+\\)"),
        "Skill 包中不应出现固定用户绝对路径。",
        "改用包内相对路径、用户输入或明确的业务环境变量。",
    ),
    Rule(
        "AGENTDOCK_SKILL_ENV_USAGE",
        "warning",
        re.compile(r"\bskill_env\b"),
        "SKILL.md 出现 AgentDock 专属 `skill_env`。",
        "把通用运行契约写成宿主注入环境；AgentDock 适配仅作为可选说明。",
        skill_md_only=True,
    ),
    Rule(
        "AGENTDOCK_EXEC_COMMAND_USAGE",
        "warning",
        re.compile(r"\bexec_command\b"),
        "SKILL.md 出现 AgentDock 专属 `exec_command`。",
        "核心流程使用相对命令；宿主工具调用只放在独立适配说明中。",
        skill_md_only=True,
    ),
    Rule(
        "AGENTDOCK_SKILL_URI_USAGE",
        "warning",
        re.compile(r"\bskill://"),
        "SKILL.md 使用 AgentDock 专属 `skill://` 资源地址。",
        "核心文档引用包内相对路径，例如 `references/api.md`。",
        skill_md_only=True,
    ),
)


def emit(value: object) -> None:
    print(json.dumps(value, ensure_ascii=False, separators=(",", ":")))


def fail(code: str, message: str, details: object | None = None) -> None:
    error: dict[str, object] = {"code": code, "message": message}
    if details is not None:
        error["details"] = details
    emit({"ok": False, "error": error})
    raise SystemExit(1)


def load_input() -> dict[str, object]:
    raw = sys.stdin.read().strip()
    if not raw:
        return {}
    try:
        value = json.loads(raw)
    except json.JSONDecodeError as exc:
        fail("INVALID_INPUT", "Input is not valid JSON", {"reason": str(exc)})
    if not isinstance(value, dict):
        fail("INVALID_INPUT", "Input must be a JSON object")
    return value


def text_files(source: Path) -> list[Path]:
    files: list[Path] = []
    for path in sorted(source.rglob("*")):
        if path.is_symlink() or not path.is_file():
            continue
        if path.parent == source and path.name in HOST_METADATA_FILES:
            continue
        if path.name == "SKILL.md" or path.suffix.lower() in TEXT_SUFFIXES:
            files.append(path)
        if len(files) > MAX_FILES:
            fail("TOO_MANY_FILES", f"Skill contains more than {MAX_FILES} text files")
    return files


def line_number(content: str, offset: int) -> int:
    return content.count("\n", 0, offset) + 1


ADVISORY_MARKERS = (
    "不得", "禁止", "不应", "不要", "明确废弃", "硬错误", "检查以下",
    "must not", "do not", "forbidden", "prohibited", "reject",
)
LIST_ITEM_PATTERN = re.compile(r"^(?:[-*+]|\d+\.)\s+")


def has_advisory_marker(value: str) -> bool:
    lowered = value.lower()
    return any(marker in lowered for marker in ADVISORY_MARKERS)


def markdown_match_is_advisory(content: str, start: int) -> bool:
    lines = content.splitlines()
    line_index = content.count("\n", 0, start)
    current = lines[line_index].strip() if line_index < len(lines) else ""
    if has_advisory_marker(current):
        return True
    if not LIST_ITEM_PATTERN.match(current):
        return False

    # 列表中的禁止项只继承同一列表前的说明句，不读取任意邻近段落。
    for previous in reversed(lines[:line_index]):
        stripped = previous.strip()
        if not stripped:
            continue
        if LIST_ITEM_PATTERN.match(stripped):
            continue
        return has_advisory_marker(stripped)
    return False


def is_advisory_match(relative: str, content: str, start: int, end: int) -> bool:
    if relative.startswith("tests/"):
        return True

    # lint 实现自身会包含规则正则；这里只跳过完整 Rule(...) 定义，不跳过普通脚本中的真实使用。
    rule_start = content.rfind("Rule(", 0, start)
    if rule_start >= 0:
        rule_end = content.find("\n    ),", rule_start)
        if rule_end >= start:
            return True

    line_start = content.rfind("\n", 0, start) + 1
    line_end = content.find("\n", end)
    if line_end < 0:
        line_end = len(content)
    line = content[line_start:line_end]
    if "re.compile(" in line and "Rule(" in content[max(0, line_start - 240):line_start]:
        return True

    return relative.endswith(".md") and markdown_match_is_advisory(content, start)


def scan_file(source: Path, path: Path) -> list[dict[str, object]]:
    try:
        data = path.read_bytes()
    except OSError as exc:
        fail("READ_FAILED", "Unable to read Skill file", {"file": str(path), "reason": str(exc)})
    if len(data) > MAX_TEXT_BYTES:
        return [{
            "code": "TEXT_FILE_TOO_LARGE",
            "severity": "warning",
            "file": path.relative_to(source).as_posix(),
            "line": 1,
            "message": f"文本文件超过 {MAX_TEXT_BYTES} 字节，未执行内容检查。",
            "suggestion": "拆分或删除不必要的大文件后重新运行 lint。",
        }]
    try:
        content = data.decode("utf-8")
    except UnicodeDecodeError:
        return [{
            "code": "NON_UTF8_TEXT_FILE",
            "severity": "warning",
            "file": path.relative_to(source).as_posix(),
            "line": 1,
            "message": "候选文本文件不是 UTF-8，未执行内容检查。",
            "suggestion": "将 Skill 文档和脚本保存为 UTF-8。",
        }]

    relative = path.relative_to(source).as_posix()
    issues: list[dict[str, object]] = []
    for rule in RULES:
        if rule.skill_md_only and relative != "SKILL.md":
            continue
        for match in rule.pattern.finditer(content):
            if is_advisory_match(relative, content, match.start(), match.end()):
                continue
            issues.append({
                "code": rule.code,
                "severity": rule.severity,
                "file": relative,
                "line": line_number(content, match.start()),
                "message": rule.message,
                "suggestion": rule.suggestion,
            })
            break
    return issues


def check_portable_execution(source: Path, skill_md: str) -> list[dict[str, object]]:
    entry = source / "run.py"
    if not entry.is_file():
        return []
    if re.search(r"(?:python3?|\./)\s+(?:\./)?run\.py\b", skill_md):
        return []
    return [{
        "code": "MISSING_PORTABLE_EXECUTION",
        "severity": "warning",
        "file": "SKILL.md",
        "line": 1,
        "message": "包内存在 run.py，但 SKILL.md 没有说明从 Skill 根目录使用相对路径执行。",
        "suggestion": "增加通用执行示例，例如 `python3 run.py`，并说明环境由运行宿主注入。",
    }]


def lint(source_value: object) -> dict[str, object]:
    if not isinstance(source_value, str) or not source_value.strip():
        fail("INVALID_INPUT", "source is required for lint")
    source = Path(source_value).expanduser().resolve()
    if not source.is_dir():
        fail("SOURCE_NOT_FOUND", "source must be an existing Skill directory", {"source": str(source)})
    skill_md_path = source / "SKILL.md"
    if not skill_md_path.is_file():
        fail("SKILL_DOCUMENT_MISSING", "source does not contain SKILL.md", {"source": str(source)})

    files = text_files(source)
    issues: list[dict[str, object]] = []
    for path in files:
        issues.extend(scan_file(source, path))
    try:
        skill_md = skill_md_path.read_text(encoding="utf-8")
    except (OSError, UnicodeDecodeError) as exc:
        fail("READ_FAILED", "Unable to read SKILL.md", {"reason": str(exc)})
    issues.extend(check_portable_execution(source, skill_md))
    issues.sort(key=lambda item: (item["severity"] != "error", str(item["file"]), int(item["line"]), str(item["code"])))

    error_count = sum(item["severity"] == "error" for item in issues)
    warning_count = sum(item["severity"] == "warning" for item in issues)
    return {
        "ok": True,
        "action": "lint",
        "source": str(source),
        "portable": error_count == 0,
        "error_count": error_count,
        "warning_count": warning_count,
        "issues": issues,
        "checked_files": len(files),
        "skill_version": VERSION,
    }


def main() -> None:
    request = load_input()
    action = str(request.get("skill_action", "status")).strip().lower()
    if action == "status":
        emit({
            "ok": True,
            "action": "status",
            "skill_version": VERSION,
            "lint_rule_count": len(RULES) + 1,
            "environment_required": False,
        })
        return
    if action == "lint":
        emit(lint(request.get("source")))
        return
    fail("UNKNOWN_ACTION", "Unsupported skill_action", {"action": action, "allowed": ["status", "lint"]})


if __name__ == "__main__":
    main()

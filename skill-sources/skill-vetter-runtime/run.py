#!/usr/bin/env python3
import json
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path

VERSION = "0.1.3"
BASE = Path(__file__).resolve().parent
AGENTDOCK_HOME = Path(os.environ.get("AGENTDOCK_HOME", Path.home() / ".agentdock"))
WORKSPACE = Path(os.environ.get("AGENTDOCK_DEFAULT_DIR", Path.home() / "AgentDock"))
INSTALLED_ROOT = AGENTDOCK_HOME / "skill-store" / "installed"

TEXT_EXTENSIONS = {
    ".md", ".txt", ".json", ".yaml", ".yml", ".py", ".js", ".ts", ".tsx", ".jsx",
    ".sh", ".bash", ".zsh", ".toml", ".ini", ".env", ".html", ".css", ".go", ".rs",
}

PATTERNS = [
    ("credential_file_access", "HIGH", re.compile(r"(~/(?:\.ssh|\.aws)|id_rsa|known_hosts|cookie(?:s)?\.sqlite|(?:read|open|cat|copy|access|touch|write).{0,50}(credential|token|secret|password))", re.I)),
    ("browser_cookie_or_session_access", "HIGH", re.compile(r"(Chrome|Firefox|Safari).{0,80}(Cookie|Session|Login Data)|Cookies/Cookies|Default/Cookies", re.I)),
    ("secret_or_token_reference", "MEDIUM", re.compile(r"\b(API[_-]?KEY|ACCESS[_-]?TOKEN|PRIVATE[_-]?KEY|BW_SESSION|GITHUB_TOKEN|SECRET_KEY|PASSWORD\s*[=:])", re.I)),
    ("base64_or_obfuscation", "MEDIUM", re.compile(r"(base64\s+(-d|--decode)|base64\.b64decode|atob\(|fromCharCode|eval\s*\()", re.I)),
    ("dynamic_code_execution", "HIGH", re.compile(r"\b(eval|exec)\s*\(|new Function\s*\(|vm\.runIn", re.I)),
    ("shell_execution", "MEDIUM", re.compile(r"(subprocess\.(run|Popen|call)|os\.system|child_process\.(exec|spawn)|shell=True)", re.I)),
    ("package_install", "MEDIUM", re.compile(r"\b(pip3?|npm|pnpm|yarn|brew|curl)\s+(install|add)|npm\s+i\b", re.I)),
    ("sudo_or_privileged_command", "EXTREME", re.compile(r"\bsudo\b|chmod\s+777|chown\s+root|/etc/sudoers", re.I)),
    ("persistence_hook", "HIGH", re.compile(r"(launchctl|LaunchAgents|crontab|systemctl|plist|KeepAlive|RunAtLoad)", re.I)),
    ("destructive_file_command", "HIGH", re.compile(r"rm\s+-rf\s+(/|~|\$HOME)|shutil\.rmtree\s*\(", re.I)),
    ("network_fetch_or_post", "MEDIUM", re.compile(r"(curl|wget|requests\.(get|post|put)|httpx\.|fetch\(|axios\.|urllib\.request|https?://)", re.I)),
    ("raw_ip_network_target", "HIGH", re.compile(r"https?://\d{1,3}(?:\.\d{1,3}){3}(?::\d+)?", re.I)),
    ("system_path_write", "HIGH", re.compile(r"(rm|mv|cp|install|tee|chmod|chown|write_text|open\s*\().{0,80}(/etc/|/Library/Launch|/System/|/usr/local/bin|/opt/homebrew/bin)", re.I)),
]

RISK_ORDER = {"LOW": 0, "MEDIUM": 1, "HIGH": 2, "EXTREME": 3}


def emit(value):
    print(json.dumps(value, ensure_ascii=False, separators=(",", ":")))


def fail(code, message, details=None, exit_code=1):
    payload = {"ok": False, "error": {"code": code, "message": message}}
    if details is not None:
        payload["error"]["details"] = details
    emit(payload)
    raise SystemExit(exit_code)


def load_input():
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


def find_clawhub():
    found = shutil.which("clawhub")
    if found:
        return found
    for candidate in ("/opt/homebrew/bin/clawhub", "/usr/local/bin/clawhub"):
        if Path(candidate).is_file() and os.access(candidate, os.X_OK):
            return candidate
    return None


def run_clawhub(args, timeout=45):
    cli = find_clawhub()
    if not cli:
        fail("CLI_NOT_FOUND", "clawhub CLI was not found")
    env = os.environ.copy()
    env["NO_COLOR"] = "1"
    try:
        completed = subprocess.run([cli, *args], capture_output=True, text=True, timeout=timeout, check=False, env=env)
    except subprocess.TimeoutExpired:
        fail("CLI_TIMEOUT", "clawhub command timed out", {"args": args})
    if completed.returncode != 0:
        fail("CLI_FAILED", "clawhub command failed", {
            "args": args,
            "exit_code": completed.returncode,
            "stderr": completed.stderr.strip()[:4000],
            "stdout": completed.stdout.strip()[:4000],
        })
    return completed.stdout.strip()


def parse_json(text, label):
    try:
        return json.loads(text)
    except json.JSONDecodeError as exc:
        fail("INVALID_RESPONSE", f"{label} returned invalid JSON", {"reason": str(exc), "stdout": text[:4000]})


def text_file(path):
    return path.suffix.lower() in TEXT_EXTENSIONS or path.name in {"SKILL.md", "Dockerfile"}


def scan_text(path, content):
    findings = []
    for name, severity, pattern in PATTERNS:
        for match in pattern.finditer(content):
            line = content.count("\n", 0, match.start()) + 1
            snippet = content[max(0, match.start() - 80):match.end() + 80].replace("\n", " ").strip()
            context = content[max(0, match.start() - 900):match.end() + 300].replace("\n", " ").strip()
            lower_snippet = snippet.lower()
            lower_context = context.lower()
            advisory_markers = (
                "reject immediately if you see",
                "check for these",
                "red flags",
                "red_flags",
                "quick vet commands",
                "for github-hosted skills",
                "questions to answer",
                "evaluate:",
                "does not access",
                "doesn't access",
                "do not access",
                "do not install",
                "without clear reason",
                "examples:",
            )
            if any(marker in lower_context or marker in lower_snippet for marker in advisory_markers):
                continue
            findings.append({
                "id": name,
                "severity": severity,
                "file": path,
                "line": line,
                "match": match.group(0)[:120],
                "snippet": snippet[:260],
            })
            break
    return findings


def detect_permissions(files):
    commands = set()
    network = set()
    file_paths = set()
    for item in files:
        content = item.get("content") or ""
        for cmd in re.findall(r"\b(python3?|node|npm|pnpm|yarn|pip3?|curl|wget|gh|git|docker|brew|osascript|security|pbcopy|launchctl|systemctl)\b", content, re.I):
            commands.add(cmd.lower())
        for url in re.findall(r"https?://[^\s'\"<>]+", content):
            network.add(url[:160])
        for pth in re.findall(r"(?:~|/Users/[^\s'\"]+|/etc/[^\s'\"]+|/Library/[^\s'\"]+|/System/[^\s'\"]+)", content):
            file_paths.add(pth[:160])
    return {
        "commands": sorted(commands),
        "network": sorted(network),
        "files": sorted(file_paths),
    }


def risk_from_findings(findings, metadata=None):
    risk = "LOW"
    for finding in findings:
        sev = finding.get("severity", "LOW")
        if RISK_ORDER[sev] > RISK_ORDER[risk]:
            risk = sev
    if metadata:
        security = metadata.get("security") or {}
        status = str(security.get("status", "")).lower()
        if status and status not in {"clean", "ok", "safe"}:
            risk = "HIGH" if RISK_ORDER[risk] < RISK_ORDER["HIGH"] else risk
    return risk


def verdict_for(risk):
    if risk == "LOW":
        return "SAFE_TO_REVIEWED_INSTALL"
    if risk == "MEDIUM":
        return "INSTALL_WITH_CAUTION"
    if risk == "HIGH":
        return "HUMAN_APPROVAL_REQUIRED"
    return "DO_NOT_INSTALL"


def build_report(source, target, files, metadata=None, notes=None):
    findings = []
    for item in files:
        content = item.get("content")
        if isinstance(content, str):
            findings.extend(scan_text(item["path"], content))
    permissions = detect_permissions(files)
    risk = risk_from_findings(findings, metadata)
    report = {
        "source": source,
        "target": target,
        "risk_level": risk,
        "verdict": verdict_for(risk),
        "files_reviewed": len(files),
        "reviewed_files": [{"path": f["path"], "size": f.get("size"), "truncated": f.get("truncated", False)} for f in files],
        "red_flags": findings,
        "permissions_detected": permissions,
        "metadata": metadata or {},
        "notes": notes or [],
    }
    return report


def handle_status():
    clawhub = find_clawhub()
    emit({
        "ok": True,
        "skill_version": VERSION,
        "python": sys.executable,
        "clawhub": clawhub,
        "clawhub_available": bool(clawhub),
        "workspace": str(WORKSPACE),
        "installed_root": str(INSTALLED_ROOT),
    })


def as_int(args, name, default, lower, upper):
    value = args.get(name, default)
    if isinstance(value, bool) or not isinstance(value, int) or not lower <= value <= upper:
        fail("INVALID_INPUT", f"{name} must be an integer between {lower} and {upper}")
    return value


def handle_vet_clawhub(args):
    slug = args.get("slug")
    if not isinstance(slug, str) or not slug.strip():
        fail("INVALID_INPUT", "slug must be a non-empty string")
    slug = slug.strip()
    max_files = as_int(args, "max_files", 30, 1, 80)
    max_file_chars = as_int(args, "max_file_chars", 50000, 1000, 200000)

    data = parse_json(run_clawhub(["inspect", slug, "--files", "--json"], timeout=60), "clawhub inspect")
    version = data.get("version") or {}
    listed = version.get("files") or []
    files = []
    notes = []
    for info in listed[:max_files]:
        path = info.get("path")
        size = info.get("size")
        content_type = info.get("contentType", "")
        if not path:
            continue
        suffix = Path(path).suffix.lower()
        if suffix not in TEXT_EXTENSIONS and path != "SKILL.md" and not content_type.startswith("text/"):
            files.append({"path": path, "size": size, "content": "", "truncated": False})
            continue
        file_data = parse_json(run_clawhub(["inspect", slug, "--file", path, "--json"], timeout=60), f"clawhub inspect {path}")
        content = (((file_data.get("file") or {}).get("content")) or "")
        truncated = len(content) > max_file_chars
        files.append({"path": path, "size": size, "content": content[:max_file_chars], "truncated": truncated})
    if len(listed) > max_files:
        notes.append(f"Reviewed first {max_files} files out of {len(listed)} listed files.")
    metadata = {
        "display_name": (data.get("skill") or {}).get("displayName"),
        "summary": (data.get("skill") or {}).get("summary"),
        "owner": ((data.get("owner") or {}).get("handle")),
        "version": version.get("version"),
        "stats": (data.get("skill") or {}).get("stats"),
        "security": version.get("security"),
    }
    emit({"ok": True, "report": build_report("clawhub", slug, files, metadata, notes)})


def allowed_local_path(path, allow_outside):
    resolved = path.expanduser().resolve()
    if allow_outside:
        return resolved
    allowed = [WORKSPACE.resolve(), INSTALLED_ROOT.resolve()]
    if any(resolved == root or root in resolved.parents for root in allowed):
        return resolved
    fail("PATH_NOT_ALLOWED", "Path is outside AgentDock skill roots", {"path": str(resolved), "allowed_roots": [str(p) for p in allowed]})


def handle_vet_local(args):
    raw_path = args.get("path")
    if not isinstance(raw_path, str) or not raw_path.strip():
        fail("INVALID_INPUT", "path must be a non-empty string")
    max_files = as_int(args, "max_files", 80, 1, 200)
    max_file_chars = as_int(args, "max_file_chars", 50000, 1000, 200000)
    allow_outside = bool(args.get("allow_outside_agentdock", False))
    root = allowed_local_path(Path(raw_path), allow_outside)
    if not root.exists():
        fail("PATH_NOT_FOUND", "Path does not exist", {"path": str(root)})
    paths = [root] if root.is_file() else sorted(p for p in root.rglob("*") if p.is_file())
    files = []
    notes = []
    for path in paths[:max_files]:
        rel = str(path.relative_to(root)) if root.is_dir() else path.name
        if not text_file(path):
            files.append({"path": rel, "size": path.stat().st_size, "content": "", "truncated": False})
            continue
        try:
            raw = path.read_text(encoding="utf-8", errors="replace")
        except OSError as exc:
            files.append({"path": rel, "size": path.stat().st_size, "content": "", "truncated": False, "read_error": str(exc)})
            continue
        truncated = len(raw) > max_file_chars
        files.append({"path": rel, "size": path.stat().st_size, "content": raw[:max_file_chars], "truncated": truncated})
    if len(paths) > max_files:
        notes.append(f"Reviewed first {max_files} files out of {len(paths)} files under the path.")
    metadata = {}
    emit({"ok": True, "report": build_report("local_path", str(root), files, metadata, notes)})


def main():
    args = load_input()
    action = str(args.pop("skill_action", "status"))
    if action == "status":
        handle_status()
    elif action == "vet-clawhub-slug":
        handle_vet_clawhub(args)
    elif action == "vet-local-path":
        handle_vet_local(args)
    else:
        fail("UNKNOWN_OPERATION", "Unknown operation", {"operation": operation})


if __name__ == "__main__":
    main()

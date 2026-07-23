import datetime
import errno
import fnmatch
import hashlib
import json
import os
import re
import stat
import sys
import tempfile

MAX_TEXT_FILE_BYTES = 32 << 20
DEFAULT_SKIPPED_DIRS = {
    ".git",
    ".reference",
    "node_modules",
    "target",
    "dist",
    "build",
    ".venv",
    "venv",
    ".tox",
    ".mypy_cache",
    ".pytest_cache",
    ".ruff_cache",
    "__pycache__",
}
WRITE_BLOCKED_ROOTS = ("/proc", "/sys", "/dev", "/run")


class ToolFailure(Exception):
    def __init__(self, code, message, details=None):
        super().__init__(message)
        self.code = code
        self.message = message
        self.details = details or {}


def fail(code, message, **details):
    raise ToolFailure(code, message, details)


def checked_path(value, write=False):
    if not isinstance(value, str) or not value.startswith("/"):
        fail("INVALID_ARGUMENT", "WSL file paths must be absolute Linux paths", path=value)
    if "\x00" in value:
        fail("INVALID_ARGUMENT", "path contains an invalid byte", path=value)
    path = os.path.normpath(value)
    if write:
        for blocked in WRITE_BLOCKED_ROOTS:
            if path == blocked or path.startswith(blocked + "/"):
                fail("PROTECTED_WSL_PATH", "file_edit does not allow writes under protected WSL system paths", path=path)
        current = "/"
        for component in path.strip("/").split("/")[:-1]:
            current = os.path.join(current, component)
            try:
                if stat.S_ISLNK(os.lstat(current).st_mode):
                    fail("SYMLINK_NOT_ALLOWED", "file_edit does not allow symlink path components", path=current)
            except FileNotFoundError:
                break
    return path


def kind_from_mode(mode):
    if stat.S_ISREG(mode):
        return "file"
    if stat.S_ISDIR(mode):
        return "directory"
    if stat.S_ISLNK(mode):
        return "symlink"
    if stat.S_ISSOCK(mode):
        return "socket"
    if stat.S_ISFIFO(mode):
        return "fifo"
    if stat.S_ISCHR(mode) or stat.S_ISBLK(mode):
        return "device"
    return "other"


def timestamp(value):
    return datetime.datetime.fromtimestamp(value, datetime.timezone.utc).isoformat().replace("+00:00", "Z")


def read_text(path, reject_symlink=False, allow_missing=False):
    path = checked_path(path)
    try:
        link_info = os.lstat(path)
    except FileNotFoundError:
        if allow_missing:
            return None
        fail("PATH_NOT_FOUND", "WSL path does not exist", path=path)
    if stat.S_ISLNK(link_info.st_mode):
        if reject_symlink:
            fail("SYMLINK_NOT_ALLOWED", "file_edit does not allow symlink targets", path=path)
        try:
            info = os.stat(path)
        except FileNotFoundError:
            fail("PATH_NOT_FOUND", "WSL symlink target does not exist", path=path)
    else:
        info = link_info
    if stat.S_ISDIR(info.st_mode):
        fail("IS_DIRECTORY", "cannot read directory", path=path)
    if not stat.S_ISREG(info.st_mode):
        fail("NOT_REGULAR_FILE", "text tools only support regular files", path=path, type=kind_from_mode(info.st_mode))
    if info.st_size > MAX_TEXT_FILE_BYTES:
        fail(
            "FILE_TOO_LARGE",
            "text file exceeds the input limit",
            path=path,
            size_bytes=info.st_size,
            max_size_bytes=MAX_TEXT_FILE_BYTES,
        )
    with open(path, "rb") as handle:
        data = handle.read(MAX_TEXT_FILE_BYTES + 1)
    if len(data) > MAX_TEXT_FILE_BYTES:
        fail("FILE_TOO_LARGE", "text file exceeds the input limit", path=path, max_size_bytes=MAX_TEXT_FILE_BYTES)
    if b"\x00" in data[:8192]:
        fail("BINARY_FILE", "binary file read blocked for text tool", path=path)
    try:
        content = data.decode("utf-8")
    except UnicodeDecodeError:
        fail("ENCODING_UNSUPPORTED", "file is not valid utf-8", path=path)
    return {
        "path": path,
        "content": content,
        "size_bytes": len(data),
        "mode": stat.S_IMODE(info.st_mode),
        "modified": timestamp(info.st_mtime),
        "uid": info.st_uid,
        "gid": info.st_gid,
        "symlink": stat.S_ISLNK(link_info.st_mode),
    }


def parse_ignore_file(root):
    rules = []
    try:
        with open(os.path.join(root, ".gitignore"), "r", encoding="utf-8") as handle:
            lines = handle.readlines()
    except (FileNotFoundError, UnicodeDecodeError, OSError):
        return rules
    for raw in lines:
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        negate = line.startswith("!")
        if negate:
            line = line[1:]
        rooted = line.startswith("/")
        if rooted:
            line = line[1:]
        directory_only = line.endswith("/")
        if directory_only:
            line = line[:-1]
        if line:
            rules.append((line, negate, directory_only, rooted))
    return rules


def glob_matches(pattern, rel):
    rel = rel.replace(os.sep, "/")
    pattern = pattern.replace("\\", "/")
    if pattern in ("*", "**", "**/*"):
        return True
    if fnmatch.fnmatchcase(rel, pattern) or fnmatch.fnmatchcase(os.path.basename(rel), pattern):
        return True
    if pattern.startswith("**/") and fnmatch.fnmatchcase(rel, pattern[3:]):
        return True
    return False


def ignored_by_rules(rel, is_directory, rules):
    rel = rel.replace(os.sep, "/")
    ignored = False
    for pattern, negate, directory_only, rooted in rules:
        if directory_only and not is_directory:
            continue
        matched = glob_matches(pattern, rel) if rooted else any(
            glob_matches(pattern, suffix)
            for suffix in [rel, *["/".join(rel.split("/")[index:]) for index in range(len(rel.split("/")))]]
        )
        if matched:
            ignored = not negate
    return ignored


def hidden_path(rel):
    return any(part.startswith(".") and part not in (".", "..") for part in rel.replace("\\", "/").split("/"))


def entry_record(root, full_path, name=None):
    info = os.lstat(full_path)
    rel = os.path.relpath(full_path, root).replace(os.sep, "/")
    return {
        "name": name if name is not None else os.path.basename(full_path),
        "path": full_path.replace(os.sep, "/"),
        "relative_path": rel,
        "type": kind_from_mode(info.st_mode),
        "size_bytes": info.st_size,
        "modified": timestamp(info.st_mtime),
        "mode": stat.S_IMODE(info.st_mode),
        "is_hidden": hidden_path(rel),
    }


def iter_tree(root, include_hidden, include_ignored, max_depth=None):
    rules = [] if include_ignored else parse_ignore_file(root)
    stack = [(root, 0)]
    while stack:
        current, depth = stack.pop()
        try:
            entries = sorted(os.scandir(current), key=lambda item: item.name, reverse=True)
        except PermissionError:
            continue
        for entry in entries:
            full_path = entry.path
            rel = os.path.relpath(full_path, root).replace(os.sep, "/")
            is_directory = entry.is_dir(follow_symlinks=False)
            if not include_hidden and hidden_path(rel):
                continue
            if not include_ignored and (entry.name in DEFAULT_SKIPPED_DIRS or ignored_by_rules(rel, is_directory, rules)):
                continue
            yield full_path, entry, depth + 1
            if is_directory and (max_depth is None or depth + 1 < max_depth):
                stack.append((full_path, depth + 1))


def ensure_directory(path):
    if not os.path.exists(path):
        fail("PATH_NOT_FOUND", "WSL path does not exist", path=path)
    if not os.path.isdir(path):
        fail("NOT_A_DIRECTORY", "WSL path is not a directory", path=path)


def list_directory(request):
    root = checked_path(request.get("path"))
    ensure_directory(root)
    include_hidden = bool(request.get("include_hidden"))
    include_ignored = bool(request.get("include_ignored"))
    recursive = bool(request.get("recursive"))
    max_depth = max(1, min(int(request.get("max_depth") or 1), 20))
    max_entries = max(1, min(int(request.get("max_entries") or 200), 2000))
    items = []
    if recursive:
        iterator = iter_tree(root, include_hidden, include_ignored, max_depth=max_depth)
    else:
        iterator = iter_tree(root, include_hidden, include_ignored, max_depth=1)
    truncated = False
    for full_path, entry, _ in iterator:
        if len(items) >= max_entries:
            truncated = True
            break
        items.append(entry_record(root, full_path, entry.name))
    items.sort(key=lambda item: item["path"])
    return {"path": root, "entries": items, "recursive": recursive, "max_depth": max_depth, "truncated": truncated}


def list_files(request):
    root = checked_path(request.get("path"))
    ensure_directory(root)
    include_hidden = bool(request.get("include_hidden"))
    include_ignored = bool(request.get("include_ignored"))
    patterns = request.get("patterns") or ["**/*"]
    exclude_patterns = request.get("exclude_patterns") or []
    max_results = max(1, min(int(request.get("max_results") or 500), 5000))
    items = []
    truncated = False
    for full_path, entry, _ in iter_tree(root, include_hidden, include_ignored):
        if not entry.is_file(follow_symlinks=False):
            continue
        rel = os.path.relpath(full_path, root).replace(os.sep, "/")
        if not any(glob_matches(pattern, rel) for pattern in patterns):
            continue
        if any(glob_matches(pattern, rel) for pattern in exclude_patterns):
            continue
        if len(items) >= max_results:
            truncated = True
            break
        items.append(entry_record(root, full_path, entry.name))
    items.sort(key=lambda item: item["path"])
    return {"path": root, "files": items, "truncated": truncated}


def search_text(request):
    root = checked_path(request.get("path"))
    if not os.path.exists(root):
        fail("PATH_NOT_FOUND", "WSL path does not exist", path=root)
    query = request.get("query") or ""
    if not query:
        fail("INVALID_ARGUMENT", "query is required")
    regex = bool(request.get("regex"))
    case_sensitive = bool(request.get("case_sensitive"))
    include_hidden = bool(request.get("include_hidden"))
    include_ignored = bool(request.get("include_ignored"))
    include_globs = request.get("include_globs") or []
    exclude_globs = request.get("exclude_globs") or []
    context_lines = max(0, min(int(request.get("context_lines") or 0), 20))
    max_results = max(1, min(int(request.get("max_results") or 100), 1000))
    flags = 0 if case_sensitive else re.IGNORECASE
    matcher = re.compile(query if regex else re.escape(query), flags)
    matches = []

    if os.path.isfile(root):
        candidates = [(root, os.path.basename(root))]
    elif os.path.isdir(root):
        candidates = (
            (full_path, os.path.relpath(full_path, root).replace(os.sep, "/"))
            for full_path, entry, _ in iter_tree(root, include_hidden, include_ignored)
            if entry.is_file(follow_symlinks=False)
        )
    else:
        fail("NOT_REGULAR_FILE", "search_text only supports regular files or directories", path=root)

    truncated = False
    for full_path, rel in candidates:
        if include_globs and not any(glob_matches(pattern, rel) for pattern in include_globs):
            continue
        if any(glob_matches(pattern, rel) for pattern in exclude_globs):
            continue
        try:
            info = os.stat(full_path)
            if info.st_size > MAX_TEXT_FILE_BYTES:
                continue
            with open(full_path, "rb") as handle:
                data = handle.read(MAX_TEXT_FILE_BYTES + 1)
            if len(data) > MAX_TEXT_FILE_BYTES or b"\x00" in data[:8192]:
                continue
            text = data.decode("utf-8")
        except (OSError, UnicodeDecodeError):
            continue
        lines = text.splitlines()
        for index, line in enumerate(lines):
            found = matcher.search(line)
            if not found:
                continue
            before_start = max(0, index - context_lines)
            after_end = min(len(lines), index + context_lines + 1)
            matches.append(
                {
                    "path": full_path.replace(os.sep, "/"),
                    "relative_path": rel,
                    "line": index + 1,
                    "column": len(line[: found.start()].encode("utf-8")) + 1,
                    "preview": line[:500],
                    "match_text": found.group(0)[:500],
                    "before": lines[before_start:index],
                    "after": lines[index + 1 : after_end],
                    "context_start_line": before_start + 1,
                    "context_end_line": after_end,
                }
            )
            if len(matches) >= max_results:
                truncated = True
                break
        if truncated:
            break
    return {
        "path": root,
        "query": query,
        "engine": "python_wsl",
        "matches": matches,
        "total_matches": len(matches),
        "truncated": truncated,
    }


def fsync_directory(path):
    try:
        descriptor = os.open(path, os.O_RDONLY | getattr(os, "O_DIRECTORY", 0))
        try:
            os.fsync(descriptor)
        finally:
            os.close(descriptor)
    except OSError:
        pass


def atomic_write(request):
    path = checked_path(request.get("path"), write=True)
    content = request.get("content")
    if not isinstance(content, str):
        fail("INVALID_ARGUMENT", "content must be UTF-8 text", path=path)
    overwrite = bool(request.get("overwrite"))
    must_exist = bool(request.get("must_exist"))
    existing = None
    try:
        existing = os.lstat(path)
    except FileNotFoundError:
        if must_exist:
            fail("PATH_NOT_FOUND", "WSL path does not exist", path=path)
    if existing is not None:
        if stat.S_ISLNK(existing.st_mode):
            fail("SYMLINK_NOT_ALLOWED", "file_edit does not allow symlink targets", path=path)
        if not stat.S_ISREG(existing.st_mode):
            fail("NOT_REGULAR_FILE", "file_edit only supports regular files", path=path, type=kind_from_mode(existing.st_mode))
        if not overwrite and not must_exist:
            fail("FILE_EXISTS", "file already exists; set overwrite=true to replace it", path=path)
    parent = os.path.dirname(path)
    os.makedirs(parent, mode=0o700, exist_ok=True)
    mode = stat.S_IMODE(existing.st_mode) if existing is not None else int(request.get("mode") or 0o644)
    owner_uid = existing.st_uid if existing is not None else request.get("owner_uid")
    owner_gid = existing.st_gid if existing is not None else request.get("owner_gid")
    preserve_owner = owner_uid is not None and owner_gid is not None
    payload = content.encode("utf-8")
    descriptor, temporary = tempfile.mkstemp(prefix=".agentdock-atomic-", dir=parent)
    committed = False
    try:
        os.fchmod(descriptor, mode)
        if preserve_owner:
            try:
                os.fchown(descriptor, int(owner_uid), int(owner_gid))
            except PermissionError:
                fail(
                    "OWNERSHIP_CHANGE_BLOCKED",
                    "atomic replacement could not preserve file ownership",
                    path=path,
                    owner_uid=owner_uid,
                    owner_gid=owner_gid,
                    current_uid=os.geteuid(),
                    current_gid=os.getegid(),
                )
        with os.fdopen(descriptor, "wb", closefd=True) as handle:
            descriptor = -1
            handle.write(payload)
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary, path)
        committed = True
        fsync_directory(parent)
    finally:
        if descriptor >= 0:
            os.close(descriptor)
        if not committed:
            try:
                os.remove(temporary)
            except FileNotFoundError:
                pass
    with open(path, "rb") as handle:
        stored = handle.read()
    if stored != payload:
        fail("WRITE_VERIFICATION_FAILED", "file content verification failed after atomic replacement", path=path)
    return {
        "path": path,
        "size_bytes": len(payload),
        "mode": mode,
        "sha256": hashlib.sha256(payload).hexdigest(),
    }


def delete_file(request):
    path = checked_path(request.get("path"), write=True)
    try:
        info = os.lstat(path)
    except FileNotFoundError:
        fail("PATH_NOT_FOUND", "WSL path does not exist", path=path)
    if stat.S_ISLNK(info.st_mode):
        fail("SYMLINK_NOT_ALLOWED", "file_edit does not allow symlink targets", path=path)
    if not stat.S_ISREG(info.st_mode):
        fail("NOT_REGULAR_FILE", "file_edit delete only supports regular files", path=path, type=kind_from_mode(info.st_mode))
    os.remove(path)
    fsync_directory(os.path.dirname(path))
    return {"path": path}


def move_file(request):
    source = checked_path(request.get("path"), write=True)
    destination = checked_path(request.get("new_path"), write=True)
    overwrite = bool(request.get("overwrite"))
    try:
        source_info = os.lstat(source)
    except FileNotFoundError:
        fail("PATH_NOT_FOUND", "WSL source path does not exist", path=source)
    if stat.S_ISLNK(source_info.st_mode):
        fail("SYMLINK_NOT_ALLOWED", "file_edit does not allow symlink sources", path=source)
    if not stat.S_ISREG(source_info.st_mode):
        fail("NOT_REGULAR_FILE", "file_edit move only supports regular files", path=source, type=kind_from_mode(source_info.st_mode))
    try:
        destination_info = os.lstat(destination)
    except FileNotFoundError:
        destination_info = None
    if destination_info is not None:
        if stat.S_ISLNK(destination_info.st_mode):
            fail("SYMLINK_NOT_ALLOWED", "file_edit does not allow symlink destinations", path=destination)
        if not stat.S_ISREG(destination_info.st_mode):
            fail("NOT_REGULAR_FILE", "file_edit move only supports regular file destinations", path=destination, type=kind_from_mode(destination_info.st_mode))
        if not overwrite:
            fail("FILE_EXISTS", "destination already exists; set overwrite=true to replace it", path=destination)
    os.makedirs(os.path.dirname(destination), mode=0o700, exist_ok=True)
    try:
        if overwrite:
            os.replace(source, destination)
        else:
            os.rename(source, destination)
    except OSError as error:
        if error.errno == errno.EXDEV:
            fail("CROSS_DEVICE_MOVE", "WSL file moves must stay on the same filesystem", path=source, new_path=destination)
        raise
    fsync_directory(os.path.dirname(source))
    fsync_directory(os.path.dirname(destination))
    return {"path": source, "new_path": destination}


def dispatch(request):
    action = request.get("action")
    if action == "read":
        result = read_text(
            request.get("path"),
            reject_symlink=bool(request.get("reject_symlink")),
            allow_missing=bool(request.get("allow_missing")),
        )
        return {"exists": result is not None, **(result or {})}
    if action == "list_dir":
        return list_directory(request)
    if action == "list_files":
        return list_files(request)
    if action == "search_text":
        return search_text(request)
    if action == "write_atomic":
        return atomic_write(request)
    if action == "delete":
        return delete_file(request)
    if action == "move":
        return move_file(request)
    fail("INVALID_ACTION", "unsupported WSL file helper action", action=action)


def main():
    try:
        request = json.load(sys.stdin)
        result = dispatch(request)
        json.dump({"ok": True, **result}, sys.stdout, ensure_ascii=False, separators=(",", ":"))
    except ToolFailure as error:
        json.dump(
            {"ok": False, "code": error.code, "message": error.message, "details": error.details},
            sys.stdout,
            ensure_ascii=False,
            separators=(",", ":"),
        )
    except re.error as error:
        json.dump(
            {"ok": False, "code": "INVALID_REGEX", "message": str(error), "details": {}},
            sys.stdout,
            ensure_ascii=False,
            separators=(",", ":"),
        )
    except Exception as error:
        json.dump(
            {"ok": False, "code": "WSL_FILE_RUNTIME_ERROR", "message": str(error), "details": {"type": type(error).__name__}},
            sys.stdout,
            ensure_ascii=False,
            separators=(",", ":"),
        )


if __name__ == "__main__":
    main()

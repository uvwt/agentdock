import datetime
import ctypes
import errno
import fcntl
import fnmatch
import hashlib
import json
import os
import re
import stat
import sys
import tempfile
import uuid

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
    if rel.startswith("./"):
        rel = rel[2:]
    if pattern.startswith("./"):
        pattern = pattern[2:]

    pattern_parts = pattern.split("/")
    path_parts = rel.split("/")
    memo = {}

    def match(pattern_index, path_index):
        key = (pattern_index, path_index)
        if key in memo:
            return memo[key]
        if pattern_index == len(pattern_parts):
            result = path_index == len(path_parts)
        elif pattern_parts[pattern_index] == "**":
            result = match(pattern_index + 1, path_index) or (
                path_index < len(path_parts) and match(pattern_index, path_index + 1)
            )
        else:
            result = (
                path_index < len(path_parts)
                and fnmatch.fnmatchcase(path_parts[path_index], pattern_parts[pattern_index])
                and match(pattern_index + 1, path_index + 1)
            )
        memo[key] = result
        return result

    return match(0, 0)


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
        "path": rel,
        "type": "directory" if stat.S_ISDIR(info.st_mode) else "file",
        "size_bytes": info.st_size,
        "modified": timestamp(info.st_mtime),
        "is_hidden": hidden_path(rel),
    }


def iter_tree(root, include_hidden, include_ignored, max_depth=None, skipped_paths=None):
    rules = [] if include_ignored else parse_ignore_file(root)

    def scan_directory(current):
        try:
            return sorted(os.scandir(current), key=lambda item: item.name)
        except PermissionError:
            if current == root:
                raise
            if skipped_paths is not None:
                skipped_paths.append(os.path.relpath(current, root).replace(os.sep, "/"))
            return []

    # Match filepath.WalkDir's lexical depth-first order without Python recursion.
    stack = [(entry.path, entry, 1) for entry in reversed(scan_directory(root))]
    while stack:
        full_path, entry, depth = stack.pop()
        rel = os.path.relpath(full_path, root).replace(os.sep, "/")
        try:
            is_directory = entry.is_dir(follow_symlinks=False)
        except PermissionError:
            if skipped_paths is not None:
                skipped_paths.append(rel)
            continue
        if not include_hidden and hidden_path(rel):
            continue
        if not include_ignored and (entry.name in DEFAULT_SKIPPED_DIRS or ignored_by_rules(rel, is_directory, rules)):
            continue
        yield full_path, entry, depth
        if is_directory and (max_depth is None or depth < max_depth):
            children = scan_directory(full_path)
            for child in reversed(children):
                stack.append((child.path, child, depth + 1))


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
    max_depth = max(1, min(int(request.get("max_depth") or 1), 20))
    max_entries = max(1, min(int(request.get("max_entries") or 200), 5000))
    patterns = request.get("patterns") or ["**/*"]
    exclude_patterns = request.get("exclude_patterns") or []
    entry_type = request.get("entry_type") or "any"
    if entry_type not in ("any", "file", "directory"):
        fail("INVALID_ARGUMENT", "entry_type must be any, file, or directory", entry_type=entry_type)

    items = []
    skipped_paths = []
    truncated = False
    iterator = iter_tree(
        root,
        include_hidden,
        include_ignored,
        max_depth=max_depth,
        skipped_paths=skipped_paths,
    )
    for full_path, entry, _ in iterator:
        rel = os.path.relpath(full_path, root).replace(os.sep, "/")
        try:
            kind = "directory" if entry.is_dir(follow_symlinks=False) else "file"
        except PermissionError:
            skipped_paths.append(rel)
            continue
        if entry_type != "any" and entry_type != kind:
            continue
        if not any(glob_matches(pattern, rel) for pattern in patterns):
            continue
        if any(glob_matches(pattern, rel) for pattern in exclude_patterns):
            continue
        if len(items) >= max_entries:
            truncated = True
            break
        try:
            items.append(entry_record(root, full_path, entry.name))
        except PermissionError:
            skipped_paths.append(rel)

    items.sort(key=lambda item: item["path"])
    skipped_paths = sorted(set(skipped_paths))
    return {
        "path": root,
        "entries": items,
        "truncated": truncated,
        "partial": bool(skipped_paths),
        "skipped_paths": skipped_paths,
    }


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


def fsync_directory_strict(path):
    descriptor = os.open(path, os.O_RDONLY | getattr(os, "O_DIRECTORY", 0))
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


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


def transaction_state_dir(workdir):
    state_home = os.environ.get("XDG_STATE_HOME")
    if not state_home:
        state_home = os.path.join(os.path.expanduser("~"), ".local", "state")
    key = hashlib.sha256(workdir.encode("utf-8")).hexdigest()
    root = os.path.join(state_home, "agentdock", "wsl-patch-transactions", key)
    os.makedirs(root, mode=0o700, exist_ok=True)
    try:
        os.chmod(root, 0o700)
    except OSError:
        pass
    return root


def acquire_transaction_lock(state_dir):
    lock_path = os.path.join(state_dir, "lock")
    descriptor = os.open(lock_path, os.O_CREAT | os.O_RDWR, 0o600)
    try:
        fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        os.close(descriptor)
        fail("PATCH_BUSY", "another WSL patch transaction is active for this workdir")
    return descriptor


def write_json_atomic(path, value):
    parent = os.path.dirname(path)
    temporary = path + ".tmp-" + uuid.uuid4().hex
    descriptor = os.open(temporary, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    replaced = False
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8", closefd=True) as handle:
            descriptor = -1
            json.dump(value, handle, ensure_ascii=False, separators=(",", ":"), sort_keys=True)
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary, path)
        replaced = True
        # Journal durability is part of crash recovery semantics, unlike the
        # best-effort directory fsync used by ordinary file operations.
        fsync_directory_strict(parent)
    finally:
        if descriptor >= 0:
            os.close(descriptor)
        if not replaced:
            try:
                os.remove(temporary)
            except FileNotFoundError:
                pass


def path_snapshot(path):
    path = checked_path(path, write=True)
    flags = os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0)
    try:
        descriptor = os.open(path, flags)
    except FileNotFoundError:
        return None
    except OSError as error:
        if error.errno == errno.ELOOP:
            fail("SYMLINK_NOT_ALLOWED", "file_edit does not allow symlink targets", path=path)
        raise
    try:
        info = os.fstat(descriptor)
        if not stat.S_ISREG(info.st_mode):
            fail("NOT_REGULAR_FILE", "file_edit patch only supports regular files", path=path, type=kind_from_mode(info.st_mode))
        if info.st_size > MAX_TEXT_FILE_BYTES:
            fail("FILE_TOO_LARGE", "patch target exceeds the text file input limit", path=path, size_bytes=info.st_size)
        with os.fdopen(descriptor, "rb", closefd=True) as handle:
            descriptor = -1
            data = handle.read(MAX_TEXT_FILE_BYTES + 1)
    finally:
        if descriptor >= 0:
            os.close(descriptor)
    if len(data) > MAX_TEXT_FILE_BYTES:
        fail("FILE_TOO_LARGE", "patch target exceeds the text file input limit", path=path)
    return {
        "sha256": hashlib.sha256(data).hexdigest(),
        "mode": stat.S_IMODE(info.st_mode),
        "uid": info.st_uid,
        "gid": info.st_gid,
    }


def snapshot_matches(snapshot, sha256_value, mode=None, uid=None, gid=None):
    if snapshot is None or snapshot.get("sha256") != sha256_value:
        return False
    if mode is not None and snapshot.get("mode") != int(mode):
        return False
    if uid is not None and snapshot.get("uid") != int(uid):
        return False
    if gid is not None and snapshot.get("gid") != int(gid):
        return False
    return True


def missing_parent_directories(path):
    missing = []
    cursor = os.path.dirname(path)
    while True:
        try:
            info = os.lstat(cursor)
        except FileNotFoundError:
            missing.append(cursor)
            parent = os.path.dirname(cursor)
            if parent == cursor:
                fail("PATH_NOT_FOUND", "patch parent directory does not exist", path=path)
            cursor = parent
            continue
        if stat.S_ISLNK(info.st_mode):
            fail("SYMLINK_NOT_ALLOWED", "file_edit does not allow symlink path components", path=cursor)
        if not stat.S_ISDIR(info.st_mode):
            fail("NOT_A_DIRECTORY", "patch parent is not a directory", path=cursor)
        break
    missing.reverse()
    return missing


def rename_no_replace(source, destination):
    # renameat2(RENAME_NOREPLACE) closes the final TOCTOU window without ever
    # replacing a concurrently-created target. Hard-link installation is a
    # safe fallback because transaction temps always live beside the target.
    libc = ctypes.CDLL(None, use_errno=True)
    renameat2 = getattr(libc, "renameat2", None)
    if renameat2 is not None:
        renameat2.argtypes = [ctypes.c_int, ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p, ctypes.c_uint]
        renameat2.restype = ctypes.c_int
        result = renameat2(-100, os.fsencode(source), -100, os.fsencode(destination), 1)
        if result == 0:
            return
        error = ctypes.get_errno()
        if error == errno.EEXIST:
            raise FileExistsError(error, os.strerror(error), destination)
        if error not in (errno.ENOSYS, errno.EINVAL, errno.EOPNOTSUPP):
            raise OSError(error, os.strerror(error), destination)
    try:
        os.link(source, destination)
    except FileExistsError:
        raise
    except OSError as error:
        fail(
            "NO_REPLACE_UNSUPPORTED",
            "WSL filesystem cannot install patch files without replace semantics",
            path=destination,
            reason=str(error),
        )
    os.remove(source)


def normalize_transaction_changes(request, transaction_id):
    changes = request.get("changes")
    if not isinstance(changes, list) or not changes:
        fail("INVALID_ARGUMENT", "patch transaction requires at least one staged change")
    items = []
    seen = set()
    created_dirs = []
    created_seen = set()
    for index, raw in enumerate(changes):
        if not isinstance(raw, dict):
            fail("INVALID_ARGUMENT", "patch transaction changes must be objects", index=index)
        path = checked_path(raw.get("path"), write=True)
        if path in seen:
            fail("INVALID_ARGUMENT", "patch transaction contains a duplicate path", path=path)
        seen.add(path)
        expected_exists = raw.get("expected_exists")
        new_exists = raw.get("new_exists")
        if not isinstance(expected_exists, bool) or not isinstance(new_exists, bool):
            fail("INVALID_ARGUMENT", "patch transaction existence fields must be booleans", path=path)

        item = {
            "path": path,
            "expected_exists": expected_exists,
            "new_exists": new_exists,
            "expected_sha256": raw.get("expected_sha256"),
            "expected_mode": raw.get("expected_mode"),
            "expected_uid": raw.get("expected_uid"),
            "expected_gid": raw.get("expected_gid"),
            "new_sha256": raw.get("sha256"),
            "new_mode": raw.get("mode"),
            "new_uid": raw.get("owner_uid"),
            "new_gid": raw.get("owner_gid"),
            "temp_path": None,
            "backup_path": None,
        }
        if expected_exists:
            if not isinstance(item["expected_sha256"], str) or len(item["expected_sha256"]) != 64:
                fail("INVALID_ARGUMENT", "existing patch target requires expected_sha256", path=path)
            if item["expected_mode"] is None or item["expected_uid"] is None or item["expected_gid"] is None:
                fail("INVALID_ARGUMENT", "existing patch target requires expected mode and ownership", path=path)
            item["backup_path"] = os.path.join(
                os.path.dirname(path), f".agentdock-patch-backup-{transaction_id}-{index}"
            )
        if new_exists:
            content = raw.get("content")
            if not isinstance(content, str):
                fail("INVALID_ARGUMENT", "new patch content must be UTF-8 text", path=path)
            payload = content.encode("utf-8")
            if len(payload) > MAX_TEXT_FILE_BYTES:
                fail("FILE_TOO_LARGE", "new patch content exceeds the text file input limit", path=path)
            actual_hash = hashlib.sha256(payload).hexdigest()
            if raw.get("sha256") != actual_hash:
                fail("INVALID_ARGUMENT", "new patch content hash does not match request", path=path)
            if item["new_mode"] is None:
                fail("INVALID_ARGUMENT", "new patch content requires mode", path=path)
            if (item["new_uid"] is None) != (item["new_gid"] is None):
                fail("INVALID_ARGUMENT", "owner_uid and owner_gid must be provided together", path=path)
            item["content"] = content
            item["temp_path"] = os.path.join(
                os.path.dirname(path), f".agentdock-patch-write-{transaction_id}-{index}"
            )
            for directory in missing_parent_directories(path):
                if directory not in created_seen:
                    created_seen.add(directory)
                    created_dirs.append(directory)
        items.append(item)
    created_dirs.sort(key=lambda value: (value.count(os.sep), value))
    return items, created_dirs


def verify_transaction_preflight(items):
    for item in items:
        path = item["path"]
        snapshot = path_snapshot(path)
        if not item["expected_exists"]:
            if snapshot is not None:
                fail("PATCH_CONFLICT", "patch target was created concurrently", path=path)
            continue
        if not snapshot_matches(
            snapshot,
            item["expected_sha256"],
            item["expected_mode"],
            item["expected_uid"],
            item["expected_gid"],
        ):
            fail("PATCH_CONFLICT", "patch target changed before commit", path=path)


def create_transaction_temp(item):
    if not item["new_exists"]:
        return
    checked_path(item["temp_path"], write=True)
    payload = item["content"].encode("utf-8")
    descriptor = os.open(item["temp_path"], os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    try:
        os.fchmod(descriptor, int(item["new_mode"]))
        if item["new_uid"] is not None:
            try:
                os.fchown(descriptor, int(item["new_uid"]), int(item["new_gid"]))
            except PermissionError:
                fail(
                    "OWNERSHIP_CHANGE_BLOCKED",
                    "patch transaction could not preserve file ownership",
                    path=item["path"],
                    owner_uid=item["new_uid"],
                    owner_gid=item["new_gid"],
                    current_uid=os.geteuid(),
                    current_gid=os.getegid(),
                )
        with os.fdopen(descriptor, "wb", closefd=True) as handle:
            descriptor = -1
            handle.write(payload)
            handle.flush()
            os.fsync(handle.fileno())
    finally:
        if descriptor >= 0:
            os.close(descriptor)


def verify_backup(item):
    snapshot = path_snapshot(item["backup_path"])
    if not snapshot_matches(
        snapshot,
        item["expected_sha256"],
        item["expected_mode"],
        item["expected_uid"],
        item["expected_gid"],
    ):
        fail("PATCH_CONFLICT", "patch target changed while commit was starting", path=item["path"])


def new_target_matches(item):
    # Existing/moved files explicitly carry an owner and therefore verify it.
    # A newly-added file intentionally inherits the helper process owner; when
    # no owner was requested, a later external chown is not transaction-owned
    # state and must not be treated as a rollback conflict.
    snapshot = path_snapshot(item["path"])
    return snapshot_matches(snapshot, item["new_sha256"], item["new_mode"], item["new_uid"], item["new_gid"])


def original_target_matches(item):
    snapshot = path_snapshot(item["path"])
    return snapshot_matches(
        snapshot,
        item["expected_sha256"],
        item["expected_mode"],
        item["expected_uid"],
        item["expected_gid"],
    )


def rollback_transaction(journal):
    errors = []
    for item in reversed(journal.get("items") or []):
        path = item["path"]
        backup_path = item.get("backup_path")
        backup_exists = bool(backup_path and os.path.lexists(backup_path))
        target_exists = os.path.lexists(path)

        if item["expected_exists"] and not backup_exists:
            # This item was never backed up, so its current target still belongs
            # to the pre-transaction world. Never remove it based on hash alone.
            if not target_exists:
                errors.append(f"original target and backup are both missing: {path}")
        elif target_exists:
            try:
                original_unchanged = item["expected_exists"] and backup_exists and original_target_matches(item)
                installed_unchanged = item["new_exists"] and new_target_matches(item)
            except (ToolFailure, OSError):
                # A target that became unreadable, a symlink, or another file
                # type during rollback belongs to an external actor or an I/O
                # boundary we cannot safely classify. Preserve it; programming
                # errors still propagate instead of being mistaken for concurrency.
                original_unchanged = False
                installed_unchanged = False
            if original_unchanged:
                # Safe no-replace fallback may implement rename as link+unlink.
                # A crash between those syscalls leaves the original and backup
                # as equivalent links; rollback is already complete once the
                # duplicate backup is removed.
                try:
                    os.remove(backup_path)
                    fsync_directory_strict(os.path.dirname(path))
                    backup_exists = False
                except OSError as error:
                    errors.append(f"remove duplicate patch backup {backup_path}: {error}")
            elif installed_unchanged:
                try:
                    os.remove(path)
                    fsync_directory_strict(os.path.dirname(path))
                    target_exists = False
                except OSError as error:
                    errors.append(f"remove partially installed {path}: {error}")
            else:
                errors.append(f"patched target changed during rollback; preserving current file: {path}")

        if backup_exists and not target_exists:
            try:
                # The source may have been changed by another process after our
                # preflight but before we renamed it to the backup path. In that
                # case verify_backup intentionally fails the commit, but rollback
                # must still put that externally-changed file back where it came
                # from. Only require that the backup is still a regular file;
                # never overwrite a concurrently recreated target.
                path_snapshot(backup_path)
                rename_no_replace(backup_path, path)
                fsync_directory_strict(os.path.dirname(path))
                backup_exists = False
            except Exception as error:
                errors.append(f"restore patch backup for {path}: {error}")

        temp_path = item.get("temp_path")
        if temp_path:
            try:
                os.remove(temp_path)
            except FileNotFoundError:
                pass
            except OSError as error:
                errors.append(f"remove patch temp {temp_path}: {error}")

    for directory in reversed(journal.get("created_dirs") or []):
        try:
            os.rmdir(directory)
            fsync_directory_strict(os.path.dirname(directory))
        except FileNotFoundError:
            pass
        except OSError as error:
            errors.append(f"remove transaction-created directory {directory}: {error}")
    return errors


def cleanup_committed_transaction(journal):
    errors = []
    affected_dirs = set()
    for item in journal.get("items") or []:
        affected_dirs.add(os.path.dirname(item["path"]))
        for key in ("backup_path", "temp_path"):
            candidate = item.get(key)
            if not candidate:
                continue
            try:
                os.remove(candidate)
            except FileNotFoundError:
                pass
            except OSError as error:
                errors.append(f"remove committed transaction artifact {candidate}: {error}")
    for directory in sorted(affected_dirs):
        try:
            fsync_directory_strict(directory)
        except OSError as error:
            errors.append(f"fsync committed transaction directory {directory}: {error}")
    return errors


def recover_transaction_journals(state_dir):
    recovered = 0
    try:
        names = sorted(name for name in os.listdir(state_dir) if name.endswith(".json"))
    except FileNotFoundError:
        return 0
    for name in names:
        journal_path = os.path.join(state_dir, name)
        try:
            with open(journal_path, "r", encoding="utf-8") as handle:
                journal = json.load(handle)
        except Exception as error:
            fail("PATCH_RECOVERY_REQUIRED", "cannot read WSL patch transaction journal", journal=journal_path, reason=str(error))
        if journal.get("version") != 1:
            fail("PATCH_RECOVERY_REQUIRED", "unsupported WSL patch transaction journal version", journal=journal_path)
        if journal.get("phase") == "committed":
            errors = cleanup_committed_transaction(journal)
        else:
            errors = rollback_transaction(journal)
        if errors:
            fail("PATCH_RECOVERY_REQUIRED", "WSL patch transaction recovery is incomplete", journal=journal_path, errors=errors)
        try:
            os.remove(journal_path)
        except FileNotFoundError:
            pass
        fsync_directory_strict(state_dir)
        recovered += 1
    return recovered


def patch_transaction(request):
    workdir = checked_path(request.get("workdir"))
    ensure_directory(workdir)
    state_dir = transaction_state_dir(workdir)
    lock_descriptor = acquire_transaction_lock(state_dir)
    try:
        recovered = recover_transaction_journals(state_dir)
        transaction_id = uuid.uuid4().hex
        items, planned_dirs = normalize_transaction_changes(request, transaction_id)
        verify_transaction_preflight(items)

        journal_path = os.path.join(state_dir, transaction_id + ".json")
        journal = {
            "version": 1,
            "transaction_id": transaction_id,
            "workdir": workdir,
            "phase": "preparing",
            "created_dirs": [],
            "items": [{key: value for key, value in item.items() if key != "content"} for item in items],
        }
        write_json_atomic(journal_path, journal)

        try:
            for directory in planned_dirs:
                try:
                    os.mkdir(directory, 0o755)
                except FileExistsError:
                    info = os.lstat(directory)
                    if stat.S_ISLNK(info.st_mode):
                        fail("SYMLINK_NOT_ALLOWED", "file_edit does not allow symlink path components", path=directory)
                    if not stat.S_ISDIR(info.st_mode):
                        fail("NOT_A_DIRECTORY", "patch parent is not a directory", path=directory)
                    continue
                journal["created_dirs"].append(directory)
                write_json_atomic(journal_path, journal)

            # Re-check path components after parent creation. An external actor
            # must not be able to replace a just-created parent with a symlink
            # and redirect temp/backup/install I/O elsewhere.
            for item in items:
                checked_path(item["path"], write=True)
            for item in items:
                create_transaction_temp(item)
            journal["phase"] = "prepared"
            write_json_atomic(journal_path, journal)

            # Destructive commit starts only after every target passed preflight,
            # every parent exists, every new file is fsynced, and the recovery
            # journal is durable.
            for item in items:
                checked_path(item["path"], write=True)
            for item in items:
                if not item["expected_exists"]:
                    continue
                try:
                    rename_no_replace(item["path"], item["backup_path"])
                except FileExistsError:
                    fail("PATCH_CONFLICT", "patch backup path already exists", path=item["path"])
                fsync_directory_strict(os.path.dirname(item["path"]))
                verify_backup(item)

            for item in items:
                if not item["new_exists"]:
                    continue
                try:
                    rename_no_replace(item["temp_path"], item["path"])
                except FileExistsError:
                    fail("PATCH_CONFLICT", "patch target was created concurrently", path=item["path"])
                fsync_directory_strict(os.path.dirname(item["path"]))

            for item in items:
                if item["new_exists"]:
                    if not new_target_matches(item):
                        fail("WRITE_VERIFICATION_FAILED", "patch transaction content verification failed", path=item["path"])
                elif os.path.lexists(item["path"]):
                    fail("PATCH_CONFLICT", "deleted patch target was recreated concurrently", path=item["path"])

            affected_dirs = sorted({os.path.dirname(item["path"]) for item in items})
            for directory in affected_dirs:
                fsync_directory_strict(directory)
            journal["phase"] = "committed"
            write_json_atomic(journal_path, journal)
        except Exception as cause:
            rollback_errors = rollback_transaction(journal)
            if rollback_errors:
                fail(
                    "PATCH_ROLLBACK_INCOMPLETE",
                    "WSL patch transaction failed and rollback is incomplete",
                    journal=journal_path,
                    reason=str(cause),
                    rollback_errors=rollback_errors,
                )
            try:
                os.remove(journal_path)
            except FileNotFoundError:
                pass
            fsync_directory_strict(state_dir)
            raise

        cleanup_errors = cleanup_committed_transaction(journal)
        cleanup_pending = bool(cleanup_errors)
        if not cleanup_pending:
            try:
                os.remove(journal_path)
            except FileNotFoundError:
                pass
            fsync_directory_strict(state_dir)
        return {
            "transaction_id": transaction_id,
            "files_changed": len(items),
            "recovered_transactions": recovered,
            "cleanup_pending": cleanup_pending,
        }
    finally:
        os.close(lock_descriptor)


def recover_patch_transactions(request):
    workdir = checked_path(request.get("workdir"))
    ensure_directory(workdir)
    state_dir = transaction_state_dir(workdir)
    lock_descriptor = acquire_transaction_lock(state_dir)
    try:
        return {"recovered_transactions": recover_transaction_journals(state_dir)}
    finally:
        os.close(lock_descriptor)


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
    if action == "search_text":
        return search_text(request)
    if action == "write_atomic":
        return atomic_write(request)
    if action == "delete":
        return delete_file(request)
    if action == "move":
        return move_file(request)
    if action == "patch_transaction":
        return patch_transaction(request)
    if action == "recover_patch_transactions":
        return recover_patch_transactions(request)
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

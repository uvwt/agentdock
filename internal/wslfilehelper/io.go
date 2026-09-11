//go:build linux

package wslfilehelper

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

const maxTextFileBytes = 32 << 20

func readText(path string, rejectSymlink, allowMissing bool) (*Response, error) {
	path, err := checkedPath(path, false)
	if err != nil {
		return nil, err
	}
	linkInfo, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if allowMissing {
			return &Response{Exists: boolPtr(false)}, nil
		}
		return nil, fail("PATH_NOT_FOUND", "WSL path does not exist", map[string]any{"path": path})
	}
	if err != nil {
		return nil, err
	}
	isSymlink := linkInfo.Mode()&os.ModeSymlink != 0
	if isSymlink && rejectSymlink {
		return nil, fail("SYMLINK_NOT_ALLOWED", "file_edit does not allow symlink targets", map[string]any{"path": path})
	}

	var file *os.File
	if rejectSymlink {
		file, err = openNoFollow(path)
	} else {
		file, err = os.Open(path)
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, fail("PATH_NOT_FOUND", "WSL symlink target does not exist", map[string]any{"path": path})
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fail("IS_DIRECTORY", "cannot read directory", map[string]any{"path": path})
	}
	if !info.Mode().IsRegular() {
		return nil, fail("NOT_REGULAR_FILE", "text tools only support regular files", map[string]any{"path": path, "type": kindFromMode(info.Mode())})
	}
	if info.Size() > maxTextFileBytes {
		return nil, fail("FILE_TOO_LARGE", "text file exceeds the input limit", map[string]any{"path": path, "size_bytes": info.Size(), "max_size_bytes": maxTextFileBytes})
	}
	data, err := io.ReadAll(io.LimitReader(file, maxTextFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxTextFileBytes {
		return nil, fail("FILE_TOO_LARGE", "text file exceeds the input limit", map[string]any{"path": path, "max_size_bytes": maxTextFileBytes})
	}
	probe := data
	if len(probe) > 8192 {
		probe = probe[:8192]
	}
	if bytes.IndexByte(probe, 0) >= 0 {
		return nil, fail("BINARY_FILE", "binary file read blocked for text tool", map[string]any{"path": path})
	}
	if !utf8.Valid(data) {
		return nil, fail("ENCODING_UNSUPPORTED", "file is not valid utf-8", map[string]any{"path": path})
	}
	mode, uid, gid := statIdentity(info)
	return &Response{
		Exists: boolPtr(true), Path: path, Content: string(data), SizeBytes: int64Ptr(int64(len(data))),
		Mode: intPtr(mode), Modified: timestamp(info.ModTime()), UID: intPtr(uid), GID: intPtr(gid), Symlink: boolPtr(isSymlink),
	}, nil
}

func fsyncDirectory(path string) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return
	}
	defer unix.Close(fd)
	_ = unix.Fsync(fd)
}

func fsyncDirectoryStrict(path string) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	return unix.Fsync(fd)
}

func atomicWrite(req *Request) (*Response, error) {
	path, err := checkedPath(req.Path, true)
	if err != nil {
		return nil, err
	}
	if req.Content == nil {
		return nil, fail("INVALID_ARGUMENT", "content must be UTF-8 text", map[string]any{"path": path})
	}
	var existing os.FileInfo
	existing, err = os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		existing = nil
		if req.MustExist {
			return nil, fail("PATH_NOT_FOUND", "WSL path does not exist", map[string]any{"path": path})
		}
	} else if err != nil {
		return nil, err
	}
	if existing != nil {
		if existing.Mode()&os.ModeSymlink != 0 {
			return nil, fail("SYMLINK_NOT_ALLOWED", "file_edit does not allow symlink targets", map[string]any{"path": path})
		}
		if !existing.Mode().IsRegular() {
			return nil, fail("NOT_REGULAR_FILE", "file_edit only supports regular files", map[string]any{"path": path, "type": kindFromMode(existing.Mode())})
		}
		if !req.Overwrite && !req.MustExist {
			return nil, fail("FILE_EXISTS", "file already exists; set overwrite=true to replace it", map[string]any{"path": path})
		}
	}

	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return nil, err
	}
	mode := 0o644
	var ownerUID, ownerGID *int
	if existing != nil {
		existingMode, uid, gid := statIdentity(existing)
		mode = existingMode
		ownerUID, ownerGID = intPtr(uid), intPtr(gid)
	} else {
		if req.Mode != nil && *req.Mode != 0 {
			mode = *req.Mode
		}
		ownerUID, ownerGID = req.OwnerUID, req.OwnerGID
	}
	if (ownerUID == nil) != (ownerGID == nil) {
		return nil, fail("INVALID_ARGUMENT", "owner_uid and owner_gid must be provided together", map[string]any{"path": path})
	}

	payload := []byte(*req.Content)
	temp, err := os.CreateTemp(parent, ".agentdock-atomic-")
	if err != nil {
		return nil, err
	}
	tempPath := temp.Name()
	committed := false
	defer func() {
		_ = temp.Close()
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()
	if err := unix.Fchmod(int(temp.Fd()), uint32(mode)); err != nil {
		return nil, err
	}
	if ownerUID != nil {
		if err := unix.Fchown(int(temp.Fd()), *ownerUID, *ownerGID); err != nil {
			if errors.Is(err, os.ErrPermission) || errors.Is(err, unix.EPERM) {
				return nil, fail("OWNERSHIP_CHANGE_BLOCKED", "atomic replacement could not preserve file ownership", map[string]any{
					"path": path, "owner_uid": *ownerUID, "owner_gid": *ownerGID, "current_uid": os.Geteuid(), "current_gid": os.Getegid(),
				})
			}
			return nil, err
		}
	}
	if _, err := temp.Write(payload); err != nil {
		return nil, err
	}
	if err := temp.Sync(); err != nil {
		return nil, err
	}
	if err := temp.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return nil, err
	}
	committed = true
	fsyncDirectory(parent)

	stored, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(stored, payload) {
		return nil, fail("WRITE_VERIFICATION_FAILED", "file content verification failed after atomic replacement", map[string]any{"path": path})
	}
	sum := sha256.Sum256(payload)
	return &Response{Path: path, SizeBytes: int64Ptr(int64(len(payload))), Mode: intPtr(mode), SHA256: hex.EncodeToString(sum[:])}, nil
}

func deleteFile(req *Request) (*Response, error) {
	path, err := checkedPath(req.Path, true)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fail("PATH_NOT_FOUND", "WSL path does not exist", map[string]any{"path": path})
	}
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fail("SYMLINK_NOT_ALLOWED", "file_edit does not allow symlink targets", map[string]any{"path": path})
	}
	if !info.Mode().IsRegular() {
		return nil, fail("NOT_REGULAR_FILE", "file_edit delete only supports regular files", map[string]any{"path": path, "type": kindFromMode(info.Mode())})
	}
	if err := os.Remove(path); err != nil {
		return nil, err
	}
	fsyncDirectory(filepath.Dir(path))
	return &Response{Path: path}, nil
}

func moveFile(req *Request) (*Response, error) {
	source, err := checkedPath(req.Path, true)
	if err != nil {
		return nil, err
	}
	destination, err := checkedPath(req.NewPath, true)
	if err != nil {
		return nil, err
	}
	sourceInfo, err := os.Lstat(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fail("PATH_NOT_FOUND", "WSL source path does not exist", map[string]any{"path": source})
	}
	if err != nil {
		return nil, err
	}
	if sourceInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fail("SYMLINK_NOT_ALLOWED", "file_edit does not allow symlink sources", map[string]any{"path": source})
	}
	if !sourceInfo.Mode().IsRegular() {
		return nil, fail("NOT_REGULAR_FILE", "file_edit move only supports regular files", map[string]any{"path": source, "type": kindFromMode(sourceInfo.Mode())})
	}
	if destinationInfo, err := os.Lstat(destination); err == nil {
		if destinationInfo.Mode()&os.ModeSymlink != 0 {
			return nil, fail("SYMLINK_NOT_ALLOWED", "file_edit does not allow symlink destinations", map[string]any{"path": destination})
		}
		if !destinationInfo.Mode().IsRegular() {
			return nil, fail("NOT_REGULAR_FILE", "file_edit move only supports regular file destinations", map[string]any{"path": destination, "type": kindFromMode(destinationInfo.Mode())})
		}
		if !req.Overwrite {
			return nil, fail("FILE_EXISTS", "destination already exists; set overwrite=true to replace it", map[string]any{"path": destination})
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return nil, err
	}
	if req.Overwrite {
		err = os.Rename(source, destination)
	} else {
		err = renameNoReplace(source, destination)
	}
	if errors.Is(err, unix.EXDEV) {
		return nil, fail("CROSS_DEVICE_MOVE", "WSL file moves must stay on the same filesystem", map[string]any{"path": source, "new_path": destination})
	}
	if errors.Is(err, os.ErrExist) {
		return nil, fail("FILE_EXISTS", "destination already exists; set overwrite=true to replace it", map[string]any{"path": destination})
	}
	if err != nil {
		return nil, err
	}
	fsyncDirectory(filepath.Dir(source))
	fsyncDirectory(filepath.Dir(destination))
	return &Response{Path: source, NewPath: destination}, nil
}

func renameNoReplace(source, destination string) error {
	err := unix.Renameat2(unix.AT_FDCWD, source, unix.AT_FDCWD, destination, unix.RENAME_NOREPLACE)
	if err == nil {
		return nil
	}
	if errors.Is(err, unix.EEXIST) {
		return os.ErrExist
	}
	if !errors.Is(err, unix.ENOSYS) && !errors.Is(err, unix.EINVAL) && !errors.Is(err, unix.EOPNOTSUPP) {
		return err
	}
	if err := os.Link(source, destination); err != nil {
		if errors.Is(err, os.ErrExist) {
			return os.ErrExist
		}
		return fail("NO_REPLACE_UNSUPPORTED", "WSL filesystem cannot install patch files without replace semantics", map[string]any{"path": destination, "reason": err.Error()})
	}
	if err := os.Remove(source); err != nil {
		return err
	}
	return nil
}

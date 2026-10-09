package resourcelibrary

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	skills "github.com/uvwt/agentdock/internal/skill"
)

// Export 是一份只含可移植源码快照的 ZIP 摘要。本地绝对路径不会出现在结果里。
type Export struct {
	Files         []FileEntry
	Warnings      []string
	Size          int64
	ArchiveDigest string
	ContentDigest string
}

type FileEntry struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

var hardcodedSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`-----BEGIN (?:RSA |OPENSSH |EC |DSA )?PRIVATE KEY-----`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`(?i)(api[_-]?key|access[_-]?token|secret|password|passwd)\s*[:=]\s*['"][A-Za-z0-9+/=_\-]{8,}`),
}

var devicePathMarkers = []string{
	"/.agentdock/",
	`\.agentdock\`,
	"AGENTDOCK_HOME",
	"SKILL_DATA_DIR",
	"PLUGIN_DATA_DIR",
}

// ExportDirectory 把一个已安装的受管目录打成 ZIP。
// 白名单按递归路径生效：不在可移植快照里的文件会让整个导出失败，而不是静默丢弃。
func ExportDirectory(root, kind, destination string) (Export, error) {
	if kind != KindSkill && kind != KindPlugin {
		return Export{}, failed("PACKAGE_KIND_INVALID", "validation", "package kind must be skill or plugin")
	}
	info, err := os.Lstat(root)
	if err != nil {
		return Export{}, failed("PACKAGE_NOT_FOUND", "not_found", "managed package directory cannot be inspected")
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Export{}, failed("PACKAGE_INVALID", "validation", "managed package must be a regular directory")
	}

	files, warnings, err := collectPortableFiles(root, kind)
	if err != nil {
		return Export{}, err
	}
	if kind == KindSkill && !hasFile(files, "SKILL.md") {
		return Export{}, failed("PACKAGE_INVALID", "validation", "managed Skill export requires SKILL.md")
	}
	if kind == KindPlugin && !hasFile(files, "plugin.json") {
		return Export{}, failed("PACKAGE_INVALID", "validation", "managed Plugin export requires plugin.json")
	}

	contentDigest, err := skills.DigestPackageContent(root)
	if err != nil {
		return Export{}, failed("PACKAGE_INVALID", "validation", "managed package content digest failed")
	}
	size, archiveDigest, err := writePortableZip(root, files, destination, archiveLimit(kind))
	if err != nil {
		return Export{}, err
	}
	return Export{
		Files: files, Warnings: warnings, Size: size,
		ArchiveDigest: archiveDigest, ContentDigest: contentDigest,
	}, nil
}

func hasFile(files []FileEntry, name string) bool {
	for _, file := range files {
		if file.Path == name {
			return true
		}
	}
	return false
}

func collectPortableFiles(root, kind string) ([]FileEntry, []string, error) {
	fsRoot, err := os.OpenRoot(root)
	if err != nil {
		return nil, nil, failed("PACKAGE_INVALID", "validation", "managed package directory cannot be opened")
	}
	defer fsRoot.Close()

	files := make([]FileEntry, 0)
	warnings := make([]string, 0)
	var total int64
	err = filepath.WalkDir(root, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return failed("PACKAGE_INVALID", "validation", "managed package cannot be walked")
		}
		if current == root {
			return nil
		}
		relative, err := filepath.Rel(root, current)
		if err != nil || !filepath.IsLocal(relative) {
			return failed("PACKAGE_INVALID", "validation", "managed package path escapes its root")
		}
		relative = filepath.ToSlash(relative)
		if skills.IsIgnoredPackageMetadataPath(relative) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		// os.Root 逐段拒绝符号链接替换。WalkDir 看到的类型不够，因为中间目录可能在遍历后被换掉。
		info, err := fsRoot.Lstat(relative)
		if err != nil {
			return failed("PACKAGE_INVALID", "validation", "managed package path cannot be inspected")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return failedf("PACKAGE_INVALID", "validation", "symlink is not allowed: %s", relative)
		}
		if reason := rejectedPrivatePath(relative); reason != "" {
			return failedf("PACKAGE_PRIVATE_DATA", "validation", "export refused %s: %s", reason, relative)
		}
		if info.IsDir() {
			if !allowedDirectory(kind, relative) {
				return failedf("PACKAGE_NOT_PORTABLE", "validation", "path is outside the portable allowlist: %s", relative)
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return failedf("PACKAGE_INVALID", "validation", "special file is not allowed: %s", relative)
		}
		if !allowedFile(kind, relative) {
			return failedf("PACKAGE_NOT_PORTABLE", "validation", "path is outside the portable allowlist: %s", relative)
		}
		if info.Size() < 0 || total > extractLimit(kind)-info.Size() {
			return failedf("PACKAGE_INVALID", "validation", "package exceeds %d bytes", extractLimit(kind))
		}
		total += info.Size()
		if len(files)+1 > MaxPackageFiles {
			return failedf("PACKAGE_INVALID", "validation", "package exceeds %d files", MaxPackageFiles)
		}
		found, scanErr := scanHardcodedSecrets(fsRoot, relative)
		if scanErr != nil {
			return scanErr
		}
		warnings = append(warnings, found...)
		files = append(files, FileEntry{Path: relative, Size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if len(files) == 0 {
		return nil, nil, failed("PACKAGE_INVALID", "validation", "managed package has no portable files")
	}
	return files, warnings, nil
}

func scanHardcodedSecrets(root *os.Root, relative string) ([]string, error) {
	extension := strings.ToLower(filepath.Ext(relative))
	switch extension {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return nil, nil
	}
	file, err := root.Open(relative)
	if err != nil {
		return nil, failed("PACKAGE_INVALID", "validation", "managed package file cannot be read")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxSecretScanBytes+1))
	if err != nil {
		return nil, failed("PACKAGE_INVALID", "validation", "managed package file cannot be read")
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return nil, nil
	}
	scanned := data
	partial := false
	if int64(len(data)) > maxSecretScanBytes {
		scanned = data[:maxSecretScanBytes]
		partial = true
	}
	text := string(scanned)
	warnings := make([]string, 0, 1)
	for _, pattern := range hardcodedSecretPatterns {
		if pattern.MatchString(text) {
			// 警告只指向文件，不回显匹配片段，避免把凭证再写进控制面响应。
			warnings = append(warnings, relative+" may contain a hardcoded credential")
			break
		}
	}
	for _, marker := range devicePathMarkers {
		if strings.Contains(text, marker) {
			warnings = append(warnings, relative+" may contain a device-local path")
			break
		}
	}
	if partial {
		warnings = append(warnings, relative+" was only scanned for the first 1 MiB")
	}
	return warnings, nil
}

func writePortableZip(root string, files []FileEntry, destination string, limit int64) (int64, string, error) {
	fsRoot, err := os.OpenRoot(root)
	if err != nil {
		return 0, "", failed("PACKAGE_INVALID", "validation", "managed package directory cannot be opened")
	}
	defer fsRoot.Close()

	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, "", failed("PACKAGE_INVALID", "validation", "export archive cannot be created")
	}
	writer := zip.NewWriter(output)
	failedWrite := false
	for _, file := range files {
		info, err := fsRoot.Lstat(file.Path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			failedWrite = true
			break
		}
		header := &zip.FileHeader{Name: file.Path, Method: zip.Deflate}
		// 0600 与安装器的内容摘要权限掩码一致，避免导出后再安装产生虚假 digest 差异。
		header.SetMode(0o600)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			failedWrite = true
			break
		}
		source, err := fsRoot.Open(file.Path)
		if err != nil {
			failedWrite = true
			break
		}
		_, copyErr := io.Copy(entry, io.LimitReader(source, extractLimit(KindPlugin)+1))
		closeErr := source.Close()
		if copyErr != nil || closeErr != nil {
			failedWrite = true
			break
		}
	}
	closeZipErr := writer.Close()
	closeFileErr := output.Close()
	if failedWrite || closeZipErr != nil || closeFileErr != nil {
		_ = os.Remove(destination)
		return 0, "", failed("PACKAGE_INVALID", "validation", "export archive cannot be written")
	}
	info, err := os.Lstat(destination)
	if err != nil {
		_ = os.Remove(destination)
		return 0, "", failed("PACKAGE_INVALID", "validation", "export archive cannot be inspected")
	}
	if info.Size() > limit || !info.Mode().IsRegular() {
		_ = os.Remove(destination)
		return 0, "", failedf("PACKAGE_INVALID", "validation", "export archive exceeds %d bytes", limit)
	}
	digest, err := skills.DigestFile(destination)
	if err != nil {
		_ = os.Remove(destination)
		return 0, "", failed("PACKAGE_INVALID", "validation", "export archive digest failed")
	}
	return info.Size(), digest, nil
}

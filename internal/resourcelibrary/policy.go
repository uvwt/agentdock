package resourcelibrary

import (
	"archive/zip"
	"io"
	"os"
	"path"
	"strings"
	"unicode"

	"github.com/uvwt/agentdock/internal/skillspec"
)

const (
	KindSkill  = "skill"
	KindPlugin = "plugin"

	// 与现有安装器相同的上限。云端资源库不能另定一套更大的包。
	SkillArchiveLimit  int64 = 128 << 20
	PluginArchiveLimit int64 = 64 << 20
	SkillExtractLimit  int64 = 128 << 20
	PluginExtractLimit int64 = 256 << 20
	MaxPackageFiles          = 10000
	maxControlBody           = 64 * 1024
	maxSecretScanBytes int64 = 1 << 20
)

func archiveLimit(kind string) int64 {
	if kind == KindPlugin {
		return PluginArchiveLimit
	}
	return SkillArchiveLimit
}

func extractLimit(kind string) int64 {
	if kind == KindPlugin {
		return PluginExtractLimit
	}
	return SkillExtractLimit
}

// rejectedPrivatePath 拒绝私人运行时数据和密钥文件名。
// 这是路径规则，不是内容扫描；内容里的疑似凭证只产生警告。
func rejectedPrivatePath(relative string) string {
	relative = strings.ToLower(path.Clean(relative))
	if relative == "." {
		return ""
	}
	for _, segment := range strings.Split(relative, "/") {
		if segment == "" || segment == "." {
			continue
		}
		extension := path.Ext(segment)
		stem := strings.TrimSuffix(segment, extension)
		switch extension {
		case ".pem", ".key", ".p12", ".pfx", ".keystore", ".jks":
			return "key material"
		}
		if segment == ".env" || strings.HasPrefix(segment, ".env.") || extension == ".env" {
			return "environment file"
		}
		switch stem {
		case "cookie", "cookies", "session", "sessions", "secret", "secrets",
			"credential", "credentials", "id_rsa", "id_dsa", "id_ecdsa", "id_ed25519",
			"key", "privatekey", "private_key":
			return "private runtime data"
		}
		if strings.HasSuffix(stem, "-key") || strings.HasSuffix(stem, "_key") ||
			strings.HasSuffix(stem, "-secret") || strings.HasSuffix(stem, "_secret") ||
			strings.HasSuffix(stem, "-cookie") || strings.HasSuffix(stem, "_cookie") ||
			strings.HasSuffix(stem, "-session") || strings.HasSuffix(stem, "_session") {
			return "private runtime data"
		}
		switch segment {
		case "skill-data", ".skill-data", "__pycache__", "node_modules":
			return "private runtime data"
		}
	}
	return ""
}

func allowedSkillFile(relative string) bool {
	switch relative {
	case "SKILL.md", "run.py", "README.md", "LICENSE", "LICENSE.md", "NOTICE", "NOTICE.md":
		return true
	}
	directory, ok := allowedSkillDirectory(path.Dir(relative))
	if !ok {
		return false
	}
	base := path.Base(relative)
	if !safePathSegment(base) {
		return false
	}
	extension := strings.ToLower(path.Ext(base))
	if extension == "" {
		// 无扩展名只允许出现在 scripts 下，避免把任意二进制放进可移植快照。
		return directory == "scripts"
	}
	return allowedSourceExtension(extension)
}

func allowedSkillDirectory(directory string) (string, bool) {
	if directory == "." {
		return "", false
	}
	parts := strings.Split(directory, "/")
	switch parts[0] {
	case "references", "scripts", "tests", "assets", "templates", "docs":
	default:
		return "", false
	}
	for _, part := range parts {
		if !safePathSegment(part) {
			return "", false
		}
	}
	return parts[0], true
}

func allowedPluginFile(relative string) bool {
	switch relative {
	case "plugin.json", "mcp.json", "README.md", "LICENSE", "LICENSE.md", "NOTICE", "NOTICE.md":
		return true
	}
	parts := strings.Split(relative, "/")
	if len(parts) < 3 || parts[0] != "skills" {
		return false
	}
	if err := skillspec.ValidateName(parts[1]); err != nil {
		return false
	}
	if !safePathSegment(parts[1]) {
		return false
	}
	inner := strings.Join(parts[2:], "/")
	return allowedSkillFile(inner)
}

func allowedDirectory(kind, relative string) bool {
	relative = path.Clean(relative)
	if kind == KindPlugin {
		parts := strings.Split(relative, "/")
		if parts[0] != "skills" || !safePathSegment(parts[0]) {
			return false
		}
		if len(parts) == 1 {
			return true
		}
		if err := skillspec.ValidateName(parts[1]); err != nil || !safePathSegment(parts[1]) {
			return false
		}
		if len(parts) == 2 {
			return true
		}
		_, ok := allowedSkillDirectory(strings.Join(parts[2:], "/"))
		return ok
	}
	_, ok := allowedSkillDirectory(relative)
	return ok
}

func allowedFile(kind, relative string) bool {
	if kind == KindPlugin {
		return allowedPluginFile(relative)
	}
	return allowedSkillFile(relative)
}

func safePathSegment(segment string) bool {
	if segment == "" || segment == "." || segment == ".." || strings.HasPrefix(segment, ".") {
		return false
	}
	for _, char := range segment {
		if char > unicode.MaxASCII || char < 0x20 || char == '\\' || char == ':' {
			return false
		}
	}
	return true
}

func allowedSourceExtension(extension string) bool {
	switch extension {
	case ".md", ".txt", ".py", ".sh", ".bash", ".json", ".yml", ".yaml", ".toml",
		".js", ".ts", ".html", ".css", ".svg", ".png", ".jpg", ".jpeg", ".gif", ".webp",
		".csv", ".xml", ".tpl", ".jinja", ".go", ".rs", ".rb", ".php", ".ps1":
		return true
	default:
		return false
	}
}

// InspectArchive 在把候选 ZIP 交给原生安装器之前拒绝明显危险的包。
// 扩展名白名单只用于导出；入站包仍由 Skill/Plugin 安装器决定能不能安装，
// 这里负责路径穿越、符号链接、特殊文件、私钥文件名、文件数和解压大小。
func InspectArchive(archivePath, kind string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return failed("PACKAGE_INVALID", "validation", "candidate archive is not a readable ZIP")
	}
	defer reader.Close()

	var files int
	var total int64
	limit := extractLimit(kind)
	for _, file := range reader.File {
		if file.Flags&0x1 != 0 {
			return failed("PACKAGE_INVALID", "validation", "encrypted ZIP entries are not allowed")
		}
		if file.Mode()&os.ModeSymlink != 0 {
			return failedf("PACKAGE_INVALID", "validation", "ZIP symlink is not allowed: %s", file.Name)
		}
		name := strings.TrimSuffix(file.Name, "/")
		if err := validateArchiveName(name); err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			if reason := rejectedPrivatePath(name); reason != "" {
				return failedf("PACKAGE_PRIVATE_DATA", "validation", "ZIP directory contains %s: %s", reason, name)
			}
			continue
		}
		mode := file.Mode()
		if !mode.IsRegular() {
			return failedf("PACKAGE_INVALID", "validation", "ZIP special file is not allowed: %s", file.Name)
		}
		if reason := rejectedPrivatePath(name); reason != "" {
			return failedf("PACKAGE_PRIVATE_DATA", "validation", "ZIP entry contains %s: %s", reason, name)
		}
		files++
		if files > MaxPackageFiles {
			return failedf("PACKAGE_INVALID", "validation", "package exceeds %d files", MaxPackageFiles)
		}
		body, err := file.Open()
		if err != nil {
			return failed("PACKAGE_INVALID", "validation", "ZIP entry cannot be read")
		}
		copied, copyErr := io.Copy(io.Discard, io.LimitReader(body, limit-total+1))
		closeErr := body.Close()
		if copyErr != nil {
			return failed("PACKAGE_INVALID", "validation", "ZIP entry cannot be read")
		}
		if closeErr != nil {
			return failed("PACKAGE_INVALID", "validation", "ZIP entry cannot be read")
		}
		total += copied
		if total > limit {
			return failedf("PACKAGE_INVALID", "validation", "uncompressed package exceeds %d bytes", limit)
		}
	}
	if files == 0 {
		return failed("PACKAGE_INVALID", "validation", "candidate archive has no files")
	}
	return nil
}

func validateArchiveName(name string) error {
	if name == "" {
		return failed("PACKAGE_INVALID", "validation", "ZIP entry path is empty")
	}
	if strings.HasPrefix(name, "/") || strings.Contains(name, `\`) || strings.Contains(name, ":") {
		return failedf("PACKAGE_INVALID", "validation", "ZIP path escapes package root: %s", name)
	}
	for _, segment := range strings.Split(name, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return failedf("PACKAGE_INVALID", "validation", "ZIP path escapes package root: %s", name)
		}
	}
	return nil
}

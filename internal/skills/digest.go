package skills

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func DigestFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func DigestDirectory(root string) (string, error) {
	return digestDirectory(root, false)
}

// digestPackageContent 计算安装后的稳定内容摘要。
// 传输层 ZIP 摘要用于校验下载来源；版本冲突判断必须只看最终包内容，
// 并忽略 AgentDock 自己写入的安装元数据。
func digestPackageContent(root string) (string, error) {
	return digestDirectory(root, true)
}

func digestDirectory(root string, packageContent bool) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	var paths []string
	err = filepath.WalkDir(rootAbs, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == rootAbs {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink is not allowed in skill package: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(rootAbs, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if packageContent && rel == ".agentdock-install.json" {
			return nil
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, rel := range paths {
		path := filepath.Join(rootAbs, filepath.FromSlash(rel))
		info, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		mode := info.Mode().Perm()
		if packageContent {
			// copyPackage 与 ZIP 解压都会把文件权限收敛到 0755 范围，
			// 内容摘要使用相同规则，避免目录与 ZIP 传输产生虚假差异。
			mode &= 0o755
			if mode == 0 {
				mode = 0o600
			}
		}
		fmt.Fprintf(h, "%s\x00%o\x00%d\x00", rel, mode, info.Size())
		f, err := os.Open(path)
		if err != nil {
			return "", err
		}
		_, copyErr := io.Copy(h, f)
		closeErr := f.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func normalizeDigest(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	if !strings.HasPrefix(value, "sha256:") {
		value = "sha256:" + value
	}
	return value
}

func extractZip(src, dest string, maxBytes int64) error {
	reader, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer reader.Close()
	var total int64
	for _, file := range reader.File {
		if file.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("zip symlink is not allowed: %s", file.Name)
		}
		name := strings.TrimSuffix(file.Name, "/")
		if err := validateRelativePackagePath(name); err != nil {
			return fmt.Errorf("zip path escapes package root: %s: %w", file.Name, err)
		}
		target := filepath.Join(dest, filepath.FromSlash(name))
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
			continue
		}
		total += int64(file.UncompressedSize64)
		if total > maxBytes {
			return fmt.Errorf("uncompressed package exceeds %d bytes", maxBytes)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		rc, err := file.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, file.Mode().Perm()&0o755)
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, io.LimitReader(rc, maxBytes+1))
		closeOutErr := out.Close()
		closeInErr := rc.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeOutErr != nil {
			return closeOutErr
		}
		if closeInErr != nil {
			return closeInErr
		}
	}
	return nil
}

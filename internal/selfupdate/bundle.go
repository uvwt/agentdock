package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	processcontrol "github.com/uvwt/agentdock/internal/process"
)

func extractCoreSkillBundle(archiveData []byte, goos, tempDir string) (string, error) {
	bundlePath := filepath.Join(tempDir, "core-skills")
	if err := os.Mkdir(bundlePath, 0o700); err != nil {
		return "", err
	}
	foundManifest := false
	var total int64

	writeEntry := func(name string, mode os.FileMode, reader io.Reader, size int64) error {
		name = strings.TrimPrefix(filepath.ToSlash(name), "./")
		if !strings.HasPrefix(name, coreSkillBundlePrefix) {
			return nil
		}
		relative := strings.TrimPrefix(name, coreSkillBundlePrefix)
		if relative == "" {
			return nil
		}
		clean := filepath.Clean(filepath.FromSlash(relative))
		if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("Bundle 文件路径越界: %s", name)
		}
		if !mode.IsRegular() {
			return fmt.Errorf("Bundle 只允许普通文件: %s", name)
		}
		if size < 0 || size > maxExtractedPayloadBytes-total {
			return fmt.Errorf("Bundle 解压内容超过 %d 字节限制", maxExtractedPayloadBytes)
		}
		target := filepath.Join(bundlePath, clean)
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		written, copyErr := io.Copy(file, io.LimitReader(reader, size+1))
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if written != size {
			return fmt.Errorf("Bundle 文件大小不匹配: %s", name)
		}
		total += written
		if clean == "manifest.json" {
			foundManifest = true
		}
		return nil
	}

	if goos == "windows" {
		reader, err := zip.NewReader(bytes.NewReader(archiveData), int64(len(archiveData)))
		if err != nil {
			return "", err
		}
		for _, file := range reader.File {
			name := strings.TrimPrefix(filepath.ToSlash(file.Name), "./")
			if !strings.HasPrefix(name, coreSkillBundlePrefix) || strings.HasSuffix(name, "/") {
				continue
			}
			if file.UncompressedSize64 > uint64(maxExtractedPayloadBytes) {
				return "", fmt.Errorf("Bundle 解压内容超过 %d 字节限制", maxExtractedPayloadBytes)
			}
			opened, err := file.Open()
			if err != nil {
				return "", err
			}
			writeErr := writeEntry(name, file.Mode(), opened, int64(file.UncompressedSize64))
			closeErr := opened.Close()
			if writeErr != nil {
				return "", writeErr
			}
			if closeErr != nil {
				return "", closeErr
			}
		}
	} else {
		gzipReader, err := gzip.NewReader(bytes.NewReader(archiveData))
		if err != nil {
			return "", err
		}
		defer gzipReader.Close()
		tarReader := tar.NewReader(gzipReader)
		for {
			header, err := tarReader.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return "", err
			}
			name := strings.TrimPrefix(filepath.ToSlash(header.Name), "./")
			if !strings.HasPrefix(name, coreSkillBundlePrefix) || header.FileInfo().IsDir() {
				continue
			}
			if err := writeEntry(name, header.FileInfo().Mode(), tarReader, header.Size); err != nil {
				return "", err
			}
		}
	}
	if !foundManifest {
		return "", errors.New("Release 压缩包缺少核心 Skill manifest")
	}
	return bundlePath, nil
}

func bootstrapBundledSkills(ctx context.Context, binaryPath, bundlePath string, output io.Writer) error {
	if strings.TrimSpace(bundlePath) == "" {
		return errors.New("核心 Skill Bundle 路径不能为空")
	}
	command := exec.CommandContext(ctx, binaryPath, "skill", "bootstrap", "--bundle", bundlePath)
	processcontrol.ConfigureBackground(command)
	combined, err := command.CombinedOutput()
	if len(combined) > 0 && output != nil {
		_, _ = output.Write(combined)
	}
	if err != nil {
		return fmt.Errorf("执行核心 Skill bootstrap 失败: %w: %s", err, strings.TrimSpace(string(combined)))
	}
	return nil
}

// finalizeLegacySkillMigration 必须只在外层更新已经越过 rollback commit point 后调用。
// 旧版 updater 不认识这个新动作时不会调用它，因此 legacy roots 会继续原地保留，
// 仍可供旧二进制回滚读取；新版 updater 则在最终成功后显式收口迁移。
func finalizeLegacySkillMigration(ctx context.Context, binaryPath string, output io.Writer) error {
	command := exec.CommandContext(ctx, binaryPath, "skill", "finalize-migration")
	processcontrol.ConfigureBackground(command)
	combined, err := command.CombinedOutput()
	if len(combined) > 0 && output != nil {
		_, _ = output.Write(combined)
	}
	if err != nil {
		return fmt.Errorf("完成 legacy Skill migration 失败: %w: %s", err, strings.TrimSpace(string(combined)))
	}
	return nil
}

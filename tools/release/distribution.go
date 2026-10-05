package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	sourceLatestReleaseBaseURL = "https://download.nexusdock.co/latest"
	sourcePowerShellBaseLine   = "$defaultReleaseBaseUrl = '" + sourceLatestReleaseBaseURL + "'"
)

func prepareReleaseDistribution(releaseBaseURL, distDir string) error {
	baseURL, err := normalizePublicBaseURL(releaseBaseURL)
	if err != nil {
		return err
	}
	if strings.TrimSpace(distDir) == "" {
		return errors.New("release dist 目录不能为空")
	}

	if err := replaceReleaseLine(
		filepath.Join(distDir, "install.sh"),
		fmt.Sprintf(`DEFAULT_BASE_URL="%s"`, sourceLatestReleaseBaseURL),
		fmt.Sprintf(`DEFAULT_BASE_URL="%s"`, baseURL),
	); err != nil {
		return err
	}
	if err := replaceReleaseLine(
		filepath.Join(distDir, "install.ps1"),
		sourcePowerShellBaseLine,
		fmt.Sprintf("$defaultReleaseBaseUrl = '%s'", baseURL),
	); err != nil {
		return err
	}

	for _, name := range []string{"install.sh", "install.ps1"} {
		if err := refreshReleaseChecksum(distDir, name); err != nil {
			return err
		}
	}
	return nil
}

func replaceReleaseLine(path, oldLine, newLine string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("检查 %s 失败: %w", filepath.Base(path), err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s 不是普通文件", filepath.Base(path))
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取 %s 失败: %w", filepath.Base(path), err)
	}
	text := string(content)
	if strings.Count(text, oldLine) != 1 {
		return fmt.Errorf("%s 必须且只能包含一处发布基址 %q", filepath.Base(path), oldLine)
	}
	text = strings.Replace(text, oldLine, newLine, 1)
	if err := os.WriteFile(path, []byte(text), info.Mode().Perm()); err != nil {
		return fmt.Errorf("写入 %s 失败: %w", filepath.Base(path), err)
	}
	return nil
}

func refreshReleaseChecksum(distDir, name string) error {
	sum, err := fileSHA256(filepath.Join(distDir, name))
	if err != nil {
		return fmt.Errorf("计算 %s SHA-256 失败: %w", name, err)
	}
	checksum := fmt.Sprintf("%s  %s\n", sum, name)
	if err := os.WriteFile(filepath.Join(distDir, name+".sha256"), []byte(checksum), 0o644); err != nil {
		return fmt.Errorf("写入 %s.sha256 失败: %w", name, err)
	}
	return nil
}

func normalizePublicBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("解析公开下载基址失败: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" {
		return "", errors.New("公开下载基址必须是完整 HTTPS 地址")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("公开下载基址不能包含 query 或 fragment")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

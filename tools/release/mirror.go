package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type mirrorRelease struct {
	TagName string               `json:"tag_name"`
	Assets  []mirrorReleaseAsset `json:"assets"`
}

type mirrorReleaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

func prepareMirrorBootstrap(publicBaseURL, distDir string) error {
	baseURL, err := normalizeMirrorBaseURL(publicBaseURL)
	if err != nil {
		return err
	}
	if strings.TrimSpace(distDir) == "" {
		return errors.New("mirror dist 目录不能为空")
	}

	installPath := filepath.Join(distDir, "install.sh")
	info, err := os.Stat(installPath)
	if err != nil {
		return fmt.Errorf("检查 mirror install.sh 失败: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("mirror install.sh 不是普通文件")
	}

	content, err := os.ReadFile(installPath)
	if err != nil {
		return fmt.Errorf("读取 mirror install.sh 失败: %w", err)
	}
	lines := strings.Split(string(content), "\n")
	const prefix = `DEFAULT_BASE_URL="`
	replaced := false
	for i, line := range lines {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		if replaced {
			return errors.New("mirror install.sh 包含多个 DEFAULT_BASE_URL")
		}
		lines[i] = fmt.Sprintf(`DEFAULT_BASE_URL="%s"`, baseURL)
		replaced = true
	}
	if !replaced {
		return errors.New("mirror install.sh 缺少 DEFAULT_BASE_URL")
	}

	if err := os.WriteFile(installPath, []byte(strings.Join(lines, "\n")), info.Mode().Perm()); err != nil {
		return fmt.Errorf("写入 mirror install.sh 失败: %w", err)
	}
	sum, err := fileSHA256(installPath)
	if err != nil {
		return fmt.Errorf("计算 mirror install.sh SHA-256 失败: %w", err)
	}
	checksum := fmt.Sprintf("%s  install.sh\n", sum)
	if err := os.WriteFile(filepath.Join(distDir, "install.sh.sha256"), []byte(checksum), 0o644); err != nil {
		return fmt.Errorf("写入 mirror install.sh.sha256 失败: %w", err)
	}
	return nil
}

func writeMirrorManifest(tag, publicBaseURL, distDir string, stdout io.Writer) error {
	tag = strings.TrimSpace(tag)
	if tag == "" || strings.ContainsAny(tag, `/\`) {
		return fmt.Errorf("无效 Release tag：%q", tag)
	}
	baseURL, err := normalizeMirrorBaseURL(publicBaseURL)
	if err != nil {
		return err
	}
	if strings.TrimSpace(distDir) == "" {
		return errors.New("mirror dist 目录不能为空")
	}

	manifest := mirrorRelease{TagName: tag}
	for _, artifact := range ReleaseCatalog() {
		path := filepath.Join(distDir, artifact.Name)
		info, err := os.Stat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("mirror dist 缺少 Release 资产：%s", artifact.Name)
			}
			return fmt.Errorf("检查 mirror Release 资产 %s 失败: %w", artifact.Name, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("mirror Release 资产不是普通文件：%s", artifact.Name)
		}
		manifest.Assets = append(manifest.Assets, mirrorReleaseAsset{
			Name: artifact.Name,
			URL:  baseURL + "/" + artifact.Name,
		})
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(manifest)
}

func normalizeMirrorBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("解析 mirror public base URL 失败: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" {
		return "", errors.New("mirror public base URL 必须是完整 HTTPS 地址")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("mirror public base URL 不能包含 query 或 fragment")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

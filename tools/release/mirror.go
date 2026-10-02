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

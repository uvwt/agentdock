package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/config"
)

const (
	defaultReleaseAPI = "https://api.github.com/repos/uvwt/agentdock/releases/latest"
	maxReleaseBytes   = 64 << 20
)

type release struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type options struct {
	CurrentVersion string
	ExecutablePath string
	GOOS           string
	GOARCH         string
	ReleaseAPI     string
	HTTPClient     *http.Client
	Output         io.Writer
	Apply          func(context.Context, applyRequest) (applyResult, error)
	VerifyBinary   func(context.Context, string, string) error
}

type applyRequest struct {
	CurrentPath    string
	CurrentVersion string
	StagedPath     string
	TargetVersion  string
	Output         io.Writer
}

type applyResult struct {
	Restarted bool
	HandedOff bool
}

func Run(ctx context.Context, output io.Writer) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("定位当前 AgentDock 二进制失败: %w", err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
		executable = resolved
	}
	return run(ctx, options{
		CurrentVersion: config.Version,
		ExecutablePath: executable,
		GOOS:           runtime.GOOS,
		GOARCH:         runtime.GOARCH,
		ReleaseAPI:     defaultReleaseAPI,
		HTTPClient:     &http.Client{Timeout: 5 * time.Minute},
		Output:         output,
		Apply:          applyPlatformUpdate,
		VerifyBinary:   verifyBinaryVersion,
	})
}

func run(ctx context.Context, opts options) error {
	if opts.HTTPClient == nil {
		return errors.New("更新 HTTP 客户端不能为空")
	}
	if opts.Output == nil {
		opts.Output = io.Discard
	}
	if opts.Apply == nil || opts.VerifyBinary == nil {
		return errors.New("更新执行器未配置")
	}

	latest, err := fetchLatestRelease(ctx, opts.HTTPClient, opts.ReleaseAPI)
	if err != nil {
		return err
	}
	currentVersion := normalizeVersion(opts.CurrentVersion)
	targetVersion := normalizeVersion(latest.TagName)
	if currentVersion == "vdev" || currentVersion == "" {
		return errors.New("当前是开发构建，无法通过 agentdock update 判断可升级版本")
	}
	if currentVersion == targetVersion {
		fmt.Fprintf(opts.Output, "当前已是最新版本：%s\n", targetVersion)
		return nil
	}
	if comparison, comparable := compareVersions(currentVersion, targetVersion); comparable && comparison > 0 {
		fmt.Fprintf(opts.Output, "当前版本 %s 高于最新 Release %s，不执行降级。\n", currentVersion, targetVersion)
		return nil
	}

	archiveName, executableName, err := platformAssetNames(opts.GOOS, opts.GOARCH)
	if err != nil {
		return err
	}
	archiveAsset, ok := findAsset(latest.Assets, archiveName)
	if !ok {
		return fmt.Errorf("Release %s 缺少当前平台文件 %s", targetVersion, archiveName)
	}
	checksumAsset, ok := findAsset(latest.Assets, archiveName+".sha256")
	if !ok {
		return fmt.Errorf("Release %s 缺少校验文件 %s.sha256", targetVersion, archiveName)
	}

	fmt.Fprintf(opts.Output, "当前版本：%s\n最新版本：%s\n\n", currentVersion, targetVersion)
	fmt.Fprintf(opts.Output, "正在下载 %s...\n", archiveName)
	archiveData, err := download(ctx, opts.HTTPClient, archiveAsset.URL, maxReleaseBytes)
	if err != nil {
		return fmt.Errorf("下载更新文件失败: %w", err)
	}
	checksumData, err := download(ctx, opts.HTTPClient, checksumAsset.URL, 1<<20)
	if err != nil {
		return fmt.Errorf("下载校验文件失败: %w", err)
	}
	if err := verifyChecksum(archiveData, checksumData); err != nil {
		return fmt.Errorf("更新文件校验失败，当前版本未被修改: %w", err)
	}
	fmt.Fprintln(opts.Output, "文件校验通过")

	binaryData, err := extractExecutable(archiveData, opts.GOOS, executableName)
	if err != nil {
		return fmt.Errorf("解压更新文件失败: %w", err)
	}
	tempDir, err := os.MkdirTemp("", "agentdock-update-*")
	if err != nil {
		return fmt.Errorf("创建更新临时目录失败: %w", err)
	}
	defer os.RemoveAll(tempDir)
	stagedPath := filepath.Join(tempDir, executableName)
	if err := os.WriteFile(stagedPath, binaryData, 0o755); err != nil {
		return fmt.Errorf("写入新版本二进制失败: %w", err)
	}
	if err := opts.VerifyBinary(ctx, stagedPath, targetVersion); err != nil {
		return fmt.Errorf("新版本二进制验证失败，当前版本未被修改: %w", err)
	}

	fmt.Fprintln(opts.Output, "正在备份并安装新版本...")
	result, err := opts.Apply(ctx, applyRequest{
		CurrentPath:    opts.ExecutablePath,
		CurrentVersion: currentVersion,
		StagedPath:     stagedPath,
		TargetVersion:  targetVersion,
		Output:         opts.Output,
	})
	if err != nil {
		return err
	}
	if result.HandedOff {
		fmt.Fprintf(opts.Output, "Windows 更新已交给辅助进程，将自动替换并重启到 %s。\n", targetVersion)
		return nil
	}
	if result.Restarted {
		fmt.Fprintf(opts.Output, "更新完成并已重启：%s → %s\n", currentVersion, targetVersion)
	} else {
		fmt.Fprintf(opts.Output, "更新完成：%s → %s。当前未检测到托管服务，请重新启动正在运行的 AgentDock。\n", currentVersion, targetVersion)
	}
	return nil
}

func fetchLatestRelease(ctx context.Context, client *http.Client, endpoint string) (release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return release{}, fmt.Errorf("创建 Release 请求失败: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "agentdock/"+config.Version)
	resp, err := client.Do(req)
	if err != nil {
		return release{}, fmt.Errorf("查询最新 Release 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return release{}, fmt.Errorf("查询最新 Release 失败: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var latest release
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 2<<20))
	if err := decoder.Decode(&latest); err != nil {
		return release{}, fmt.Errorf("解析最新 Release 失败: %w", err)
	}
	if normalizeVersion(latest.TagName) == "" {
		return release{}, errors.New("最新 Release 缺少 tag_name")
	}
	return latest, nil
}

func download(ctx context.Context, client *http.Client, rawURL string, limit int64) ([]byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return nil, fmt.Errorf("无效下载地址 %q", rawURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "agentdock/"+config.Version)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	reader := io.LimitReader(resp.Body, limit+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("下载内容超过 %d 字节限制", limit)
	}
	return data, nil
}

func verifyChecksum(data, checksumFile []byte) error {
	fields := strings.Fields(string(checksumFile))
	if len(fields) == 0 {
		return errors.New("校验文件为空")
	}
	expected := strings.ToLower(strings.TrimSpace(fields[0]))
	if len(expected) != sha256.Size*2 {
		return errors.New("SHA-256 格式无效")
	}
	if _, err := hex.DecodeString(expected); err != nil {
		return errors.New("SHA-256 格式无效")
	}
	actual := sha256.Sum256(data)
	if hex.EncodeToString(actual[:]) != expected {
		return errors.New("SHA-256 不匹配")
	}
	return nil
}

func extractExecutable(archiveData []byte, goos, executableName string) ([]byte, error) {
	if goos == "windows" {
		reader, err := zip.NewReader(bytes.NewReader(archiveData), int64(len(archiveData)))
		if err != nil {
			return nil, err
		}
		for _, file := range reader.File {
			if filepath.ToSlash(file.Name) != executableName {
				continue
			}
			opened, err := file.Open()
			if err != nil {
				return nil, err
			}
			defer opened.Close()
			return readLimited(opened, maxReleaseBytes)
		}
		return nil, fmt.Errorf("压缩包缺少 %s", executableName)
	}

	gzipReader, err := gzip.NewReader(bytes.NewReader(archiveData))
	if err != nil {
		return nil, err
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	wanted := "bin/" + executableName
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		name := strings.TrimPrefix(filepath.ToSlash(header.Name), "./")
		if name != wanted || !header.FileInfo().Mode().IsRegular() {
			continue
		}
		return readLimited(tarReader, maxReleaseBytes)
	}
	return nil, fmt.Errorf("压缩包缺少 %s", wanted)
}

func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("解压后的二进制超过 %d 字节限制", limit)
	}
	return data, nil
}

func verifyBinaryVersion(ctx context.Context, binaryPath, targetVersion string) error {
	verifyCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	command := exec.CommandContext(verifyCtx, binaryPath, "--version")
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("执行 --version 失败: %w: %s", err, strings.TrimSpace(string(output)))
	}
	actualVersion, err := parseVersionOutput(output)
	if err != nil {
		return err
	}
	if actualVersion != normalizeVersion(targetVersion) {
		return fmt.Errorf("版本输出为 %s，目标版本为 %s", actualVersion, normalizeVersion(targetVersion))
	}
	return nil
}

func parseVersionOutput(output []byte) (string, error) {
	firstLine := strings.TrimSpace(strings.SplitN(string(output), "\n", 2)[0])
	const prefix = "AgentDock v"
	if !strings.HasPrefix(firstLine, prefix) {
		return "", fmt.Errorf("无法识别版本输出 %q", firstLine)
	}
	version := normalizeVersion(strings.TrimPrefix(firstLine, prefix))
	if _, valid := compareVersions(version, version); !valid {
		return "", fmt.Errorf("版本输出格式无效 %q", firstLine)
	}
	return version, nil
}

func platformAssetNames(goos, goarch string) (archiveName, executableName string, err error) {
	if goarch != "amd64" && goarch != "arm64" {
		return "", "", fmt.Errorf("不支持的 CPU 架构：%s", goarch)
	}
	switch goos {
	case "darwin":
		return "agentdock_darwin_" + goarch + ".tar.gz", "agentdock", nil
	case "linux":
		return "agentdock_linux_" + goarch + ".tar.gz", "agentdock", nil
	case "windows":
		return "agentdock_windows_" + goarch + ".zip", "agentdock.exe", nil
	default:
		return "", "", fmt.Errorf("当前系统暂不支持内置更新：%s/%s", goos, goarch)
	}
}

func findAsset(assets []releaseAsset, name string) (releaseAsset, bool) {
	for _, asset := range assets {
		if asset.Name == name && strings.TrimSpace(asset.URL) != "" {
			return asset, true
		}
	}
	return releaseAsset{}, false
}

func normalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return ""
	}
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	return version
}

func compareVersions(left, right string) (int, bool) {
	parse := func(value string) ([3]int, bool) {
		var parsed [3]int
		value = strings.TrimPrefix(normalizeVersion(value), "v")
		value = strings.SplitN(value, "-", 2)[0]
		parts := strings.Split(value, ".")
		if len(parts) != len(parsed) {
			return parsed, false
		}
		for index, part := range parts {
			number, err := strconv.Atoi(part)
			if err != nil || number < 0 {
				return parsed, false
			}
			parsed[index] = number
		}
		return parsed, true
	}
	leftVersion, leftOK := parse(left)
	rightVersion, rightOK := parse(right)
	if !leftOK || !rightOK {
		return 0, false
	}
	for index := range leftVersion {
		if leftVersion[index] < rightVersion[index] {
			return -1, true
		}
		if leftVersion[index] > rightVersion[index] {
			return 1, true
		}
	}
	return 0, true
}

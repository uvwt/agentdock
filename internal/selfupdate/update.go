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

	"github.com/uvwt/agentdock/internal/buildinfo"
)

const (
	defaultReleaseAPI        = "https://api.github.com/repos/uvwt/agentdock/releases/latest"
	maxReleaseArchiveBytes   = 256 << 20
	maxDesktopArchiveBytes   = 512 << 20
	maxExtractedPayloadBytes = 64 << 20
	macOSDesktopArchiveName  = "AgentDock-macos-universal.zip"
	coreSkillBundlePrefix    = "share/agentdock/core-skills/"
)

type release struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type CheckResult struct {
	CurrentVersion         string `json:"current_version"`
	LatestVersion          string `json:"latest_version"`
	DesktopCurrentVersion  string `json:"desktop_current_version,omitempty"`
	UpdateAvailable        bool   `json:"update_available"`
	DesktopUpdateAvailable bool   `json:"desktop_update_available,omitempty"`
	Message                string `json:"message"`
}

type updateInspection struct {
	Result               CheckResult
	ArchiveName          string
	ExecutableName       string
	ArchiveAsset         releaseAsset
	ChecksumAsset        releaseAsset
	DesktopArchiveAsset  releaseAsset
	DesktopChecksumAsset releaseAsset
}

type options struct {
	CurrentVersion        string
	ExecutablePath        string
	DesktopTargetPath     string
	DesktopCurrentVersion string
	DesktopOnly           bool
	GOOS                  string
	GOARCH                string
	ReleaseAPI            string
	HTTPClient            *http.Client
	Output                io.Writer
	Progress              updateProgressReporter
	Apply                 func(context.Context, applyRequest) (applyResult, error)
	VerifyBinary          func(context.Context, string, string) error
	ExtractDesktop        func(context.Context, []byte, string, string) (string, error)
}

type applyRequest struct {
	CurrentPath       string
	CurrentVersion    string
	StagedPath        string
	BundlePath        string
	DesktopTargetPath string
	DesktopStagedPath string
	DesktopOnly       bool
	TargetVersion     string
	Output            io.Writer
	Progress          updateProgressReporter
}

type applyResult struct {
	Restarted bool
	HandedOff bool
}

func Run(ctx context.Context, output io.Writer) error {
	opts, err := runtimeOptions(output)
	if err != nil {
		return err
	}
	return run(ctx, opts)
}

func RunWithProgress(ctx context.Context, output io.Writer, progressOutput io.Writer) error {
	opts, err := runtimeOptions(output)
	if err != nil {
		return err
	}
	if progressOutput == nil {
		progressOutput = io.Discard
	}
	opts.Progress = newJSONProgressReporter(progressOutput)
	reportUpdateStage(opts.Progress, UpdateStageChecking, opts.CurrentVersion, "", "")
	if err := run(ctx, opts); err != nil {
		opts.Progress(UpdateProgressEvent{
			Type:           "failed",
			CurrentVersion: normalizeVersion(opts.CurrentVersion),
			Error:          err.Error(),
		})
		return err
	}
	return nil
}

func Check(ctx context.Context) (CheckResult, error) {
	opts, err := runtimeOptions(io.Discard)
	if err != nil {
		return CheckResult{}, err
	}
	inspection, err := inspectUpdate(ctx, opts)
	if err != nil {
		return CheckResult{}, err
	}
	return inspection.Result, nil
}

func runtimeOptions(output io.Writer) (options, error) {
	executable, err := os.Executable()
	if err != nil {
		return options{}, fmt.Errorf("定位当前 AgentDock 二进制失败: %w", err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
		executable = resolved
	}
	desktopTarget := detectDesktopUpdateTarget()
	return options{
		CurrentVersion:        buildinfo.Version,
		ExecutablePath:        executable,
		DesktopTargetPath:     desktopTarget,
		DesktopCurrentVersion: desktopUpdateVersion(desktopTarget),
		DesktopOnly:           desktopUpdateOwnsExecutable(desktopTarget, executable),
		GOOS:                  runtime.GOOS,
		GOARCH:                runtime.GOARCH,
		ReleaseAPI:            defaultReleaseAPI,
		HTTPClient:            &http.Client{Timeout: 5 * time.Minute},
		Output:                output,
		Apply:                 applyPlatformUpdate,
		VerifyBinary:          verifyBinaryVersion,
		ExtractDesktop:        extractDesktopUpdateArchive,
	}, nil
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
	if opts.DesktopTargetPath != "" && opts.ExtractDesktop == nil {
		return errors.New("桌面更新解压器未配置")
	}

	inspection, err := inspectUpdate(ctx, opts)
	if err != nil {
		return err
	}
	if !inspection.Result.UpdateAvailable {
		fmt.Fprintln(opts.Output, inspection.Result.Message)
		if opts.Progress != nil {
			opts.Progress(UpdateProgressEvent{
				Type:           "completed",
				CurrentVersion: normalizeVersion(inspection.Result.CurrentVersion),
				TargetVersion:  normalizeVersion(inspection.Result.LatestVersion),
			})
		}
		return nil
	}
	if opts.DesktopOnly || (inspection.Result.DesktopUpdateAvailable && normalizeVersion(inspection.Result.CurrentVersion) == normalizeVersion(inspection.Result.LatestVersion)) {
		return runDesktopOnlyUpdate(ctx, opts, inspection)
	}

	currentVersion := inspection.Result.CurrentVersion
	targetVersion := inspection.Result.LatestVersion
	archiveName := inspection.ArchiveName
	executableName := inspection.ExecutableName
	archiveAsset := inspection.ArchiveAsset
	checksumAsset := inspection.ChecksumAsset

	fmt.Fprintf(opts.Output, "当前版本：%s\n最新版本：%s\n\n", currentVersion, targetVersion)
	fmt.Fprintf(opts.Output, "正在下载 %s...\n", archiveName)
	reportUpdateStage(opts.Progress, UpdateStageDownloading, currentVersion, targetVersion, archiveName)
	archiveData, err := downloadWithProgress(ctx, opts.HTTPClient, archiveAsset.URL, maxReleaseArchiveBytes, func(bytesRead, totalBytes int64) {
		reportDownloadProgress(opts.Progress, currentVersion, targetVersion, archiveName, bytesRead, totalBytes)
	})
	if err != nil {
		return fmt.Errorf("下载更新文件失败: %w", err)
	}
	reportUpdateStage(opts.Progress, UpdateStageVerifying, currentVersion, targetVersion, archiveName)
	checksumData, err := download(ctx, opts.HTTPClient, checksumAsset.URL, 1<<20)
	if err != nil {
		return fmt.Errorf("下载校验文件失败: %w", err)
	}
	if err := verifyChecksum(archiveData, checksumData); err != nil {
		return fmt.Errorf("更新文件校验失败，当前版本未被修改: %w", err)
	}
	fmt.Fprintln(opts.Output, "文件校验通过")

	tempDir, err := os.MkdirTemp("", "agentdock-update-*")
	if err != nil {
		return fmt.Errorf("创建更新临时目录失败: %w", err)
	}
	defer os.RemoveAll(tempDir)

	reportUpdateStage(opts.Progress, UpdateStageExtracting, currentVersion, targetVersion, archiveName)
	binaryData, err := extractExecutable(archiveData, opts.GOOS, executableName)
	if err != nil {
		return fmt.Errorf("解压更新文件失败: %w", err)
	}
	stagedPath := filepath.Join(tempDir, executableName)
	if err := os.WriteFile(stagedPath, binaryData, 0o755); err != nil {
		return fmt.Errorf("写入新版本二进制失败: %w", err)
	}
	bundlePath, err := extractCoreSkillBundle(archiveData, opts.GOOS, tempDir)
	if err != nil {
		return fmt.Errorf("解压核心 Skill Bundle 失败: %w", err)
	}
	if err := opts.VerifyBinary(ctx, stagedPath, targetVersion); err != nil {
		return fmt.Errorf("新版本二进制验证失败，当前版本未被修改: %w", err)
	}

	desktopStagedPath := ""
	if inspection.DesktopArchiveAsset.Name != "" {
		desktopArchiveData := archiveData
		if inspection.DesktopArchiveAsset.Name != archiveAsset.Name || inspection.DesktopArchiveAsset.URL != archiveAsset.URL {
			fmt.Fprintf(opts.Output, "正在下载 %s...\n", inspection.DesktopArchiveAsset.Name)
			reportUpdateStage(opts.Progress, UpdateStageDownloading, currentVersion, targetVersion, inspection.DesktopArchiveAsset.Name)
			var downloadErr error
			desktopArchiveData, downloadErr = downloadWithProgress(ctx, opts.HTTPClient, inspection.DesktopArchiveAsset.URL, maxDesktopArchiveBytes, func(bytesRead, totalBytes int64) {
				reportDownloadProgress(opts.Progress, currentVersion, targetVersion, inspection.DesktopArchiveAsset.Name, bytesRead, totalBytes)
			})
			if downloadErr != nil {
				return fmt.Errorf("下载桌面组件更新文件失败: %w", downloadErr)
			}
			reportUpdateStage(opts.Progress, UpdateStageVerifying, currentVersion, targetVersion, inspection.DesktopArchiveAsset.Name)
			desktopChecksumData, checksumErr := download(ctx, opts.HTTPClient, inspection.DesktopChecksumAsset.URL, 1<<20)
			if checksumErr != nil {
				return fmt.Errorf("下载桌面组件校验文件失败: %w", checksumErr)
			}
			if checksumErr := verifyChecksum(desktopArchiveData, desktopChecksumData); checksumErr != nil {
				return fmt.Errorf("桌面组件更新文件校验失败，当前版本未被修改: %w", checksumErr)
			}
		}
		reportUpdateStage(opts.Progress, UpdateStageExtracting, currentVersion, targetVersion, inspection.DesktopArchiveAsset.Name)
		desktopStagedPath, err = opts.ExtractDesktop(ctx, desktopArchiveData, tempDir, targetVersion)
		if err != nil {
			return fmt.Errorf("解压桌面组件更新文件失败: %w", err)
		}
		fmt.Fprintln(opts.Output, "桌面组件更新文件校验通过")
	}

	fmt.Fprintln(opts.Output, "正在备份并安装新版本...")
	result, err := opts.Apply(ctx, applyRequest{
		CurrentPath:       opts.ExecutablePath,
		CurrentVersion:    currentVersion,
		StagedPath:        stagedPath,
		BundlePath:        bundlePath,
		DesktopTargetPath: opts.DesktopTargetPath,
		DesktopStagedPath: desktopStagedPath,
		TargetVersion:     targetVersion,
		Output:            opts.Output,
		Progress:          opts.Progress,
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
	if opts.Progress != nil {
		opts.Progress(UpdateProgressEvent{
			Type:           "completed",
			CurrentVersion: normalizeVersion(targetVersion),
			TargetVersion:  normalizeVersion(targetVersion),
		})
	}
	return nil
}

func runDesktopOnlyUpdate(ctx context.Context, opts options, inspection updateInspection) error {
	targetVersion := inspection.Result.LatestVersion
	asset := inspection.DesktopArchiveAsset
	checksumAsset := inspection.DesktopChecksumAsset
	if asset.Name == "" || checksumAsset.Name == "" {
		return errors.New("桌面组件更新缺少 Release 资源")
	}

	fmt.Fprintf(opts.Output, "当前版本：%s\n最新版本：%s\n\n", inspection.Result.CurrentVersion, targetVersion)
	fmt.Fprintf(opts.Output, "正在下载 %s...\n", asset.Name)
	reportUpdateStage(opts.Progress, UpdateStageDownloading, inspection.Result.CurrentVersion, targetVersion, asset.Name)
	archiveData, err := downloadWithProgress(ctx, opts.HTTPClient, asset.URL, maxDesktopArchiveBytes, func(bytesRead, totalBytes int64) {
		reportDownloadProgress(opts.Progress, inspection.Result.CurrentVersion, targetVersion, asset.Name, bytesRead, totalBytes)
	})
	if err != nil {
		return fmt.Errorf("下载桌面组件更新文件失败: %w", err)
	}
	reportUpdateStage(opts.Progress, UpdateStageVerifying, inspection.Result.CurrentVersion, targetVersion, asset.Name)
	checksumData, err := download(ctx, opts.HTTPClient, checksumAsset.URL, 1<<20)
	if err != nil {
		return fmt.Errorf("下载桌面组件校验文件失败: %w", err)
	}
	if err := verifyChecksum(archiveData, checksumData); err != nil {
		return fmt.Errorf("桌面组件更新文件校验失败，当前版本未被修改: %w", err)
	}

	tempDir, err := os.MkdirTemp("", "agentdock-desktop-update-*")
	if err != nil {
		return fmt.Errorf("创建桌面组件更新临时目录失败: %w", err)
	}
	defer os.RemoveAll(tempDir)
	reportUpdateStage(opts.Progress, UpdateStageExtracting, inspection.Result.CurrentVersion, targetVersion, asset.Name)
	stagedDesktop, err := opts.ExtractDesktop(ctx, archiveData, tempDir, targetVersion)
	if err != nil {
		return fmt.Errorf("解压桌面组件更新文件失败: %w", err)
	}
	fmt.Fprintln(opts.Output, "桌面组件校验通过")

	_, err = opts.Apply(ctx, applyRequest{
		CurrentPath:       opts.ExecutablePath,
		CurrentVersion:    inspection.Result.CurrentVersion,
		DesktopTargetPath: opts.DesktopTargetPath,
		DesktopStagedPath: stagedDesktop,
		DesktopOnly:       true,
		TargetVersion:     targetVersion,
		Output:            opts.Output,
		Progress:          opts.Progress,
	})
	if err == nil && opts.Progress != nil {
		opts.Progress(UpdateProgressEvent{
			Type:           "completed",
			CurrentVersion: normalizeVersion(targetVersion),
			TargetVersion:  normalizeVersion(targetVersion),
		})
	}
	return err
}

func inspectUpdate(ctx context.Context, opts options) (updateInspection, error) {
	if opts.HTTPClient == nil {
		return updateInspection{}, errors.New("更新 HTTP 客户端不能为空")
	}
	latest, err := fetchLatestRelease(ctx, opts.HTTPClient, opts.ReleaseAPI)
	if err != nil {
		return updateInspection{}, err
	}

	currentVersion := normalizeVersion(opts.CurrentVersion)
	targetVersion := normalizeVersion(latest.TagName)
	if currentVersion == "vdev" || currentVersion == "" {
		return updateInspection{}, errors.New("当前是开发构建，无法通过 agentdock update 判断可升级版本")
	}

	desktopVersion := normalizeVersion(opts.DesktopCurrentVersion)
	desktopNeedsUpdate := false
	if strings.TrimSpace(opts.DesktopTargetPath) != "" {
		comparison, comparable := compareVersions(desktopVersion, targetVersion)
		desktopNeedsUpdate = desktopVersion == "" || !comparable || comparison < 0
	}

	result := CheckResult{
		CurrentVersion:         currentVersion,
		LatestVersion:          targetVersion,
		DesktopCurrentVersion:  desktopVersion,
		DesktopUpdateAvailable: desktopNeedsUpdate,
	}
	if comparison, comparable := compareVersions(currentVersion, targetVersion); comparable && comparison > 0 {
		result.Message = fmt.Sprintf("当前版本 %s 高于最新 Release %s，不执行降级。", currentVersion, targetVersion)
		return updateInspection{Result: result}, nil
	}
	if currentVersion == targetVersion && !desktopNeedsUpdate {
		result.Message = fmt.Sprintf("当前已是最新版本：%s", targetVersion)
		return updateInspection{Result: result}, nil
	}

	if opts.DesktopOnly {
		desktopArchiveAsset, ok := findAsset(latest.Assets, macOSDesktopArchiveName)
		if !ok {
			return updateInspection{}, fmt.Errorf("Release %s 缺少 macOS 桌面更新文件 %s", targetVersion, macOSDesktopArchiveName)
		}
		desktopChecksumAsset, ok := findAsset(latest.Assets, macOSDesktopArchiveName+".sha256")
		if !ok {
			return updateInspection{}, fmt.Errorf("Release %s 缺少校验文件 %s.sha256", targetVersion, macOSDesktopArchiveName)
		}
		result.UpdateAvailable = true
		result.DesktopUpdateAvailable = true
		result.Message = fmt.Sprintf("发现 AgentDock App 更新：%s → %s", currentVersion, targetVersion)
		return updateInspection{
			Result:               result,
			DesktopArchiveAsset:  desktopArchiveAsset,
			DesktopChecksumAsset: desktopChecksumAsset,
		}, nil
	}

	archiveName, executableName, err := platformAssetNames(opts.GOOS, opts.GOARCH)
	if err != nil {
		return updateInspection{}, err
	}
	archiveAsset, ok := findAsset(latest.Assets, archiveName)
	if !ok {
		return updateInspection{}, fmt.Errorf("Release %s 缺少当前平台文件 %s", targetVersion, archiveName)
	}
	checksumAsset, ok := findAsset(latest.Assets, archiveName+".sha256")
	if !ok {
		return updateInspection{}, fmt.Errorf("Release %s 缺少校验文件 %s.sha256", targetVersion, archiveName)
	}

	desktopArchiveAsset := releaseAsset{}
	desktopChecksumAsset := releaseAsset{}
	if desktopNeedsUpdate {
		switch opts.GOOS {
		case "darwin":
			desktopArchiveAsset, ok = findAsset(latest.Assets, macOSDesktopArchiveName)
			if !ok {
				return updateInspection{}, fmt.Errorf("Release %s 缺少 macOS 桌面更新文件 %s", targetVersion, macOSDesktopArchiveName)
			}
			desktopChecksumAsset, ok = findAsset(latest.Assets, macOSDesktopArchiveName+".sha256")
			if !ok {
				return updateInspection{}, fmt.Errorf("Release %s 缺少校验文件 %s.sha256", targetVersion, macOSDesktopArchiveName)
			}
		case "windows":
			// Windows 控制面板与核心位于同一个 Release ZIP；复用已经校验过的归档，
			// 避免同一次升级重复下载数百 MB 文件。
			desktopArchiveAsset = archiveAsset
			desktopChecksumAsset = checksumAsset
		}
	}

	result.UpdateAvailable = true
	if currentVersion == targetVersion && desktopNeedsUpdate {
		result.Message = fmt.Sprintf("发现控制面板更新：%s → %s", desktopVersion, targetVersion)
	} else {
		result.Message = fmt.Sprintf("发现新版本：%s → %s", currentVersion, targetVersion)
	}
	return updateInspection{
		Result:               result,
		ArchiveName:          archiveName,
		ExecutableName:       executableName,
		ArchiveAsset:         archiveAsset,
		ChecksumAsset:        checksumAsset,
		DesktopArchiveAsset:  desktopArchiveAsset,
		DesktopChecksumAsset: desktopChecksumAsset,
	}, nil
}

func fetchLatestRelease(ctx context.Context, client *http.Client, endpoint string) (release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return release{}, fmt.Errorf("创建 Release 请求失败: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "agentdock/"+buildinfo.Version)
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
	return downloadWithProgress(ctx, client, rawURL, limit, nil)
}

func downloadWithProgress(
	ctx context.Context,
	client *http.Client,
	rawURL string,
	limit int64,
	onProgress func(bytesRead int64, totalBytes int64),
) ([]byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return nil, fmt.Errorf("无效下载地址 %q", rawURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "agentdock/"+buildinfo.Version)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	totalBytes := resp.ContentLength
	if totalBytes <= 0 {
		totalBytes = -1
	} else if totalBytes > limit {
		return nil, fmt.Errorf("下载内容超过 %d 字节限制", limit)
	}
	if onProgress != nil {
		onProgress(0, totalBytes)
	}

	reader := io.LimitReader(resp.Body, limit+1)
	var buffer bytes.Buffer
	chunk := make([]byte, 64*1024)
	var bytesRead int64
	var lastReported int64 = -1
	lastReportAt := time.Now()
	for {
		n, readErr := reader.Read(chunk)
		if n > 0 {
			bytesRead += int64(n)
			if bytesRead > limit {
				return nil, fmt.Errorf("下载内容超过 %d 字节限制", limit)
			}
			if _, err := buffer.Write(chunk[:n]); err != nil {
				return nil, err
			}
			if onProgress != nil && time.Since(lastReportAt) >= 100*time.Millisecond {
				onProgress(bytesRead, totalBytes)
				lastReported = bytesRead
				lastReportAt = time.Now()
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return nil, readErr
			}
			break
		}
	}
	if onProgress != nil && lastReported != bytesRead {
		onProgress(bytesRead, totalBytes)
	}
	return buffer.Bytes(), nil
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
			return readLimited(opened, maxExtractedPayloadBytes)
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
		return readLimited(tarReader, maxExtractedPayloadBytes)
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

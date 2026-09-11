package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// RunLocalArchive lets Windows Setup hand an already verified/downloaded Release
// artifact to the same generation Update Engine used by online self-update.
// The legacy layout never calls this entrypoint; it is only valid once stable
// shims + active-version.json have established generation ownership.
func RunLocalArchive(ctx context.Context, archivePath, checksumPath, targetVersion string, output io.Writer) error {
	if runtime.GOOS != "windows" {
		return errors.New("local Release archive update is currently supported only on Windows")
	}
	opts, err := runtimeOptions(output)
	if err != nil {
		return err
	}
	if opts.Output == nil {
		opts.Output = io.Discard
	}
	currentVersion := normalizeVersion(opts.CurrentVersion)
	targetVersion = normalizeVersion(targetVersion)
	if currentVersion == "" || currentVersion == "vdev" || targetVersion == "" {
		return errors.New("local Release archive update requires released source and target versions")
	}
	comparison, comparable := compareVersions(currentVersion, targetVersion)
	if comparable && comparison >= 0 {
		return fmt.Errorf("local Release archive target %s must be newer than current %s", targetVersion, currentVersion)
	}

	archiveData, err := readBoundedLocalFile(archivePath, maxDesktopArchiveBytes)
	if err != nil {
		return fmt.Errorf("read local update archive: %w", err)
	}
	checksumData, err := readBoundedLocalFile(checksumPath, 1<<20)
	if err != nil {
		return fmt.Errorf("read local update checksum: %w", err)
	}
	if err := verifyChecksum(archiveData, checksumData); err != nil {
		return fmt.Errorf("local update archive checksum failed: %w", err)
	}
	_, executableName, err := platformAssetNames(opts.GOOS, opts.GOARCH)
	if err != nil {
		return err
	}
	tempDir, err := os.MkdirTemp("", "agentdock-local-update-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	binaryData, err := extractExecutable(archiveData, opts.GOOS, executableName)
	if err != nil {
		return fmt.Errorf("extract local update core: %w", err)
	}
	stagedPath := filepath.Join(tempDir, executableName)
	if err := os.WriteFile(stagedPath, binaryData, 0o755); err != nil {
		return fmt.Errorf("stage local update core: %w", err)
	}
	bundlePath, err := extractCoreSkillBundle(archiveData, opts.GOOS, tempDir)
	if err != nil {
		return fmt.Errorf("extract local core Skill Bundle: %w", err)
	}
	if err := opts.VerifyBinary(ctx, stagedPath, targetVersion); err != nil {
		return fmt.Errorf("verify local update core: %w", err)
	}
	desktopStagedPath, err := opts.ExtractDesktop(ctx, archiveData, tempDir, targetVersion)
	if err != nil {
		return fmt.Errorf("extract local desktop payload: %w", err)
	}

	fmt.Fprintf(opts.Output, "使用本地 Release 归档更新：%s → %s\n", currentVersion, targetVersion)
	result, err := opts.Apply(ctx, applyRequest{
		CurrentPath:       opts.ExecutablePath,
		CurrentVersion:    currentVersion,
		StagedPath:        stagedPath,
		BundlePath:        bundlePath,
		DesktopTargetPath: opts.DesktopTargetPath,
		DesktopStagedPath: desktopStagedPath,
		TargetVersion:     targetVersion,
		Output:            opts.Output,
	})
	if err != nil {
		return err
	}
	if result.HandedOff {
		return errors.New("generation local archive update unexpectedly handed off to legacy updater")
	}
	fmt.Fprintf(opts.Output, "本地 Release 归档更新已提交：%s\n", targetVersion)
	return nil
}

func readBoundedLocalFile(path string, maxBytes int64) ([]byte, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("local update file path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maxBytes {
		return nil, fmt.Errorf("local update file size is invalid: %d", info.Size())
	}
	return os.ReadFile(path)
}

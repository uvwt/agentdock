package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// prepareGitHubRelease 从已经通过完整 Release gate 的 candidate 中挑选面向人工下载的资产。
// updater、bootstrap、component metadata 和机器 sidecar checksum 仍完整发布到 R2，
// GitHub Release 只展示用户真正需要选择的安装包，并用一个 SHA256SUMS.txt 统一人工校验入口。
func prepareGitHubRelease(distDir, outputDir string, stdout io.Writer) error {
	distAbs, err := filepath.Abs(distDir)
	if err != nil {
		return fmt.Errorf("解析 dist 目录失败: %w", err)
	}
	outputAbs, err := filepath.Abs(outputDir)
	if err != nil {
		return fmt.Errorf("解析 GitHub Release 输出目录失败: %w", err)
	}
	if distAbs == outputAbs {
		return fmt.Errorf("GitHub Release 输出目录不能与 dist 目录相同")
	}

	if err := os.RemoveAll(outputAbs); err != nil {
		return fmt.Errorf("清理 GitHub Release 输出目录失败: %w", err)
	}
	if err := os.MkdirAll(outputAbs, 0o755); err != nil {
		return fmt.Errorf("创建 GitHub Release 输出目录失败: %w", err)
	}

	checksums, err := os.Create(filepath.Join(outputAbs, "SHA256SUMS.txt"))
	if err != nil {
		return fmt.Errorf("创建 SHA256SUMS.txt 失败: %w", err)
	}

	count := 0
	for _, artifact := range ReleaseCatalog() {
		if !artifact.GitHubRelease {
			continue
		}

		source := filepath.Join(distAbs, artifact.Name)
		info, err := os.Stat(source)
		if err != nil {
			checksums.Close()
			return fmt.Errorf("检查 GitHub Release 资产 %s 失败: %w", artifact.Name, err)
		}
		if !info.Mode().IsRegular() {
			checksums.Close()
			return fmt.Errorf("GitHub Release 资产不是普通文件：%s", artifact.Name)
		}

		target := filepath.Join(outputAbs, artifact.Name)
		if err := copyReleaseFile(source, target, info.Mode().Perm()); err != nil {
			checksums.Close()
			return fmt.Errorf("复制 GitHub Release 资产 %s 失败: %w", artifact.Name, err)
		}

		sum, err := fileSHA256(source)
		if err != nil {
			checksums.Close()
			return fmt.Errorf("计算 GitHub Release 资产 %s SHA-256 失败: %w", artifact.Name, err)
		}
		if _, err := fmt.Fprintf(checksums, "%s  %s\n", sum, artifact.Name); err != nil {
			checksums.Close()
			return fmt.Errorf("写入 SHA256SUMS.txt 失败: %w", err)
		}
		count++
	}
	if count == 0 {
		checksums.Close()
		return fmt.Errorf("GitHub Release 没有配置任何公开资产")
	}
	if err := checksums.Close(); err != nil {
		return fmt.Errorf("关闭 SHA256SUMS.txt 失败: %w", err)
	}

	fmt.Fprintf(stdout, "GitHub Release prepared: %d user-facing assets + SHA256SUMS.txt\n", count)
	return nil
}

func copyReleaseFile(source, target string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()

	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		return err
	}
	return output.Close()
}

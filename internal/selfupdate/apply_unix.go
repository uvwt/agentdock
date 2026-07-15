//go:build !windows

package selfupdate

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

func applyPlatformUpdate(ctx context.Context, request applyRequest) (applyResult, error) {
	service := detectManagedService(ctx, request.CurrentPath)
	candidates := uniqueStrings(append(platformHealthCandidates(ctx, request.CurrentPath), healthCandidates(request.CurrentPath)...))
	if healthyURL := findHealthyURL(ctx, candidates); healthyURL != "" {
		candidates = []string{healthyURL}
	}

	backupPath := request.CurrentPath + ".backup"
	newPath := request.CurrentPath + ".new"
	if err := installReplacement(request.StagedPath, request.CurrentPath, newPath, backupPath); err != nil {
		return applyResult{}, fmt.Errorf("安装新版本失败，当前版本未被修改: %w", err)
	}
	rollback := func(cause error) error {
		if err := restoreBackup(request.CurrentPath, backupPath); err != nil {
			return fmt.Errorf("%v；自动恢复旧版本失败: %w", cause, err)
		}
		if service != nil {
			if err := service.Restart(ctx); err != nil {
				return fmt.Errorf("%v；旧版本已恢复，但重新启动 %s 失败: %w", cause, service.Name(), err)
			}
			if err := waitForVersion(ctx, candidates, request.CurrentVersion, 30*time.Second); err != nil {
				return fmt.Errorf("%v；旧版本已恢复并重启，但健康检查失败: %w", cause, err)
			}
		}
		return fmt.Errorf("%v；已自动恢复旧版本", cause)
	}

	if err := signLocalReplacement(ctx, request.CurrentPath); err != nil {
		return applyResult{}, rollback(fmt.Errorf("新版本本地签名失败: %w", err))
	}
	if err := verifyBinaryVersion(ctx, request.CurrentPath, request.TargetVersion); err != nil {
		return applyResult{}, rollback(fmt.Errorf("替换后的二进制验证失败: %w", err))
	}
	if service == nil {
		return applyResult{}, nil
	}

	fmt.Fprintf(request.Output, "正在重启 %s...\n", service.Name())
	if err := service.Restart(ctx); err != nil {
		return applyResult{}, rollback(fmt.Errorf("重启 %s 失败: %w", service.Name(), err))
	}
	if err := waitForVersion(ctx, candidates, request.TargetVersion, 30*time.Second); err != nil {
		return applyResult{}, rollback(fmt.Errorf("新版本健康检查失败: %w", err))
	}
	fmt.Fprintln(request.Output, "健康检查通过")
	return applyResult{Restarted: true}, nil
}

func installReplacement(stagedPath, targetPath, newPath, backupPath string) error {
	info, err := os.Stat(targetPath)
	if err != nil {
		return fmt.Errorf("读取当前二进制失败: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("当前二进制不是普通文件: %s", targetPath)
	}
	if err := os.Remove(newPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("清理临时文件失败: %w", err)
	}
	if err := copyFile(stagedPath, newPath, info.Mode().Perm()); err != nil {
		return err
	}
	if err := os.Remove(backupPath); err != nil && !os.IsNotExist(err) {
		_ = os.Remove(newPath)
		return fmt.Errorf("清理旧备份失败: %w", err)
	}
	if err := copyFile(targetPath, backupPath, info.Mode().Perm()); err != nil {
		_ = os.Remove(newPath)
		return fmt.Errorf("备份当前二进制失败: %w", err)
	}

	// 新文件先落在目标同目录，再用 rename 原子覆盖；即使替换瞬间崩溃，目标路径也不会消失。
	if err := os.Rename(newPath, targetPath); err != nil {
		_ = os.Remove(newPath)
		return fmt.Errorf("原子替换新二进制失败: %w", err)
	}
	if directory, err := os.Open(filepath.Dir(targetPath)); err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}

func copyFile(sourcePath, targetPath string, mode os.FileMode) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("打开新版本二进制失败: %w", err)
	}
	defer source.Close()
	target, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("创建同目录更新文件失败: %w", err)
	}
	copied := false
	defer func() {
		_ = target.Close()
		if !copied {
			_ = os.Remove(targetPath)
		}
	}()
	if _, err := io.Copy(target, source); err != nil {
		return fmt.Errorf("复制新版本二进制失败: %w", err)
	}
	if err := target.Sync(); err != nil {
		return fmt.Errorf("同步新版本二进制失败: %w", err)
	}
	if err := target.Close(); err != nil {
		return fmt.Errorf("关闭新版本二进制失败: %w", err)
	}
	copied = true
	return nil
}

func restoreBackup(targetPath, backupPath string) error {
	return os.Rename(backupPath, targetPath)
}

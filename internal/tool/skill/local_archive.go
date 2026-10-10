package skill

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	skills "github.com/uvwt/agentdock/internal/skill"
)

// ManagedPackageRoot 返回已安装受管 Skill 的包目录。
// 只按受管名称查找，不接受调用方传入的文件系统路径。
func (s *Service) ManagedPackageRoot(name string) (string, error) {
	path, err := s.state.SkillPath(name)
	if err != nil {
		return "", toolErrorCause("SKILL_NAME_INVALID", "managed Skill name is invalid", "validation", map[string]any{"skill": name}, err)
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", toolErrorDetails("SKILL_NOT_FOUND", "managed Skill is not installed", "not_found", map[string]any{"skill": name})
	}
	if err != nil {
		return "", toolErrorCause("SKILL_PACKAGE_INVALID", "managed Skill path cannot be inspected", "runtime", map[string]any{"skill": name}, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", toolErrorDetails("SKILL_PACKAGE_INVALID", "managed Skill path is not a regular directory", "validation", map[string]any{"skill": name})
	}
	return path, nil
}

// ValidateLocalArchive 用现有 Skill 安装器预检设备暂存的 ZIP，不写入受管目录。
func (s *Service) ValidateLocalArchive(ctx context.Context, archive, digest string) (skills.ValidateResult, error) {
	if err := requireDeviceArchive(archive); err != nil {
		return skills.ValidateResult{}, err
	}
	return s.manager.Validate(ctx, skills.ValidateRequest{Source: archive, DigestSHA256: digest})
}

// InstallLocalArchive 把已经通过预检的 ZIP 交给现有 Skill 安装器。
// 摘要校验、包结构校验和同内容 no-op 都留在 Manager.Install。
func (s *Service) InstallLocalArchive(ctx context.Context, archive, digest string) (skills.InstallResult, error) {
	if err := requireDeviceArchive(archive); err != nil {
		return skills.InstallResult{}, err
	}
	return s.manager.Install(ctx, skills.InstallRequest{Source: archive, DigestSHA256: digest})
}

func requireDeviceArchive(archive string) error {
	archive = strings.TrimSpace(archive)
	if archive == "" || strings.Contains(archive, "://") {
		return toolErrorDetails("VALIDATION_ERROR", "device archive must be a local ZIP path", "validation", map[string]any{"field": "source"})
	}
	absolute, err := filepath.Abs(archive)
	if err != nil {
		return toolErrorCause("SKILL_SOURCE_INVALID", "Skill archive path is invalid", "validation", nil, err)
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return toolErrorCause("SKILL_SOURCE_INVALID", "Skill archive cannot be inspected", "validation", nil, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || !strings.EqualFold(filepath.Ext(absolute), ".zip") {
		return toolErrorDetails("VALIDATION_ERROR", "device archive must be a regular ZIP file", "validation", map[string]any{"field": "source"})
	}
	return nil
}

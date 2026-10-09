package plugin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	pluginruntime "github.com/uvwt/agentdock/internal/plugin"
	toolcore "github.com/uvwt/agentdock/internal/tool/core"
)

// InstallReviewedArchive 安装设备已经暂存的候选 ZIP。
// 资源库下载的包不在用户工作区，不能走 plugin_manage 的 sourcePath；
// 审核令牌、激活和版本冲突仍使用与普通安装相同的 installResolved。
func (s *Service) InstallReviewedArchive(ctx context.Context, archive string, enabled bool, reviewToken string) (Result, error) {
	if err := requireDeviceArchive(archive); err != nil {
		return nil, err
	}
	return s.installResolved(ctx, archive, enabled, reviewToken)
}

// UpdateReviewedArchive 升级设备已经暂存的候选 ZIP，确认语义与普通 update 相同。
func (s *Service) UpdateReviewedArchive(ctx context.Context, archive string, reviewToken string) (Result, error) {
	if err := requireDeviceArchive(archive); err != nil {
		return nil, err
	}
	return s.updateResolved(ctx, archive, reviewToken)
}

// InstalledPackage 读取已安装 Plugin 的受管快照。资源库只按名称导出，不接受外部路径。
func (s *Service) InstalledPackage(name string) (pluginruntime.Installed, error) {
	return s.manager.Inspect(name)
}

// ValidateLocalArchive 只产生原生审核报告，不安装。
func (s *Service) ValidateLocalArchive(ctx context.Context, archive string) (pluginruntime.Review, error) {
	if err := requireDeviceArchive(archive); err != nil {
		return pluginruntime.Review{}, err
	}
	return s.manager.ValidateSource(ctx, archive), nil
}

func (s *Service) installResolved(ctx context.Context, source string, enabled bool, reviewToken string) (Result, error) {
	result, err := s.manager.InstallReviewedSource(ctx, source, enabled, reviewToken)
	if err != nil {
		return nil, pluginToolError(err)
	}
	if !result.Changed {
		if err := s.ReconcileMCP(); err != nil {
			return nil, toolcore.NewErrorCause(
				"PLUGIN_RUNTIME_ACTIVATION_FAILED",
				"Plugin runtime reconciliation failed",
				"runtime",
				map[string]any{"plugin_name": result.Name},
				err,
			)
		}
		return changeResult(result), nil
	}
	if err := s.reconcileMCPActivation(result.Name); err != nil {
		abortErr := s.manager.AbortActivation(ctx, result.Name)
		reconcileErr := s.ReconcileMCP()
		return nil, toolcore.NewErrorCause(
			"PLUGIN_RUNTIME_ACTIVATION_FAILED",
			"Plugin install candidate could not activate its runtime; installation was aborted",
			"runtime",
			map[string]any{"plugin_name": result.Name},
			errors.Join(err, abortErr, reconcileErr),
		)
	}
	if err := s.manager.FinalizeActivation(result.Name); err != nil {
		deactivateErr := s.reconcileMCPExcluding(result.Name)
		abortErr := s.manager.AbortActivation(ctx, result.Name)
		reconcileErr := s.ReconcileMCP()
		return nil, toolcore.NewErrorCause(
			"PLUGIN_INSTALL_FINALIZE_FAILED",
			"Plugin runtime activated but durable install finalization failed; installation was aborted",
			"runtime",
			map[string]any{"plugin_name": result.Name, "version": result.Version},
			errors.Join(err, deactivateErr, abortErr, reconcileErr),
		)
	}
	return changeResult(result), nil
}

func (s *Service) updateResolved(ctx context.Context, source string, reviewToken string) (Result, error) {
	var previous pluginruntime.State
	deactivated := false
	var deactivationErr error
	result, err := s.manager.UpdateReviewedSource(ctx, source, reviewToken, func(state pluginruntime.State) error {
		previous = state
		if !state.Enabled {
			return nil
		}
		if reconcileErr := s.reconcileMCPExcluding(state.Name); reconcileErr != nil {
			deactivationErr = toolcore.NewErrorCause(
				"PLUGIN_RUNTIME_DEACTIVATION_FAILED",
				"could not stop the current Plugin MCP runtime before update",
				"runtime",
				map[string]any{"plugin_name": state.Name, "previous_version": state.Version},
				reconcileErr,
			)
			return deactivationErr
		}
		deactivated = true
		return nil
	})
	if err != nil {
		if deactivationErr != nil {
			return nil, deactivationErr
		}
		if deactivated {
			reconcileErr := s.ReconcileMCP()
			if reconcileErr != nil {
				return nil, toolcore.NewErrorCause(
					"PLUGIN_UPDATE_FAILED",
					"Plugin update failed and the previous MCP runtime could not be fully restored",
					"runtime",
					map[string]any{"plugin_name": previous.Name, "previous_version": previous.Version},
					errors.Join(err, reconcileErr),
				)
			}
		}
		return nil, pluginToolError(err)
	}
	if err := s.reconcileMCPActivation(result.Name); err != nil {
		restoreErr := s.manager.AbortActivation(ctx, result.Name)
		reconcileErr := s.ReconcileMCP()
		return nil, toolcore.NewErrorCause(
			"PLUGIN_RUNTIME_ACTIVATION_FAILED",
			"Plugin update could not activate its runtime; the previous Plugin state was restored",
			"runtime",
			map[string]any{"plugin_name": previous.Name, "previous_version": previous.Version},
			errors.Join(err, restoreErr, reconcileErr),
		)
	}
	if err := s.manager.FinalizeActivation(result.Name); err != nil {
		deactivateErr := s.reconcileMCPExcluding(result.Name)
		restoreErr := s.manager.AbortActivation(ctx, result.Name)
		reconcileErr := s.ReconcileMCP()
		return nil, toolcore.NewErrorCause(
			"PLUGIN_UPDATE_FINALIZE_FAILED",
			"Plugin runtime activated but durable update finalization failed; the candidate was rolled back",
			"runtime",
			map[string]any{"plugin_name": result.Name, "version": result.Version},
			errors.Join(err, deactivateErr, restoreErr, reconcileErr),
		)
	}
	return changeResult(result), nil
}

// requireDeviceArchive 只接受设备暂存的普通 ZIP。
// 这里故意不调用工作区解析：调用方必须已经把候选包放进 AgentDock 自己的临时目录。
func requireDeviceArchive(archive string) error {
	archive = strings.TrimSpace(archive)
	if archive == "" || strings.Contains(archive, "://") {
		return validationError("device archive must be a local ZIP path", "source")
	}
	absolute, err := filepath.Abs(archive)
	if err != nil {
		return toolcore.NewErrorCause("PLUGIN_SOURCE_INVALID", "Plugin archive path is invalid", "validation", nil, err)
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return toolcore.NewErrorCause("PLUGIN_SOURCE_INVALID", "Plugin archive cannot be inspected", "validation", nil, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || !strings.EqualFold(filepath.Ext(absolute), ".zip") {
		return validationError("device archive must be a regular ZIP file", "source")
	}
	return nil
}

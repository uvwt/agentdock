package startupdiag

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/logfile"
)

const (
	startupLogMaxBytes int64 = 1 << 20
	startupLogMaxFiles       = 2
)

// Log 记录一个启动阶段的完成时间。阶段日志只包含耗时和非敏感定位信息，
// 用于把 Windows Task Host、Core 初始化和 HTTP 监听串成一条可比较的时间线。
func Log(logger *slog.Logger, component, stage string, startedAt time.Time, attrs ...slog.Attr) {
	if logger == nil {
		return
	}
	fields := []slog.Attr{
		slog.String("component", component),
		slog.String("stage", stage),
		slog.Int64("duration_ms", time.Since(startedAt).Milliseconds()),
		slog.Int("pid", os.Getpid()),
	}
	fields = append(fields, attrs...)
	logger.LogAttrs(context.Background(), slog.LevelInfo, "startup stage", fields...)
}

// Append 把稳定 Task Host 的早期阶段写入独立 startup.log。这里每次写完即关闭，
// 避免长期持有 Core 的 agentdock.err.log，也不会让诊断日志失败阻断真实启动链路。
func Append(runtimeRoot, component, stage string, startedAt time.Time, attrs ...slog.Attr) error {
	runtimeRoot = strings.TrimSpace(runtimeRoot)
	if runtimeRoot == "" {
		return nil
	}
	writer, err := logfile.Open(
		filepath.Join(runtimeRoot, "logs", "startup.log"),
		startupLogMaxBytes,
		startupLogMaxFiles,
	)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(writer, nil))
	Log(logger, component, stage, startedAt, attrs...)
	return writer.Close()
}

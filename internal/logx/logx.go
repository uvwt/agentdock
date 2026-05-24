package logx

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

func Setup(level string) {
	var slogLevel slog.Level
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		slogLevel = slog.LevelDebug
	case "warn", "warning":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}
	// 容器排障优先：默认输出 JSON 到 stderr，docker logs / compose logs 可直接收集。
	// 不写固定日志文件，避免容器重建后路径和权限问题；需要持久化时交给容器运行时采集。
	handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slogLevel})
	slog.SetDefault(slog.New(handler))
}

func Info(msg string, attrs ...any)  { slog.Info(msg, attrs...) }
func Debug(msg string, attrs ...any) { slog.Debug(msg, attrs...) }
func Warn(msg string, attrs ...any)  { slog.Warn(msg, attrs...) }
func Error(msg string, attrs ...any) { slog.Error(msg, attrs...) }

func InfoContext(ctx context.Context, msg string, attrs ...any) { slog.InfoContext(ctx, msg, attrs...) }
func DebugContext(ctx context.Context, msg string, attrs ...any) {
	slog.DebugContext(ctx, msg, attrs...)
}
func WarnContext(ctx context.Context, msg string, attrs ...any) { slog.WarnContext(ctx, msg, attrs...) }
func ErrorContext(ctx context.Context, msg string, attrs ...any) {
	slog.ErrorContext(ctx, msg, attrs...)
}

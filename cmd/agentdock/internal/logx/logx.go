package logx

import (
	"io"
	"log/slog"
	"strings"
)

func Setup(level string, output io.Writer) {
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
	// 输出目标由启动链路决定：普通进程继续走 stderr；桌面后台进程可传入轮转文件。
	handler := slog.NewJSONHandler(output, &slog.HandlerOptions{Level: slogLevel})
	slog.SetDefault(slog.New(handler))
}

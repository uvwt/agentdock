package desktopruntime

import (
	"errors"
	"io"
	"os"
)

type tunnelLogCursor struct {
	size int64
}

func captureTunnelLogCursor(path string) (tunnelLogCursor, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return tunnelLogCursor{}, nil
	}
	if err != nil {
		return tunnelLogCursor{}, err
	}
	return tunnelLogCursor{size: info.Size()}, nil
}

func readTunnelLogSince(path string, cursor tunnelLogCursor) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	offset := cursor.size
	// 轮转或截断后 active log 会比启动前更短，此时新一代日志从文件开头读取。
	if info.Size() < offset {
		offset = 0
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(file)
}

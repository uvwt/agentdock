package logfile

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const (
	// DefaultMaxBytes 将单个日志控制在 10 MiB，兼顾日常排障和轮转频率。
	DefaultMaxBytes int64 = 10 << 20
	// DefaultMaxFiles 包含当前日志和编号备份，总占用约不超过 50 MiB。
	DefaultMaxFiles = 5
)

// Writer 按文件大小轮转日志；当前文件使用原路径，历史文件依次为 .1 到 .(maxFiles-1)。
type Writer struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	maxFiles int
	file     *os.File
	size     int64
}

func Open(path string, maxBytes int64, maxFiles int) (*Writer, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("日志单文件上限必须大于 0")
	}
	if maxFiles < 1 {
		return nil, fmt.Errorf("日志保留文件数必须至少为 1")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("创建日志目录失败: %w", err)
	}

	writer := &Writer{path: path, maxBytes: maxBytes, maxFiles: maxFiles}
	if err := writer.openActive(); err != nil {
		return nil, err
	}
	if writer.size >= writer.maxBytes {
		if err := writer.rotate(); err != nil {
			_ = writer.file.Close()
			return nil, err
		}
	}
	return writer, nil
}

func OpenDefault(path string) (*Writer, error) {
	return Open(path, DefaultMaxBytes, DefaultMaxFiles)
}

func (writer *Writer) Write(data []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()

	written := 0
	for len(data) > 0 {
		if writer.file == nil {
			if err := writer.openActive(); err != nil {
				return written, err
			}
		}
		if writer.size >= writer.maxBytes {
			if err := writer.rotate(); err != nil {
				return written, err
			}
		}

		remaining := writer.maxBytes - writer.size
		chunk := data
		if int64(len(chunk)) > remaining {
			chunk = chunk[:remaining]
		}
		n, err := writer.file.Write(chunk)
		written += n
		writer.size += int64(n)
		data = data[n:]
		if err != nil {
			return written, err
		}
		if n == 0 {
			return written, io.ErrShortWrite
		}
	}
	return written, nil
}

func (writer *Writer) Close() error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.file == nil {
		return nil
	}
	err := writer.file.Close()
	writer.file = nil
	return err
}

func (writer *Writer) openActive() error {
	file, err := os.OpenFile(writer.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("打开日志文件失败 %s: %w", writer.path, err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("设置日志权限失败 %s: %w", writer.path, err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("读取日志大小失败 %s: %w", writer.path, err)
	}
	writer.file = file
	writer.size = info.Size()
	return nil
}

func (writer *Writer) rotate() error {
	if writer.file != nil {
		if err := writer.file.Close(); err != nil {
			return fmt.Errorf("关闭待轮转日志失败 %s: %w", writer.path, err)
		}
		writer.file = nil
	}
	if writer.size > writer.maxBytes {
		if err := keepFileTail(writer.path, writer.maxBytes); err != nil {
			return err
		}
	}

	if writer.maxFiles == 1 {
		if err := os.Remove(writer.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("清理旧日志失败 %s: %w", writer.path, err)
		}
	} else {
		oldest := fmt.Sprintf("%s.%d", writer.path, writer.maxFiles-1)
		if err := os.Remove(oldest); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("删除最旧日志失败 %s: %w", oldest, err)
		}
		for generation := writer.maxFiles - 2; generation >= 1; generation-- {
			from := fmt.Sprintf("%s.%d", writer.path, generation)
			to := fmt.Sprintf("%s.%d", writer.path, generation+1)
			if err := os.Rename(from, to); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("轮转日志失败 %s -> %s: %w", from, to, err)
			}
		}
		if err := os.Rename(writer.path, writer.path+".1"); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("轮转当前日志失败 %s: %w", writer.path, err)
		}
	}
	return writer.openActive()
}

func keepFileTail(path string, maxBytes int64) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("打开超限日志失败 %s: %w", path, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("读取超限日志大小失败 %s: %w", path, err)
	}
	if info.Size() <= maxBytes {
		return nil
	}
	if maxBytes > int64(^uint(0)>>1) {
		return fmt.Errorf("日志单文件上限过大: %d", maxBytes)
	}

	// 旧版本可能已经留下超限文件。升级首次接管时保留最新尾部，而不是把超限文件
	// 原样变成备份，确保当前文件和所有轮转文件立即满足同一上限。
	tail := make([]byte, int(maxBytes))
	if _, err := file.ReadAt(tail, info.Size()-maxBytes); err != nil {
		return fmt.Errorf("读取超限日志尾部失败 %s: %w", path, err)
	}
	if _, err := file.WriteAt(tail, 0); err != nil {
		return fmt.Errorf("压缩超限日志失败 %s: %w", path, err)
	}
	if err := file.Truncate(maxBytes); err != nil {
		return fmt.Errorf("截断超限日志失败 %s: %w", path, err)
	}
	return nil
}

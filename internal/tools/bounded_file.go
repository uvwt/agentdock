package tools

import (
	"io"
	"os"
)

type boundedFileRead struct {
	Info     os.FileInfo
	Data     []byte
	Size     int64
	TooLarge bool
}

func readBoundedFile(path string, maxBytes int64) (boundedFileRead, error) {
	file, err := os.Open(path)
	if err != nil {
		return boundedFileRead{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return boundedFileRead{}, err
	}
	result := boundedFileRead{Info: info, Size: info.Size()}
	if info.Size() > maxBytes {
		result.TooLarge = true
		return result, nil
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return boundedFileRead{}, err
	}
	result.Data = data
	if int64(len(data)) > result.Size {
		result.Size = int64(len(data))
	}
	result.TooLarge = int64(len(data)) > maxBytes
	if result.TooLarge {
		result.Data = nil
	}
	return result, nil
}

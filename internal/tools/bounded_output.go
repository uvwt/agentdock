package tools

import (
	"bytes"
	"os/exec"
	"sync"
)

type boundedCommandOutput struct {
	mu        sync.Mutex
	buffer    bytes.Buffer
	limit     int
	total     int64
	truncated bool
}

func newBoundedCommandOutput(limit int) *boundedCommandOutput {
	return &boundedCommandOutput{limit: limit}
}

func (w *boundedCommandOutput) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	original := len(data)
	w.total += int64(original)
	remaining := w.limit - w.buffer.Len()
	if remaining <= 0 {
		w.truncated = w.truncated || original > 0
		return original, nil
	}
	if len(data) > remaining {
		data = data[:remaining]
		w.truncated = true
	}
	_, _ = w.buffer.Write(data)
	return original, nil
}

func (w *boundedCommandOutput) Snapshot() ([]byte, int64, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.buffer.Bytes()...), w.total, w.truncated
}

func runBoundedCombinedOutput(cmd *exec.Cmd, limit int) ([]byte, int64, bool, error) {
	output := newBoundedCommandOutput(limit)
	cmd.Stdout = output
	cmd.Stderr = output
	err := cmd.Run()
	data, total, truncated := output.Snapshot()
	return data, total, truncated, err
}

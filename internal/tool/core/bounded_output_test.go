package core

import (
	"bytes"
	"os/exec"
	"runtime"
	"sync"
	"testing"
)

func TestBoundedCommandOutputCountsAndCapsConcurrentWrites(t *testing.T) {
	const (
		writers = 32
		chunk   = 4096
		limit   = 8192
	)
	output := NewBoundedOutput(limit)
	var wg sync.WaitGroup
	wg.Add(writers)
	for range writers {
		go func() {
			defer wg.Done()
			if n, err := output.Write(bytes.Repeat([]byte("x"), chunk)); err != nil || n != chunk {
				t.Errorf("Write() = %d, %v", n, err)
			}
		}()
	}
	wg.Wait()
	data, total, truncated := output.Snapshot()
	if len(data) != limit || total != writers*chunk || !truncated {
		t.Fatalf("snapshot = len:%d total:%d truncated:%v", len(data), total, truncated)
	}
}

func TestRunBoundedCombinedOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test command uses POSIX shell syntax")
	}
	cmd := exec.Command("/bin/sh", "-c", "printf 'stdout'; printf 'stderr' >&2; printf '%01000d' 0")
	data, total, truncated, err := RunBoundedCombinedOutput(cmd, 16)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 16 || total != int64(len("stdout")+len("stderr")+1000) || !truncated {
		t.Fatalf("output = len:%d total:%d truncated:%v data:%q", len(data), total, truncated, data)
	}
}

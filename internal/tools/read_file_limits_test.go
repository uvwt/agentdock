package tools

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFileRejectsOversizedInputBeforeReading(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	path := filepath.Join(root, "oversized.txt")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxTextFileReadBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = rt.readFile(map[string]any{"path": "oversized.txt"})
	var toolErr *ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected ToolError, got %T: %v", err, err)
	}
	if toolErr.Code != "FILE_TOO_LARGE" || toolErr.Details["max_size_bytes"] != maxTextFileReadBytes {
		t.Fatalf("unexpected error: %#v", toolErr)
	}
}

func TestReadFileBoundsRequestedOutput(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	content := strings.Repeat("line content\n", 30000)
	if err := os.WriteFile(filepath.Join(root, "large.txt"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, maxBytes := range []int{-1, 0, maxTextOutputBytes + 1} {
		result, err := rt.readFile(map[string]any{"path": "large.txt", "max_bytes": maxBytes})
		if err != nil {
			t.Fatal(err)
		}
		got := len(result["content"].(string))
		limit := 262144
		if maxBytes > maxTextOutputBytes {
			limit = maxTextOutputBytes
		}
		if got > limit {
			t.Fatalf("max_bytes=%d returned %d bytes, want <= %d", maxBytes, got, limit)
		}
	}
}

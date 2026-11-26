package file

import (
	"context"
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

	_, err = rt.readFileTest(context.Background(), ReadRequest{Path: "oversized.txt"})
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
		result, err := rt.readFileTest(context.Background(), ReadRequest{Path: "large.txt", MaxBytes: &maxBytes})
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

func TestReadFileOmitsEncodingAndRejectsNonUTF8(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	if err := os.WriteFile(filepath.Join(root, "utf8.txt"), []byte("hello\nworld\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := rt.readFileTest(context.Background(), ReadRequest{Path: "utf8.txt"})
	if err != nil {
		t.Fatalf("read_file utf8 error = %v", err)
	}
	if _, exists := result["encoding"]; exists {
		t.Fatalf("read_file result should omit constant encoding field: %#v", result)
	}
	if result["content"] != "hello\nworld\n" || result["size_bytes"] != 12 {
		t.Fatalf("unexpected read_file result: %#v", result)
	}

	schema, ok := OutputSchema(ToolReadFile)
	if !ok {
		t.Fatal("missing OutputSchema for read_file")
	}
	props, _ := schema["properties"].(map[string]any)
	if _, exists := props["encoding"]; exists {
		t.Fatalf("read_file OutputSchema should not declare removed encoding property: %#v", props)
	}

	if err := os.WriteFile(filepath.Join(root, "invalid.txt"), []byte{0xff, 0xfe, 'a'}, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = rt.readFileTest(context.Background(), ReadRequest{Path: "invalid.txt"})
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "ENCODING_UNSUPPORTED" {
		t.Fatalf("non-utf8 read_file error = %#v, want ENCODING_UNSUPPORTED", err)
	}
}

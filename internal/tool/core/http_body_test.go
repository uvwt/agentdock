package core

import (
	"errors"
	"strings"
	"testing"
)

func TestReadBoundedBody(t *testing.T) {
	data, err := ReadBoundedBody(strings.NewReader("exact"), 5)
	if err != nil {
		t.Fatalf("ReadBoundedBody() error = %v", err)
	}
	if string(data) != "exact" {
		t.Fatalf("data = %q", data)
	}
	if _, err := ReadBoundedBody(strings.NewReader("oversized"), 5); err == nil || !strings.Contains(err.Error(), "exceeds 5 bytes") {
		t.Fatalf("oversized error = %v", err)
	}
	if _, err := ReadBoundedBody(strings.NewReader("data"), 0); err == nil {
		t.Fatal("ReadBoundedBody() accepted zero limit")
	}
	if _, err := ReadBoundedBody(failingReader{}, 5); err == nil || !strings.Contains(err.Error(), "read response body") {
		t.Fatalf("reader error = %v", err)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

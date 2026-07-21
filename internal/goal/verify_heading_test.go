package goal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGrepHeadingFileScaleGate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.md")
	if err := os.WriteFile(path, []byte("# title\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	expr := "grep -q '^#' '" + path + "'"
	ok, detail, done := matchFileScaleExpression(expr)
	if !done || !ok {
		t.Fatalf("ok=%v done=%v detail=%s", ok, done, detail)
	}
	// no heading
	if err := os.WriteFile(path, []byte("no heading\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, _, done = matchFileScaleExpression(expr)
	if !done || ok {
		t.Fatalf("expected fail, ok=%v", ok)
	}
}

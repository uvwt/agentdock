package publicartifacts

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestPublishDirectoryPreservesSymlinksWithoutFollowingThem(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "project")
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "target.txt"), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(root, "external")
	if err := os.MkdirAll(external, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(external, "outside.txt"), []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	links := map[string]string{
		"nested/file-link": "../target.txt",
		"directory-link":   "../external",
	}
	for name, target := range links {
		if err := os.Symlink(filepath.FromSlash(target), filepath.Join(source, filepath.FromSlash(name))); err != nil {
			t.Skipf("symlink creation unavailable on this platform: %v", err)
		}
	}
	store := New(filepath.Join(root, "home"), "", 0)
	published, err := store.Publish(PublishRequest{Path: source})
	if err != nil {
		t.Fatal(err)
	}
	_, data, err := store.Read(published.ArtifactID, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	headers := map[string]tar.Header{}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		headers[header.Name] = *header
	}
	for name, target := range links {
		header, ok := headers["project/"+name]
		if !ok || header.Typeflag != tar.TypeSymlink || header.Linkname != filepath.FromSlash(target) {
			t.Errorf("archived link %s = %+v, exists = %v; want target %q", name, header, ok, target)
		}
	}
	if _, followed := headers["project/directory-link/outside.txt"]; followed {
		t.Fatal("archive followed a directory symlink outside the source")
	}
	if _, ok := headers["project/target.txt"]; !ok {
		t.Fatal("archive omitted the ordinary source file")
	}
}

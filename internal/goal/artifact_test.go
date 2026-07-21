package goal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestArtifactStoreDedupAndURI(t *testing.T) {
	store, err := NewArtifactStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	meta1, err := store.PutBytes("log.txt", []byte("hello evidence"), "log", "test log", "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	if meta1.SHA256 == "" || meta1.URI == "" || meta1.Size != 14 {
		t.Fatalf("meta1=%#v", meta1)
	}
	meta2, err := store.PutBytes("other.txt", []byte("hello evidence"), "log", "dup", "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	if meta1.SHA256 != meta2.SHA256 || meta1.URI != meta2.URI {
		t.Fatalf("dedup failed: %#v vs %#v", meta1, meta2)
	}
	got, err := store.GetMeta(meta1.URI)
	if err != nil {
		t.Fatal(err)
	}
	if got.SHA256 != meta1.SHA256 {
		t.Fatalf("get meta: %#v", got)
	}
	f, _, err := store.Open(meta1.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	buf := make([]byte, 32)
	n, _ := f.Read(buf)
	if string(buf[:n]) != "hello evidence" {
		t.Fatalf("blob=%q", buf[:n])
	}

	path := filepath.Join(t.TempDir(), "file.bin")
	if err := os.WriteFile(path, []byte("from-file"), 0o600); err != nil {
		t.Fatal(err)
	}
	meta3, err := store.PutFile(path, "file", "from disk")
	if err != nil {
		t.Fatal(err)
	}
	if meta3.Size != 9 {
		t.Fatalf("file meta=%#v", meta3)
	}
	ev := EvidenceFromMeta(meta3, "crit_1", map[string]any{"exit_code": 0})
	if ev.URI != meta3.URI || ev.Data["criterion_id"] != "crit_1" {
		t.Fatalf("evidence=%#v", ev)
	}
}

package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreRejectsTrailingRegistryJSON(t *testing.T) {
	store := newStore(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(store.path), 0o700); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"version":1,"servers":[]} {"version":1,"servers":[]}`)
	if err := os.WriteFile(store.path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.load(); err == nil {
		t.Fatal("registry with trailing JSON was accepted")
	}
}

func TestStoreRejectsOversizedRegistryBeforeDecode(t *testing.T) {
	store := newStore(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(store.path), 0o700); err != nil {
		t.Fatal(err)
	}
	data := `{"version":1,"servers":[]}` + strings.Repeat(" ", (1<<20)+1)
	if err := os.WriteFile(store.path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.load(); err == nil {
		t.Fatal("oversized registry was accepted")
	}
}

package envstore

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func TestStoreRoundTripPermissionsAndUnset(t *testing.T) {
	home := t.TempDir()
	store, err := New(home)
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{Kind: ScopeSkill, Name: "demo-skill"}
	values := map[string]string{
		"EMPTY":  "",
		"PLAIN":  "value",
		"QUOTED": "line 1\nline '2' with $HOME and \\ slash",
	}
	for key, value := range values {
		if err := store.Set(scope, key, value); err != nil {
			t.Fatalf("Set(%s): %v", key, err)
		}
	}

	loaded, err := store.Load(scope)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, values) {
		t.Fatalf("round trip mismatch\nwant: %#v\n got: %#v", values, loaded)
	}

	for _, dir := range []string{store.Root(), filepath.Join(store.Root(), "skill"), filepath.Join(store.Root(), "mcp")} {
		assertMode(t, dir, 0o700)
	}
	path, err := store.Path(scope)
	if err != nil {
		t.Fatal(err)
	}
	assertMode(t, path, 0o600)

	items, err := store.List(scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || items[0].Key != "EMPTY" || items[0].Configured || !items[1].Configured || !items[2].Configured {
		t.Fatalf("unexpected env list: %#v", items)
	}

	for key := range values {
		removed, err := store.Unset(scope, key)
		if err != nil {
			t.Fatalf("Unset(%s): %v", key, err)
		}
		if !removed {
			t.Fatalf("Unset(%s) did not report removal", key)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("empty env file still exists: %v", err)
	}
}

func TestStoreConcurrentSetDoesNotLoseValues(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{Kind: ScopeMCP, Name: "demo"}

	var wg sync.WaitGroup
	for index := 0; index < 64; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			key := fmt.Sprintf("KEY_%02d", index)
			if err := store.Set(scope, key, fmt.Sprintf("value-%02d", index)); err != nil {
				t.Errorf("Set(%s): %v", key, err)
			}
		}(index)
	}
	wg.Wait()

	values, err := store.Load(scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 64 {
		t.Fatalf("concurrent updates lost data: got %d keys", len(values))
	}
}

func TestStoreRejectsInvalidScopeAndKey(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []Scope{
		{Kind: ScopeSkill, Name: "../escape"},
		{Kind: ScopeMCP, Name: "nested/name"},
		{Kind: ScopeKind("other"), Name: "demo"},
	} {
		if err := store.Set(scope, "KEY", "value"); err == nil {
			t.Fatalf("invalid scope accepted: %#v", scope)
		}
	}
	if err := store.Set(Scope{Kind: ScopeSkill, Name: "demo"}, "BAD-NAME", "value"); err == nil {
		t.Fatal("invalid environment key accepted")
	}
}

func TestParserAcceptsShellEnvSyntax(t *testing.T) {
	input := []byte("# comment\nexport A=plain\nB='single quoted'\nC=double\\ value\nD=hash#literal\nE=value # comment\n")
	values, err := parse(input)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"A": "plain",
		"B": "single quoted",
		"C": "double value",
		"D": "hash#literal",
		"E": "value",
	}
	if !reflect.DeepEqual(values, want) {
		t.Fatalf("parsed values mismatch\nwant: %#v\n got: %#v", want, values)
	}
}

func assertMode(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != mode {
		t.Fatalf("%s mode = %04o, want %04o", path, got, mode)
	}
}

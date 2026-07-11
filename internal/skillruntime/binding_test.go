package skillruntime

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestBindingStoreLoadsYAMLDefaultBinding(t *testing.T) {
	store, err := NewBindingStore(filepath.Join(t.TempDir(), "bindings"))
	if err != nil {
		t.Fatalf("NewBindingStore() error = %v", err)
	}
	data := []byte(`
default: production
bindings:
  production:
    env:
      BASE_URL: https://api.example
    secrets:
      API_TOKEN: secret-token
  staging:
    env:
      BASE_URL: https://staging.example
`)
	if err := os.WriteFile(store.Path("demo-skill"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	binding, err := store.Load("demo-skill", "")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if binding.Name != "production" || binding.Env["BASE_URL"] != "https://api.example" || binding.Secrets["API_TOKEN"] != "secret-token" {
		t.Fatalf("binding = %#v", binding)
	}
}

func TestBindingStoreLoadsJSONAndExplicitSelection(t *testing.T) {
	store, err := NewBindingStore(filepath.Join(t.TempDir(), "bindings"))
	if err != nil {
		t.Fatalf("NewBindingStore() error = %v", err)
	}
	data := []byte(`{"default":"first","bindings":{"first":{"env":{"NAME":"one"}},"second":{"env":{"NAME":"two"}}}}`)
	if err := os.WriteFile(store.Path("demo-skill"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	binding, err := store.Load("demo-skill", "second")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if binding.Name != "second" || binding.Env["NAME"] != "two" {
		t.Fatalf("binding = %#v", binding)
	}
	if binding.Secrets == nil {
		t.Fatal("Secrets map is nil")
	}
}

func TestBindingStoreSelectsOnlyBinding(t *testing.T) {
	store, err := NewBindingStore(filepath.Join(t.TempDir(), "bindings"))
	if err != nil {
		t.Fatalf("NewBindingStore() error = %v", err)
	}
	if err := os.WriteFile(store.Path("demo-skill"), []byte("bindings:\n  only:\n    env:\n      NAME: value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	binding, err := store.Load("demo-skill", "")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if binding.Name != "only" {
		t.Fatalf("binding name = %q, want only", binding.Name)
	}
}

func TestBindingStoreRejectsInvalidInputs(t *testing.T) {
	if _, err := NewBindingStore("  "); err == nil {
		t.Fatal("NewBindingStore() accepted empty root")
	}
	store, err := NewBindingStore(filepath.Join(t.TempDir(), "bindings"))
	if err != nil {
		t.Fatalf("NewBindingStore() error = %v", err)
	}
	if _, err := store.Load("../escape", ""); err == nil {
		t.Fatal("Load() accepted invalid skill name")
	} else {
		var runtimeErr *Error
		if !errors.As(err, &runtimeErr) || runtimeErr.Code != ErrBindingInvalid {
			t.Fatalf("Load() error = %v", err)
		}
	}

	cases := []struct {
		name string
		data string
	}{
		{name: "missing bindings", data: "default: one\n"},
		{name: "ambiguous selection", data: "bindings:\n  one: {}\n  two: {}\n"},
		{name: "unknown selection", data: "bindings:\n  one: {}\n"},
		{name: "wrong env type", data: "bindings:\n  one:\n    env:\n      PORT: 123\n"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(store.Path("demo-skill"), []byte(test.data), 0o600); err != nil {
				t.Fatal(err)
			}
			selected := ""
			if test.name == "unknown selection" {
				selected = "missing"
			}
			if _, err := store.Load("demo-skill", selected); err == nil {
				t.Fatal("Load() error = nil, want failure")
			}
		})
	}
}

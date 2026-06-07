package commandqueue

import "testing"

func TestRegisterAdaptersIncludesDefaultCommandSet(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	executor := NewExecutor(store)

	if err := RegisterAdapters(executor, AdapterDependencies{}); err != nil {
		t.Fatalf("register adapters: %v", err)
	}
}

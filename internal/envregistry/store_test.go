package envregistry

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func TestStoreSetInspectRedactsValues(t *testing.T) {
	root := t.TempDir()
	store, err := New(root, func() []Definition {
		return []Definition{{Skill: "weread-skills", Name: "WEREAD_API_KEY", Kind: KindSecret, Source: "manifest"}}
	})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := store.Set("weread-skills", "WEREAD_API_KEY", KindSecret, "wrk-secret-value")
	if err != nil {
		t.Fatal(err)
	}
	if !entry.Configured || entry.Length != len("wrk-secret-value") || entry.SHA256Prefix == "" {
		t.Fatalf("unexpected entry: %#v", entry)
	}
	if entry.SHA256Prefix == "wrk-secret-value" {
		t.Fatal("secret value leaked into sha prefix")
	}
	valuesPath := filepath.Join(root, "values", "weread-skills.json")
	info, err := os.Stat(valuesPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); runtime.GOOS != "windows" && got != 0o600 {
		t.Fatalf("values mode = %o, want 600", got)
	}
	items, err := store.Inspect("weread-skills")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || !items[0].Configured || items[0].Length == 0 {
		t.Fatalf("unexpected inspect result: %#v", items)
	}
}

func TestConcurrentSetPreservesEveryVariable(t *testing.T) {
	store, err := New(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	const workers = 32
	start := make(chan struct{})
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for worker := range workers {
		go func() {
			defer wg.Done()
			<-start
			name := fmt.Sprintf("VALUE_%02d", worker)
			_, err := store.Set("demo-skill", name, KindPlain, name)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Set() error = %v", err)
		}
	}

	entries, err := store.Inspect("demo-skill")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != workers {
		t.Fatalf("configured variables = %d, want %d", len(entries), workers)
	}
	for _, entry := range entries {
		if !entry.Configured {
			t.Fatalf("entry is not configured: %#v", entry)
		}
	}
}

func TestEnvForSkillTreatsProcessSecretAsRedactionValue(t *testing.T) {
	root := t.TempDir()
	store, err := New(root, func() []Definition {
		return []Definition{{Skill: "demo-skill", Name: "DEMO_API_TOKEN", Kind: KindSecret, Source: "manifest"}}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEMO_API_TOKEN", "process-secret-value")

	env, secrets, err := store.EnvForSkill("demo-skill", store.KnownDefinitions("demo-skill"))
	if err != nil {
		t.Fatal(err)
	}
	if env["DEMO_API_TOKEN"] != "process-secret-value" {
		t.Fatalf("env DEMO_API_TOKEN = %q", env["DEMO_API_TOKEN"])
	}
	if len(secrets) != 1 || secrets[0] != "process-secret-value" {
		t.Fatalf("secrets = %#v, want process secret", secrets)
	}
}

package installer

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/updateengine"
)

func withStubCommands(t *testing.T, scripts map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range scripts {
		path := filepath.Join(dir, name)
		if runtime.GOOS == "windows" {
			path += ".cmd"
			if !strings.HasPrefix(strings.TrimSpace(body), "@") {
				body = "@echo off\r\n" + body
			}
		} else if !strings.HasPrefix(body, "#!") {
			body = "#!/bin/sh\n" + body
		}
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func writeUnixPayload(t *testing.T, root, name, body string) string {
	t.Helper()
	dir := filepath.Join(root, name, "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agentdock"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(dir)
}

func unixInstallRequest(installRoot, runtimeRoot, payload, version string) Request {
	return Request{
		InstallRoot:    installRoot,
		RuntimeRoot:    runtimeRoot,
		PayloadDir:     payload,
		Version:        version,
		SkipHealth:     true,
		StartService:   false,
		SkipSkills:     true,
		ServiceManager: "none",
	}
}

func writeWindowsTrialPointer(t *testing.T, installRoot, version, transactionID string) {
	t.Helper()
	store, err := updateengine.NewStore(installRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WriteActive(updateengine.ActiveVersion{
		SchemaVersion: updateengine.SchemaVersion,
		ActiveVersion: version,
		State:         updateengine.StateTrial,
		TransactionID: transactionID,
	}); err != nil {
		t.Fatal(err)
	}
}

func readInstallStore(t *testing.T, runtimeRoot string) (*Store, Transaction, Result) {
	t.Helper()
	store, err := NewStore(runtimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := store.ReadTransaction()
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.ReadCurrentResult()
	if err != nil {
		result = Result{}
	}
	return store, tx, result
}

func journalFile(runtimeRoot, transactionID string) string {
	return filepath.Join(runtimeRoot, "install", "rollback", transactionID, "journal.json")
}

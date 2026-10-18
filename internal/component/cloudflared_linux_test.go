//go:build linux

package component

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallRepairsLinuxComponentExecutablePermissions(t *testing.T) {
	stubPlatformTrust(t)
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(store.root, 0o700); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(mustTempDir(t, store.root, ".staging-linux-permissions-*"), binaryFilename("linux"))
	writeFakeCloudflared(t, staged, "2026.9.3", "permissions")
	digest := mustSHA256(t, staged)
	if err := store.commitStaged(context.Background(), staged, "2026.9.3", digest); err != nil {
		t.Fatal(err)
	}

	paths := []string{
		filepath.Join(root, "components"),
		store.root,
		filepath.Join(store.root, "versions"),
		filepath.Join(store.root, "versions", "2026.9.3"),
		store.binaryPath("2026.9.3"),
	}
	for _, path := range paths {
		if err := os.Chmod(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Install(context.Background(), InstallOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o755 {
			t.Fatalf("mode %s = %o, want 755", path, got)
		}
	}
}

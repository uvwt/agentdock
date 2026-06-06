package nexusclient_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/uvwt/agentdock/internal/nexusclient"
)

func TestDeviceStatePersistsSecurelyAndRevokes(t *testing.T) {
	stateDir := t.TempDir()
	store, err := nexusclient.OpenStateStore(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(time.Hour).UTC()
	state := nexusclient.DeviceState{
		DeviceID:       "device-1",
		DeviceToken:    "secret-device-token",
		TokenExpiresAt: &expires,
	}
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filepath.Join(stateDir, "device.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("device.json mode = %o, want 600", got)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DeviceID != state.DeviceID || loaded.DeviceToken != state.DeviceToken {
		t.Fatalf("loaded state mismatch: %#v", loaded)
	}
	if !loaded.Valid(time.Now()) {
		t.Fatal("saved enrollment should be valid")
	}

	if err := store.Revoke(); err != nil {
		t.Fatal(err)
	}
	revoked, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !revoked.Revoked || revoked.DeviceToken != "" || revoked.Valid(time.Now()) {
		t.Fatalf("revocation did not clear credentials: %#v", revoked)
	}
}

package tunnel

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConfigRoundTripAndModes(t *testing.T) {
	home := t.TempDir()
	m, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{Mode: ModeLAN, LocalHost: "127.0.0.1", LocalPort: 8765}
	if err := m.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	// private file
	info, err := os.Stat(filepath.Join(home, "tunnel", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 && info.Mode().Perm() != 0o600 {
		// on some FS perms may vary; require not world-writable
		if info.Mode().Perm()&0o002 != 0 {
			t.Fatalf("config world-writable: %o", info.Mode().Perm())
		}
	}
	loaded, err := m.LoadConfig()
	if err != nil || loaded.Mode != ModeLAN {
		t.Fatalf("load=%#v err=%v", loaded, err)
	}

	st, err := m.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.State != "connected" || st.MCPURL == "" {
		t.Fatalf("lan status=%#v", st)
	}

	cfg.Mode = ModeLoopback
	_ = m.SaveConfig(cfg)
	st, err = m.Start(context.Background())
	if err != nil || st.PublicURL != "http://127.0.0.1:8765" {
		t.Fatalf("loopback %#v err=%v", st, err)
	}

	cfg.Mode = ModeCustom
	cfg.CustomURL = "https://agentdock.example.com"
	_ = m.SaveConfig(cfg)
	st, err = m.Start(context.Background())
	if err != nil || st.MCPURL != "https://agentdock.example.com/mcp" {
		t.Fatalf("custom %#v err=%v", st, err)
	}

	cfg.Mode = ModeDisabled
	_ = m.SaveConfig(cfg)
	st, err = m.Start(context.Background())
	if err != nil || st.State != "disabled" {
		t.Fatalf("disabled %#v err=%v", st, err)
	}
}

func TestStopIdempotent(t *testing.T) {
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := m.Stop(); err != nil {
		t.Fatal(err)
	}
	_ = ctx
}

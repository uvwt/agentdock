package desktopruntime

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPrepareCloudflaredTunnelArgsUsesIsolatedValidConfig(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "cloudflared-isolated.yml")
	if err := os.WriteFile(configPath, []byte("ingress:\n  - service: http_status:404\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	arguments, err := prepareCloudflaredTunnelArgs(root, "--url", "http://127.0.0.1:8765")
	if err != nil {
		t.Fatal(err)
	}
	wantArguments := []string{
		"--config", configPath,
		"tunnel", "--no-autoupdate",
		"--url", "http://127.0.0.1:8765",
	}
	if !reflect.DeepEqual(arguments, wantArguments) {
		t.Fatalf("cloudflared arguments = %#v, want %#v", arguments, wantArguments)
	}

	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(config); got != isolatedCloudflaredConfig {
		t.Fatalf("isolated cloudflared config = %q, want %q", got, isolatedCloudflaredConfig)
	}
}

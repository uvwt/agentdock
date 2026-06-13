package artifactrelay

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type sourceFetchClient struct{ result FetchResultRequest }

func (c *sourceFetchClient) UploadArtifactFetch(context.Context, DeviceCredentials, string, string, string, FetchManifest) (FetchJob, error) {
	return FetchJob{ID: "fet_test", Status: FetchReady}, nil
}
func (c *sourceFetchClient) ReportArtifactFetchResult(_ context.Context, _ DeviceCredentials, _ string, _ string, request FetchResultRequest) error {
	c.result = request
	return nil
}

func TestSourceFetcherDenyRulesAndSymlink(t *testing.T) {
	root := t.TempDir()
	allowed, denied := filepath.Join(root, "allowed"), filepath.Join(root, "denied")
	if err := os.MkdirAll(allowed, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(denied, 0o700); err != nil {
		t.Fatal(err)
	}
	safe := filepath.Join(allowed, "safe.txt")
	secret := filepath.Join(denied, "secret.txt")
	if err := os.WriteFile(safe, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(allowed, "link.txt")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatal(err)
	}
	fetcher, err := NewSourceFetcher(SourceFetcherConfig{
		Client: &sourceFetchClient{}, Credentials: func() (DeviceCredentials, error) {
			return DeviceCredentials{DeviceID: "dev_test", DeviceToken: "token"}, nil
		},
		TempRoot: filepath.Join(root, "temp"), AdditionalDenyPaths: []string{denied}, StateDir: filepath.Join(root, "state"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved, _, err := fetcher.resolveAllowedSource(safe); err != nil || resolved != safe {
		t.Fatalf("safe path rejected: %q %v", resolved, err)
	}
	if _, _, err := fetcher.resolveAllowedSource(secret); err == nil {
		t.Fatal("configured denied path accepted")
	}
	if _, _, err := fetcher.resolveAllowedSource(link); err == nil {
		t.Fatal("symbolic link accepted")
	}
	env := filepath.Join(allowed, ".env")
	if err := os.WriteFile(env, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fetcher.resolveAllowedSource(env); err == nil {
		t.Fatal("immutable sensitive basename accepted")
	}
}

func TestSourceFetcherListsDirectoryWithoutSensitiveEntries(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "visible.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &sourceFetchClient{}
	fetcher, err := NewSourceFetcher(SourceFetcherConfig{
		Client: client, Credentials: func() (DeviceCredentials, error) {
			return DeviceCredentials{DeviceID: "dev_test", DeviceToken: "token"}, nil
		},
		TempRoot: filepath.Join(t.TempDir(), "temp"), StateDir: filepath.Join(t.TempDir(), "state"),
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(FetchCommandPayload{
		FetchID: "fet_test", RequesterDeviceID: "dev_requester", SourcePath: root,
		ReceiverPublicKey: "receiver", UploadToken: "upload", UploadPath: "/v1/upload",
		ResultPath: "/v1/result", ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano), MaxCipherBytes: MaxCipherBytes,
	})
	result, err := fetcher.Fetch(context.Background(), payload)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if result == nil || client.result.Status != FetchListed {
		t.Fatalf("unexpected listing result %#v %#v", result, client.result)
	}
	if len(client.result.Listing) != 1 || client.result.Listing[0].Name != "visible.txt" {
		t.Fatalf("sensitive listing leaked: %#v", client.result.Listing)
	}
}

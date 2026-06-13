package artifactrelay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type requesterFetchClient struct {
	request    CreateFetchRequest
	job        FetchJob
	ciphertext []byte
	mounted    bool
}

func (c *requesterFetchClient) CreateDeviceArtifactFetch(_ context.Context, _ DeviceCredentials, request CreateFetchRequest) (CreateFetchResult, error) {
	c.request = request
	return CreateFetchResult{Fetch: FetchJob{ID: "fet_roundtrip", RequesterDeviceID: "dev_requester", SourceDeviceID: request.SourceDeviceID, Status: FetchQueued, ExpiresAt: time.Now().Add(time.Hour)}, DownloadToken: "download-token"}, nil
}
func (c *requesterFetchClient) GetDeviceArtifactFetch(context.Context, DeviceCredentials, string, string) (FetchJob, error) {
	return c.job, nil
}
func (c *requesterFetchClient) DownloadArtifactFetch(_ context.Context, _ DeviceCredentials, _ string, _ string, output io.Writer) (DownloadResult, error) {
	n, err := output.Write(c.ciphertext)
	return DownloadResult{Bytes: int64(n), CipherSHA256: c.job.CipherSHA256, PlainSHA256: c.job.PlainSHA256}, err
}
func (c *requesterFetchClient) ConfirmArtifactFetchMounted(context.Context, DeviceCredentials, string, string) (FetchJob, error) {
	c.mounted = true
	c.job.Status = FetchMounted
	return c.job, nil
}

func TestFetchRequesterTransientKeyRoundTrip(t *testing.T) {
	root := t.TempDir()
	store, err := OpenFetchStore(filepath.Join(root, "store"))
	if err != nil {
		t.Fatal(err)
	}
	client := &requesterFetchClient{}
	requester, err := NewFetchRequester(FetchRequesterConfig{
		Client: client, Store: store,
		Credentials: func() (DeviceCredentials, error) {
			return DeviceCredentials{DeviceID: "dev_requester", DeviceToken: "device-token"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err := requester.Create(context.Background(), FetchCreateInput{SourceDeviceID: "dev_source", SourcePath: "/tmp/source.txt", RetentionSeconds: 3600})
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("artifact fetch requester round trip\n")
	source := filepath.Join(root, "source.txt")
	if err := os.WriteFile(source, plain, 0o600); err != nil {
		t.Fatal(err)
	}
	encrypted := filepath.Join(root, "payload.adr")
	enc, err := EncryptFile(source, encrypted)
	if err != nil {
		t.Fatal(err)
	}
	wrapped, nonce, err := WrapFileKey(enc.EphemeralPrivateKey, client.request.ReceiverPublicKey, enc.FileKey, job.ID, job.ID, "dev_requester")
	if err != nil {
		t.Fatal(err)
	}
	client.ciphertext, _ = os.ReadFile(encrypted)
	cipherHash := sha256.Sum256(client.ciphertext)
	client.job = FetchJob{
		ID: job.ID, RequesterDeviceID: "dev_requester", SourceDeviceID: "dev_source", Status: FetchReady,
		Filename: "source.txt", ContentType: "text/plain", EphemeralPublicKey: enc.EphemeralPublicKey,
		WrappedKey: wrapped, WrapNonce: nonce, PlainSize: int64(len(plain)), PlainSHA256: enc.PlainSHA256,
		CipherSize: int64(len(client.ciphertext)), CipherSHA256: hex.EncodeToString(cipherHash[:]), ExpiresAt: time.Now().Add(time.Hour),
	}
	output, err := requester.Download(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	actual, err := os.ReadFile(output.FilePath)
	if err != nil || string(actual) != string(plain) {
		t.Fatalf("output mismatch %q err=%v", actual, err)
	}
	if output.OutputToken == "" {
		t.Fatal("output token missing")
	}
	if _, err := store.ResolveOutput(job.ID, output.OutputToken, time.Now()); err != nil {
		t.Fatalf("ResolveOutput: %v", err)
	}
	mounted, err := requester.ConfirmMounted(context.Background(), job.ID)
	if err != nil || mounted.Status != FetchMounted || !client.mounted {
		t.Fatalf("ConfirmMounted=%#v err=%v", mounted, err)
	}
	if _, err := store.Load(job.ID); err == nil {
		t.Fatal("transient state was not deleted")
	}
}

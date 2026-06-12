package artifactrelay

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type receiverClient struct {
	ciphertext []byte
	result     DeliveryResultRequest
}

func (c *receiverClient) DownloadArtifact(_ context.Context, _ DeviceCredentials, _ string, _ string, output io.Writer) (DownloadResult, error) {
	n, err := output.Write(c.ciphertext)
	return DownloadResult{Bytes: int64(n)}, err
}
func (c *receiverClient) ReportArtifactResult(_ context.Context, _ DeviceCredentials, _ string, _ string, request DeliveryResultRequest) error {
	c.result = request
	return nil
}

func TestReceiverPullsDecryptsAndReports(t *testing.T) {
	dir := t.TempDir()
	plainPath := filepath.Join(dir, "source.txt")
	plain := []byte("private but legitimate source code\n")
	if err := os.WriteFile(plainPath, plain, 0o600); err != nil {
		t.Fatal(err)
	}
	cipherPath := filepath.Join(dir, "payload.adr")
	enc, err := EncryptFile(plainPath, cipherPath)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, _ := os.ReadFile(cipherPath)
	device, _ := ecdh.X25519().GenerateKey(rand.Reader)
	wrapped, nonce, err := WrapFileKey(enc.EphemeralPrivateKey, base64.RawURLEncoding.EncodeToString(device.PublicKey().Bytes()), enc.FileKey, "art_test", "del_test", "dev_test")
	if err != nil {
		t.Fatal(err)
	}
	client := &receiverClient{ciphertext: ciphertext}
	receiver, err := NewReceiver(ReceiverConfig{
		Client: client, Credentials: func() (DeviceCredentials, error) {
			return DeviceCredentials{DeviceID: "dev_test", DeviceToken: "device-token"}, nil
		},
		PrivateKey: device.Bytes(), InboxRoot: filepath.Join(dir, "inbox"),
	})
	if err != nil {
		t.Fatal(err)
	}
	cipherHash := sha256.Sum256(ciphertext)
	payload := PullPayload{
		ArtifactID: "art_test", DeliveryID: "del_test", Filename: "source.txt", CipherSize: int64(len(ciphertext)),
		CipherSHA256: hex.EncodeToString(cipherHash[:]), PlainSize: int64(len(plain)), PlainSHA256: enc.PlainSHA256,
		EphemeralPublicKey: enc.EphemeralPublicKey, WrappedKey: wrapped, WrapNonce: nonce,
		DownloadToken: "delivery-token", DownloadPath: "/v1/download", ResultPath: "/v1/result",
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano), ConflictPolicy: "reject", LogicalTarget: "inbox",
	}
	raw, _ := jsonMarshal(payload)
	result, err := receiver.Pull(context.Background(), raw)
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	actual, _ := os.ReadFile(result.Path)
	if string(actual) != string(plain) {
		t.Fatalf("unexpected output %q", actual)
	}
	if client.result.Status != "completed" || client.result.LocalPath != result.Path {
		t.Fatalf("unexpected report %#v", client.result)
	}
}

func jsonMarshal(value any) ([]byte, error) {
	return json.Marshal(value)
}

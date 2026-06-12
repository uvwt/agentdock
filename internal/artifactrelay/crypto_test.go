package artifactrelay

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptDecryptAndWrapRoundTrip(t *testing.T) {
	dir := t.TempDir()
	plain := bytes.Repeat([]byte("AgentDock-artifact\x00"), 180000)
	source := filepath.Join(dir, "source.bin")
	if err := os.WriteFile(source, plain, 0o600); err != nil {
		t.Fatal(err)
	}
	encrypted := filepath.Join(dir, "payload.adr")
	enc, err := EncryptFile(source, encrypted)
	if err != nil {
		t.Fatalf("EncryptFile: %v", err)
	}
	device, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	wrapped, nonce, err := WrapFileKey(enc.EphemeralPrivateKey, base64.RawURLEncoding.EncodeToString(device.PublicKey().Bytes()), enc.FileKey, "art_test", "del_test", "dev_test")
	if err != nil {
		t.Fatalf("WrapFileKey: %v", err)
	}
	fileKey, err := UnwrapFileKey(device.Bytes(), enc.EphemeralPublicKey, wrapped, nonce, "art_test", "del_test", "dev_test")
	if err != nil {
		t.Fatalf("UnwrapFileKey: %v", err)
	}
	output := filepath.Join(dir, "output.bin")
	dec, err := DecryptFile(encrypted, output, fileKey)
	if err != nil {
		t.Fatalf("DecryptFile: %v", err)
	}
	actual, _ := os.ReadFile(output)
	if !bytes.Equal(actual, plain) {
		t.Fatal("decrypted bytes differ")
	}
	if dec.PlainSize != int64(len(plain)) || dec.PlainSHA256 != enc.PlainSHA256 {
		t.Fatalf("integrity metadata differs: %#v %#v", dec, enc)
	}
}

func TestTamperedCiphertextAndWrongDeviceKeyFail(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	if err := os.WriteFile(source, bytes.Repeat([]byte("x"), DefaultChunkSize+37), 0o600); err != nil {
		t.Fatal(err)
	}
	encrypted := filepath.Join(dir, "payload.adr")
	enc, err := EncryptFile(source, encrypted)
	if err != nil {
		t.Fatal(err)
	}
	device, _ := ecdh.X25519().GenerateKey(rand.Reader)
	wrong, _ := ecdh.X25519().GenerateKey(rand.Reader)
	wrapped, nonce, err := WrapFileKey(enc.EphemeralPrivateKey, base64.RawURLEncoding.EncodeToString(device.PublicKey().Bytes()), enc.FileKey, "art", "del", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UnwrapFileKey(wrong.Bytes(), enc.EphemeralPublicKey, wrapped, nonce, "art", "del", "dev"); err == nil {
		t.Fatal("wrong device private key unexpectedly unwrapped file key")
	}
	data, _ := os.ReadFile(encrypted)
	data[len(data)/2] ^= 0x40
	if err := os.WriteFile(encrypted, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := DecryptFile(encrypted, filepath.Join(dir, "out"), enc.FileKey); err == nil {
		t.Fatal("tampered ciphertext unexpectedly decrypted")
	}
}

func TestPrepareSourceAndSafeExtraction(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "a.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareSource(root, t.TempDir())
	if err != nil {
		t.Fatalf("PrepareSource: %v", err)
	}
	defer prepared.Cleanup()
	if !prepared.Archive || prepared.Filename != "project.tar.gz" {
		t.Fatalf("unexpected prepared source: %#v", prepared)
	}
	destination := filepath.Join(t.TempDir(), "extract")
	if err := ExtractTarGzip(prepared.Path, destination, 100, 1024); err != nil {
		t.Fatalf("ExtractTarGzip: %v", err)
	}
	content, _ := os.ReadFile(filepath.Join(destination, "sub", "a.txt"))
	if string(content) != "hello" {
		t.Fatalf("unexpected extracted content %q", content)
	}
}

func TestExtractionRejectsTraversalAndSymlink(t *testing.T) {
	for _, test := range []struct {
		name   string
		header *tar.Header
	}{
		{"traversal", &tar.Header{Name: "../escape", Mode: 0o600, Size: 1, Typeflag: tar.TypeReg}},
		{"symlink", &tar.Header{Name: "link", Linkname: "/tmp/target", Typeflag: tar.TypeSymlink}},
	} {
		t.Run(test.name, func(t *testing.T) {
			archive := filepath.Join(t.TempDir(), "bad.tar.gz")
			file, _ := os.Create(archive)
			gz := gzip.NewWriter(file)
			tw := tar.NewWriter(gz)
			if err := tw.WriteHeader(test.header); err != nil {
				t.Fatal(err)
			}
			if test.header.Typeflag == tar.TypeReg {
				_, _ = tw.Write([]byte("x"))
			}
			_ = tw.Close()
			_ = gz.Close()
			_ = file.Close()
			if err := ExtractTarGzip(archive, filepath.Join(t.TempDir(), "out"), 100, 1024); err == nil {
				t.Fatal("unsafe archive unexpectedly extracted")
			}
		})
	}
}

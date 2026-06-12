package artifactrelay

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type KeyPair struct {
	Private []byte
	Public  []byte
}

func (k KeyPair) PublicEncoded() string {
	return base64.RawURLEncoding.EncodeToString(k.Public)
}

func EnsureKeyPair(dir string) (KeyPair, error) {
	if strings.TrimSpace(dir) == "" {
		return KeyPair{}, errors.New("artifact key directory is required")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return KeyPair{}, fmt.Errorf("create artifact key directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return KeyPair{}, fmt.Errorf("secure artifact key directory: %w", err)
	}
	privatePath := filepath.Join(dir, "x25519.private")
	publicPath := filepath.Join(dir, "x25519.public")
	if privateText, err := os.ReadFile(privatePath); err == nil {
		publicText, publicErr := os.ReadFile(publicPath)
		if publicErr != nil {
			return KeyPair{}, fmt.Errorf("read artifact public key: %w", publicErr)
		}
		privateKey, privateErr := base64.RawURLEncoding.DecodeString(strings.TrimSpace(string(privateText)))
		publicKey, publicErr := base64.RawURLEncoding.DecodeString(strings.TrimSpace(string(publicText)))
		if privateErr != nil || publicErr != nil || len(privateKey) != 32 || len(publicKey) != 32 {
			return KeyPair{}, errors.New("stored artifact X25519 key pair is invalid")
		}
		curve := ecdh.X25519()
		parsed, err := curve.NewPrivateKey(privateKey)
		if err != nil || !equalBytes(parsed.PublicKey().Bytes(), publicKey) {
			return KeyPair{}, errors.New("stored artifact X25519 key pair does not match")
		}
		return KeyPair{Private: privateKey, Public: publicKey}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return KeyPair{}, fmt.Errorf("read artifact private key: %w", err)
	}
	private, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return KeyPair{}, fmt.Errorf("generate artifact X25519 key: %w", err)
	}
	pair := KeyPair{Private: private.Bytes(), Public: private.PublicKey().Bytes()}
	if err := atomicWrite(privatePath, []byte(base64.RawURLEncoding.EncodeToString(pair.Private)+"\n"), 0o600); err != nil {
		return KeyPair{}, err
	}
	if err := atomicWrite(publicPath, []byte(base64.RawURLEncoding.EncodeToString(pair.Public)+"\n"), 0o600); err != nil {
		_ = os.Remove(privatePath)
		return KeyPair{}, err
	}
	return pair, nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	temp := path + ".tmp"
	if err := os.WriteFile(temp, data, mode); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if err := os.Chmod(temp, mode); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("secure %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("replace %s: %w", filepath.Base(path), err)
	}
	return nil
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

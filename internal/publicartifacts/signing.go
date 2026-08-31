package publicartifacts

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (s Store) ensureSecret() ([]byte, error) {
	secret, err := readSecret(s.SecretPath)
	if err == nil {
		return secret, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}

	secretDir := filepath.Dir(s.SecretPath)
	if err := os.MkdirAll(secretDir, 0o700); err != nil {
		return nil, fmt.Errorf("create secret dir: %w", err)
	}
	if err := os.Chmod(secretDir, 0o700); err != nil {
		return nil, fmt.Errorf("secure secret dir: %w", err)
	}
	secret = make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generate public url secret: %w", err)
	}

	// 先完整写入同目录临时文件，再用硬链接以“不覆盖”语义发布。
	// 并发进程只有一个能创建最终路径，其他进程读取胜出的同一份密钥。
	tmp, err := os.CreateTemp(secretDir, ".public-url-secret-*")
	if err != nil {
		return nil, fmt.Errorf("create public url secret temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("secure public url secret temp file: %w", err)
	}
	if _, err := tmp.WriteString(hex.EncodeToString(secret) + "\n"); err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("write public url secret temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("sync public url secret temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("close public url secret temp file: %w", err)
	}
	if err := os.Link(tmpPath, s.SecretPath); err == nil {
		return secret, nil
	} else if !os.IsExist(err) {
		return nil, fmt.Errorf("publish public url secret: %w", err)
	}
	return readSecret(s.SecretPath)
}

func readSecret(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	decoded, err := hex.DecodeString(strings.TrimSpace(string(data)))
	if err != nil || len(decoded) < 32 {
		return nil, errors.New("public url secret is invalid")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, fmt.Errorf("secure public url secret: %w", err)
	}
	return decoded, nil
}

func parsePublicPath(pathValue, prefix string) (string, string, bool) {
	rest := strings.TrimPrefix(pathValue, prefix)
	if rest == pathValue || rest == "" {
		return "", "", false
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 2 {
		return "", "", false
	}
	id, err1 := url.PathUnescape(parts[0])
	name, err2 := url.PathUnescape(parts[1])
	if err1 != nil || err2 != nil || id == "" || name == "" || id != filepath.Base(id) || name != filepath.Base(name) || strings.Contains(name, "\\") {
		return "", "", false
	}
	return id, name, true
}

func sign(secret []byte, id, filename string, expires int64, sha string) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = fmt.Fprintf(mac, "%s\n%s\n%d\n%s", id, filename, expires, sha)
	return hex.EncodeToString(mac.Sum(nil))
}

func retention(seconds int) time.Duration {
	if seconds <= 0 {
		return DefaultRetention
	}
	d := time.Duration(seconds) * time.Second
	if d > MaxRetention {
		return MaxRetention
	}
	return d
}

func randomHex(byteCount int) (string, error) {
	if byteCount <= 0 {
		return "", errors.New("random byte count must be positive")
	}
	b := make([]byte, byteCount)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

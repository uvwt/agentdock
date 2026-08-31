package publicartifacts

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strconv"
	"time"

	protocol "github.com/uvwt/agentdock-protocol"
)

func (s Store) ServeHTTP(w http.ResponseWriter, r *http.Request, prefix string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, name, ok := parsePublicPath(r.URL.Path, prefix)
	if !ok {
		http.NotFound(w, r)
		return
	}
	expires, err := strconv.ParseInt(r.URL.Query().Get("expires"), 10, 64)
	if err != nil || expires <= 0 {
		http.NotFound(w, r)
		return
	}
	if time.Now().UTC().Unix() > expires {
		http.Error(w, http.StatusText(http.StatusGone), http.StatusGone)
		return
	}
	meta, payload, err := s.resolveArtifact(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if meta.Filename != name || meta.ExpiresAt.Unix() != expires {
		http.NotFound(w, r)
		return
	}
	secret, err := s.ensureSecret()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	expected := sign(secret, meta.ArtifactID, meta.Filename, expires, meta.SHA256)
	if !hmac.Equal([]byte(expected), []byte(r.URL.Query().Get("sig"))) {
		http.NotFound(w, r)
		return
	}
	file, err := os.OpenInRoot(s.Root, payload)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != meta.Size {
		http.NotFound(w, r)
		return
	}
	payloadSHA, err := streamSHA256(file)
	if err != nil || payloadSHA != meta.SHA256 {
		http.NotFound(w, r)
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", firstNonEmpty(meta.MimeType, "application/octet-stream"))
	w.Header().Set("Content-Disposition", mime.FormatMediaType(contentDisposition(meta.MimeType), map[string]string{"filename": meta.Filename}))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// 即使未来某种主动内容被错误标成 inline，浏览器也不能执行脚本、提交表单或加载外部资源。
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.ServeContent(w, r, meta.Filename, meta.CreatedAt, file)
}

func (s Store) Read(artifactID string, maxBytes int64) (Metadata, []byte, error) {
	meta, payload, err := s.resolveArtifact(artifactID)
	if err != nil {
		return Metadata{}, nil, fmt.Errorf("read artifact metadata: %w", err)
	}
	if time.Now().UTC().After(meta.ExpiresAt) {
		return Metadata{}, nil, errors.New("artifact has expired")
	}
	if maxBytes > 0 && meta.Size > maxBytes {
		return Metadata{}, nil, fmt.Errorf("artifact size %d exceeds limit %d", meta.Size, maxBytes)
	}
	file, err := os.OpenInRoot(s.Root, payload)
	if err != nil {
		return Metadata{}, nil, fmt.Errorf("read artifact payload: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return Metadata{}, nil, fmt.Errorf("read artifact payload: %w", err)
	}
	if int64(len(data)) != meta.Size {
		return Metadata{}, nil, errors.New("artifact payload size does not match metadata")
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != meta.SHA256 {
		return Metadata{}, nil, errors.New("artifact payload checksum does not match metadata")
	}
	return meta, data, nil
}

// ReadChunk returns one verified slice of an Artifact for the outbound-only
// Nexus Bridge. A zero maxBytes performs a metadata-only read for HTTP HEAD.
func (s Store) ReadChunk(artifactID string, offset int64, maxBytes int) (Metadata, []byte, bool, error) {
	if offset < 0 {
		return Metadata{}, nil, false, errors.New("artifact offset must not be negative")
	}
	if maxBytes < 0 {
		return Metadata{}, nil, false, errors.New("artifact chunk size must not be negative")
	}
	if maxBytes > protocol.MaxArtifactChunkBytes {
		maxBytes = protocol.MaxArtifactChunkBytes
	}
	meta, payload, err := s.resolveArtifact(artifactID)
	if err != nil {
		return Metadata{}, nil, false, fmt.Errorf("read artifact metadata: %w", err)
	}
	if time.Now().UTC().After(meta.ExpiresAt) {
		return Metadata{}, nil, false, errors.New("artifact has expired")
	}
	if offset > meta.Size {
		return Metadata{}, nil, false, fmt.Errorf("artifact offset %d exceeds payload size %d", offset, meta.Size)
	}

	file, err := os.OpenInRoot(s.Root, payload)
	if err != nil {
		return Metadata{}, nil, false, fmt.Errorf("open artifact payload: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != meta.Size {
		return Metadata{}, nil, false, errors.New("artifact payload size does not match metadata")
	}
	if maxBytes == 0 {
		return meta, nil, offset == meta.Size, nil
	}
	// The first chunk verifies the immutable snapshot before any bytes leave the
	// node. Nexus additionally verifies the complete stream against this digest.
	if offset == 0 {
		payloadSHA, hashErr := streamSHA256(file)
		if hashErr != nil || payloadSHA != meta.SHA256 {
			return Metadata{}, nil, false, errors.New("artifact payload checksum does not match metadata")
		}
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return Metadata{}, nil, false, fmt.Errorf("seek artifact payload: %w", err)
	}
	remaining := meta.Size - offset
	readBytes := int64(maxBytes)
	if remaining < readBytes {
		readBytes = remaining
	}
	data, err := io.ReadAll(io.LimitReader(file, readBytes))
	if err != nil {
		return Metadata{}, nil, false, fmt.Errorf("read artifact chunk: %w", err)
	}
	if int64(len(data)) != readBytes {
		return Metadata{}, nil, false, errors.New("artifact payload changed while it was being read")
	}
	nextOffset := offset + int64(len(data))
	return meta, data, nextOffset == meta.Size, nil
}

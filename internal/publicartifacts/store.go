package publicartifacts

import (
	"archive/tar"
	"compress/gzip"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	DefaultRetention = 24 * time.Hour
	MaxRetention     = 7 * 24 * time.Hour
)

type Store struct {
	Root       string
	SecretPath string
	Port       int
	ServerURL  string
}

type Metadata struct {
	ArtifactID string    `json:"artifact_id"`
	Filename   string    `json:"filename"`
	MimeType   string    `json:"mime_type"`
	Size       int64     `json:"size_bytes"`
	SHA256     string    `json:"sha256"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Archive    bool      `json:"archive"`
	Width      int       `json:"width,omitempty"`
	Height     int       `json:"height,omitempty"`
}

type PublishRequest struct {
	Path             string
	RetentionSeconds int
	Now              time.Time
	BaseURL          string
}

type PublishBytesRequest struct {
	Filename         string
	Data             []byte
	MimeType         string
	Width            int
	Height           int
	RetentionSeconds int
	Now              time.Time
	BaseURL          string
}

type PublishResult struct {
	Metadata
	URL string `json:"url"`
}

func New(agentDockHome, serverURL string, port int) Store {
	return Store{Root: filepath.Join(agentDockHome, "public-artifacts"), SecretPath: filepath.Join(agentDockHome, "secrets", "public-url-secret"), ServerURL: serverURL, Port: port}
}

func (s Store) Publish(req PublishRequest) (PublishResult, error) {
	now := normalizeNow(req.Now)
	if err := s.Cleanup(now); err != nil {
		return PublishResult{}, err
	}
	info, err := os.Stat(req.Path)
	if err != nil {
		return PublishResult{}, fmt.Errorf("stat publish source: %w", err)
	}
	dir, payload, id, err := s.prepareArtifactDir()
	if err != nil {
		return PublishResult{}, err
	}
	filename := filepath.Base(req.Path)
	archive := false
	if info.IsDir() {
		archive = true
		filename += ".tar.gz"
		if err := writeTarGz(req.Path, payload); err != nil {
			_ = os.RemoveAll(dir)
			return PublishResult{}, err
		}
	} else {
		if err := copyFile(req.Path, payload); err != nil {
			_ = os.RemoveAll(dir)
			return PublishResult{}, err
		}
	}
	return s.finishPublishedPayload(publishPayloadRequest{
		ID:               id,
		Dir:              dir,
		Payload:          payload,
		Filename:         filename,
		Archive:          archive,
		RetentionSeconds: req.RetentionSeconds,
		Now:              now,
		BaseURL:          req.BaseURL,
	})
}

func (s Store) PublishBytes(req PublishBytesRequest) (PublishResult, error) {
	now := normalizeNow(req.Now)
	if len(req.Data) == 0 {
		return PublishResult{}, errors.New("publish bytes payload is empty")
	}
	if err := s.Cleanup(now); err != nil {
		return PublishResult{}, err
	}
	dir, payload, id, err := s.prepareArtifactDir()
	if err != nil {
		return PublishResult{}, err
	}
	if err := os.WriteFile(payload, req.Data, 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return PublishResult{}, fmt.Errorf("write payload: %w", err)
	}
	return s.finishPublishedPayload(publishPayloadRequest{
		ID:               id,
		Dir:              dir,
		Payload:          payload,
		Filename:         req.Filename,
		MimeType:         req.MimeType,
		Width:            req.Width,
		Height:           req.Height,
		RetentionSeconds: req.RetentionSeconds,
		Now:              now,
		BaseURL:          req.BaseURL,
	})
}

type publishPayloadRequest struct {
	ID               string
	Dir              string
	Payload          string
	Filename         string
	MimeType         string
	Archive          bool
	Width            int
	Height           int
	RetentionSeconds int
	Now              time.Time
	BaseURL          string
}

func (s Store) prepareArtifactDir() (string, string, string, error) {
	if err := os.MkdirAll(s.Root, 0o700); err != nil {
		return "", "", "", fmt.Errorf("create public artifacts root: %w", err)
	}
	for attempt := 0; attempt < 10; attempt++ {
		id, err := randomHex(16)
		if err != nil {
			return "", "", "", err
		}
		dir := filepath.Join(s.Root, id)
		if err := os.Mkdir(dir, 0o700); err == nil {
			return dir, filepath.Join(dir, "payload"), id, nil
		} else if !os.IsExist(err) {
			return "", "", "", fmt.Errorf("create artifact dir: %w", err)
		}
	}
	return "", "", "", errors.New("create artifact dir: random identifier collision limit reached")
}

func (s Store) finishPublishedPayload(req publishPayloadRequest) (PublishResult, error) {
	stat, err := os.Stat(req.Payload)
	if err != nil {
		_ = os.RemoveAll(req.Dir)
		return PublishResult{}, err
	}
	sha, err := fileSHA256(req.Payload)
	if err != nil {
		_ = os.RemoveAll(req.Dir)
		return PublishResult{}, err
	}
	filename := safeDownloadName(req.Filename)
	mimeType := firstNonEmpty(req.MimeType, detectMime(req.Payload, filename, req.Archive))
	width, height := req.Width, req.Height
	if width <= 0 || height <= 0 {
		width, height = imageDimensions(req.Payload, mimeType)
	}
	retention := retention(req.RetentionSeconds)
	meta := Metadata{ArtifactID: req.ID, Filename: filename, MimeType: mimeType, Size: stat.Size(), SHA256: sha, CreatedAt: req.Now, ExpiresAt: req.Now.Add(retention), Archive: req.Archive, Width: width, Height: height}
	encoded, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		_ = os.RemoveAll(req.Dir)
		return PublishResult{}, err
	}
	if err := os.WriteFile(filepath.Join(req.Dir, "metadata.json"), encoded, 0o600); err != nil {
		_ = os.RemoveAll(req.Dir)
		return PublishResult{}, fmt.Errorf("write artifact metadata: %w", err)
	}
	base := strings.TrimRight(firstNonEmpty(req.BaseURL, s.ServerURL), "/")
	publicURL := ""
	if base != "" {
		secret, err := s.ensureSecret()
		if err != nil {
			_ = os.RemoveAll(req.Dir)
			return PublishResult{}, err
		}
		sig := sign(secret, meta.ArtifactID, meta.Filename, meta.ExpiresAt.Unix(), meta.SHA256)
		publicURL = base + "/artifacts/public/" + url.PathEscape(meta.ArtifactID) + "/" + url.PathEscape(meta.Filename) + "?expires=" + strconv.FormatInt(meta.ExpiresAt.Unix(), 10) + "&sig=" + url.QueryEscape(sig)
	}
	return PublishResult{Metadata: meta, URL: publicURL}, nil
}

func normalizeNow(now time.Time) time.Time {
	if now.IsZero() {
		return time.Now().UTC()
	}
	return now.UTC()
}

func (s Store) EnsureSecret() error {
	_, err := s.ensureSecret()
	return err
}

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
	meta, err := s.readMetadata(id)
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
	payload := filepath.Join(s.Root, id, "payload")
	file, err := os.Open(payload)
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
	http.ServeContent(w, r, meta.Filename, meta.CreatedAt, file)
}

func (s Store) Read(artifactID string, maxBytes int64) (Metadata, []byte, error) {
	meta, err := s.readMetadata(artifactID)
	if err != nil {
		return Metadata{}, nil, fmt.Errorf("read artifact metadata: %w", err)
	}
	if time.Now().UTC().After(meta.ExpiresAt) {
		return Metadata{}, nil, errors.New("artifact has expired")
	}
	if maxBytes > 0 && meta.Size > maxBytes {
		return Metadata{}, nil, fmt.Errorf("artifact size %d exceeds limit %d", meta.Size, maxBytes)
	}
	payload := filepath.Join(s.Root, artifactID, "payload")
	data, err := os.ReadFile(payload)
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

func (s Store) Cleanup(now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	if err := os.MkdirAll(s.Root, 0o700); err != nil {
		return fmt.Errorf("create public artifacts root: %w", err)
	}
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		return err
	}
	var cleanupErrs []error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(s.Root, entry.Name())
		info, statErr := entry.Info()
		if statErr != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("inspect artifact directory %s: %w", dir, statErr))
			continue
		}
		meta, metaErr := readMetadataPath(filepath.Join(dir, "metadata.json"))
		payloadInfo, payloadErr := os.Stat(filepath.Join(dir, "payload"))
		oldBroken := now.Sub(info.ModTime()) > 24*time.Hour
		remove := metaErr == nil && meta.ExpiresAt.Before(now) ||
			metaErr != nil && oldBroken ||
			payloadErr != nil && oldBroken ||
			payloadErr == nil && !payloadInfo.Mode().IsRegular() && oldBroken
		if remove {
			if err := os.RemoveAll(dir); err != nil {
				cleanupErrs = append(cleanupErrs, fmt.Errorf("remove artifact directory %s: %w", dir, err))
			}
		}
	}
	return errors.Join(cleanupErrs...)
}

func (s Store) readMetadata(id string) (Metadata, error) {
	if id == "" || id != filepath.Base(id) {
		return Metadata{}, errors.New("invalid artifact id")
	}
	return readMetadataPath(filepath.Join(s.Root, id, "metadata.json"))
}

func readMetadataPath(path string) (Metadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Metadata{}, err
	}
	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return Metadata{}, err
	}
	if meta.ArtifactID == "" || meta.Filename == "" || meta.SHA256 == "" || meta.Size < 0 || meta.ExpiresAt.IsZero() {
		return Metadata{}, errors.New("invalid metadata")
	}
	return meta, nil
}

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

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open publish source: %w", err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create payload: %w", err)
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return fmt.Errorf("copy payload: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close payload: %w", closeErr)
	}
	return nil
}

func writeTarGz(src, dst string) error {
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create archive payload: %w", err)
	}
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	walkErr := filepath.WalkDir(src, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		name, err := filepath.Rel(filepath.Dir(src), path)
		if err != nil {
			return err
		}
		name = filepath.ToSlash(name)
		if name == "." {
			return nil
		}
		h, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		h.Name = name
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			in, err := os.Open(path)
			if err != nil {
				return err
			}
			_, err = io.Copy(tw, in)
			closeErr := in.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
		}
		return nil
	})
	closeTar := tw.Close()
	closeGz := gz.Close()
	closeOut := out.Close()
	if walkErr != nil {
		return fmt.Errorf("archive source: %w", walkErr)
	}
	if closeTar != nil {
		return closeTar
	}
	if closeGz != nil {
		return closeGz
	}
	if closeOut != nil {
		return closeOut
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	return streamSHA256(file)
}

func streamSHA256(reader io.Reader) (string, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, reader); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func imageDimensions(path, mimeType string) (int, int) {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(mimeType)), "image/") {
		return 0, 0
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer file.Close()
	cfg, _, err := image.DecodeConfig(file)
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

func contentDisposition(mimeType string) string {
	value := strings.ToLower(strings.TrimSpace(mimeType))
	if strings.HasPrefix(value, "image/") || strings.HasPrefix(value, "text/") {
		return "inline"
	}
	return "attachment"
}

func detectMime(path, filename string, archive bool) string {
	if archive {
		return "application/gzip"
	}
	if mt := mime.TypeByExtension(filepath.Ext(filename)); mt != "" {
		return mt
	}
	f, err := os.Open(path)
	if err != nil {
		return "application/octet-stream"
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return "application/octet-stream"
	}
	if n == 0 {
		return "application/octet-stream"
	}
	return http.DetectContentType(buf[:n])
}

func safeDownloadName(value string) string {
	value = strings.ToValidUTF8(value, "_")
	value = strings.Map(func(char rune) rune {
		if char < 0x20 || char == 0x7f {
			return '_'
		}
		return char
	}, value)
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	value = filepath.Base(value)
	if value == "" || value == "." || value == ".." || strings.ContainsAny(value, "/\\") {
		return "artifact.bin"
	}
	if len(value) <= 240 {
		return value
	}

	// 下载名受 HTTP 头和文件系统共同约束。按字节限制时必须停在 rune 边界，
	// 否则长中文名会生成非法 UTF-8，并继续污染 metadata、URL 与响应头。
	ext := filepath.Ext(value)
	if len(ext) >= 240 {
		ext = ""
	}
	base := strings.TrimSuffix(value, ext)
	base = truncateUTF8Bytes(base, 240-len(ext))
	if base == "" {
		return "artifact.bin"
	}
	return base + ext
}

func truncateUTF8Bytes(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	for maxBytes > 0 && !utf8.ValidString(value[:maxBytes]) {
		maxBytes--
	}
	return value[:maxBytes]
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

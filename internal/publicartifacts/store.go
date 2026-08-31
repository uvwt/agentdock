package publicartifacts

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
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

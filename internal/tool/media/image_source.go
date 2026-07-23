package media

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/publicartifacts"
)

const (
	defaultImageSourceBytes = 20 * 1024 * 1024
	hardImageSourceBytes    = 100 * 1024 * 1024
)

type loadedImageSource struct {
	Data   []byte
	Name   string
	Source map[string]any
}

func (s *Service) loadImageSource(ctx context.Context, args map[string]any) (loadedImageSource, error) {
	artifactID := strings.TrimSpace(stringArg(args, "artifact_id", ""))
	pathValue := strings.TrimSpace(stringArg(args, "path", ""))
	urlValue := strings.TrimSpace(stringArg(args, "url", ""))
	provided := 0
	for _, value := range []string{artifactID, pathValue, urlValue} {
		if value != "" {
			provided++
		}
	}
	if provided == 0 {
		return loadedImageSource{}, toolError("IMAGE_SOURCE_REQUIRED", "one of artifact_id, path, or url is required", "validation")
	}
	if provided != 1 {
		return loadedImageSource{}, toolError("IMAGE_SOURCE_CONFLICT", "artifact_id, path, and url are mutually exclusive", "validation")
	}

	maxSourceBytes := int64(intArg(args, "max_source_bytes", defaultImageSourceBytes))
	if maxSourceBytes <= 0 {
		maxSourceBytes = defaultImageSourceBytes
	}
	if maxSourceBytes > hardImageSourceBytes {
		maxSourceBytes = hardImageSourceBytes
	}

	switch {
	case artifactID != "":
		store := publicartifacts.New(s.cfg.AgentDockHome, s.cfg.OAuthServerURL, s.cfg.Port)
		meta, data, err := store.Read(artifactID, maxSourceBytes)
		if err != nil {
			return loadedImageSource{}, toolErrorDetails("IMAGE_ARTIFACT_READ_FAILED", "cannot read image artifact", "validation", map[string]any{"artifact_id": artifactID, "reason": err.Error()})
		}
		return loadedImageSource{
			Data: data,
			Name: meta.Filename,
			Source: map[string]any{
				"type":        "artifact",
				"artifact_id": meta.ArtifactID,
				"filename":    meta.Filename,
				"mime_type":   meta.MimeType,
				"size_bytes":  meta.Size,
				"sha256":      meta.SHA256,
				"expires_at":  meta.ExpiresAt.Format(time.RFC3339),
			},
		}, nil
	case pathValue != "":
		resolved, err := s.ws.ResolveExisting(pathValue)
		if err != nil {
			return loadedImageSource{}, err
		}
		info, err := os.Stat(resolved.Abs)
		if err != nil {
			return loadedImageSource{}, err
		}
		if !info.Mode().IsRegular() {
			return loadedImageSource{}, toolError("IMAGE_SOURCE_NOT_FILE", "image path must be a regular file", "validation")
		}
		if info.Size() > maxSourceBytes {
			return loadedImageSource{}, toolErrorDetails("IMAGE_SOURCE_TOO_LARGE", "image source exceeds max_source_bytes", "validation", map[string]any{"size_bytes": info.Size(), "max_source_bytes": maxSourceBytes})
		}
		data, err := os.ReadFile(resolved.Abs)
		if err != nil {
			return loadedImageSource{}, err
		}
		return loadedImageSource{Data: data, Name: filepath.Base(resolved.Abs), Source: map[string]any{"type": "path", "path": resolved.Display, "size_bytes": len(data)}}, nil
	default:
		return loadRemoteImageSource(ctx, urlValue, maxSourceBytes, intArg(args, "source_timeout_ms", 15000))
	}
}

func loadRemoteImageSource(ctx context.Context, rawURL string, maxSourceBytes int64, timeoutMS int) (loadedImageSource, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return loadedImageSource{}, toolErrorDetails("IMAGE_URL_INVALID", "image url must be an absolute HTTP(S) URL", "validation", map[string]any{"url": rawURL})
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return loadedImageSource{}, toolErrorDetails("IMAGE_URL_SCHEME_UNSUPPORTED", "image url must use http or https", "validation", map[string]any{"scheme": parsed.Scheme})
	}
	if timeoutMS <= 0 {
		timeoutMS = 15000
	}
	if timeoutMS > 120000 {
		timeoutMS = 120000
	}
	client := &http.Client{
		Timeout: time.Duration(timeoutMS) * time.Millisecond,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			req.URL.Scheme = strings.ToLower(req.URL.Scheme)
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("redirect uses unsupported scheme %q", req.URL.Scheme)
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return loadedImageSource{}, toolErrorDetails("IMAGE_URL_INVALID", "cannot create image request", "validation", map[string]any{"reason": err.Error()})
	}
	resp, err := client.Do(req)
	if err != nil {
		return loadedImageSource{}, toolErrorDetails("IMAGE_URL_FETCH_FAILED", "cannot download image url", "runtime", map[string]any{"reason": err.Error()})
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return loadedImageSource{}, toolErrorDetails("IMAGE_URL_HTTP_ERROR", "image url returned a non-success status", "runtime", map[string]any{"status": resp.StatusCode})
	}
	if resp.ContentLength > maxSourceBytes {
		return loadedImageSource{}, toolErrorDetails("IMAGE_SOURCE_TOO_LARGE", "image source exceeds max_source_bytes", "validation", map[string]any{"size_bytes": resp.ContentLength, "max_source_bytes": maxSourceBytes})
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSourceBytes+1))
	if err != nil {
		return loadedImageSource{}, toolErrorDetails("IMAGE_URL_FETCH_FAILED", "cannot read downloaded image", "runtime", map[string]any{"reason": err.Error()})
	}
	if int64(len(data)) > maxSourceBytes {
		return loadedImageSource{}, toolErrorDetails("IMAGE_SOURCE_TOO_LARGE", "image source exceeds max_source_bytes", "validation", map[string]any{"size_bytes": len(data), "max_source_bytes": maxSourceBytes})
	}
	name := filepath.Base(parsed.Path)
	if name == "" || name == "." || name == "/" {
		name = "remote-image"
	}
	return loadedImageSource{Data: data, Name: name, Source: map[string]any{"type": "url", "url": rawURL, "size_bytes": len(data)}}, nil
}

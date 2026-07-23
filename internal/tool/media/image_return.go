package media

import (
	"context"
	"encoding/base64"
	"path/filepath"
	"strings"

	"github.com/uvwt/agentdock/internal/httpx/requestmeta"
	"github.com/uvwt/agentdock/internal/publicartifacts"
)

const (
	defaultViewImageBytes = 750000
	hardViewImageBytes    = 2 * 1024 * 1024
)

func (s *Service) publishImageBytes(ctx context.Context, data []byte, filename string, info imageInfo, retentionSeconds int) (map[string]any, error) {
	store := publicartifacts.New(s.cfg.AgentDockHome, s.cfg.OAuthServerURL, s.cfg.Port)
	published, err := store.PublishBytes(publicartifacts.PublishBytesRequest{
		Filename:         imageFilename(filename, info.MIME),
		Data:             data,
		MimeType:         info.MIME,
		Width:            info.Width,
		Height:           info.Height,
		RetentionSeconds: retentionSeconds,
		BaseURL:          requestmeta.BaseURL(ctx),
	})
	if err != nil {
		return nil, err
	}
	return artifactResult(published), nil
}

func artifactResult(published publicartifacts.PublishResult) map[string]any {
	result := map[string]any{
		"artifact_id": published.ArtifactID,
		"expires_at":  published.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"),
		"filename":    published.Filename,
		"mime_type":   published.MimeType,
		"size_bytes":  published.Size,
		"sha256":      published.SHA256,
		"archive":     published.Archive,
	}
	if published.URL != "" {
		result["url"] = published.URL
	}
	if published.Width > 0 {
		result["width"] = published.Width
	}
	if published.Height > 0 {
		result["height"] = published.Height
	}
	return result
}

func imageMetadata(filename string, info imageInfo, size int) map[string]any {
	return map[string]any{
		"filename":   imageFilename(filename, info.MIME),
		"mime_type":  info.MIME,
		"size_bytes": size,
		"width":      info.Width,
		"height":     info.Height,
	}
}

func attachMCPImage(result Result, data []byte, mimeType string) {
	result["_mcp_image_base64"] = base64.StdEncoding.EncodeToString(data)
	result["_mcp_image_mime_type"] = mimeType
}

func imageFilename(pathValue, mimeType string) string {
	name := filepath.Base(strings.TrimSpace(pathValue))
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "image"
	}
	ext := strings.ToLower(filepath.Ext(name))
	switch mimeType {
	case "image/jpeg":
		if ext != ".jpg" && ext != ".jpeg" {
			name = strings.TrimSuffix(name, filepath.Ext(name)) + ".jpg"
		}
	case "image/png":
		if ext != ".png" {
			name = strings.TrimSuffix(name, filepath.Ext(name)) + ".png"
		}
	}
	return name
}

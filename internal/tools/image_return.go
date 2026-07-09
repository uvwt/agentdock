package tools

import (
	"context"
	"encoding/base64"
	"path/filepath"
	"strings"

	"github.com/uvwt/agentdock/internal/publicartifacts"
	"github.com/uvwt/agentdock/internal/requestmeta"
)

const (
	imageReturnURL      = "url"
	imageReturnNone     = "none"
	imageReturnMCPImage = "mcp_image"
	imageReturnBase64   = "base64"
	imageReturnDataURL  = "data_url"
	imageReturnBoth     = "both"

	defaultInlineImageBytes = 750000
	hardInlineImageBytes    = 2 * 1024 * 1024
)

func imageReturnMode(args map[string]any, key string) (string, error) {
	mode := strings.TrimSpace(strings.ToLower(stringArg(args, key, imageReturnURL)))
	if mode == "" {
		mode = imageReturnURL
	}
	switch mode {
	case imageReturnURL, imageReturnNone, imageReturnMCPImage, imageReturnBase64, imageReturnDataURL, imageReturnBoth:
		return mode, nil
	default:
		return "", toolErrorDetails("INVALID_RETURN_MODE", "unsupported image return mode", "validation", map[string]any{"return_mode": mode, "allowed": []string{imageReturnURL, imageReturnNone, imageReturnMCPImage, imageReturnBase64, imageReturnDataURL, imageReturnBoth}})
	}
}

func needsPublicURL(mode string) bool {
	return mode == imageReturnURL || mode == imageReturnBoth
}

func needsInlineImage(mode string) bool {
	return mode == imageReturnMCPImage || mode == imageReturnBase64 || mode == imageReturnDataURL || mode == imageReturnBoth
}

func maxInlineImageBytes(args map[string]any) int {
	limit := intArg(args, "max_inline_bytes", defaultInlineImageBytes)
	if limit <= 0 {
		limit = defaultInlineImageBytes
	}
	if limit > hardInlineImageBytes {
		return hardInlineImageBytes
	}
	return limit
}

func (r *Runtime) publishImageBytes(ctx context.Context, data []byte, filename string, info imageInfo, retentionSeconds int) (map[string]any, error) {
	store := publicartifacts.New(r.cfg.AgentDockHome, r.cfg.OAuthServerURL, r.cfg.Port)
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
		"url":         published.URL,
		"expires_at":  published.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"),
		"filename":    published.Filename,
		"mime_type":   published.MimeType,
		"size_bytes":  published.Size,
		"sha256":      published.SHA256,
		"archive":     published.Archive,
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

func attachInlineImage(result Result, data []byte, mimeType, mode string, args map[string]any) error {
	if !needsInlineImage(mode) {
		return nil
	}
	limit := maxInlineImageBytes(args)
	if len(data) > limit {
		return toolErrorDetails("IMAGE_INLINE_TOO_LARGE", "image exceeds max_inline_bytes", "validation", map[string]any{"bytes": len(data), "max_inline_bytes": limit, "return_mode": mode})
	}
	inline := map[string]any{"mode": mode, "mime_type": mimeType, "size_bytes": len(data)}
	encoded := base64.StdEncoding.EncodeToString(data)
	switch mode {
	case imageReturnMCPImage:
		inline["attached"] = true
		result["_mcp_image_base64"] = encoded
		result["_mcp_image_mime_type"] = mimeType
	case imageReturnBase64, imageReturnBoth:
		inline["base64"] = encoded
	case imageReturnDataURL:
		inline["data_url"] = "data:" + mimeType + ";base64," + encoded
	}
	result["inline"] = inline
	return nil
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

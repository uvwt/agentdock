package nexusbridge

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

func (c *Client) readArtifactChunk(artifactID string, offset int64, maxBytes int) (map[string]any, error) {
	meta, data, eof, err := c.artifacts.ReadChunk(artifactID, offset, maxBytes)
	if err != nil {
		return nil, err
	}
	result := artifactChunkResult{
		ArtifactID: meta.ArtifactID,
		Filename:   meta.Filename,
		MIMEType:   meta.MimeType,
		Size:       meta.Size,
		SHA256:     meta.SHA256,
		CreatedAt:  meta.CreatedAt,
		ExpiresAt:  meta.ExpiresAt,
		Archive:    meta.Archive,
		Width:      meta.Width,
		Height:     meta.Height,
		Offset:     offset,
		NextOffset: offset + int64(len(data)),
		DataBase64: base64.StdEncoding.EncodeToString(data),
		EOF:        eof,
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("编码 Artifact Bridge 结果: %w", err)
	}
	var mapped map[string]any
	if err := json.Unmarshal(encoded, &mapped); err != nil {
		return nil, fmt.Errorf("构造 Artifact Bridge 结果: %w", err)
	}
	return mapped, nil
}

type artifactChunkResult struct {
	ArtifactID string    `json:"artifact_id"`
	Filename   string    `json:"filename"`
	MIMEType   string    `json:"mime_type"`
	Size       int64     `json:"size_bytes"`
	SHA256     string    `json:"sha256"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Archive    bool      `json:"archive"`
	Width      int       `json:"width,omitempty"`
	Height     int       `json:"height,omitempty"`
	Offset     int64     `json:"offset"`
	NextOffset int64     `json:"next_offset"`
	DataBase64 string    `json:"data_base64"`
	EOF        bool      `json:"eof"`
}

package media

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/uvwt/agentdock/internal/httpx/requestmeta"
	"github.com/uvwt/agentdock/internal/publicartifacts"
)

func (s *Service) FilePublish(ctx context.Context, request FilePublishRequest) (Result, error) {
	pathValue, err := s.filePublishSourcePath(request)
	if err != nil {
		return nil, err
	}
	store := publicartifacts.New(s.cfg.AgentDockHome, s.cfg.OAuthServerURL, s.cfg.Port)
	published, err := store.Publish(publicartifacts.PublishRequest{Path: pathValue, RetentionSeconds: intValue(request.RetentionSeconds, 0), BaseURL: requestmeta.BaseURL(ctx)})
	if err != nil {
		return nil, fmt.Errorf("publish file: %w", err)
	}
	result := Result{}
	for key, value := range artifactResult(published) {
		result[key] = value
	}
	return result, nil
}

func (s *Service) filePublishSourcePath(request FilePublishRequest) (string, error) {
	if request.File != nil {
		if pathValue := connectorLocalPath(request.File); pathValue != "" {
			resolved, err := s.ws.ResolveExisting(pathValue)
			if err != nil {
				return "", err
			}
			return resolved.Abs, nil
		}
	}
	pathValue := strings.TrimSpace(request.Path)
	if pathValue == "" {
		return "", toolError("FILE_PUBLISH_SOURCE_REQUIRED", "file or path is required", "validation")
	}
	resolved, err := s.ws.ResolveExisting(pathValue)
	if err != nil {
		return "", err
	}
	return resolved.Abs, nil
}

func connectorLocalPath(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case map[string]any:
		for _, key := range []string{"local_path", "file_path", "mount_path", "path"} {
			if raw, ok := v[key].(string); ok && strings.TrimSpace(raw) != "" {
				return strings.TrimSpace(raw)
			}
		}
		if raw, ok := v["filename"].(string); ok && strings.TrimSpace(raw) != "" && filepath.IsAbs(raw) {
			return strings.TrimSpace(raw)
		}
	}
	return ""
}

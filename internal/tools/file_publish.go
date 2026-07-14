package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/uvwt/agentdock/internal/publicartifacts"
	"github.com/uvwt/agentdock/internal/requestmeta"
)

func (r *Runtime) filePublish(ctx context.Context, args map[string]any) (Result, error) {
	pathValue, err := r.filePublishSourcePath(args)
	if err != nil {
		return nil, err
	}
	store := publicartifacts.New(r.cfg.AgentDockHome, r.cfg.OAuthServerURL, r.cfg.Port)
	published, err := store.Publish(publicartifacts.PublishRequest{Path: pathValue, RetentionSeconds: intArg(args, "retention_seconds", 0), BaseURL: requestmeta.BaseURL(ctx)})
	if err != nil {
		return nil, fmt.Errorf("publish file: %w", err)
	}
	result := Result{}
	for key, value := range artifactResult(published) {
		result[key] = value
	}
	return result, nil
}

func (r *Runtime) filePublishSourcePath(args map[string]any) (string, error) {
	if fileValue, ok := args["file"]; ok && fileValue != nil {
		if pathValue := connectorLocalPath(fileValue); pathValue != "" {
			resolved, err := r.ws.ResolveExisting(pathValue)
			if err != nil {
				return "", err
			}
			return resolved.Abs, nil
		}
	}
	pathValue := strings.TrimSpace(stringArg(args, "path", ""))
	if pathValue == "" {
		return "", toolError("FILE_PUBLISH_SOURCE_REQUIRED", "file or path is required", "validation")
	}
	resolved, err := r.ws.ResolveExisting(pathValue)
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

package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/uvwt/agentdock/internal/component"
)

const cloudflaredMetadataPath = "packaging/components/cloudflared.json"

type pinnedCloudflaredMetadata struct {
	SchemaVersion  int                         `json:"schema_version"`
	Component      string                      `json:"component"`
	Version        string                      `json:"version"`
	UpstreamSource string                      `json:"upstream_source"`
	Artifacts      []component.CatalogArtifact `json:"artifacts"`
}

func loadPinnedCloudflaredMetadata() (pinnedCloudflaredMetadata, error) {
	var data []byte
	var err error
	for _, candidate := range []string{
		cloudflaredMetadataPath,
		filepath.Join("..", "..", cloudflaredMetadataPath),
	} {
		data, err = os.ReadFile(candidate)
		if err == nil {
			break
		}
	}
	if err != nil {
		return pinnedCloudflaredMetadata{}, fmt.Errorf("read pinned cloudflared metadata: %w", err)
	}
	var metadata pinnedCloudflaredMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return pinnedCloudflaredMetadata{}, fmt.Errorf("parse pinned cloudflared metadata: %w", err)
	}
	if err := validatePinnedCloudflaredMetadata(metadata); err != nil {
		return pinnedCloudflaredMetadata{}, err
	}
	return metadata, nil
}

func validatePinnedCloudflaredMetadata(metadata pinnedCloudflaredMetadata) error {
	if metadata.SchemaVersion != 1 {
		return fmt.Errorf("unsupported cloudflared metadata schema: %d", metadata.SchemaVersion)
	}
	if metadata.Component != component.CloudflaredName {
		return fmt.Errorf("unexpected pinned component %q", metadata.Component)
	}
	if strings.TrimSpace(metadata.Version) == "" {
		return errors.New("pinned cloudflared version is required")
	}
	expectedSource := "https://github.com/cloudflare/cloudflared/releases/tag/" + metadata.Version
	if metadata.UpstreamSource != expectedSource {
		return errors.New("pinned cloudflared upstream source must be the exact Cloudflare release tag")
	}

	expected := map[string]struct {
		format string
		name   string
	}{
		"windows/amd64": {format: "binary", name: "cloudflared-windows-amd64.exe"},
		"darwin/amd64":  {format: "tgz", name: "cloudflared-darwin-amd64.tgz"},
		"darwin/arm64":  {format: "tgz", name: "cloudflared-darwin-arm64.tgz"},
	}
	seen := make(map[string]bool, len(metadata.Artifacts))
	for _, artifact := range metadata.Artifacts {
		key := artifact.OS + "/" + artifact.Arch
		want, ok := expected[key]
		if !ok {
			return fmt.Errorf("unsupported pinned cloudflared artifact %s", key)
		}
		if seen[key] {
			return fmt.Errorf("duplicate pinned cloudflared artifact %s", key)
		}
		seen[key] = true
		if artifact.Format != want.format {
			return fmt.Errorf("pinned cloudflared %s format = %q, want %q", key, artifact.Format, want.format)
		}
		if len(artifact.SHA256) != 64 {
			return fmt.Errorf("pinned cloudflared %s SHA-256 has invalid length", key)
		}
		if _, err := hex.DecodeString(artifact.SHA256); err != nil {
			return fmt.Errorf("pinned cloudflared %s SHA-256 is invalid", key)
		}

		parsed, err := url.Parse(artifact.URL)
		if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" ||
			parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("pinned cloudflared %s URL is not a fixed GitHub HTTPS asset", key)
		}
		expectedPath := "/cloudflare/cloudflared/releases/download/" + metadata.Version + "/" + want.name
		if parsed.Path != expectedPath {
			return fmt.Errorf("pinned cloudflared %s URL is not the expected official asset", key)
		}
	}
	for key := range expected {
		if !seen[key] {
			return fmt.Errorf("pinned cloudflared metadata missing %s", key)
		}
	}
	if len(metadata.Artifacts) != len(expected) {
		return errors.New("pinned cloudflared metadata contains unexpected artifacts")
	}
	return nil
}

func writeCloudflaredComponentCatalog(stdout io.Writer) error {
	metadata, err := loadPinnedCloudflaredMetadata()
	if err != nil {
		return err
	}
	catalog := component.Catalog{
		SchemaVersion: 1,
		Components: []component.CatalogComponent{{
			Component:       metadata.Component,
			Version:         metadata.Version,
			UpstreamVersion: metadata.Version,
			UpstreamSource:  metadata.UpstreamSource,
			Artifacts:       metadata.Artifacts,
		}},
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(catalog)
}

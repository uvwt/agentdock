package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/uvwt/agentdock/internal/component"
)

const cloudflaredComponentVersion = "2026.9.1"

type cloudflaredReleaseArtifact struct {
	OS   string
	Arch string
	Name string
}

var cloudflaredReleaseArtifacts = []cloudflaredReleaseArtifact{
	{OS: "darwin", Arch: "amd64", Name: "cloudflared_darwin_amd64"},
	{OS: "darwin", Arch: "arm64", Name: "cloudflared_darwin_arm64"},
	{OS: "windows", Arch: "amd64", Name: "cloudflared_windows_amd64.exe"},
}

func writeCloudflaredComponentCatalog(stdout io.Writer, releaseTag, repository, distDir string) error {
	releaseTag = strings.TrimSpace(releaseTag)
	repository = strings.Trim(strings.TrimSpace(repository), "/")
	if releaseTag == "" || repository == "" {
		return fmt.Errorf("component catalog requires release tag and repository")
	}
	artifacts := make([]component.CatalogArtifact, 0, len(cloudflaredReleaseArtifacts))
	for _, item := range cloudflaredReleaseArtifacts {
		path := filepath.Join(distDir, item.Name)
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("component artifact %s: %w", item.Name, err)
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("component artifact must be a regular non-symlink file: %s", item.Name)
		}
		digest, err := fileSHA256(path)
		if err != nil {
			return err
		}
		artifacts = append(artifacts, component.CatalogArtifact{
			OS:     item.OS,
			Arch:   item.Arch,
			URL:    fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repository, releaseTag, item.Name),
			SHA256: digest,
		})
	}
	catalog := component.Catalog{
		SchemaVersion: 1,
		Components: []component.CatalogComponent{{
			Component:       component.CloudflaredName,
			Version:         cloudflaredComponentVersion,
			UpstreamVersion: cloudflaredComponentVersion,
			UpstreamSource:  "https://github.com/cloudflare/cloudflared/releases/tag/" + cloudflaredComponentVersion,
			Artifacts:       artifacts,
		}},
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(catalog)
}

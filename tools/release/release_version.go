package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/uvwt/agentdock/internal/releaseversion"
)

type releaseMetadata struct {
	Tag        string `json:"tag"`
	Version    string `json:"version"`
	Core       string `json:"core_version"`
	Prerelease bool   `json:"prerelease"`
}

func releaseMetadataForTag(tag string) (releaseMetadata, error) {
	tag = strings.TrimSpace(tag)
	if !strings.HasPrefix(tag, "v") {
		return releaseMetadata{}, errors.New("release tag must start with v")
	}
	normalized, ok := releaseversion.Normalize(tag)
	if !ok || normalized != tag {
		return releaseMetadata{}, fmt.Errorf("invalid release SemVer tag %q", tag)
	}
	core, _ := releaseversion.Core(normalized)
	return releaseMetadata{
		Tag:        normalized,
		Version:    strings.TrimPrefix(normalized, "v"),
		Core:       core,
		Prerelease: releaseversion.IsPrerelease(normalized),
	}, nil
}

func writeReleaseMetadata(tag string, stdout io.Writer) error {
	metadata, err := releaseMetadataForTag(tag)
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(metadata)
}

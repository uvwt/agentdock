//go:build linux

package desktopruntime

import (
	"context"
	"errors"
	"strings"
)

func unixCloudflaredComponentStatus(_ string, manifest unixRuntimeManifest) (path, state, version string) {
	return strings.TrimSpace(manifest.CloudflaredBinary), "", ""
}

func prepareUnixCloudflared(_ context.Context, _ string, manifest *unixRuntimeManifest) error {
	if manifest == nil || strings.TrimSpace(manifest.CloudflaredBinary) == "" {
		return errors.New("cloudflared binary is unavailable")
	}
	return nil
}

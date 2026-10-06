//go:build windows

package component

import (
	"context"
	"fmt"

	"github.com/uvwt/agentdock/internal/authenticode"
)

func verifyPlatformTrust(ctx context.Context, path string, _ bool) error {
	if err := authenticode.VerifyFile(ctx, path); err != nil {
		return fmt.Errorf("cloudflared Authenticode signature verification failed: %w", err)
	}
	return nil
}

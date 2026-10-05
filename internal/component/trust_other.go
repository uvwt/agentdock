//go:build !darwin && !windows

package component

import (
	"context"
	"errors"
)

func verifyPlatformTrust(context.Context, string, bool) error {
	return errors.New("cloudflared optional component is supported only on macOS and Windows")
}

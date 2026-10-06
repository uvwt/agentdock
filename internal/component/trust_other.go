//go:build !darwin && !windows && !linux

package component

import (
	"context"
	"errors"
)

func verifyPlatformTrust(context.Context, string, bool) error {
	return errors.New("cloudflared optional component is unsupported on this platform")
}

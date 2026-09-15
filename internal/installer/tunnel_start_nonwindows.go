//go:build !windows

package installer

import "errors"

func launchWindowsTunnelProxy(string) error {
	return errors.New("Windows Tunnel proxy is unavailable on this platform")
}

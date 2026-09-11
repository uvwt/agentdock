//go:build linux

package selfupdate

import "context"

func applyManagedDesktopOnlyUpdate(context.Context, applyRequest) (applyResult, bool, error) {
	return applyResult{}, false, nil
}

//go:build !windows

package selfupdate

import "context"

func HandleInternalCommand(context.Context, []string) (bool, error) {
	return false, nil
}

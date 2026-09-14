//go:build !windows

package desktopruntime

import (
	"context"
	"fmt"
)

func platformStartScheduledTask(context.Context, string, string) error {
	return fmt.Errorf("Windows Scheduled Task 仅支持 Windows")
}

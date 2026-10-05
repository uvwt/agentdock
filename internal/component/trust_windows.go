//go:build windows

package component

import (
	"context"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

func verifyPlatformTrust(ctx context.Context, path string, _ bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	pathUTF16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("encode cloudflared path for Authenticode verification: %w", err)
	}
	fileInfo := &windows.WinTrustFileInfo{
		Size:     uint32(unsafe.Sizeof(windows.WinTrustFileInfo{})),
		FilePath: pathUTF16,
	}
	trustData := &windows.WinTrustData{
		Size:                            uint32(unsafe.Sizeof(windows.WinTrustData{})),
		UIChoice:                        windows.WTD_UI_NONE,
		RevocationChecks:                windows.WTD_REVOKE_NONE,
		UnionChoice:                     windows.WTD_CHOICE_FILE,
		StateAction:                     windows.WTD_STATEACTION_VERIFY,
		FileOrCatalogOrBlobOrSgnrOrCert: unsafe.Pointer(fileInfo),
	}
	verifyErr := windows.WinVerifyTrustEx(
		windows.InvalidHWND,
		&windows.WINTRUST_ACTION_GENERIC_VERIFY_V2,
		trustData,
	)
	trustData.StateAction = windows.WTD_STATEACTION_CLOSE
	closeErr := windows.WinVerifyTrustEx(
		windows.InvalidHWND,
		&windows.WINTRUST_ACTION_GENERIC_VERIFY_V2,
		trustData,
	)
	if verifyErr != nil {
		if closeErr != nil {
			return fmt.Errorf(
				"cloudflared Authenticode signature verification failed: %w (close trust state: %v)",
				verifyErr,
				closeErr,
			)
		}
		return fmt.Errorf("cloudflared Authenticode signature verification failed: %w", verifyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close cloudflared Authenticode trust state: %w", closeErr)
	}
	return nil
}

//go:build windows

package atomicfile

import (
	"errors"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

var windowsReplaceMu sync.Mutex

func replaceFile(source, target string) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}

	// Windows can transiently reject a replace while another writer or ACL update
	// still holds the target. Serialize local writers and retry only sharing-related
	// errors so concurrent state persistence remains atomic without hiding real failures.
	windowsReplaceMu.Lock()
	defer windowsReplaceMu.Unlock()
	for attempt := 0; attempt < 16; attempt++ {
		err = windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
		if err == nil {
			return nil
		}
		if !errors.Is(err, windows.ERROR_ACCESS_DENIED) &&
			!errors.Is(err, windows.ERROR_SHARING_VIOLATION) &&
			!errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return err
		}
		time.Sleep(time.Duration(attempt+1) * 5 * time.Millisecond)
	}
	return err
}

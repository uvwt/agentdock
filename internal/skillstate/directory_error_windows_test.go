//go:build windows

package skillstate

import (
	"errors"
	"testing"

	"golang.org/x/sys/windows"
)

func TestIsTransientLockContention(t *testing.T) {
	for _, err := range []error{
		windows.ERROR_ACCESS_DENIED,
		windows.ERROR_SHARING_VIOLATION,
	} {
		if !isTransientLockContention(err) {
			t.Fatalf("isTransientLockContention(%v) = false", err)
		}
	}

	if isTransientLockContention(errors.New("permanent failure")) {
		t.Fatal("isTransientLockContention accepted an unrelated error")
	}
}

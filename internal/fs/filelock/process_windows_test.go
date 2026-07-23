//go:build windows

package filelock

import (
	"errors"
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

func TestRetryableLockCreationErrorOnWindows(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "exists", err: os.ErrExist, want: true},
		{name: "access denied while deleting", err: windows.ERROR_ACCESS_DENIED, want: true},
		{name: "sharing violation", err: windows.ERROR_SHARING_VIOLATION, want: true},
		{name: "invalid name", err: windows.ERROR_INVALID_NAME, want: false},
		{name: "unrelated", err: errors.New("unrelated"), want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := retryableLockCreationError(test.err); got != test.want {
				t.Fatalf("retryableLockCreationError(%v) = %v, want %v", test.err, got, test.want)
			}
		})
	}
}

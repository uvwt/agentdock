//go:build windows

package desktopruntime

import "testing"

func TestTaskSessionProceduresResolve(t *testing.T) {
	for _, test := range []struct {
		name string
		find func() error
	}{
		{name: "WTSEnumerateSessionsW", find: procWTSEnumerateSessionsW.Find},
		{name: "WTSQuerySessionInformationW", find: procWTSQuerySessionInformationW.Find},
		{name: "WTSFreeMemory", find: procWTSFreeMemory.Find},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.find(); err != nil {
				t.Fatalf("resolve Windows session procedure %s: %v", test.name, err)
			}
		})
	}
}

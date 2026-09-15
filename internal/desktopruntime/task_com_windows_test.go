//go:build windows

package desktopruntime

import (
	"testing"
	"unsafe"
)

func TestTaskCOMProceduresResolve(t *testing.T) {
	for _, test := range []struct {
		name string
		find func() error
	}{
		{name: "CoInitializeEx", find: procCoInitializeEx.Find},
		{name: "CoUninitialize", find: procCoUninitialize.Find},
		{name: "CoCreateInstance", find: procCoCreateInstance.Find},
		{name: "CLSIDFromProgID", find: procCLSIDFromProgID.Find},
		{name: "SysAllocString", find: procSysAllocString.Find},
		{name: "SysFreeString", find: procSysFreeString.Find},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.find(); err != nil {
				t.Fatalf("resolve Windows COM procedure %s: %v", test.name, err)
			}
		})
	}
}

func TestVariantMatchesWindows64BitABI(t *testing.T) {
	if got := unsafe.Sizeof(variant{}); got != 24 {
		t.Fatalf("VARIANT size = %d, want 24 bytes on 64-bit Windows", got)
	}
	if got := unsafe.Offsetof(variant{}.Val); got != 8 {
		t.Fatalf("VARIANT value union offset = %d, want 8", got)
	}
}

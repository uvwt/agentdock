//go:build windows

package desktopruntime

import "testing"

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

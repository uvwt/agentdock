package skillruntime

import "testing"

func TestValidateRelativePackagePathRejectsCrossPlatformAbsoluteForms(t *testing.T) {
	for _, value := range []string{
		"/absolute/run.ps1",
		"C:/absolute/run.ps1",
		`C:\absolute\run.ps1`,
		`..\escape.ps1`,
		"../escape.ps1",
	} {
		t.Run(value, func(t *testing.T) {
			if err := validateRelativePackagePath(value); err == nil {
				t.Fatalf("validateRelativePackagePath(%q) accepted unsafe path", value)
			}
		})
	}
}

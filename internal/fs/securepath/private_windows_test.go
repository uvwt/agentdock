//go:build windows

package securepath

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestEnsurePrivateRecognizesEquivalentWindowsDACL(t *testing.T) {
	root := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := EnsurePrivate(root); err != nil {
		t.Fatalf("secure directory: %v", err)
	}

	desired := testPrivateDACL(t, true)
	if !privateDACLMatches(root, desired) {
		t.Fatal("privateDACLMatches() = false after EnsurePrivate; repeated startup would rewrite the directory DACL")
	}
	if err := EnsurePrivate(root); err != nil {
		t.Fatalf("secure already-private directory: %v", err)
	}
}

func TestPrivateDACLComparisonIgnoresCanonicalACEOrder(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatalf("read current user: %v", err)
	}
	userSID := user.User.Sid.String()

	first := testDACLFromSDDL(t, fmt.Sprintf("D:P(A;OICI;FA;;;%s)(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)", userSID))
	second := testDACLFromSDDL(t, fmt.Sprintf("D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FA;;;%s)", userSID))
	if !sameDACL(first, second) {
		t.Fatal("sameDACL() = false for equivalent ACE sets with different canonical order")
	}

	extra := testDACLFromSDDL(t, fmt.Sprintf("D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FA;;;%s)(A;OICI;FR;;;WD)", userSID))
	if sameDACL(first, extra) {
		t.Fatal("sameDACL() = true for DACL with an extra Everyone ACE")
	}
}

func TestPrivateDACLComparisonTreatsDuplicateEquivalentACEsAsSame(t *testing.T) {
	withDuplicate := testDACLFromSDDL(t, "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)")
	withoutDuplicate := testDACLFromSDDL(t, "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)")

	if !sameDACL(withDuplicate, withoutDuplicate) {
		t.Fatal("sameDACL() = false when the only difference is a duplicate equivalent ACE")
	}
}

func testPrivateDACL(t *testing.T, directory bool) *windows.ACL {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatalf("read current user: %v", err)
	}
	inheritance := ""
	if directory {
		inheritance = "OICI"
	}
	return testDACLFromSDDL(t, fmt.Sprintf(
		"D:P(A;%s;FA;;;%s)(A;%s;FA;;;SY)(A;%s;FA;;;BA)",
		inheritance,
		user.User.Sid.String(),
		inheritance,
		inheritance,
	))
}

func testDACLFromSDDL(t *testing.T, sddl string) *windows.ACL {
	t.Helper()
	descriptor, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Fatalf("build security descriptor: %v", err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		t.Fatalf("read DACL: %v", err)
	}
	return dacl
}

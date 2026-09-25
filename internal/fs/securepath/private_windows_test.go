//go:build windows

package securepath

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

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

func TestEnsurePrivateRepairsUnsafeWindowsDACL(t *testing.T) {
	root := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}

	unsafeDACL := testDACLFromSDDL(t, "D:P(A;OICI;FA;;;WD)")
	if err := windows.SetNamedSecurityInfo(
		root,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		unsafeDACL,
		nil,
	); err != nil {
		t.Fatalf("install unsafe DACL: %v", err)
	}

	desired := testPrivateDACL(t, true)
	if privateDACLMatches(root, desired) {
		t.Fatal("privateDACLMatches() = true before unsafe DACL was repaired")
	}
	if err := EnsurePrivate(root); err != nil {
		t.Fatalf("repair unsafe directory DACL: %v", err)
	}
	if !privateDACLMatches(root, desired) {
		t.Fatal("privateDACLMatches() = false after EnsurePrivate repaired the DACL")
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

	denied := testDACLFromSDDL(t, fmt.Sprintf("D:P(D;OICI;FR;;;WD)(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FA;;;%s)", userSID))
	if sameDACL(first, denied) {
		t.Fatal("sameDACL() = true for DACL containing a deny ACE")
	}

	differentFlags := testDACLFromSDDL(t, fmt.Sprintf("D:P(A;OICIIO;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FA;;;%s)", userSID))
	if sameDACL(first, differentFlags) {
		t.Fatal("sameDACL() = true for DACL with different inheritance flags")
	}

	differentMask := testDACLFromSDDL(t, fmt.Sprintf("D:P(A;OICI;FR;;;SY)(A;OICI;FA;;;BA)(A;OICI;FA;;;%s)", userSID))
	if sameDACL(first, differentMask) {
		t.Fatal("sameDACL() = true for DACL with different access mask")
	}
}

func TestPrivateDACLComparisonTreatsDuplicateEquivalentACEsAsSame(t *testing.T) {
	withDuplicate := testDACLFromSDDL(t, "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)")
	withoutDuplicate := testDACLFromSDDL(t, "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)")

	if !sameDACL(withDuplicate, withoutDuplicate) {
		t.Fatal("sameDACL() = false when the only difference is a duplicate equivalent ACE")
	}
}

func TestPrivateDACLMatchesRequiresProtectedDACL(t *testing.T) {
	root := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	desired := testPrivateDACL(t, true)
	if err := windows.SetNamedSecurityInfo(
		root,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.UNPROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		desired,
		nil,
	); err != nil {
		t.Fatalf("install unprotected DACL: %v", err)
	}
	if privateDACLMatches(root, desired) {
		t.Fatal("privateDACLMatches() = true for an unprotected DACL")
	}
}

func TestReadPrivateACERejectsTruncatedSID(t *testing.T) {
	dacl := testPrivateDACL(t, true)
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil {
		t.Fatalf("read first ACE: %v", err)
	}
	ace.Header.AceSize = uint16(unsafe.Offsetof(ace.SidStart) + 4)
	if _, ok := readPrivateACE(dacl, 0); ok {
		t.Fatal("readPrivateACE() accepted an ACE too small for a SID header")
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

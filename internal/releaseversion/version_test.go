package releaseversion

import "testing"

func TestCompareFollowsSemVerPrereleaseOrder(t *testing.T) {
	ordered := []string{
		"1.0.0-beta.1",
		"1.0.0-beta.2",
		"1.0.0-rc.1",
		"1.0.0",
		"1.1.0-beta.1",
		"1.1.0",
	}
	for index := 0; index < len(ordered)-1; index++ {
		if comparison, ok := Compare(ordered[index], ordered[index+1]); !ok || comparison >= 0 {
			t.Fatalf("Compare(%q, %q) = %d, %t; want lower", ordered[index], ordered[index+1], comparison, ok)
		}
	}
}

func TestCoreAndPrerelease(t *testing.T) {
	if got, ok := Core("v1.2.3-rc.4"); !ok || got != "1.2.3" {
		t.Fatalf("Core() = %q, %t", got, ok)
	}
	if !IsPrerelease("1.2.3-rc.4") {
		t.Fatal("expected prerelease")
	}
	if IsPrerelease("1.2.3") {
		t.Fatal("stable version reported as prerelease")
	}
	if _, ok := Normalize("1.2"); ok {
		t.Fatal("invalid SemVer unexpectedly accepted")
	}
}

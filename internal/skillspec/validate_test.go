package skillspec

import (
	"strings"
	"testing"
)

func TestValidateNameAgentSkillsBoundaries(t *testing.T) {
	for _, valid := range []string{"a", "1", "demo-skill", strings.Repeat("a", 64)} {
		if err := ValidateName(valid); err != nil {
			t.Fatalf("ValidateName(%q) = %v", valid, err)
		}
	}
	for _, invalid := range []string{
		"", "-a", "a-", "a--b", "A", "a_b", " a", "a ", strings.Repeat("a", 65),
	} {
		if err := ValidateName(invalid); err == nil {
			t.Fatalf("ValidateName(%q) unexpectedly succeeded", invalid)
		}
	}
}

func TestValidateDescriptionBoundaries(t *testing.T) {
	if err := ValidateDescription(strings.Repeat("x", 1025)); err == nil {
		t.Fatal("oversized description was accepted")
	}
	if err := ValidateDescription("Demo"); err != nil {
		t.Fatalf("valid description rejected: %v", err)
	}
}

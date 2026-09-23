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

func TestNormalizeOfficialFrontmatterTypes(t *testing.T) {
	if _, err := NormalizeMetadata(map[string]any{"owner": 42}); err == nil {
		t.Fatal("numeric metadata value was accepted")
	}
	if _, err := NormalizeAllowedTools([]any{"exec_command"}); err == nil {
		t.Fatal("allowed-tools array was accepted")
	}
	if got, err := NormalizeAllowedTools("exec_command read_file"); err != nil || got != "exec_command read_file" {
		t.Fatalf("allowed-tools string = %q err=%v", got, err)
	}
	if err := ValidateDescription(strings.Repeat("x", 1025)); err == nil {
		t.Fatal("oversized description was accepted")
	}
	if err := ValidateCompatibility(strings.Repeat("x", 501)); err == nil {
		t.Fatal("oversized compatibility was accepted")
	}
}

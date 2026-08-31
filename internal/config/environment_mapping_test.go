package config

import (
	"fmt"
	"strings"
	"testing"
)

func TestValidateEnvironmentMapping(t *testing.T) {
	if err := validateEnvironmentMapping(map[string]string{
		"NIX_LD":      "NIX_LD",
		"CHILD_TOKEN": "HOST_TOKEN_2",
	}); err != nil {
		t.Fatalf("valid environment mapping rejected: %v", err)
	}

	tooMany := make(map[string]string, 65)
	for i := 0; i < 65; i++ {
		key := fmt.Sprintf("KEY_%d", i)
		tooMany[key] = key
	}
	if err := validateEnvironmentMapping(tooMany); err == nil || !strings.Contains(err.Error(), "maximum is 64") {
		t.Fatalf("too many mappings error = %v", err)
	}
}

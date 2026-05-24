package tools

import (
	"strings"
	"testing"
)

func TestRedactSecrets(t *testing.T) {
	input := "password=abc github_pat_123456 https://user:token@github.com/owner/repo.git"
	got := redactSecrets(input, nil)
	for _, leaked := range []string{"abc", "github_pat_123456", "token@github.com"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("redacted output leaked %q: %s", leaked, got)
		}
	}
}

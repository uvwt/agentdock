package agentinstructions

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsCaseInsensitiveDefaultBoundary(t *testing.T) {
	root := filepath.Join(t.TempDir(), "MixedCaseProject")
	child := filepath.Join(root, "src")
	writeGuidance(t, root, "root rules")
	writeGuidance(t, child, "child rules")
	snapshot := loadGuidance(t, Options{DefaultDir: strings.ToUpper(root), Workdir: child})
	if got := strings.Join(loadedContents(snapshot), "|"); got != "root rules|child rules" {
		t.Fatalf("case-variant default directory lost ancestor instructions: %q", got)
	}
}

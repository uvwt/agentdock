package commandqueue

import (
	"strings"
	"testing"
)

func TestDefaultAllowedCommandTypesDoNotIncludeRemovedArtifactCommands(t *testing.T) {
	for _, commandType := range DefaultAllowedCommandTypes() {
		if strings.HasPrefix(commandType, "artifact.") {
			t.Fatalf("removed artifact command type is still allowed: %s", commandType)
		}
	}
}

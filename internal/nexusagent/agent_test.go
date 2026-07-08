package nexusagent

import "testing"

func TestEnvDefinitionsWithoutSkillStateDoesNotInferLegacyCompatVariables(t *testing.T) {
	if definitions := envDefinitions(nil); len(definitions) != 0 {
		t.Fatalf("unexpected inferred env definitions: %#v", definitions)
	}
}

package skill

import (
	"strings"
	"testing"
)

func TestParseSkillDocumentAcceptsAgentSkillsFieldsWithoutVersion(t *testing.T) {
	doc, err := ParseSkillDocument([]byte(`---
name: demo-skill
description: Use this Skill for a demo workflow.
license: Apache-2.0
compatibility:
  requires: git
metadata:
  version: 1.2
  owner: example
allowed-tools:
  - exec_command
  - read_file
vendor-field:
  nested: true
---

# Demo Skill

Use existing tools to complete the workflow.
`))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Name != "demo-skill" || doc.Description != "Use this Skill for a demo workflow." {
		t.Fatalf("unexpected identity: %#v", doc)
	}
	if !strings.Contains(doc.Body, "Demo Skill") {
		t.Fatalf("markdown body missing: %#v", doc)
	}
}

func TestParseSkillDocumentIgnoresTopLevelVersionAsInstallIdentity(t *testing.T) {
	doc, err := ParseSkillDocument([]byte(`---
name: demo-skill
description: Demo.
version: 9.9.9
---

# Demo
`))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Name != "demo-skill" || doc.Description != "Demo." {
		t.Fatalf("unexpected document: %#v", doc)
	}
}

func TestParseSkillDocumentSupportsFoldedDescription(t *testing.T) {
	doc, err := ParseSkillDocument([]byte(`---
name: folded-skill
description: >
  First sentence.
  Second sentence.
---

# Folded
`))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Description != "First sentence. Second sentence." {
		t.Fatalf("description = %q", doc.Description)
	}
}

func TestParseSkillDocumentRejectsMissingRequiredFieldsOrBody(t *testing.T) {
	for name, document := range map[string]string{
		"missing name": `---
description: Demo.
---

# Demo
`,
		"missing description": `---
name: demo-skill
---

# Demo
`,
		"missing body": `---
name: demo-skill
description: Demo.
---
`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseSkillDocument([]byte(document)); err == nil {
				t.Fatal("ParseSkillDocument() succeeded for invalid document")
			}
		})
	}
}

func TestParseSkillMetadataAcceptsCommonSkillWithoutVersion(t *testing.T) {
	metadata, err := ParseSkillMetadata([]byte(`---
name: common-skill
description: Common Agent Skill without package version.
---

# Common Skill
`))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Name != "common-skill" || metadata.Description != "Common Agent Skill without package version." {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}
}

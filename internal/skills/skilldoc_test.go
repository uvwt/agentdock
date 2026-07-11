package skills

import (
	"strings"
	"testing"
)

func TestParseSkillDocumentRequiresNameDescriptionVersionAndBody(t *testing.T) {
	doc, err := ParseSkillDocument([]byte(`---
name: demo-skill
description: Use this Skill for a demo workflow.
version: 1.2.3
---

# Demo Skill

Use existing tools to complete the workflow.
`))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Name != "demo-skill" || doc.Version != "1.2.3" || !strings.Contains(doc.Body, "Demo Skill") {
		t.Fatalf("unexpected document: %#v", doc)
	}
}

func TestParseSkillDocumentRejectsMissingVersion(t *testing.T) {
	_, err := ParseSkillDocument([]byte(`---
name: demo-skill
description: Demo.
---

# Demo
`))
	if err == nil || !strings.Contains(err.Error(), "version is required") {
		t.Fatalf("expected version error, got %v", err)
	}
}

func TestParseSkillDocumentSupportsFoldedDescription(t *testing.T) {
	doc, err := ParseSkillDocument([]byte(`---
name: folded-skill
description: >
  First sentence.
  Second sentence.
version: 1.0.0
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

func TestParseSkillDocumentRejectsEmptyBlockDescription(t *testing.T) {
	_, err := ParseSkillDocument([]byte(`---
name: empty-description
description: |
version: 1.0.0
---

# Empty
`))
	if err == nil || !strings.Contains(err.Error(), "description is required") {
		t.Fatalf("expected empty description error, got %v", err)
	}
}

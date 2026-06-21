package skillruntime

import (
	"strings"
	"testing"
)

func TestParseSkillDocumentRequiresFrontmatterAndBody(t *testing.T) {
	doc, err := ParseSkillDocument([]byte(`---
name: demo-skill
description: Use this skill when a demo operation is needed.
---

# Demo Skill

Follow the documented workflow.
`))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Name != "demo-skill" || doc.Description == "" || !strings.Contains(doc.Body, "Demo Skill") {
		t.Fatalf("unexpected skill doc: %#v", doc)
	}
}

func TestParseSkillDocumentRejectsMissingFields(t *testing.T) {
	_, err := ParseSkillDocument([]byte(`---
title: Demo
---
`))
	if err == nil {
		t.Fatal("expected invalid SKILL.md error")
	}
	message := err.Error()
	for _, want := range []string{"name is required", "description is required", "markdown body is required"} {
		if !strings.Contains(message, want) {
			t.Fatalf("error %q does not contain %q", message, want)
		}
	}
}

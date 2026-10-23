package contract

import (
	"encoding/json"
	"testing"
)

func TestRegressionConstObjectIsPreserved(t *testing.T) {
	literal := map[string]any{"type": "string", "nullable": true}
	for _, keyword := range []string{"const", "enum"} {
		t.Run(keyword, func(t *testing.T) {
			var value any = literal
			if keyword == "enum" {
				value = []map[string]any{literal}
			}
			v, err := CompileInputSchema(map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{keyword: value}}})
			if err != nil {
				t.Fatal(err)
			}
			if err := v.Validate(map[string]any{"value": literal}); err != nil {
				t.Fatalf("original literal object rejected: %v", err)
			}
			if err := v.Validate(map[string]any{"value": map[string]any{"type": []any{"string", "null"}}}); err == nil {
				t.Fatal("rewritten object accepted instead of literal")
			}
		})
	}
}

func TestNullableNestedTypedSchemaPreservesInput(t *testing.T) {
	raw := map[string]any{"type": "object", "properties": map[string]any{
		"value": map[string]any{"oneOf": []map[string]any{
			{"type": "string", "nullable": true}, {"type": "integer"},
		}},
	}}
	before, _ := json.Marshal(raw)
	v, err := CompileInputSchema(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{nil, "text", 42} {
		if err := v.Validate(map[string]any{"value": value}); err != nil {
			t.Fatal(err)
		}
	}
	if err := v.Validate(map[string]any{"value": true}); err == nil {
		t.Fatal("boolean unexpectedly accepted")
	}
	after, _ := json.Marshal(raw)
	if string(before) != string(after) {
		t.Fatal("compilation modified caller's schema")
	}
}

func TestRegressionSchemaPropertyNamedNullable(t *testing.T) {
	v, err := CompileInputSchema(map[string]any{"type": "object", "properties": map[string]any{"nullable": true, "type": map[string]any{"type": "integer"}}})
	if err != nil {
		t.Fatalf("valid property map rejected: %v", err)
	}
	if err := v.Validate(map[string]any{"nullable": "any value", "type": 3}); err != nil {
		t.Fatal(err)
	}
}

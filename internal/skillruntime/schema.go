package skillruntime

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
)

// ValidateJSONSchemaDocument validates the deterministic JSON Schema subset
// enforced by the local runtime. Unsupported keywords are ignored by design;
// type, required, properties, additionalProperties, items and enum are enforced.
func ValidateJSONSchemaDocument(schema map[string]any) error {
	if len(schema) == 0 {
		return errors.New("schema must not be empty")
	}
	typeValue, ok := schema["type"].(string)
	if !ok || typeValue == "" {
		return errors.New("schema root must declare string type")
	}
	return nil
}

func ValidateJSON(schema map[string]any, raw []byte) error {
	if err := ValidateJSONSchemaDocument(schema); err != nil {
		return err
	}
	var value any
	if len(raw) == 0 {
		raw = []byte("null")
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return validateSchemaValue(schema, value, "$")
}

func validateSchemaValue(schema map[string]any, value any, path string) error {
	typeName, _ := schema["type"].(string)
	if !matchesJSONType(typeName, value) {
		return fmt.Errorf("%s must be %s", path, typeName)
	}
	if enumValues, ok := schema["enum"].([]any); ok {
		matched := false
		for _, allowed := range enumValues {
			if reflect.DeepEqual(value, allowed) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%s is not an allowed enum value", path)
		}
	}
	switch typed := value.(type) {
	case map[string]any:
		properties, _ := schema["properties"].(map[string]any)
		for _, required := range toStringSlice(schema["required"]) {
			if _, ok := typed[required]; !ok {
				return fmt.Errorf("%s.%s is required", path, required)
			}
		}
		additionalAllowed := true
		if flag, ok := schema["additionalProperties"].(bool); ok {
			additionalAllowed = flag
		}
		for key, child := range typed {
			childSchema, exists := properties[key].(map[string]any)
			if !exists {
				if !additionalAllowed {
					return fmt.Errorf("%s.%s is not allowed", path, key)
				}
				continue
			}
			if err := validateSchemaValue(childSchema, child, path+"."+key); err != nil {
				return err
			}
		}
	case []any:
		if itemSchema, ok := schema["items"].(map[string]any); ok {
			for i, child := range typed {
				if err := validateSchemaValue(itemSchema, child, fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func matchesJSONType(typeName string, value any) bool {
	switch typeName {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "number":
		_, ok := value.(float64)
		return ok
	case "integer":
		number, ok := value.(float64)
		return ok && number == float64(int64(number))
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "null":
		return value == nil
	default:
		return false
	}
}

func toStringSlice(value any) []string {
	items, _ := value.([]any)
	result := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

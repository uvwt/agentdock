package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
)

// Dynamic MCP accepts the deterministic JSON Schema subset needed by common MCP
// tool inputs. Unknown annotation keywords stay forward-compatible, while the
// structural keywords below are enforced before forwarding a tools/call request.
func validateToolInputSchema(schema map[string]any) error {
	if len(schema) == 0 {
		return fmt.Errorf("inputSchema must not be empty")
	}
	if !schemaAllowsType(schema["type"], "object") {
		return fmt.Errorf("inputSchema root must allow object")
	}
	return nil
}

func validateToolArguments(tool Tool, arguments map[string]any) error {
	if arguments == nil {
		arguments = map[string]any{}
	}
	if err := validateToolInputSchema(tool.InputSchema); err != nil {
		return newError(
			"MCP_SCHEMA_INVALID",
			"upstream MCP tool returned an invalid input schema",
			false,
			map[string]any{"tool": tool.Name, "reason": err.Error()},
			err,
		)
	}

	// Normalize integers and other Go-native numeric values through JSON so the
	// validator observes the same data model the upstream MCP server receives.
	raw, err := json.Marshal(arguments)
	if err != nil {
		return newError("MCP_ARGUMENT_INVALID", "encode MCP tool arguments", false, map[string]any{"tool": tool.Name}, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var normalized any
	if err := decoder.Decode(&normalized); err != nil {
		return newError("MCP_ARGUMENT_INVALID", "decode MCP tool arguments", false, map[string]any{"tool": tool.Name}, err)
	}
	if err := validateSchemaValue(tool.InputSchema, normalized, "$"); err != nil {
		return newError(
			"MCP_ARGUMENT_INVALID",
			"MCP tool arguments do not match the discovered input schema",
			false,
			map[string]any{"tool": tool.Name, "reason": err.Error()},
			err,
		)
	}
	return nil
}

func validateSchemaValue(schema map[string]any, value any, path string) error {
	if nullable, _ := schema["nullable"].(bool); nullable && value == nil {
		return nil
	}
	if rawType, exists := schema["type"]; exists && !schemaMatchesType(rawType, value) {
		return fmt.Errorf("%s must be %s", path, schemaTypeLabel(rawType))
	}
	if enumValues, ok := schema["enum"].([]any); ok && !matchesEnum(value, enumValues) {
		return fmt.Errorf("%s is not an allowed enum value", path)
	}
	if constValue, exists := schema["const"]; exists && !jsonEquivalent(value, constValue) {
		return fmt.Errorf("%s does not match const", path)
	}
	if variants, ok := schema["allOf"].([]any); ok {
		for _, raw := range variants {
			variant, ok := raw.(map[string]any)
			if ok {
				if err := validateSchemaValue(variant, value, path); err != nil {
					return err
				}
			}
		}
	}
	if variants, ok := schema["anyOf"].([]any); ok && !matchesAnyVariant(variants, value, path) {
		return fmt.Errorf("%s does not match anyOf", path)
	}
	if variants, ok := schema["oneOf"].([]any); ok {
		matches := 0
		for _, raw := range variants {
			variant, ok := raw.(map[string]any)
			if ok && validateSchemaValue(variant, value, path) == nil {
				matches++
			}
		}
		if matches != 1 {
			return fmt.Errorf("%s must match exactly one oneOf schema", path)
		}
	}

	switch typed := value.(type) {
	case map[string]any:
		properties, _ := schema["properties"].(map[string]any)
		for _, required := range schemaStringSlice(schema["required"]) {
			if _, exists := typed[required]; !exists {
				return fmt.Errorf("%s.%s is required", path, required)
			}
		}
		additional := schema["additionalProperties"]
		for key, child := range typed {
			childSchema, exists := properties[key].(map[string]any)
			if exists {
				if err := validateSchemaValue(childSchema, child, path+"."+key); err != nil {
					return err
				}
				continue
			}
			switch rule := additional.(type) {
			case bool:
				if !rule {
					return fmt.Errorf("%s.%s is not allowed", path, key)
				}
			case map[string]any:
				if err := validateSchemaValue(rule, child, path+"."+key); err != nil {
					return err
				}
			}
		}
	case []any:
		if minimum, ok := schemaInteger(schema["minItems"]); ok && len(typed) < minimum {
			return fmt.Errorf("%s must contain at least %d items", path, minimum)
		}
		if maximum, ok := schemaInteger(schema["maxItems"]); ok && len(typed) > maximum {
			return fmt.Errorf("%s must contain at most %d items", path, maximum)
		}
		if itemSchema, ok := schema["items"].(map[string]any); ok {
			for index, child := range typed {
				if err := validateSchemaValue(itemSchema, child, fmt.Sprintf("%s[%d]", path, index)); err != nil {
					return err
				}
			}
		}
	case string:
		if minimum, ok := schemaInteger(schema["minLength"]); ok && len([]rune(typed)) < minimum {
			return fmt.Errorf("%s must contain at least %d characters", path, minimum)
		}
		if maximum, ok := schemaInteger(schema["maxLength"]); ok && len([]rune(typed)) > maximum {
			return fmt.Errorf("%s must contain at most %d characters", path, maximum)
		}
	case json.Number:
		number, err := typed.Float64()
		if err != nil {
			return fmt.Errorf("%s must be a valid number", path)
		}
		if minimum, ok := schemaNumber(schema["minimum"]); ok && number < minimum {
			return fmt.Errorf("%s must be at least %v", path, minimum)
		}
		if maximum, ok := schemaNumber(schema["maximum"]); ok && number > maximum {
			return fmt.Errorf("%s must be at most %v", path, maximum)
		}
	}
	return nil
}

func schemaMatchesType(rawType any, value any) bool {
	switch typed := rawType.(type) {
	case string:
		return matchesJSONType(typed, value)
	case []any:
		for _, item := range typed {
			name, _ := item.(string)
			if matchesJSONType(name, value) {
				return true
			}
		}
	case []string:
		for _, name := range typed {
			if matchesJSONType(name, value) {
				return true
			}
		}
	}
	return false
}

func schemaAllowsType(rawType any, expected string) bool {
	if rawType == nil {
		return expected == "object"
	}
	switch typed := rawType.(type) {
	case string:
		return typed == expected
	case []any:
		for _, item := range typed {
			if item == expected {
				return true
			}
		}
	case []string:
		for _, item := range typed {
			if item == expected {
				return true
			}
		}
	}
	return false
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
		_, ok := value.(json.Number)
		return ok
	case "integer":
		number, ok := value.(json.Number)
		if !ok {
			return false
		}
		if _, err := number.Int64(); err == nil {
			return true
		}
		parsed, err := number.Float64()
		return err == nil && !math.IsInf(parsed, 0) && !math.IsNaN(parsed) && math.Trunc(parsed) == parsed
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "null":
		return value == nil
	default:
		return false
	}
}

func matchesEnum(value any, enumValues []any) bool {
	for _, allowed := range enumValues {
		if jsonEquivalent(value, allowed) {
			return true
		}
	}
	return false
}

func jsonEquivalent(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	if leftErr == nil && rightErr == nil {
		return bytes.Equal(leftJSON, rightJSON)
	}
	return reflect.DeepEqual(left, right)
}

func matchesAnyVariant(variants []any, value any, path string) bool {
	for _, raw := range variants {
		variant, ok := raw.(map[string]any)
		if ok && validateSchemaValue(variant, value, path) == nil {
			return true
		}
	}
	return false
}

func schemaStringSlice(value any) []string {
	switch typed := value.(type) {
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				items = append(items, text)
			}
		}
		return items
	case []string:
		return append([]string(nil), typed...)
	default:
		return nil
	}
}

func schemaTypeLabel(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		return fmt.Sprint(typed)
	case []string:
		return fmt.Sprint(typed)
	default:
		return "the declared JSON type"
	}
}

func schemaInteger(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case float64:
		if math.Trunc(typed) == typed {
			return int(typed), true
		}
	case json.Number:
		parsed, err := strconv.Atoi(string(typed))
		return parsed, err == nil
	}
	return 0, false
}

func schemaNumber(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case float64:
		return typed, true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	}
	return 0, false
}

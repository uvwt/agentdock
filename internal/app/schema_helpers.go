package app

func schemaStringProperty(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func schemaIntegerProperty(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}

func schemaBoundedIntegerProperty(description string, minimum, maximum int) map[string]any {
	return map[string]any{"type": "integer", "description": description, "minimum": minimum, "maximum": maximum}
}

func schemaBooleanProperty(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

func schemaArrayObjectsProperty(description string) map[string]any {
	return map[string]any{"type": "array", "description": description, "items": map[string]any{"type": "object", "additionalProperties": true}}
}

func schemaArrayStringsProperty(description string) map[string]any {
	return map[string]any{"type": "array", "description": description, "items": map[string]any{"type": "string"}}
}

func schemaOpenObjectProperty(description string) map[string]any {
	return map[string]any{"type": "object", "description": description, "additionalProperties": true}
}

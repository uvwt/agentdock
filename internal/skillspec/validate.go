package skillspec

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	MaxNameLength          = 64
	MaxDescriptionLength   = 1024
	MaxCompatibilityLength = 500
)

func ValidateName(name string) error {
	if name != strings.TrimSpace(name) {
		return errors.New("Skill name cannot contain leading or trailing whitespace")
	}
	if len(name) < 1 || len(name) > MaxNameLength {
		return fmt.Errorf("Skill name must be 1-%d characters", MaxNameLength)
	}
	if name[0] == '-' || name[len(name)-1] == '-' || strings.Contains(name, "--") {
		return errors.New("Skill name cannot start/end with '-' or contain '--'")
	}
	for _, char := range name {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-' {
			continue
		}
		return errors.New("Skill name may contain only lowercase ASCII letters, digits, and hyphens")
	}
	return nil
}

func ValidateDescription(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("description is required")
	}
	if utf8.RuneCountInString(value) > MaxDescriptionLength {
		return fmt.Errorf("description exceeds %d characters", MaxDescriptionLength)
	}
	return nil
}

func ValidateCompatibility(value string) error {
	if utf8.RuneCountInString(value) > MaxCompatibilityLength {
		return fmt.Errorf("compatibility exceeds %d characters", MaxCompatibilityLength)
	}
	return nil
}

func NormalizeMetadata(value map[string]any) (map[string]string, error) {
	if value == nil {
		return nil, nil
	}
	out := make(map[string]string, len(value))
	for key, raw := range value {
		text, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("metadata.%s must be a string", key)
		}
		out[key] = text
	}
	return out, nil
}

func NormalizeAllowedTools(value any) (string, error) {
	if value == nil {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", errors.New("allowed-tools must be a space-separated string")
	}
	if strings.ContainsAny(text, "\r\n\t") {
		return "", errors.New("allowed-tools must be a single-line space-separated string")
	}
	return strings.TrimSpace(text), nil
}

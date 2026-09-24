package skillspec

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	MaxNameLength        = 64
	MaxDescriptionLength = 1024
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

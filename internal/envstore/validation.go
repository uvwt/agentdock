package envstore

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	scopeNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	envKeyPattern    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

func validateScope(scope Scope) error {
	if scope.Kind != ScopeSkill && scope.Kind != ScopeMCP {
		return fmt.Errorf("invalid environment scope kind %q", scope.Kind)
	}
	name := strings.TrimSpace(scope.Name)
	if name != scope.Name || !scopeNamePattern.MatchString(name) || filepath.Base(name) != name || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("invalid %s environment scope name %q", scope.Kind, scope.Name)
	}
	return nil
}

func ValidateKey(key string) error {
	if !envKeyPattern.MatchString(key) {
		return fmt.Errorf("invalid environment variable name %q", key)
	}
	return nil
}

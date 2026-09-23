package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"

	semver "github.com/Masterminds/semver/v3"
)

var (
	envNamePattern    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	componentPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	headerNamePattern = regexp.MustCompile(`^[!#$%&'*+.^_` + "`" + `|~0-9A-Za-z-]+$`)
)

func ValidateName(name string) error {
	name = strings.TrimSpace(name)
	if len(name) < 1 || len(name) > 64 || !isLowerAlphaNumeric(name[0]) || !isLowerAlphaNumeric(name[len(name)-1]) {
		return errors.New("Plugin name must be 1-64 characters and start/end with a lowercase letter or digit")
	}
	if strings.Contains(name, "--") || strings.Contains(name, "..") {
		return errors.New("Plugin name cannot contain -- or ..")
	}
	for index := 0; index < len(name); index++ {
		char := name[index]
		if isLowerAlphaNumeric(char) || char == '-' || char == '.' {
			continue
		}
		return errors.New("Plugin name may contain only lowercase ASCII letters, digits, hyphens, and periods")
	}
	return nil
}

func isLowerAlphaNumeric(char byte) bool {
	return (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9')
}

func ValidateVersion(version string) error {
	version = strings.TrimSpace(version)
	if version == VersionLocal {
		return nil
	}
	parsed, err := semver.StrictNewVersion(version)
	if err != nil || parsed.String() != version {
		return fmt.Errorf("Plugin version must be valid canonical SemVer or %q", VersionLocal)
	}
	return nil
}

func validateComponentName(name string) error {
	if !componentPattern.MatchString(strings.TrimSpace(name)) {
		return fmt.Errorf("component name must match %s", componentPattern.String())
	}
	return nil
}

func validateEnvName(name string) error {
	if !envNamePattern.MatchString(strings.TrimSpace(name)) {
		return fmt.Errorf("invalid environment variable name %q", name)
	}
	return nil
}

func RuntimeMCPName(pluginName, componentName string) string {
	base := "plugin." + pluginName + "." + componentName
	if len(base) <= 64 && componentPattern.MatchString(base) {
		return base
	}
	sum := sha256.Sum256([]byte(pluginName + "\x00" + componentName))
	suffix := hex.EncodeToString(sum[:8])
	prefix := componentName
	if len(prefix) > 36 {
		prefix = prefix[:36]
	}
	prefix = strings.Trim(prefix, ".-_")
	if prefix == "" {
		prefix = "mcp"
	}
	return "plugin." + prefix + "." + suffix
}

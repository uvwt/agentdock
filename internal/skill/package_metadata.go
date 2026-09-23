package skill

import (
	"path/filepath"
	"strings"
)

// IsIgnoredPackageMetadataPath reports transport/OS metadata that is outside the
// semantic Skill/Plugin package content. These entries are intentionally
// excluded from package snapshots/digests so Finder or macOS ZIP metadata cannot
// mutate an installed package after review.
func IsIgnoredPackageMetadataPath(relative string) bool {
	relative = filepath.ToSlash(filepath.Clean(relative))
	if relative == "." || relative == "" {
		return false
	}
	for _, segment := range strings.Split(relative, "/") {
		switch {
		case segment == ".DS_Store":
			return true
		case strings.HasPrefix(segment, "._"):
			return true
		case segment == "__MACOSX":
			return true
		}
	}
	return false
}

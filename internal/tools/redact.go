package tools

import (
	"regexp"
	"strings"
)

var defaultSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]+`),
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9_]+`),
	regexp.MustCompile(`(?i)(password|token|secret)=([^\s&]+)`),
	regexp.MustCompile(`https://[^\s/@:]+:[^\s/@]+@github\.com`),
}

func redactSecrets(value string, extraPatterns []string) string {
	out := value
	for _, re := range defaultSecretPatterns {
		out = re.ReplaceAllStringFunc(out, func(match string) string {
			if strings.HasPrefix(strings.ToLower(match), "https://") {
				return "https://***@github.com"
			}
			if idx := strings.Index(match, "="); idx >= 0 {
				return match[:idx+1] + "***"
			}
			return "***"
		})
	}
	for _, pattern := range extraPatterns {
		if pattern == "" {
			continue
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			continue
		}
		out = re.ReplaceAllString(out, "***")
	}
	return out
}

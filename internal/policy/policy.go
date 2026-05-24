package policy

import (
	"regexp"
	"strings"
)

var (
	networkPattern     = regexp.MustCompile(`(?i)(https?://|\bcurl\b|\bwget\b|\bssh\b|\bscp\b|\bftp\b|\bnc\b|\bnetcat\b|socket\.|requests\.|urllib\.)`)
	destructivePattern = regexp.MustCompile(`(?i)(^|\s)(sudo|su|chmod\s+-R|chown\s+-R|mkfs|mount|umount|rm\s+-[^\s]*r[^\s]*f|rm\s+-[^\s]*f[^\s]*r)\b`)
	expansionPattern   = regexp.MustCompile("(`|\\$\\(|\\$\\{)")
)

type Decision struct {
	Allowed    bool
	Permission string
	Reason     string
	Command    string
}

func CheckCommand(command string, skipPermissions bool) Decision {
	compact := strings.Join(strings.Fields(command), " ")
	if skipPermissions {
		return Decision{Allowed: true, Command: compact}
	}
	if expansionPattern.MatchString(command) {
		return Decision{Allowed: false, Permission: "shell_expansion", Reason: "shell expansion requires permission", Command: compact}
	}
	if destructivePattern.MatchString(command) {
		return Decision{Allowed: false, Permission: "destructive_command", Reason: "destructive commands require permission", Command: compact}
	}
	trimmed := strings.TrimSpace(command)
	if networkPattern.MatchString(command) && !strings.HasPrefix(trimmed, "git ") {
		return Decision{Allowed: false, Permission: "network", Reason: "network-looking commands require permission", Command: compact}
	}
	return Decision{Allowed: true, Command: compact}
}


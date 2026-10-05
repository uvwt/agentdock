package releaseversion

import (
	"strings"

	"golang.org/x/mod/semver"
)

// Normalize 把产品版本规范成带 v 前缀的 SemVer。
// 返回 false 表示输入不是合法 SemVer；调用方不能再退化成字符串比较。
func Normalize(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if !strings.HasPrefix(value, "v") {
		value = "v" + value
	}
	if !semver.IsValid(value) {
		return "", false
	}
	core := strings.TrimPrefix(value, "v")
	if index := strings.IndexAny(core, "-+"); index >= 0 {
		core = core[:index]
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return "", false
	}
	for _, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return "", false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return "", false
			}
		}
	}
	return value, true
}

// Compare 按 SemVer 2.0.0 比较版本，包括 prerelease 优先级。
// 只有两侧都合法时 comparable 才为 true。
func Compare(left, right string) (comparison int, comparable bool) {
	left, leftOK := Normalize(left)
	right, rightOK := Normalize(right)
	if !leftOK || !rightOK {
		return 0, false
	}
	return semver.Compare(left, right), true
}

func Core(value string) (string, bool) {
	normalized, ok := Normalize(value)
	if !ok {
		return "", false
	}
	core := normalized
	if index := strings.IndexAny(core, "-+"); index >= 0 {
		core = core[:index]
	}
	return strings.TrimPrefix(core, "v"), true
}

func IsPrerelease(value string) bool {
	normalized, ok := Normalize(value)
	return ok && semver.Prerelease(normalized) != ""
}

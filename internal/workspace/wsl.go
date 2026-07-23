package workspace

import "strings"

// WindowsPathToWSL 把绝对 Windows 盘符路径转换为 WSL /mnt 路径。
func WindowsPathToWSL(raw string) (string, bool) {
	path := strings.TrimSpace(raw)
	if strings.HasPrefix(path, `\\?\`) {
		path = strings.TrimPrefix(path, `\\?\`)
	}
	if len(path) < 3 || path[1] != ':' || (path[2] != '\\' && path[2] != '/') {
		return "", false
	}
	drive := path[0]
	if (drive < 'A' || drive > 'Z') && (drive < 'a' || drive > 'z') {
		return "", false
	}
	rest := strings.ReplaceAll(path[2:], `\`, "/")
	rest = strings.TrimLeft(rest, "/")
	if rest == "" {
		return "/mnt/" + strings.ToLower(string(drive)), true
	}
	return "/mnt/" + strings.ToLower(string(drive)) + "/" + rest, true
}

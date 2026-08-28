//go:build windows

package command

import (
	"path/filepath"
	"strings"
)

// platformCommandEnvKeys 是 Windows 原生进程可靠运行所需的宿主环境基线。
// 这里只透传操作系统身份/路径信息；工具链加载路径、代理和凭据必须由 Skill env 或显式 env 提供。
func platformCommandEnvKeys() []string {
	return []string{
		"SYSTEMROOT",
		"WINDIR",
		"SYSTEMDRIVE",
		"COMSPEC",
		"PATHEXT",
		"WSLENV",
		"USERPROFILE",
		"HOMEDRIVE",
		"HOMEPATH",
		"APPDATA",
		"LOCALAPPDATA",
		"PROGRAMDATA",
		"PROGRAMFILES",
		"PROGRAMFILES(X86)",
		"PROGRAMW6432",
		"ALLUSERSPROFILE",
		"PROCESSOR_ARCHITECTURE",
		"PROCESSOR_ARCHITEW6432",
		"NUMBER_OF_PROCESSORS",
		"USERNAME",
		"OS",
	}
}

func repairPlatformCommandEnv(env map[string]string) {
	if env["SYSTEMDRIVE"] == "" {
		for _, key := range []string{"SYSTEMROOT", "WINDIR"} {
			if drive := windowsDriveFromPath(env[key]); drive != "" {
				env["SYSTEMDRIVE"] = drive
				break
			}
		}
	}
	if env["PROGRAMDATA"] == "" && env["SYSTEMDRIVE"] != "" {
		env["PROGRAMDATA"] = filepath.Join(env["SYSTEMDRIVE"]+`\`, "ProgramData")
	}
}

func configurePlatformCommandTempEnv(env map[string]string, tempDir string) {
	env["TEMP"] = tempDir
	env["TMP"] = tempDir
}

func setPlatformCommandEnv(env map[string]string, key, value string) {
	env[strings.ToUpper(key)] = value
}

func windowsDriveFromPath(path string) string {
	volume := filepath.VolumeName(strings.TrimSpace(path))
	if len(volume) == 2 && volume[1] == ':' {
		return strings.ToUpper(volume)
	}
	return ""
}

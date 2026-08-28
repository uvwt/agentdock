//go:build !windows

package command

func platformCommandEnvKeys() []string {
	return nil
}

func repairPlatformCommandEnv(_ map[string]string) {}

func configurePlatformCommandTempEnv(_ map[string]string, _ string) {}

func setPlatformCommandEnv(env map[string]string, key, value string) {
	env[key] = value
}

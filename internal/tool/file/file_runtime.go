package file

import "strings"

type fileRuntimeSelection struct {
	Runtime      string
	Distribution string
}

func (selection fileRuntimeSelection) isWSL() bool {
	return selection.Runtime == "wsl"
}

func addFileRuntimeResult(result Result, selection fileRuntimeSelection) Result {
	if result == nil || selection.Runtime == "" {
		return result
	}
	result["runtime"] = selection.Runtime
	if selection.Distribution != "" {
		result["wsl_distribution"] = selection.Distribution
	}
	return result
}

func normalizedFileRuntime(options RuntimeOptions, defaultRuntime string) (string, string) {
	runtimeName := strings.ToLower(strings.TrimSpace(options.Runtime))
	if runtimeName == "" {
		runtimeName = defaultRuntime
	}
	return runtimeName, strings.TrimSpace(options.WSLDistribution)
}

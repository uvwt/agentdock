package tools

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

func normalizedFileRuntime(args map[string]any, defaultRuntime string) (string, string) {
	runtimeName := strings.ToLower(strings.TrimSpace(stringArg(args, "runtime", defaultRuntime)))
	if runtimeName == "" {
		runtimeName = defaultRuntime
	}
	return runtimeName, strings.TrimSpace(stringArg(args, "wsl_distribution", ""))
}

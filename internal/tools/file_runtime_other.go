//go:build !windows

package tools

func selectFileRuntime(args map[string]any) (fileRuntimeSelection, error) {
	runtimeName, distribution := normalizedFileRuntime(args, "")
	if runtimeName != "" {
		return fileRuntimeSelection{}, toolError("INVALID_ARGUMENT", "runtime is only supported by AgentDock file tools on Windows", "validation")
	}
	if distribution != "" {
		return fileRuntimeSelection{}, toolError("INVALID_ARGUMENT", "wsl_distribution is only supported by AgentDock file tools on Windows", "validation")
	}
	return fileRuntimeSelection{}, nil
}

func addFileRuntimeProperties(_ map[string]any) {}

func addFileRuntimeOutputProperties(_ map[string]any) {}

func filePathDescription(hostDescription string) string { return hostDescription }

func fileToolDescription(base string) string { return base }

func fileEditToolDescription(base string) string { return base }

func filePatchDescription(base string) string { return base }

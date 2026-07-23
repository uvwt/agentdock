//go:build !windows

package file

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

func AddRuntimeProperties(_ map[string]any) {}

func AddRuntimeOutputProperties(_ map[string]any) {}

func PathDescription(hostDescription string) string { return hostDescription }

func ToolDescription(base string) string { return base }

func EditDescription(base string) string { return base }

func PatchDescription(base string) string { return base }

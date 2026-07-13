//go:build windows

package tools

func selectFileRuntime(args map[string]any) (fileRuntimeSelection, error) {
	runtimeName, distribution := normalizedFileRuntime(args, "windows")
	switch runtimeName {
	case "windows":
		if distribution != "" {
			return fileRuntimeSelection{}, toolError("INVALID_ARGUMENT", "wsl_distribution is only valid when runtime is wsl", "validation")
		}
		return fileRuntimeSelection{Runtime: "windows"}, nil
	case "wsl":
		return fileRuntimeSelection{Runtime: "wsl", Distribution: distribution}, nil
	default:
		return fileRuntimeSelection{}, toolErrorDetails(
			"INVALID_ARGUMENT",
			"runtime must be windows or wsl",
			"validation",
			map[string]any{"runtime": runtimeName},
		)
	}
}

func addFileRuntimeProperties(props map[string]any) {
	props["runtime"] = map[string]any{
		"type":        "string",
		"description": "File runtime on Windows. Defaults to windows; use wsl for native Linux filesystem semantics.",
		"enum":        []string{"windows", "wsl"},
	}
	props["wsl_distribution"] = map[string]any{
		"type":        "string",
		"description": "Optional WSL distribution name used only when runtime=wsl. Omit it to use the system default distribution.",
	}
}

func addFileRuntimeOutputProperties(props map[string]any) {
	props["runtime"] = map[string]any{"type": "string", "description": "File runtime used by the Windows host: windows or wsl."}
	props["wsl_distribution"] = map[string]any{"type": "string", "description": "WSL distribution used when runtime=wsl and a distribution was explicitly selected."}
}

func filePathDescription(hostDescription string) string {
	return hostDescription + " With runtime=wsl, use an absolute Linux path such as /home/user/project or /mnt/d/Project; Windows drive paths are converted automatically."
}

func fileToolDescription(base string) string {
	return base + " On Windows, runtime=wsl uses native Linux filesystem semantics through the selected WSL distribution."
}

func fileEditToolDescription(base string) string {
	return fileToolDescription(base) + " WSL writes require absolute paths and regular UTF-8 files, reject symlinks and protected system paths, and accept only structured envelope patches."
}

func filePatchDescription(base string) string {
	return base + " With runtime=wsl, patch must use the structured *** Begin Patch envelope and contain exactly one file operation per call."
}

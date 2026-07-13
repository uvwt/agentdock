package tools

func validateWSLPatchOperations(operations []patchOperation) error {
	if len(operations) == 1 {
		return nil
	}
	return toolErrorDetails(
		"WSL_PATCH_SINGLE_OPERATION_REQUIRED",
		"runtime=wsl patch accepts exactly one file operation per call so writes remain atomic and recoverable",
		"validation",
		map[string]any{"operations": len(operations)},
	)
}

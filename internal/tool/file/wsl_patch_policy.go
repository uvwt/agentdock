package file

func validateWSLPatchOperations(operations []patchOperation) error {
	if len(operations) == 0 {
		return toolError("PATCH_FAILED", "patch contains no file operations", "validation")
	}
	return nil
}

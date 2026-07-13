package tools

import (
	"errors"
	"testing"
)

func TestValidateWSLPatchOperationsRequiresSingleOperation(t *testing.T) {
	if err := validateWSLPatchOperations([]patchOperation{{Kind: "update", Path: "a.txt"}}); err != nil {
		t.Fatalf("single operation rejected: %v", err)
	}
	for _, operations := range [][]patchOperation{
		nil,
		{{Kind: "update", Path: "a.txt"}, {Kind: "update", Path: "b.txt"}},
	} {
		err := validateWSLPatchOperations(operations)
		var toolErr *ToolError
		if !errors.As(err, &toolErr) || toolErr.Code != "WSL_PATCH_SINGLE_OPERATION_REQUIRED" {
			t.Fatalf("operations=%d error=%#v", len(operations), err)
		}
	}
}

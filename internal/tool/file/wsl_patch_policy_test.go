package file

import "testing"

func TestValidateWSLPatchOperationsAllowsMultipleOperations(t *testing.T) {
	operations := []patchOperation{{Kind: "update", Path: "a.txt"}, {Kind: "update", Path: "b.txt"}}
	if err := validateWSLPatchOperations(operations); err != nil {
		t.Fatalf("multi-file operations rejected: %v", err)
	}
	if err := validateWSLPatchOperations(nil); err == nil {
		t.Fatal("empty operation list should fail")
	}
}

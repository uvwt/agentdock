//go:build windows

package main

import (
	"path/filepath"

	"github.com/uvwt/agentdock/internal/updateengine"
	"github.com/uvwt/agentdock/internal/updateplatform"
)

func newPlatformDriver(root string) (updateengine.Driver, error) {
	return updateplatform.NewWindowsDriver(root)
}

func expectedSourceArbiter(transaction updateengine.Transaction) string {
	if transaction.Windows == nil || transaction.Windows.SourceGeneration == "" {
		return ""
	}
	return filepath.Join(transaction.Windows.SourceGeneration, updateengine.GenerationArbiterName)
}

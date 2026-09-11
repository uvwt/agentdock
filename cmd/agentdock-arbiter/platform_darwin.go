//go:build darwin

package main

import (
	"github.com/uvwt/agentdock/internal/updateengine"
	"github.com/uvwt/agentdock/internal/updateplatform"
)

func newPlatformDriver(root string) (updateengine.Driver, error) {
	return updateplatform.NewDarwinDriver(root)
}

func expectedSourceArbiter(transaction updateengine.Transaction) string {
	if transaction.MacOS == nil {
		return ""
	}
	return transaction.MacOS.SourceArbiterPath
}

//go:build !windows && !darwin

package main

import (
	"errors"

	"github.com/uvwt/agentdock/internal/updateengine"
)

func newPlatformDriver(string) (updateengine.Driver, error) {
	return nil, errors.New("update arbiter is not supported on this platform")
}

func expectedSourceArbiter(updateengine.Transaction) string { return "" }

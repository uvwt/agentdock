//go:build !windows

package main

import (
	"context"
	"errors"
	"io"
)

func runInstallPrepareWindowsLegacy(_ context.Context, _ []string, _, _ io.Writer) error {
	return errors.New("prepare-windows-legacy 仅支持 Windows")
}

func runInstallDetachEngine(_ context.Context, _ []string, _, _ io.Writer) error {
	return errors.New("detach-engine 仅支持 Windows")
}

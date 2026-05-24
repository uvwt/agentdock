//go:build !linux

package sandbox

import (
	"os/exec"
)

type Status struct {
	Enabled  bool     `json:"enabled"`
	Warnings []string `json:"warnings,omitempty"`
}

func StatusForWorkspace(_ string) Status {
	return Status{Enabled: false, Warnings: []string{"Landlock is only available on Linux; command execution relies on external sandboxing."}}
}

func PrepareCommand(cmd *exec.Cmd, _ string) (func(), Status) {
	return func() {}, StatusForWorkspace("")
}

func ExecRestricted(_ string) error {
	return nil
}

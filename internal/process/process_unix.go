//go:build darwin || linux

package process

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
)

// Controller owns the operating-system process group created for one command.
type Controller struct {
	pid int
}

// Configure makes the child the leader of a dedicated process group before it starts.
func Configure(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// Attach records the process group after the command has started.
func Attach(cmd *exec.Cmd) (*Controller, error) {
	if cmd == nil || cmd.Process == nil {
		return nil, fmt.Errorf("attach process controller: command has not started")
	}
	return AttachPID(cmd.Process.Pid)
}

func AttachPID(pid int) (*Controller, error) {
	if pid <= 0 {
		return nil, fmt.Errorf("attach process controller: invalid pid %d", pid)
	}
	return &Controller{pid: pid}, nil
}

// Terminate stops the whole process tree represented by the process group.
func (c *Controller) Terminate() error {
	if c == nil || c.pid <= 0 {
		return nil
	}
	err := syscall.Kill(-c.pid, syscall.SIGKILL)
	// 进程可能在终止请求到达前已经自然退出；如果整个进程组已不存在，
	// shutdown 的目标已经达成，不应把 ESRCH 当成终止失败。
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

// Close releases platform resources. Unix process groups do not own handles.
func (c *Controller) Close() error { return nil }

//go:build darwin || linux

package process

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

const (
	processGroupTerminateAttempts = 5
	processGroupTerminateDelay    = time.Millisecond
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

	// killpg 只会向调用瞬间已经属于进程组的成员发信号。shell 若正处于 fork，
	// 新 child 可能在第一次 SIGKILL 的进程组快照之后出生并继续持有 stdout/stderr，
	// 让 cmd.Wait 长时间无法返回。第一次成功后做有界重试：只要还能命中可终止成员，
	// 就再给 fork 窗口一个调度周期；组消失后立即结束。
	for attempt := 0; attempt < processGroupTerminateAttempts; attempt++ {
		err := syscall.Kill(-c.pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		if err != nil {
			// Darwin 对只剩 zombie、已无可 signal 成员的进程组可能返回 EPERM。
			// 首次 kill 已成功时，这和 ESRCH 一样表示当前用户已无可继续终止的成员；
			// 首次就 EPERM 仍然保留为真实权限错误。
			if attempt > 0 && errors.Is(err, syscall.EPERM) {
				return nil
			}
			return err
		}
		if attempt+1 < processGroupTerminateAttempts {
			time.Sleep(processGroupTerminateDelay)
		}
	}
	return nil
}

// Close releases platform resources. Unix process groups do not own handles.
func (c *Controller) Close() error { return nil }

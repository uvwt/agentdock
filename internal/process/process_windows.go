//go:build windows

package process

import (
	"fmt"
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Controller owns a Windows Job Object. Closing the job terminates any descendants
// that outlive the direct child, which keeps command, Skill and browser process trees bounded.
type Controller struct {
	mu  sync.Mutex
	job windows.Handle
}

// Configure is intentionally empty. Job Object ownership is attached immediately
// after Start because os/exec does not expose the suspended primary thread handle.
func Configure(_ *exec.Cmd) {}

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
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create Windows Job Object: %w", err)
	}
	closeJob := true
	defer func() {
		if closeJob {
			_ = windows.CloseHandle(job)
		}
	}()

	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)),
		uint32(unsafe.Sizeof(limits)),
	); err != nil {
		return nil, fmt.Errorf("configure Windows Job Object: %w", err)
	}

	process, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(pid),
	)
	if err != nil {
		return nil, fmt.Errorf("open child process for Job Object: %w", err)
	}
	defer windows.CloseHandle(process)
	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		return nil, fmt.Errorf("assign child process to Job Object: %w", err)
	}
	closeJob = false
	return &Controller{job: job}, nil
}

func (c *Controller) Terminate() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.job == 0 {
		return nil
	}
	if err := windows.TerminateJobObject(c.job, 1); err != nil {
		return fmt.Errorf("terminate Windows Job Object: %w", err)
	}
	return nil
}

func (c *Controller) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.job == 0 {
		return nil
	}
	err := windows.CloseHandle(c.job)
	c.job = 0
	if err != nil {
		return fmt.Errorf("close Windows Job Object: %w", err)
	}
	return nil
}

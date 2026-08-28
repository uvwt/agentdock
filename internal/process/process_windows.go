//go:build windows

package process

import (
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Controller owns a Windows Job Object. Closing the job terminates any descendants
// that outlive the direct child, which keeps command, Skill and browser process trees bounded.
type Controller struct {
	mu           sync.Mutex
	job          windows.Handle
	pid          uint32
	terminated   bool
	terminateErr error
}

// Configure 在后台子进程启动前禁用控制台窗口，同时保留调用方已有的创建标志。
// 标准命令、WSL 文件辅助进程和 stdio MCP 通过该入口启动；ACP 为兼容 Windows sandbox 使用独立启动策略。
func Configure(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
}

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
	return &Controller{job: job, pid: uint32(pid)}, nil
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
	if c.terminated {
		return c.terminateErr
	}
	c.terminated = true

	// PowerShell 和部分 launcher 在嵌套 Job 环境中启动 native child 时，
	// 后代进程不一定会进入 AgentDock 新建的 Job。先按父子关系固定这些进程的
	// handle，再终止 Job 中的 wrapper，避免 wrapper 退出后子进程重新挂接而丢失。
	descendants, snapshotErr := descendantProcessIDs(c.pid)
	handles, openErr := openProcessesForTermination(descendants)
	defer closeProcessHandles(handles)

	var terminateErr error
	if err := windows.TerminateJobObject(c.job, 1); err != nil {
		terminateErr = fmt.Errorf("terminate Windows Job Object: %w", err)
	}
	if err := terminateProcessHandles(handles); err != nil {
		terminateErr = errors.Join(terminateErr, err)
	}
	c.terminateErr = errors.Join(snapshotErr, openErr, terminateErr)
	return c.terminateErr
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

type processHandle struct {
	pid    uint32
	handle windows.Handle
}

func descendantProcessIDs(root uint32) ([]uint32, error) {
	if root == 0 {
		return nil, nil
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("snapshot Windows process tree: %w", err)
	}
	defer windows.CloseHandle(snapshot)

	children := make(map[uint32][]uint32)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
			return nil, nil
		}
		return nil, fmt.Errorf("read Windows process snapshot: %w", err)
	}
	for {
		children[entry.ParentProcessID] = append(children[entry.ParentProcessID], entry.ProcessID)
		entry.Size = uint32(unsafe.Sizeof(windows.ProcessEntry32{}))
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				break
			}
			return nil, fmt.Errorf("read Windows process snapshot: %w", err)
		}
	}

	// 后序遍历让更深的后代先终止；visited 同时防御异常父子环。
	visited := map[uint32]bool{root: true}
	result := make([]uint32, 0)
	var visit func(uint32)
	visit = func(parent uint32) {
		for _, child := range children[parent] {
			if child == 0 || visited[child] {
				continue
			}
			visited[child] = true
			visit(child)
			result = append(result, child)
		}
	}
	visit(root)
	return result, nil
}

func openProcessesForTermination(pids []uint32) ([]processHandle, error) {
	handles := make([]processHandle, 0, len(pids))
	var result error
	for _, pid := range pids {
		handle, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, pid)
		if err != nil {
			// 进程可能在快照和 OpenProcess 之间正常退出。
			if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
				continue
			}
			result = errors.Join(result, fmt.Errorf("open descendant process %d: %w", pid, err))
			continue
		}
		handles = append(handles, processHandle{pid: pid, handle: handle})
	}
	return handles, result
}

func terminateProcessHandles(processes []processHandle) error {
	var result error
	for _, process := range processes {
		state, err := windows.WaitForSingleObject(process.handle, 0)
		if err != nil {
			result = errors.Join(result, fmt.Errorf("inspect descendant process %d: %w", process.pid, err))
			continue
		}
		if state == windows.WAIT_OBJECT_0 {
			continue
		}

		terminateErr := windows.TerminateProcess(process.handle, 1)

		// TerminateJobObject 可能已经开始结束同一个后代。这个竞态窗口里，
		// 上面的零等待仍可能看到进程存活，而 TerminateProcess 会返回 Access Denied。
		// 不按错误码猜测结果，只认预先固定的同一个 process handle 是否最终退出。
		state, waitErr := windows.WaitForSingleObject(process.handle, 2000)
		if state == windows.WAIT_OBJECT_0 && waitErr == nil {
			continue
		}
		if terminateErr != nil {
			result = errors.Join(result, fmt.Errorf("terminate descendant process %d: %w", process.pid, terminateErr))
		}
		if waitErr != nil {
			result = errors.Join(result, fmt.Errorf("wait for descendant process %d: %w", process.pid, waitErr))
		} else if state != windows.WAIT_OBJECT_0 && terminateErr == nil {
			result = errors.Join(result, fmt.Errorf("descendant process %d did not stop", process.pid))
		}
	}
	return result
}

func closeProcessHandles(processes []processHandle) {
	for _, process := range processes {
		_ = windows.CloseHandle(process.handle)
	}
}

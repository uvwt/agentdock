package session

import (
	"fmt"
	"io"
	"os/exec"

	processcontrol "github.com/uvwt/agentdock/internal/process"
)

type commandRunner interface {
	Stdin() io.WriteCloser
	Wait() (int, error)
	Kill() error
}

type standardRunner struct {
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	controller *processcontrol.Controller
}

func startStandardRunner(cmd *exec.Cmd, stdout, stderr io.Writer) (*standardRunner, error) {
	processcontrol.Configure(cmd)
	// command runner 统一拥有进程树取消权。CommandContext 默认只杀直接子进程，
	// 会和这里的进程组/Job Object 终止并发竞争；保留 ctx 的启动前检查，
	// 但关闭 os/exec 自带的直接子进程 Cancel，运行期取消由 Session watcher 处理。
	cmd.Cancel = nil
	cmd.WaitDelay = 0
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, err
	}
	controller, err := processcontrol.Attach(cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = stdin.Close()
		return nil, err
	}
	return &standardRunner{cmd: cmd, stdin: stdin, controller: controller}, nil
}

func (r *standardRunner) Stdin() io.WriteCloser { return r.stdin }

func (r *standardRunner) Wait() (int, error) {
	err := r.cmd.Wait()
	closeErr := r.controller.Close()
	if err == nil && closeErr != nil {
		err = closeErr
	}
	if r.cmd.ProcessState == nil {
		return -1, err
	}
	return r.cmd.ProcessState.ExitCode(), err
}

func (r *standardRunner) Kill() error {
	if r.controller != nil {
		return r.controller.Terminate()
	}
	if r.cmd != nil && r.cmd.Process != nil {
		return r.cmd.Process.Kill()
	}
	return fmt.Errorf("command has no running process")
}

type writerCloser struct{ io.Writer }

func (writerCloser) Close() error { return nil }

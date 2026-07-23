//go:build windows

package session

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"

	"github.com/UserExistsError/conpty"
	processcontrol "github.com/uvwt/agentdock/internal/process"
	"golang.org/x/sys/windows"
)

type conPTYRunner struct {
	ctx        context.Context
	pty        *conpty.ConPty
	controller *processcontrol.Controller
	readerDone chan struct{}
	killOnce   sync.Once
	killErr    error
}

func startInteractiveRunner(ctx context.Context, cmd *exec.Cmd, stdout, _ io.Writer) (commandRunner, bool, error) {
	if !conpty.IsConPtyAvailable() {
		return nil, false, nil
	}
	argv := make([]string, 0, len(cmd.Args))
	for index, arg := range cmd.Args {
		if index == 0 {
			arg = cmd.Path
		}
		argv = append(argv, windows.EscapeArg(arg))
	}
	pty, err := conpty.Start(
		strings.Join(argv, " "),
		conpty.ConPtyWorkDir(cmd.Dir),
		conpty.ConPtyEnv(cmd.Env),
		conpty.ConPtyDimensions(120, 30),
	)
	if err != nil {
		return nil, true, fmt.Errorf("start Windows ConPTY: %w", err)
	}
	controller, err := processcontrol.AttachPID(pty.Pid())
	if err != nil {
		_ = pty.Close()
		return nil, true, err
	}
	runner := &conPTYRunner{
		ctx:        ctx,
		pty:        pty,
		controller: controller,
		readerDone: make(chan struct{}),
	}
	go func() {
		_, _ = io.Copy(stdout, pty)
		close(runner.readerDone)
	}()
	return runner, true, nil
}

func (r *conPTYRunner) Stdin() io.WriteCloser { return writerCloser{Writer: r.pty} }

func (r *conPTYRunner) Wait() (int, error) {
	exitCode, waitErr := r.pty.Wait(r.ctx)
	if r.ctx.Err() != nil {
		_ = r.Kill()
		exitCode, _ = r.pty.Wait(context.Background())
		waitErr = r.ctx.Err()
	}
	_ = r.pty.Close()
	<-r.readerDone
	closeErr := r.controller.Close()
	if waitErr == nil && closeErr != nil {
		waitErr = closeErr
	}
	return int(exitCode), waitErr
}

func (r *conPTYRunner) Kill() error {
	r.killOnce.Do(func() { r.killErr = r.controller.Terminate() })
	return r.killErr
}

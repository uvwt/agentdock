//go:build darwin || linux

package process

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestTerminateDoesNotLeaveChildHoldingCommandPipes(t *testing.T) {
	const iterations = 128
	root := t.TempDir()

	for iteration := range iterations {
		marker := filepath.Join(root, fmt.Sprintf("started-%03d", iteration))
		cmd := exec.Command("/bin/sh", "-c", `printf started > "$AGENTDOCK_MARKER"; sleep 30`)
		cmd.Env = append(os.Environ(), "AGENTDOCK_MARKER="+marker)
		var output bytes.Buffer
		cmd.Stdout = &output
		cmd.Stderr = &output
		Configure(cmd)
		if err := cmd.Start(); err != nil {
			t.Fatalf("iteration %d Start() error = %v", iteration, err)
		}
		controller, err := Attach(cmd)
		if err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatalf("iteration %d Attach() error = %v", iteration, err)
		}
		waitDone := make(chan error, 1)
		go func() { waitDone <- cmd.Wait() }()

		deadline := time.Now().Add(time.Second)
		for {
			if _, err := os.Stat(marker); err == nil {
				break
			}
			if time.Now().After(deadline) {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				<-waitDone
				t.Fatalf("iteration %d shell did not reach fork boundary", iteration)
			}
			time.Sleep(100 * time.Microsecond)
		}

		if err := controller.Terminate(); err != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-waitDone
			t.Fatalf("iteration %d Terminate() error = %v", iteration, err)
		}
		select {
		case <-waitDone:
		case <-time.After(500 * time.Millisecond):
			// A child forked after a one-shot killpg keeps os/exec's stdout/stderr pipes open.
			// Clean it up before failing so the regression test never leaks a 30-second process.
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-waitDone
			t.Fatalf("iteration %d command pipes stayed open after process-group termination", iteration)
		}
	}
}

//go:build windows

package desktopruntime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"
)

func TestTunnelSupervisorKernelLifecycle(t *testing.T) {
	runtimeRoot := t.TempDir()
	readyPath := filepath.Join(runtimeRoot, "helper-ready")
	command := exec.Command(os.Args[0], "-test.run=^TestTunnelSupervisorHelperProcess$")
	command.Env = append(os.Environ(),
		"AGENTDOCK_TEST_TUNNEL_SUPERVISOR_HELPER=1",
		"AGENTDOCK_TEST_TUNNEL_RUNTIME_ROOT="+runtimeRoot,
		"AGENTDOCK_TEST_TUNNEL_READY_PATH="+readyPath,
	)
	var output strings.Builder
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_, _ = command.Process.Wait()
		}
	}()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(readyPath); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := os.Stat(readyPath); err != nil {
		t.Fatalf("helper did not become ready: %v\n%s", err, output.String())
	}

	pid, err := activeTunnelSupervisorPID(runtimeRoot, os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if pid != uint32(command.Process.Pid) {
		t.Fatalf("active supervisor PID=%d, want %d", pid, command.Process.Pid)
	}
	duplicate, err := acquireTunnelSupervisor(runtimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate != nil {
		duplicate.Close()
		t.Fatal("second process unexpectedly acquired Tunnel supervisor mutex")
	}

	if err := signalTunnelSupervisorStop(runtimeRoot); err != nil {
		t.Fatal(err)
	}
	if err := waitTunnelSupervisorStopped(context.Background(), runtimeRoot, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("helper exit failed: %v\n%s", err, output.String())
	}
	if _, err := os.Stat(filepath.Join(runtimeRoot, tunnelSupervisorPIDFile)); !os.IsNotExist(err) {
		t.Fatalf("supervisor PID file should be removed, err=%v", err)
	}
}

func TestTunnelSupervisorHelperProcess(t *testing.T) {
	if os.Getenv("AGENTDOCK_TEST_TUNNEL_SUPERVISOR_HELPER") != "1" {
		t.Skip("helper process only")
	}
	runtimeRoot := os.Getenv("AGENTDOCK_TEST_TUNNEL_RUNTIME_ROOT")
	readyPath := os.Getenv("AGENTDOCK_TEST_TUNNEL_READY_PATH")
	if runtimeRoot == "" || readyPath == "" {
		t.Fatal("helper environment is incomplete")
	}

	goruntime.LockOSThread()
	defer goruntime.UnlockOSThread()
	guard, err := acquireTunnelSupervisor(runtimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	if guard == nil {
		t.Fatal("helper could not acquire supervisor")
	}
	defer guard.Close()
	if err := os.WriteFile(readyPath, []byte(fmt.Sprintf("%d", os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	stopped, err := guard.waitRetry(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !stopped {
		t.Fatal("helper was not stopped by named event")
	}
}

//go:build windows

package desktopruntime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestStopBinaryProcessesTerminatesProcessThatReappearsDuringStop(t *testing.T) {
	target := filepath.Join(t.TempDir(), "agentdock-stop-target.exe")
	copyStopBinaryTestExecutable(t, target)

	first := startStopBinaryHelper(t, target)
	waitForBinaryProcess(t, target, true)

	secondStarted := make(chan *exec.Cmd, 1)
	secondStartErr := make(chan error, 1)
	go func() {
		_ = first.Wait()
		// 旧实现会在第一批进程退出后立即返回；稍后出现的同路径进程因此会漏掉。
		time.Sleep(40 * time.Millisecond)
		second := exec.Command(target, "-test.run=^TestStopBinaryProcessesHelperProcess$")
		second.Env = append(os.Environ(), "AGENTDOCK_TEST_STOP_BINARY_HELPER=1")
		if err := second.Start(); err != nil {
			secondStartErr <- err
			return
		}
		secondStarted <- second
	}()

	stopErr := StopBinaryProcesses(context.Background(), target, 3*time.Second)

	var second *exec.Cmd
	select {
	case err := <-secondStartErr:
		t.Fatalf("start replacement helper: %v", err)
	case second = <-secondStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("replacement helper did not start")
	}
	running, err := BinaryProcessRunning(target)
	if err != nil {
		t.Fatal(err)
	}
	if running {
		_ = second.Process.Kill()
	}
	_ = second.Wait()
	if stopErr != nil {
		t.Fatalf("stop binary processes: %v", stopErr)
	}
	if running {
		t.Fatal("replacement process remained after StopBinaryProcesses returned")
	}
}

func TestStopBinaryProcessesHelperProcess(t *testing.T) {
	if os.Getenv("AGENTDOCK_TEST_STOP_BINARY_HELPER") != "1" {
		t.Skip("helper process only")
	}
	time.Sleep(30 * time.Second)
}

func copyStopBinaryTestExecutable(t *testing.T, target string) {
	t.Helper()
	data, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0o755); err != nil {
		t.Fatal(err)
	}
}

func startStopBinaryHelper(t *testing.T, target string) *exec.Cmd {
	t.Helper()
	command := exec.Command(target, "-test.run=^TestStopBinaryProcessesHelperProcess$")
	command.Env = append(os.Environ(), "AGENTDOCK_TEST_STOP_BINARY_HELPER=1")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_, _ = command.Process.Wait()
		}
	})
	return command
}

func waitForBinaryProcess(t *testing.T, target string, want bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		running, err := BinaryProcessRunning(target)
		if err != nil {
			t.Fatal(err)
		}
		if running == want {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("binary running state did not become %v: %q", want, target)
}

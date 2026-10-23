//go:build windows

package desktopruntime

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	processcontrol "github.com/uvwt/agentdock/internal/process"
	"github.com/uvwt/agentdock/internal/updateengine"
)

func recoveryTestLayout(t *testing.T) (string, Manifest, updateengine.WindowsLayout) {
	t.Helper()
	root := t.TempDir()
	layout, err := updateengine.NewWindowsLayout(root)
	if err != nil {
		t.Fatal(err)
	}
	store, err := updateengine.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WriteActive(updateengine.ActiveVersion{
		SchemaVersion: updateengine.SchemaVersion, ActiveVersion: "v0.9.1", State: updateengine.StateCommitted,
	}); err != nil {
		t.Fatal(err)
	}
	// 当前 generation 只用于路径识别；不是可执行文件，确保启动失败用例不会运行测试套件。
	current := layout.GenerationCore("v0.9.1")
	if err := os.MkdirAll(filepath.Dir(current), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(current, []byte("not an executable"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, Manifest{AgentDockBinary: layout.CoreShim(), Host: "127.0.0.1"}, layout
}

func startRecoveryListener(t *testing.T, binary, address string) (*exec.Cmd, string) {
	t.Helper()
	ready := filepath.Join(t.TempDir(), "listener-address")
	command := exec.Command(binary, "-test.run=^TestCoreRecoveryListenerProcess$")
	processcontrol.Configure(command)
	command.Env = append(os.Environ(), "AGENTDOCK_TEST_RECOVERY_LISTENER=1",
		"AGENTDOCK_TEST_RECOVERY_ADDRESS="+address, "AGENTDOCK_TEST_RECOVERY_READY="+ready)
	return command, awaitRecoveryProcess(t, command, ready)
}

func awaitRecoveryProcess(t *testing.T, command *exec.Cmd, ready string) string {
	t.Helper()
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var exitErr error
	go func() { exitErr = command.Wait(); close(done) }()
	t.Cleanup(func() {
		_ = command.Process.Kill()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("listener helper did not exit")
		}
	})
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if data, err := os.ReadFile(ready); err == nil {
			return string(data)
		}
		select {
		case <-done:
			t.Fatalf("recovery helper failed: %v", exitErr)
		case <-deadline.C:
			t.Fatal("listener helper not ready")
		case <-ticker.C:
		}
	}
}

func TestCoreRecoveryListenerProcess(t *testing.T) {
	if os.Getenv("AGENTDOCK_TEST_RECOVERY_LISTENER") != "1" {
		t.Skip("helper process only")
	}
	listener, err := net.Listen("tcp", os.Getenv("AGENTDOCK_TEST_RECOVERY_ADDRESS"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := os.WriteFile(os.Getenv("AGENTDOCK_TEST_RECOVERY_READY"), []byte(listener.Addr().String()), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unhealthy old core", http.StatusServiceUnavailable)
	}))
}

func TestCoreRecoveryStopsOldGenerationAndReleasesPort(t *testing.T) {
	root, manifest, layout := recoveryTestLayout(t)
	oldCore := layout.GenerationCore("v0.9.0")
	copyTunnelSupervisorTestBinary(t, oldCore)
	_, address := startRecoveryListener(t, oldCore, "127.0.0.1:0")
	otherCore := filepath.Join(t.TempDir(), "versions", "v0.9.0", "agentdock-core.exe")
	copyTunnelSupervisorTestBinary(t, otherCore)
	startRecoveryListener(t, otherCore, "127.0.0.1:0")
	if running, err := coreProcessRunning(root, manifest); err != nil || !running {
		t.Errorf("old generation must count as running: running=%v, err=%v", running, err)
	}
	if err := stopCore(t.Context(), manifest, root); err != nil {
		t.Fatal(err)
	}
	if running, err := BinaryProcessRunning(otherCore); err != nil || !running {
		t.Fatalf("stop affected another installation: running=%v err=%v", running, err)
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("stop returned but old core still owns the port: %v", err)
	}
	_ = listener.Close()
	current := layout.GenerationCore("v0.9.1")
	copyTunnelSupervisorTestBinary(t, current)
	startRecoveryListener(t, current, address)
	if running, err := coreProcessRunning(root, manifest); err != nil || !running {
		t.Fatalf("new generation did not take over: running=%v, err=%v", running, err)
	}
	if err := stopCore(t.Context(), manifest, root); err != nil {
		t.Fatal(err)
	}
}

func TestCoreRecoveryStartCleansStaleProcessBeforeLaunchFailure(t *testing.T) {
	root, manifest, layout := recoveryTestLayout(t)
	oldCore := layout.GenerationCore("v0.9.0")
	copyTunnelSupervisorTestBinary(t, oldCore)
	_, address := startRecoveryListener(t, oldCore, "127.0.0.1:0")
	_, port, _ := net.SplitHostPort(address)
	manifest.Port, _ = strconv.Atoi(port)
	if err := startCore(t.Context(), manifest, root); err == nil {
		t.Fatal("non-executable current core unexpectedly launched")
	}
	if running, err := BinaryProcessRunning(oldCore); err != nil || running {
		t.Fatalf("start left unhealthy old core running: running=%v, err=%v", running, err)
	}
}

func TestCoreRecoveryKeepsHealthyCore(t *testing.T) {
	root, manifest, layout := recoveryTestLayout(t)
	oldCore := layout.GenerationCore("v0.9.0")
	copyTunnelSupervisorTestBinary(t, oldCore)
	startRecoveryListener(t, oldCore, "127.0.0.1:0")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	manifest.Port, _ = strconv.Atoi(port)
	if err := startCore(t.Context(), manifest, root); err != nil {
		t.Fatal(err)
	}
	if running, err := BinaryProcessRunning(oldCore); err != nil || !running {
		t.Fatalf("healthy Core should be preserved: running=%v err=%v", running, err)
	}
}

func TestCoreRecoveryAcceptsPreviousGenerationSupervisor(t *testing.T) {
	root, manifest, layout := recoveryTestLayout(t)
	oldCore := layout.GenerationCore("v0.9.0")
	copyTunnelSupervisorTestBinary(t, oldCore)
	ready := filepath.Join(root, "supervisor-ready")
	command := exec.Command(oldCore, "-test.run=^TestTunnelSupervisorHelperProcess$")
	processcontrol.Configure(command)
	command.Env = append(os.Environ(), "AGENTDOCK_TEST_TUNNEL_SUPERVISOR_HELPER=1",
		"AGENTDOCK_TEST_TUNNEL_RUNTIME_ROOT="+root, "AGENTDOCK_TEST_TUNNEL_READY_PATH="+ready)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("supervisor not ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	pid, err := activeTunnelSupervisorPIDForRuntime(root, manifest)
	if err != nil || pid != uint32(command.Process.Pid) {
		t.Errorf("previous generation supervisor rejected: pid=%d err=%v", pid, err)
	}
	if err := stopCore(t.Context(), manifest, root); err != nil {
		t.Errorf("old supervisor blocked core stop: %v", err)
	}
	if running, err := BinaryProcessRunning(oldCore); err != nil || !running {
		t.Errorf("core stop must preserve tunnel supervisor: running=%v err=%v", running, err)
	}
	// 即使有同一安装的 mutex，PID 文件也不能让其他目录的程序冒充 supervisor。
	otherCore := filepath.Join(t.TempDir(), "agentdock-core.exe")
	copyTunnelSupervisorTestBinary(t, otherCore)
	other := startStopBinaryHelper(t, otherCore)
	pidPath := filepath.Join(root, tunnelSupervisorPIDFile)
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(other.Process.Pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := activeTunnelSupervisorPIDForRuntime(root, manifest); err == nil {
		t.Error("supervisor PID outside managed paths was accepted")
	}
}

func TestCoreRecoveryToleratesProcessExitBeforeTermination(t *testing.T) {
	command := startStopBinaryHelper(t, os.Args[0])
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()
	if err := terminateProcessIDs([]uint32{uint32(command.Process.Pid)}); err != nil {
		t.Fatalf("already exited process must not fail restart: %v", err)
	}
}

func TestCoreRecoveryDoesNotTreatStableShimAsCore(t *testing.T) {
	root, manifest, _ := recoveryTestLayout(t)
	copyTunnelSupervisorTestBinary(t, manifest.AgentDockBinary)
	startStopBinaryHelper(t, manifest.AgentDockBinary)
	if running, err := coreProcessRunning(root, manifest); err != nil || running {
		t.Fatalf("stable shim must not count as Core: running=%v err=%v", running, err)
	}
	if err := stopCore(t.Context(), manifest, root); err != nil {
		t.Fatal(err)
	}
	if running, err := BinaryProcessRunning(manifest.AgentDockBinary); err != nil || !running {
		t.Fatalf("Core stop must preserve stable shim: running=%v err=%v", running, err)
	}
}

func TestCoreRecoveryPreservesUpdaterAncestorFromOldGeneration(t *testing.T) {
	root, _, layout := recoveryTestLayout(t)
	oldCore := layout.GenerationCore("v0.9.0")
	current := layout.GenerationCore("v0.9.1")
	copyTunnelSupervisorTestBinary(t, oldCore)
	copyTunnelSupervisorTestBinary(t, current)
	ready := filepath.Join(root, "updater-ready")
	command := exec.Command(oldCore, "-test.run=^TestCoreRecoveryUpdaterProcess$")
	processcontrol.Configure(command)
	command.Env = append(os.Environ(), "AGENTDOCK_TEST_RECOVERY_UPDATER=1",
		"AGENTDOCK_TEST_RECOVERY_ROOT="+root, "AGENTDOCK_TEST_RECOVERY_READY="+ready)
	awaitRecoveryProcess(t, command, ready)
	if running, err := BinaryProcessRunning(oldCore); err != nil || !running {
		t.Fatalf("source updater was killed by its stop helper: running=%v err=%v", running, err)
	}
}

func TestCoreRecoveryUpdaterProcess(t *testing.T) {
	if os.Getenv("AGENTDOCK_TEST_RECOVERY_UPDATER") != "1" {
		t.Skip("helper process only")
	}
	layout, err := updateengine.NewWindowsLayout(os.Getenv("AGENTDOCK_TEST_RECOVERY_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(layout.GenerationCore("v0.9.1"), "-test.run=^TestCoreRecoveryStopProcess$")
	processcontrol.Configure(command)
	command.Env = os.Environ()
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("stop helper: %v\n%s", err, output)
	}
	if err := os.WriteFile(os.Getenv("AGENTDOCK_TEST_RECOVERY_READY"), []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Second)
}

func TestCoreRecoveryStopProcess(t *testing.T) {
	if os.Getenv("AGENTDOCK_TEST_RECOVERY_UPDATER") != "1" {
		t.Skip("helper process only")
	}
	root := os.Getenv("AGENTDOCK_TEST_RECOVERY_ROOT")
	layout, err := updateengine.NewWindowsLayout(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := stopCore(t.Context(), Manifest{AgentDockBinary: layout.CoreShim()}, root); err != nil {
		t.Fatal(err)
	}
}

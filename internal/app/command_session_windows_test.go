//go:build windows

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const appWindowsNativeChildReadyEnv = "AGENTDOCK_TEST_WINDOWS_NATIVE_CHILD_READY"

type appWindowsNativeChildReady struct {
	PID  int `json:"pid"`
	Port int `json:"port"`
}

func TestWindowsSessionActKillTerminatesNativeChildTree(t *testing.T) {
	runtime, root := newCodeToolsRuntime(t)
	defer runtime.Close()
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	readyPath := filepath.Join(root, "native-child.ready.json")
	command := fmt.Sprintf("& '%s' -test.run '^TestWindowsSessionNativeChildHelper$'", strings.ReplaceAll(testBinary, "'", "''"))
	started, err := runtime.execCommand(context.Background(), map[string]any{
		"cmd": command, "execution_mode": "async", "timeout_ms": 60000,
		"env": map[string]any{appWindowsNativeChildReadyEnv: readyPath},
	})
	if err != nil {
		t.Fatal(err)
	}
	sessionID, _ := started["session_id"].(string)
	ready := waitForAppWindowsNativeChild(t, runtime, sessionID, readyPath)
	childPID := ready.PID
	port := ready.Port
	defer func() {
		if process, findErr := os.FindProcess(childPID); findErr == nil {
			_ = process.Kill()
		}
	}()

	result, err := runtime.sessionAct(map[string]any{"action": "kill", "session_id": sessionID})
	if err != nil {
		t.Fatalf("session_act(kill) error = %v", err)
	}
	if result["status"] != "killed" {
		t.Fatalf("kill result = %#v", result)
	}
	if err := waitForAppWindowsPort(port, false, 2*time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsSessionNativeChildHelper(t *testing.T) {
	readyPath := os.Getenv(appWindowsNativeChildReadyEnv)
	if readyPath == "" {
		return
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ready := appWindowsNativeChildReady{PID: os.Getpid(), Port: listener.Addr().(*net.TCPAddr).Port}
	data, err := json.Marshal(ready)
	if err != nil {
		t.Fatal(err)
	}
	tempReadyPath := readyPath + ".tmp"
	if err := os.WriteFile(tempReadyPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tempReadyPath, readyPath); err != nil {
		t.Fatal(err)
	}
	for {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		_ = connection.Close()
	}
}

func waitForAppWindowsNativeChild(t *testing.T, runtime *Runtime, sessionID, readyPath string) appWindowsNativeChildReady {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(readyPath)
		if err == nil {
			var ready appWindowsNativeChildReady
			if json.Unmarshal(data, &ready) == nil && ready.PID > 0 && ready.Port > 0 {
				if err := waitForAppWindowsPort(ready.Port, true, 2*time.Second); err != nil {
					t.Fatalf("native child reported ready but its port is unavailable: %v", err)
				}
				return ready
			}
		}
		if stored, ok := runtime.command.Store().Get(sessionID); ok {
			select {
			case <-stored.Done:
				status, observeErr := runtime.sessionObserve(map[string]any{
					"action": "status", "session_id": sessionID, "max_output_bytes": 4096,
				})
				t.Fatalf("native child exited before readiness: status=%#v observe_err=%v", status, observeErr)
			default:
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	status, observeErr := runtime.sessionObserve(map[string]any{
		"action": "status", "session_id": sessionID, "max_output_bytes": 4096,
	})
	t.Fatalf("native child did not start: status=%#v observe_err=%v", status, observeErr)
	return appWindowsNativeChildReady{}
}

func waitForAppWindowsPort(port int, wantOpen bool, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 50*time.Millisecond)
		if connection != nil {
			_ = connection.Close()
		}
		if (err == nil) == wantOpen {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("port %d open state did not become %t", port, wantOpen)
}

//go:build windows

package app

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestWindowsSessionActKillTerminatesNodeServer(t *testing.T) {
	runtime, root := newCodeToolsRuntime(t)
	defer runtime.Close()
	nodePath, err := exec.LookPath("node.exe")
	if err != nil {
		t.Skip("node.exe is not installed")
	}
	port := reserveAppWindowsPort(t)
	pidPath := filepath.Join(root, "node-session-kill.pid")
	nodeScript := fmt.Sprintf(
		"const fs=require('fs'); const server=require('http').createServer((req,res)=>res.end('ok')); server.listen(%d,'127.0.0.1',()=>fs.writeFileSync('%s',String(process.pid)))",
		port,
		strings.ReplaceAll(filepath.ToSlash(pidPath), "'", "\\'"),
	)
	command := fmt.Sprintf("& '%s' -e \"%s\"", strings.ReplaceAll(nodePath, "'", "''"), nodeScript)
	started, err := runtime.execCommand(context.Background(), map[string]any{
		"cmd": command, "execution_mode": "async", "timeout_ms": 60000,
	})
	if err != nil {
		t.Fatal(err)
	}
	sessionID, _ := started["session_id"].(string)
	nodePID := waitForAppWindowsNode(t, runtime, sessionID, pidPath, port)
	defer func() {
		if process, findErr := os.FindProcess(nodePID); findErr == nil {
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

func reserveAppWindowsPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func waitForAppWindowsNode(t *testing.T, runtime *Runtime, sessionID, pidPath string, port int) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(pidPath)
		if err == nil && waitForAppWindowsPort(port, true, 100*time.Millisecond) == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if parseErr == nil {
				return pid
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	status, observeErr := runtime.sessionObserve(map[string]any{
		"action": "status", "session_id": sessionID, "max_output_bytes": 4096,
	})
	t.Fatalf("Node server did not start: status=%#v observe_err=%v", status, observeErr)
	return 0
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

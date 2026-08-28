//go:build windows

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type appWindowsNodeReady struct {
	PID  int `json:"pid"`
	Port int `json:"port"`
}

func TestWindowsSessionActKillTerminatesNodeServer(t *testing.T) {
	runtime, root := newCodeToolsRuntime(t)
	defer runtime.Close()
	nodePath, err := exec.LookPath("node.exe")
	if err != nil {
		t.Skip("node.exe is not installed")
	}
	readyPath := filepath.Join(root, "node-session-kill.ready.json")
	tempReadyPath := readyPath + ".tmp"
	nodeScript := fmt.Sprintf(
		"const fs=require('fs'); const server=require('http').createServer((req,res)=>res.end('ok')); server.listen(0,'127.0.0.1',()=>{const address=server.address(); fs.writeFileSync('%s',JSON.stringify({pid:process.pid,port:address.port})); fs.renameSync('%s','%s')})",
		strings.ReplaceAll(filepath.ToSlash(tempReadyPath), "'", "\\'"),
		strings.ReplaceAll(filepath.ToSlash(tempReadyPath), "'", "\\'"),
		strings.ReplaceAll(filepath.ToSlash(readyPath), "'", "\\'"),
	)
	command := fmt.Sprintf("& '%s' -e \"%s\"", strings.ReplaceAll(nodePath, "'", "''"), nodeScript)
	started, err := runtime.execCommand(context.Background(), map[string]any{
		"cmd": command, "execution_mode": "async", "timeout_ms": 60000,
	})
	if err != nil {
		t.Fatal(err)
	}
	sessionID, _ := started["session_id"].(string)
	ready := waitForAppWindowsNode(t, runtime, sessionID, readyPath)
	nodePID := ready.PID
	port := ready.Port
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

func waitForAppWindowsNode(t *testing.T, runtime *Runtime, sessionID, readyPath string) appWindowsNodeReady {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(readyPath)
		if err == nil {
			var ready appWindowsNodeReady
			if json.Unmarshal(data, &ready) == nil && ready.PID > 0 && ready.Port > 0 {
				if err := waitForAppWindowsPort(ready.Port, true, 2*time.Second); err != nil {
					t.Fatalf("Node reported ready but its port is unavailable: %v", err)
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
				t.Fatalf("Node server exited before readiness: status=%#v observe_err=%v", status, observeErr)
			default:
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	status, observeErr := runtime.sessionObserve(map[string]any{
		"action": "status", "session_id": sessionID, "max_output_bytes": 4096,
	})
	t.Fatalf("Node server did not start: status=%#v observe_err=%v", status, observeErr)
	return appWindowsNodeReady{}
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

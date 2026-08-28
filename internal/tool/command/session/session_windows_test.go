//go:build windows

package session

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

func TestWindowsPowerShellOutputIsUTF8(t *testing.T) {
	s, _, err := Start(
		context.Background(),
		"[Console]::Out.Write('中文输出')",
		t.TempDir(),
		os.Environ(),
		5*time.Second,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Cancel()
	<-s.Done
	if err := s.WaitError(); err != nil {
		t.Fatal(err)
	}
	result := s.Snapshot("exited", 4096)
	if result["stdout"] != "中文输出" {
		t.Fatalf("stdout = %#v", result["stdout"])
	}
}

func TestWindowsTTYUsesConPTYAndAcceptsInput(t *testing.T) {
	s, _, err := StartWithTTY(
		context.Background(),
		"$line=[Console]::In.ReadLine(); [Console]::Out.Write(\"received:$line\")",
		t.TempDir(),
		os.Environ(),
		10*time.Second,
		true,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Cancel()
	if s.Terminal != "conpty" {
		t.Fatalf("terminal = %q, want conpty", s.Terminal)
	}
	if err := s.Write("hello\r\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("ConPTY command did not finish")
	}
	if err := s.WaitError(); err != nil {
		t.Fatal(err)
	}
	result := s.Snapshot("exited", 4096)
	stdout, _ := result["stdout"].(string)
	if !strings.Contains(stdout, "received:hello") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestWindowsKillTerminatesPowerShellNativeChildTree(t *testing.T) {
	nodePath, err := exec.LookPath("node.exe")
	if err != nil {
		t.Skip("node.exe is not installed")
	}
	root := t.TempDir()
	port := reserveWindowsTestPort(t)
	pidPath := filepath.Join(root, "node.pid")
	nodeScript := fmt.Sprintf(
		"const fs=require('fs'); const server=require('http').createServer((req,res)=>res.end('ok')); server.listen(%d,'127.0.0.1',()=>fs.writeFileSync('%s',String(process.pid)))",
		port,
		strings.ReplaceAll(filepath.ToSlash(pidPath), "'", "\\'"),
	)
	command := fmt.Sprintf("& '%s' -e \"%s\"", strings.ReplaceAll(nodePath, "'", "''"), nodeScript)

	s, _, err := Start(context.Background(), command, root, os.Environ(), time.Minute, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Cancel()
	defer func() {
		if s.Command != nil && s.Command.Process != nil {
			_ = s.Command.Process.Kill()
		}
	}()
	nodePID := waitForWindowsTestNode(t, s, pidPath, port)
	defer func() {
		if process, findErr := os.FindProcess(nodePID); findErr == nil {
			_ = process.Kill()
		}
	}()

	killed, err := s.Kill()
	if err != nil {
		t.Fatalf("Kill() error = %v", err)
	}
	if !killed {
		t.Fatal("Kill() did not report a running session")
	}
	select {
	case <-s.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("session did not finish after killing its process tree")
	}
	if err := waitForWindowsTestPort(port, false, 2*time.Second); err != nil {
		t.Fatal(err)
	}
}

func reserveWindowsTestPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func waitForWindowsTestNode(t *testing.T, s *Session, pidPath string, port int) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(pidPath)
		if err == nil && waitForWindowsTestPort(port, true, 100*time.Millisecond) == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if parseErr == nil {
				return pid
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	status := s.Peek("running", 4096)
	t.Fatalf("Node server did not start: stdout=%q stderr=%q", status["stdout"], status["stderr"])
	return 0
}

func waitForWindowsTestPort(port int, wantOpen bool, timeout time.Duration) error {
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

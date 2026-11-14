package mcp

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/uvwt/agentdock/internal/app"
	"github.com/uvwt/agentdock/internal/config"
)

func TestServeStdioUsesOfficialSDKTransport(t *testing.T) {
	server := NewServer(nil, config.Config{})
	clientInput, serverOutput := io.Pipe()
	serverInput, clientOutput := io.Pipe()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.ServeStdio(serverInput, serverOutput)
	}()

	client := mcpsdk.NewClient(
		&mcpsdk.Implementation{Name: "agentdock-test", Version: "1.0.0"},
		&mcpsdk.ClientOptions{Capabilities: &mcpsdk.ClientCapabilities{}},
	)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	session, err := client.Connect(ctx, &mcpsdk.IOTransport{Reader: clientInput, Writer: clientOutput}, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if result := session.InitializeResult(); result == nil || result.ProtocolVersion == "" {
		t.Fatalf("InitializeResult() = %#v", result)
	}
	// 2026-07-28 协议移除了 ping 方法，改用 ListTools 验证 stdio 请求往返。
	if _, err := session.ListTools(ctx, nil); err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatalf("ServeStdio() error = %v", err)
		}
	case <-ctx.Done():
		t.Fatal("ServeStdio() did not stop after client close")
	}
}

func TestServeStdioRejectsUninitializedServer(t *testing.T) {
	var server *Server
	if err := server.ServeStdio(nil, nil); err == nil {
		t.Fatal("ServeStdio() accepted an uninitialized server")
	}
}

func TestServeStdioAdvertisesStableContextInstructions(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.Config
		want string
	}{
		{name: "without Nexus", cfg: config.Config{}, want: baseServerInstructions},
		{name: "with Nexus", cfg: config.Config{NexusEndpoint: "http://127.0.0.1:18777"}, want: nexusServerInstructions},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := NewServer(nil, test.cfg)
			clientInput, serverOutput := io.Pipe()
			serverInput, clientOutput := io.Pipe()
			serverDone := make(chan error, 1)
			go func() {
				serverDone <- server.ServeStdio(serverInput, serverOutput)
			}()

			client := mcpsdk.NewClient(
				&mcpsdk.Implementation{Name: "agentdock-test", Version: "1.0.0"},
				&mcpsdk.ClientOptions{Capabilities: &mcpsdk.ClientCapabilities{}},
			)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			session, err := client.Connect(ctx, &mcpsdk.IOTransport{Reader: clientInput, Writer: clientOutput}, nil)
			if err != nil {
				t.Fatalf("Connect() error = %v", err)
			}
			result := session.InitializeResult()
			if result == nil || result.Instructions != test.want {
				t.Fatalf("InitializeResult() = %#v, want stable instructions %q", result, test.want)
			}
			if !strings.Contains(result.Instructions, "workspace_context") {
				t.Fatalf("stable instructions do not explain workspace_context: %q", result.Instructions)
			}
			if err := session.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
			select {
			case err := <-serverDone:
				if err != nil {
					t.Fatalf("ServeStdio() error = %v", err)
				}
			case <-ctx.Done():
				t.Fatal("ServeStdio() did not stop after client close")
			}
		})
	}
}

func TestServeStdioDoesNotInjectAGENTSContent(t *testing.T) {
	root := t.TempDir()
	cfg := config.Config{
		AgentDockHome:       filepath.Join(root, "state"),
		AgentDockDefaultDir: filepath.Join(root, "workspace"),
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	for path, marker := range map[string]string{
		filepath.Join(cfg.AgentDockHome, "AGENTS.md"):       "GLOBAL-AGENTS-MARKER",
		filepath.Join(cfg.AgentDockDefaultDir, "AGENTS.md"): "WORKSPACE-AGENTS-MARKER",
	} {
		if err := os.WriteFile(path, []byte(marker), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runtime, err := app.NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = runtime.Close() }()

	server := NewServer(runtime, cfg)
	clientInput, serverOutput := io.Pipe()
	serverInput, clientOutput := io.Pipe()
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.ServeStdio(serverInput, serverOutput) }()

	client := mcpsdk.NewClient(
		&mcpsdk.Implementation{Name: "agentdock-instructions-test", Version: "1.0.0"},
		&mcpsdk.ClientOptions{Capabilities: &mcpsdk.ClientCapabilities{}},
	)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	session, err := client.Connect(ctx, &mcpsdk.IOTransport{Reader: clientInput, Writer: clientOutput}, nil)
	if err != nil {
		t.Fatal(err)
	}
	result := session.InitializeResult()
	if result == nil || result.Instructions != baseServerInstructions {
		t.Fatalf("InitializeResult() = %#v, want stable instructions only", result)
	}
	for _, marker := range []string{"GLOBAL-AGENTS-MARKER", "WORKSPACE-AGENTS-MARKER"} {
		if strings.Contains(result.Instructions, marker) {
			t.Fatalf("MCP initialize injected AGENTS.md content %q", marker)
		}
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("ServeStdio() did not stop after client close")
	}
}

func TestServeStdioHidesNexusExtensionsWithoutNexus(t *testing.T) {
	cfg := config.Config{
		AgentDockDefaultDir: t.TempDir(),
		AgentDockHome:       filepath.Join(t.TempDir(), ".agentdock"),
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	runtime, err := app.NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = runtime.Close() }()

	server := NewServer(runtime, cfg)
	clientInput, serverOutput := io.Pipe()
	serverInput, clientOutput := io.Pipe()
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.ServeStdio(serverInput, serverOutput) }()

	client := mcpsdk.NewClient(
		&mcpsdk.Implementation{Name: "agentdock-schema-test", Version: "1.0.0"},
		&mcpsdk.ClientOptions{Capabilities: &mcpsdk.ClientCapabilities{}},
	)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	session, err := client.Connect(ctx, &mcpsdk.IOTransport{Reader: clientInput, Writer: clientOutput}, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	var taskManage *mcpsdk.Tool
	for tool, listErr := range session.Tools(ctx, nil) {
		if listErr != nil {
			t.Fatalf("Tools() error = %v", listErr)
		}
		if tool.Name == "evolve" || strings.HasPrefix(tool.Name, "recall_") || tool.Name == "workflow_template_manage" {
			t.Fatalf("Nexus-only tool leaked without Nexus: %s", tool.Name)
		}
		if tool.Name == "task_manage" {
			taskManage = tool
		}
	}
	if taskManage == nil {
		t.Fatal("task_manage should remain available without Nexus")
	}
	encoded, err := json.Marshal(taskManage)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, leaked := range []string{"learning_checks", "source_template_ids", "template_id", "guidance_context", "review_revision", "evolution_candidates", "evolution_warning", "Evolution", "evolution"} {
		if strings.Contains(text, leaked) {
			t.Fatalf("task_manage schema leaked %q without Nexus: %s", leaked, text)
		}
	}

	if err := session.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatalf("ServeStdio() error = %v", err)
		}
	case <-ctx.Done():
		t.Fatal("ServeStdio() did not stop after client close")
	}
}

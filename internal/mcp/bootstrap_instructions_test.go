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
	"github.com/uvwt/agentdock/internal/agentinstructions"
	"github.com/uvwt/agentdock/internal/app"
	"github.com/uvwt/agentdock/internal/config"
)

func writeBootstrapInstructions(t *testing.T, dir, text string) string {
	t.Helper()
	path := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func connectInstructionClient(t *testing.T, server *Server) *mcpsdk.ClientSession {
	t.Helper()
	clientInput, serverOutput := io.Pipe()
	serverInput, clientOutput := io.Pipe()
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.ServeStdio(serverInput, serverOutput) }()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "instruction-test", Version: "1.0.0"}, nil)
	// T.Context is canceled before Cleanup; this session must remain alive
	// until Cleanup closes it and observes the server's orderly shutdown.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	session, err := client.Connect(ctx, &mcpsdk.IOTransport{Reader: clientInput, Writer: clientOutput}, nil)
	if err != nil {
		cancel()
		_ = clientInput.Close()
		_ = clientOutput.Close()
		_ = serverInput.Close()
		_ = serverOutput.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer cancel()
		defer clientInput.Close()
		defer clientOutput.Close()
		defer serverInput.Close()
		defer serverOutput.Close()
		if err := session.Close(); err != nil {
			t.Error(err)
		}
		select {
		case err := <-serverDone:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("instruction test server did not stop")
		}
	})
	return session
}

func TestInstructionBootstrapAndLiveContextThroughMCP(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		name := "automatic"
		if explicit {
			name = "explicit global override"
		}
		t.Run(name, func(t *testing.T) {
			cfg := config.Config{AgentDockHome: t.TempDir(), AgentDockDefaultDir: t.TempDir()}
			global := writeBootstrapInstructions(t, cfg.AgentDockHome, "global-before-marker")
			writeBootstrapInstructions(t, cfg.AgentDockDefaultDir, "workspace-before-marker")
			if explicit {
				cfg.InstructionsFile = global
			}
			if err := cfg.Normalize(); err != nil {
				t.Fatal(err)
			}
			rt, err := app.NewRuntime(cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = rt.Close() })
			session := connectInstructionClient(t, NewServer(rt, cfg))
			initial := session.InitializeResult().Instructions
			for _, marker := range []string{"global-before-marker", "workspace-before-marker"} {
				if strings.Count(initial, marker) != 1 {
					t.Fatalf("initial instructions missing/repeated %q: %s", marker, initial)
				}
			}
			if strings.Index(initial, "global-before-marker") > strings.Index(initial, "workspace-before-marker") {
				t.Fatal("global rules must precede workspace rules")
			}
			writeBootstrapInstructions(t, cfg.AgentDockHome, "global-after-marker")
			selected := t.TempDir()
			writeBootstrapInstructions(t, selected, "selected-workspace-marker")
			result, err := session.CallTool(t.Context(), &mcpsdk.CallToolParams{Name: "agentdock_context", Arguments: map[string]any{"workdir": selected}})
			if err != nil {
				t.Fatal(err)
			}
			if result.IsError {
				t.Fatalf("MCP context failed: %#v", result)
			}
			encoded, err := json.Marshal(result.StructuredContent)
			if err != nil {
				t.Fatal(err)
			}
			var got struct {
				Instructions agentinstructions.Snapshot `json:"instruction_files"`
			}
			if err := json.Unmarshal(encoded, &got); err != nil {
				t.Fatal(err)
			}
			if len(got.Instructions.Files) != 2 || got.Instructions.Files[0].Content != "global-after-marker" || got.Instructions.Files[1].Content != "selected-workspace-marker" {
				t.Fatalf("live MCP instructions=%s", encoded)
			}
			for _, old := range []string{"global-before-marker", "workspace-before-marker"} {
				if strings.Contains(string(encoded), old) {
					t.Fatalf("live response leaked stale text: %s", old)
				}
			}
			if rt.Workspace().DefaultCWD() == selected {
				t.Fatal("MCP request changed workspace default")
			}
		})
	}
}

func TestInstructionBootstrapOptOutAndFileErrors(t *testing.T) {
	cfg := config.Config{AgentDockHome: t.TempDir(), AgentDockDefaultDir: t.TempDir(), AgentsAutoLoadDisabled: true}
	writeBootstrapInstructions(t, cfg.AgentDockHome, "disabled-global-marker")
	writeBootstrapInstructions(t, cfg.AgentDockDefaultDir, "disabled-workspace-marker")
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	rt, err := app.NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	text := initialServerInstructions(rt, cfg)
	if strings.Contains(text, "disabled-global-marker") || strings.Contains(text, "disabled-workspace-marker") {
		t.Fatal("opt-out still injected automatic files")
	}
	cfg.AgentsAutoLoadDisabled = false
	writeBootstrapInstructions(t, cfg.AgentDockHome, strings.Repeat("x", agentinstructions.MaxFileBytes+1))
	enabled, err := app.NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer enabled.Close()
	text = initialServerInstructions(enabled, cfg)
	if !strings.Contains(text, "file_size_limit") || !strings.Contains(text, "Do not claim its rules were applied") {
		t.Fatal("startup file failure is not disclosed")
	}
	if strings.Contains(text, strings.Repeat("x", 100)) {
		t.Fatal("oversized startup body leaked")
	}
}

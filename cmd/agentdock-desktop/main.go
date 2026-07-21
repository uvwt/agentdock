package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/runtimeapp"
)

// agentdock-desktop starts MCP + browser tools, opens the settings Console,
// and optionally opens ChatGPT in the dedicated browser profile.
// Long-running goal work still happens in ChatGPT web via MCP — the Console
// only simplifies connection and runtime settings.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "agentdock-desktop: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("agentdock-desktop", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	openConsole := flags.Bool("open-console", true, "open the AgentDock settings console in a browser")
	openChatGPT := flags.Bool("open-chatgpt", true, "open ChatGPT in the dedicated browser profile")
	enableBrowser := flags.Bool("browser", true, "enable browser automation tools (required for ChatGPT worker)")
	autoApproveTools := flags.Bool("chatgpt-auto-approve-tools", false, "auto-click ChatGPT web tool/connector permission prompts")
	flags.StringVar(&cfg.Host, "host", cfg.Host, "HTTP bind host")
	flags.IntVar(&cfg.Port, "port", cfg.Port, "HTTP bind port")
	flags.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "log level")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	cfg.BrowserEnabled = *enableBrowser
	cfg.ChatGPTAutoApproveTools = *autoApproveTools
	cfg.Stdio = false
	if strings.TrimSpace(cfg.Host) == "" {
		cfg.Host = "127.0.0.1"
	}

	go func() {
		time.Sleep(700 * time.Millisecond)
		host := displayHost(cfg.Host)
		consoleURL := fmt.Sprintf("http://%s:%d/console", host, cfg.Port)
		mcpURL := fmt.Sprintf("http://%s:%d/mcp", host, cfg.Port)
		if *openConsole {
			_ = openBrowser(consoleURL)
			fmt.Fprintf(os.Stdout, "Console: %s\n", consoleURL)
		}
		fmt.Fprintf(os.Stdout, "MCP:     %s\n", mcpURL)
		if *openChatGPT && cfg.BrowserEnabled {
			openCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if err := openChatGPTSession(openCtx, cfg); err != nil {
				fmt.Fprintf(os.Stderr, "agentdock-desktop: open ChatGPT: %v\n", err)
				fmt.Fprintf(os.Stderr, "  tip: ./scripts/bundle-browser-desktop.sh && source ~/.agentdock/browser/env.sh\n")
				fmt.Fprintf(os.Stderr, "  first launch: complete ChatGPT login in the dedicated profile window\n")
				return
			}
			fmt.Fprintf(os.Stdout, "ChatGPT: opened dedicated browser profile (chatgpt)\n")
		}
	}()

	return runtimeapp.Run(ctx, cfg)
}

// openChatGPTSession asks the live server Runtime to open ChatGPT.
// A throwaway tools.NewRuntime here used to start a second browser worker that
// closed on return, leaving the MCP server's worker with a stale/empty page_id.
func openChatGPTSession(ctx context.Context, cfg config.Config) error {
	if err := cfg.Normalize(); err != nil {
		return err
	}
	url := fmt.Sprintf("http://%s:%d/internal/runtime/chatgpt/worker", displayHost(cfg.Host), cfg.Port)
	var lastErr error
	// Server may still be binding; retry briefly.
	for attempt := 0; attempt < 20; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(`{"action":"open"}`))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if tok := strings.TrimSpace(cfg.AuthToken); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			lastErr = err
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(300 * time.Millisecond):
			}
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			var payload map[string]any
			_ = json.Unmarshal(body, &payload)
			if ok, _ := payload["ok"].(bool); ok || payload["ok"] == nil {
				return nil
			}
			if msg, _ := payload["error"].(string); strings.TrimSpace(msg) != "" {
				return fmt.Errorf("%s", msg)
			}
			return fmt.Errorf("open chatgpt failed: %s", strings.TrimSpace(string(body)))
		}
		// 404 while routes register, or 5xx during startup — retry.
		lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("open chatgpt: server not ready")
}

func displayHost(host string) string {
	if host == "" || host == "0.0.0.0" || host == "::" {
		return "127.0.0.1"
	}
	return host
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

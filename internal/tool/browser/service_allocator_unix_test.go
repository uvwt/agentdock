//go:build !windows

package browser

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestLocalExecAllocatorOptionsUseRequestTimeout(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "fake-browser.sh")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nsleep 5\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	profileDir := filepath.Join(root, "profile")
	req := StartRequest{
		Headless: true,
		Viewport: Viewport{Width: 800, Height: 600},
		Timeout:  200 * time.Millisecond,
	}
	allocatorCtx, allocatorCancel := chromedp.NewExecAllocator(
		context.Background(),
		localExecAllocatorOptions(executable, profileDir, req)...,
	)
	defer allocatorCancel()
	browserCtx, browserCancel := chromedp.NewContext(allocatorCtx)
	defer browserCancel()

	done := make(chan error, 1)
	started := time.Now()
	go func() {
		done <- chromedp.Run(browserCtx)
	}()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "websocket url timeout reached") {
			t.Fatalf("chromedp.Run() error = %v, want websocket URL timeout", err)
		}
		if elapsed := time.Since(started); elapsed > 2*time.Second {
			t.Fatalf("websocket URL timeout took %s, want request timeout %s", elapsed, req.Timeout)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("chromedp.Run() exceeded request-derived websocket URL timeout %s", req.Timeout)
	}
}

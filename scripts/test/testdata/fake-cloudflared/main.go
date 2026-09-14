package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("cloudflared version agentdock-test")
		return
	}

	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "resolve fake cloudflared executable:", err)
		os.Exit(1)
	}
	root := filepath.Dir(executable)
	incrementCount(filepath.Join(root, "start-count.txt"))
	if consumeFailure(filepath.Join(root, "fail-count.txt")) {
		fmt.Fprintln(os.Stderr, "fake cloudflared requested startup failure")
		os.Exit(1)
	}

	if hasArg("run") {
		runNamedTunnel(root)
		return
	}

	urlFile := filepath.Join(root, "quick-url-source.txt")
	data, err := os.ReadFile(urlFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read fake Quick Tunnel URL:", err)
		os.Exit(1)
	}
	publicURL := strings.TrimSpace(string(data))
	if publicURL == "" {
		fmt.Fprintln(os.Stderr, "fake Quick Tunnel URL is empty")
		os.Exit(1)
	}

	// Match the success marker emitted by cloudflared before the generated Quick Tunnel URL.
	fmt.Fprintln(os.Stderr, "INF Your quick Tunnel has been created! Visit it at:")
	fmt.Fprintln(os.Stderr, publicURL)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
}

func runNamedTunnel(root string) {
	token := strings.TrimSpace(os.Getenv("TUNNEL_TOKEN"))
	if token == "" {
		fmt.Fprintln(os.Stderr, "fake Named Tunnel did not receive TUNNEL_TOKEN")
		os.Exit(1)
	}
	for _, argument := range os.Args[1:] {
		if strings.Contains(argument, token) {
			fmt.Fprintln(os.Stderr, "fake Named Tunnel received its token through argv")
			os.Exit(1)
		}
	}

	// 这个固定测试值只用于驱动真实的 Named readiness 失败路径；生产代码没有测试 hook。
	// 无效 Token 仍保持进程存活，确保 readiness 失败来自认证日志，而不是瞬时进程竞态。
	if token == "agentdock-test-invalid-named-token" {
		fmt.Fprintln(os.Stderr, "ERR Provided Tunnel token is not valid.")
		waitForStop()
		return
	}

	_ = os.WriteFile(filepath.Join(root, "named-token-env-ok.txt"), []byte("true"), 0o600)
	fmt.Fprintln(os.Stderr, "INF Registered tunnel connection connIndex=0")
	waitForStop()
}

func hasArg(want string) bool {
	for _, argument := range os.Args[1:] {
		if argument == want {
			return true
		}
	}
	return false
}

func waitForStop() {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
}

func incrementCount(path string) {
	count := readCount(path) + 1
	_ = os.WriteFile(path, []byte(strconv.Itoa(count)), 0o600)
}

func consumeFailure(path string) bool {
	count := readCount(path)
	if count <= 0 {
		return false
	}
	_ = os.WriteFile(path, []byte(strconv.Itoa(count-1)), 0o600)
	return true
}

func readCount(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	count, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || count < 0 {
		return 0
	}
	return count
}

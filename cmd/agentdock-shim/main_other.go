//go:build !windows

package main

import "fmt"

func main() { panic(fmt.Errorf("agentdock-shim is only supported on Windows")) }

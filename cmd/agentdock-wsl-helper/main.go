//go:build linux

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/uvwt/agentdock/internal/wslfilehelper"
)

const protocolVersion = "1"

func main() {
	if len(os.Args) == 2 {
		switch os.Args[1] {
		case "--protocol-version":
			fmt.Println(protocolVersion)
			return
		case "--self-sha256":
			path, err := os.Executable()
			if err != nil {
				writeFailure(err)
				return
			}
			file, err := os.Open(path)
			if err != nil {
				writeFailure(err)
				return
			}
			defer file.Close()
			hash := sha256.New()
			if _, err := io.Copy(hash, file); err != nil {
				writeFailure(err)
				return
			}
			fmt.Println(hex.EncodeToString(hash.Sum(nil)))
			return
		}
	}

	decoder := json.NewDecoder(os.Stdin)
	var request wslfilehelper.Request
	if err := decoder.Decode(&request); err != nil {
		writeFailure(err)
		return
	}
	result, err := wslfilehelper.Dispatch(&request)
	if err != nil {
		writeFailure(err)
		return
	}
	result.OK = true
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func writeFailure(err error) {
	response := &wslfilehelper.Response{OK: false, Code: "WSL_FILE_RUNTIME_ERROR", Message: err.Error(), Details: map[string]any{"type": fmt.Sprintf("%T", err)}}
	var failure *wslfilehelper.ToolFailure
	if errors.As(err, &failure) {
		response.Code = failure.Code
		response.Message = failure.Message
		response.Details = failure.Details
	}
	_ = json.NewEncoder(os.Stdout).Encode(response)
}

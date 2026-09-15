//go:build windows

package main

import (
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unicode/utf8"

	"golang.org/x/sys/windows"
)

func runSetupRuntimeHost(args []string) (int, error) {
	flags := flag.NewFlagSet("setup-runtime-host", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	fileEncoded := flags.String("file-b64", "", "base64 encoded runtime executable path")
	argumentsEncoded := flags.String("args-b64", "", "base64 encoded raw Windows command line arguments")
	homeEncoded := flags.String("agentdock-home-b64", "", "base64 encoded AGENTDOCK_HOME")
	defaultDirEncoded := flags.String("agentdock-default-dir-b64", "", "base64 encoded AGENTDOCK_DEFAULT_DIR")
	stdoutEncoded := flags.String("stdout-b64", "", "base64 encoded stdout path")
	stderrEncoded := flags.String("stderr-b64", "", "base64 encoded stderr path")
	errorEncoded := flags.String("error-b64", "", "base64 encoded launcher error path")
	waitForExit := flags.Bool("wait", false, "wait for the runtime process to exit")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		if err == nil {
			err = errors.New("setup runtime host received unexpected positional arguments")
		}
		return 1, err
	}

	filePath, err := decodeSetupRuntimeValue(*fileEncoded, true)
	if err != nil {
		return 1, fmt.Errorf("decode setup runtime executable: %w", err)
	}
	arguments, err := decodeSetupRuntimeValue(*argumentsEncoded, false)
	if err != nil {
		return 1, fmt.Errorf("decode setup runtime arguments: %w", err)
	}
	agentDockHome, err := decodeSetupRuntimeValue(*homeEncoded, false)
	if err != nil {
		return 1, fmt.Errorf("decode AGENTDOCK_HOME: %w", err)
	}
	agentDockDefaultDir, err := decodeSetupRuntimeValue(*defaultDirEncoded, false)
	if err != nil {
		return 1, fmt.Errorf("decode AGENTDOCK_DEFAULT_DIR: %w", err)
	}
	stdoutPath, err := decodeSetupRuntimeValue(*stdoutEncoded, false)
	if err != nil {
		return 1, fmt.Errorf("decode setup runtime stdout path: %w", err)
	}
	stderrPath, err := decodeSetupRuntimeValue(*stderrEncoded, false)
	if err != nil {
		return 1, fmt.Errorf("decode setup runtime stderr path: %w", err)
	}
	errorPath, err := decodeSetupRuntimeValue(*errorEncoded, false)
	if err != nil {
		return 1, fmt.Errorf("decode setup runtime error path: %w", err)
	}
	if *waitForExit && (stdoutPath == "" || stderrPath == "") {
		return 1, errors.New("setup runtime host requires stdout and stderr paths when waiting")
	}

	exitCode, runErr := launchSetupRuntimeProcess(
		filePath,
		arguments,
		agentDockHome,
		agentDockDefaultDir,
		stdoutPath,
		stderrPath,
		*waitForExit,
	)
	if runErr != nil && errorPath != "" {
		if writeErr := os.WriteFile(errorPath, []byte(runErr.Error()+"\r\n"), 0o600); writeErr != nil {
			runErr = errors.Join(runErr, fmt.Errorf("write setup runtime launcher error: %w", writeErr))
		}
	}
	return exitCode, runErr
}

func decodeSetupRuntimeValue(encoded string, required bool) (string, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		if required {
			return "", errors.New("value is required")
		}
		return "", nil
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(decoded) {
		return "", errors.New("value is not valid UTF-8")
	}
	value := string(decoded)
	if required && strings.TrimSpace(value) == "" {
		return "", errors.New("value is empty")
	}
	return value, nil
}

func launchSetupRuntimeProcess(filePath, arguments, agentDockHome, agentDockDefaultDir, stdoutPath, stderrPath string, waitForExit bool) (int, error) {
	command := exec.Command(filePath)
	creationFlags := uint32(windows.CREATE_NO_WINDOW)
	if !waitForExit {
		creationFlags |= windows.CREATE_NEW_PROCESS_GROUP
	}
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: creationFlags,
		HideWindow:    true,
	}
	if strings.TrimSpace(arguments) != "" {
		// SysProcAttr.CmdLine preserves the already-quoted Windows argument string produced by
		// Setup without routing it through cmd.exe or another console host.
		command.SysProcAttr.CmdLine = syscall.EscapeArg(filePath) + " " + arguments
	}
	command.Env = os.Environ()
	if agentDockHome != "" {
		command.Env = replaceWindowsEnvironment(command.Env, "AGENTDOCK_HOME", agentDockHome)
	}
	if agentDockDefaultDir != "" {
		command.Env = replaceWindowsEnvironment(command.Env, "AGENTDOCK_DEFAULT_DIR", agentDockDefaultDir)
	}

	if waitForExit {
		stdoutFile, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return 1, fmt.Errorf("open setup runtime stdout: %w", err)
		}
		defer stdoutFile.Close()
		stderrFile, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return 1, fmt.Errorf("open setup runtime stderr: %w", err)
		}
		defer stderrFile.Close()
		command.Stdout = stdoutFile
		command.Stderr = stderrFile
		if err := command.Run(); err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				return exitErr.ExitCode(), nil
			}
			return 1, fmt.Errorf("run setup runtime process: %w", err)
		}
		return 0, nil
	}

	nullFile, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return 1, fmt.Errorf("open null device for setup runtime process: %w", err)
	}
	defer nullFile.Close()
	command.Stdin = nullFile
	command.Stdout = nullFile
	command.Stderr = nullFile
	if err := command.Start(); err != nil {
		return 1, fmt.Errorf("start setup runtime process: %w", err)
	}
	if err := command.Process.Release(); err != nil {
		return 1, fmt.Errorf("release setup runtime process: %w", err)
	}
	return 0, nil
}

func replaceWindowsEnvironment(environment []string, name, value string) []string {
	result := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		key, _, found := strings.Cut(entry, "=")
		if found && strings.EqualFold(key, name) {
			continue
		}
		result = append(result, entry)
	}
	return append(result, name+"="+value)
}

//go:build linux

package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"unsafe"
)

const (
	landlockCreateRulesetVersion = 1
	landlockRulePathBeneath      = 1
	prSetNoNewPrivs              = 38

	sysLandlockCreateRuleset = 444
	sysLandlockAddRule       = 445
	sysLandlockRestrictSelf  = 446

	accessExecute    = 1 << 0
	accessWriteFile  = 1 << 1
	accessReadFile   = 1 << 2
	accessReadDir    = 1 << 3
	accessRemoveDir  = 1 << 4
	accessRemoveFile = 1 << 5
	accessMakeChar   = 1 << 6
	accessMakeDir    = 1 << 7
	accessMakeReg    = 1 << 8
	accessMakeSock   = 1 << 9
	accessMakeFIFO   = 1 << 10
	accessMakeBlock  = 1 << 11
	accessMakeSym    = 1 << 12
	accessRefer      = 1 << 13
	accessTruncate   = 1 << 14
	accessIoctlDev   = 1 << 15
)

type Status struct {
	Enabled  bool     `json:"enabled"`
	Warnings []string `json:"warnings,omitempty"`
}

type rulesetAttr struct {
	HandledAccessFS uint64
}

type pathBeneathAttr struct {
	AllowedAccess uint64
	ParentFD      int32
}

func StatusForWorkspace(workspace string) Status {
	fd, cleanup, status := openRuleset(workspace)
	if cleanup != nil {
		cleanup()
	}
	if fd >= 0 {
		_ = syscall.Close(fd)
	}
	return status
}

func PrepareCommand(cmd *exec.Cmd, workspace string) (func(), Status) {
	fd, cleanup, status := openRuleset(workspace)
	if fd < 0 {
		if cleanup != nil {
			cleanup()
		}
		return func() {}, status
	}
	command := shellCommandString(cmd.Args)
	exe, err := os.Executable()
	if err != nil {
		_ = syscall.Close(fd)
		return func() {}, Status{Enabled: false, Warnings: []string{"failed to locate current executable for Landlock helper: " + err.Error()}}
	}
	cmd.Path = exe
	cmd.Args = []string{exe, "__landlock_exec", "3", command}
	cmd.ExtraFiles = append(cmd.ExtraFiles, os.NewFile(uintptr(fd), "landlock-ruleset"))
	return func() {
		if cleanup != nil {
			cleanup()
		}
		_ = syscall.Close(fd)
	}, status
}

func ExecRestricted(fdText string) error {
	fd, err := strconv.Atoi(fdText)
	if err != nil {
		return err
	}
	actualFD := fd
	if _, _, errno := syscall.Syscall6(syscall.SYS_PRCTL, prSetNoNewPrivs, 1, 0, 0, 0, 0); errno != 0 {
		return errno
	}
	if _, _, errno := syscall.Syscall(sysLandlockRestrictSelf, uintptr(actualFD), 0, 0); errno != 0 {
		return errno
	}
	_ = syscall.Close(actualFD)
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	if len(os.Args) < 4 {
		return fmt.Errorf("missing restricted command")
	}
	return syscall.Exec(shell, []string{shell, "-c", os.Args[3]}, os.Environ())
}

func openRuleset(workspace string) (int, func(), Status) {
	version, errno := landlockVersion()
	if errno != 0 || version <= 0 {
		return -1, nil, Status{Enabled: false, Warnings: []string{"Linux Landlock is unavailable; command execution relies on external sandboxing."}}
	}
	handled := handledAccess(version)
	attr := rulesetAttr{HandledAccessFS: handled}
	fd, _, errno := syscall.Syscall(sysLandlockCreateRuleset, uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr), 0)
	if errno != 0 {
		return -1, nil, Status{Enabled: false, Warnings: []string{"failed to create Landlock ruleset: " + errno.Error()}}
	}
	cleanup := func() {}
	readonly := handled & (accessExecute | accessReadFile | accessReadDir)
	workspaceAccess := handled
	deviceAccess := readonly | (handled & (accessWriteFile | accessIoctlDev))

	if !addPath(int(fd), workspace, workspaceAccess, true) {
		_ = syscall.Close(int(fd))
		return -1, cleanup, Status{Enabled: false, Warnings: []string{"failed to add workspace to Landlock ruleset; command execution relies on external sandboxing."}}
	}
	for _, root := range allowRoots() {
		addPath(int(fd), root, readonly, false)
	}
	for _, special := range []string{"/dev/null", "/dev/zero", "/dev/random", "/dev/urandom"} {
		addPath(int(fd), special, deviceAccess, false)
	}
	addPath(int(fd), "/dev", deviceAccess, false)
	return int(fd), cleanup, Status{Enabled: true}
}

func landlockVersion() (int, syscall.Errno) {
	version, _, errno := syscall.Syscall(sysLandlockCreateRuleset, 0, 0, landlockCreateRulesetVersion)
	return int(version), errno
}

func handledAccess(version int) uint64 {
	handled := uint64(accessExecute | accessWriteFile | accessReadFile | accessReadDir | accessRemoveDir | accessRemoveFile | accessMakeChar | accessMakeDir | accessMakeReg | accessMakeSock | accessMakeFIFO | accessMakeBlock | accessMakeSym)
	if version >= 2 {
		handled |= accessRefer
	}
	if version >= 3 {
		handled |= accessTruncate
	}
	if version >= 5 {
		handled |= accessIoctlDev
	}
	return handled
}

func addPath(rulesetFD int, path string, access uint64, required bool) bool {
	fd, err := syscall.Open(path, syscall.O_PATH|syscall.O_CLOEXEC, 0)
	if err != nil {
		return !required
	}
	defer syscall.Close(fd)
	attr := pathBeneathAttr{AllowedAccess: access, ParentFD: int32(fd)}
	_, _, errno := syscall.Syscall6(sysLandlockAddRule, uintptr(rulesetFD), landlockRulePathBeneath, uintptr(unsafe.Pointer(&attr)), 0, 0, 0)
	return errno == 0 || !required
}

func allowRoots() []string {
	roots := []string{"/bin", "/etc", "/lib", "/lib64", "/sbin", "/usr", "/proc/self", "/proc/thread-self", "/dev/fd"}
	if exe, err := os.Executable(); err == nil {
		roots = append(roots, filepath.Dir(exe))
	}
	return roots
}

func shellCommandString(args []string) string {
	if len(args) >= 3 && args[1] == "-c" {
		return args[2]
	}
	if len(args) >= 4 && args[2] == "-c" {
		return args[3]
	}
	return ""
}

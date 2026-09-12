//go:build windows

package desktopruntime

import (
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modOle32             = windows.NewLazySystemDLL("ole32.dll")
	modOleaut32          = windows.NewLazySystemDLL("oleaut32.dll")
	procCoInitializeEx   = modOle32.NewProc("CoInitializeEx")
	procCoUninitialize   = modOle32.NewProc("CoUninitialize")
	procCoCreateInstance = modOle32.NewProc("CoCreateInstance")
	procCLSIDFromProgID  = modOleaut32.NewProc("CLSIDFromProgID")
	procSysAllocString   = modOleaut32.NewProc("SysAllocString")
	procSysFreeString    = modOleaut32.NewProc("SysFreeString")
)

const (
	coInitApartmentThreaded = 0x2
	clsctxInprocServer      = 0x1
	clsctxLocalServer       = 0x4
	vtEmpty                 = 0
	vtBstr                  = 8
	vtI4                    = 3
	vtBool                  = 11
	dispatchMethod          = 0x1
	dispatchPropertyGet     = 0x2
	dispatchPropertyPut     = 0x4
	variantTrue             = 0xFFFF
)

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var iidIDispatch = guid{0x00020400, 0x0000, 0x0000, [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}

type iUnknownVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
}

type iDispatchVtbl struct {
	iUnknownVtbl
	GetTypeInfoCount uintptr
	GetTypeInfo      uintptr
	GetIDsOfNames    uintptr
	Invoke           uintptr
}

type iDispatch struct {
	lpVtbl *iDispatchVtbl
}

type variant struct {
	VT         uint16
	wReserved1 uint16
	wReserved2 uint16
	wReserved3 uint16
	Val        int64
}

type dispParams struct {
	rgvarg            *variant
	rgdispidNamedArgs *int32
	cArgs             uint32
	cNamedArgs        uint32
}

func startInteractiveScheduledTaskNative(taskName, expectedUserSID string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hr, _, _ := procCoInitializeEx.Call(0, coInitApartmentThreaded)
	// S_OK、S_FALSE 和 RPC_E_CHANGED_MODE 都表示 COM 已可用。
	if hr != 0 && hr != 1 && hr != 0x80010106 {
		return fmt.Errorf("CoInitializeEx: HRESULT 0x%X", hr)
	}
	if hr == 0 || hr == 1 {
		defer procCoUninitialize.Call()
	}

	service, err := createDispatch("Schedule.Service")
	if err != nil {
		return err
	}
	defer releaseDispatch(service)
	if _, err := invokeDispatch(service, "Connect", dispatchMethod); err != nil {
		return fmt.Errorf("Task Scheduler Connect: %w", err)
	}
	folderVar, err := invokeDispatch(service, "GetFolder", dispatchMethod, variantBSTR(`\`))
	if err != nil {
		return fmt.Errorf("Task Scheduler GetFolder: %w", err)
	}
	folder := variantDispatch(folderVar)
	if folder == nil {
		return fmt.Errorf("Task Scheduler folder is missing")
	}
	defer releaseDispatch(folder)
	taskVar, err := invokeDispatch(folder, "GetTask", dispatchMethod, variantBSTR(taskName))
	if err != nil {
		return fmt.Errorf("Task Scheduler GetTask %s: %w", taskName, err)
	}
	task := variantDispatch(taskVar)
	if task == nil {
		return fmt.Errorf("scheduled task %s is missing", taskName)
	}
	defer releaseDispatch(task)

	definitionVar, err := invokeDispatch(task, "Definition", dispatchPropertyGet)
	if err != nil {
		return err
	}
	definition := variantDispatch(definitionVar)
	if definition == nil {
		return fmt.Errorf("scheduled task %s has no definition", taskName)
	}
	defer releaseDispatch(definition)
	principalVar, err := invokeDispatch(definition, "Principal", dispatchPropertyGet)
	if err != nil {
		return err
	}
	principal := variantDispatch(principalVar)
	if principal == nil {
		return fmt.Errorf("scheduled task %s has no principal", taskName)
	}
	defer releaseDispatch(principal)

	userVar, err := invokeDispatch(principal, "UserId", dispatchPropertyGet)
	if err != nil {
		return err
	}
	taskUser := strings.TrimSpace(variantString(userVar))
	taskSID, err := windowsUserSID(taskUser)
	if err != nil {
		return fmt.Errorf("resolve scheduled-task user %s: %w", taskUser, err)
	}
	if strings.TrimSpace(expectedUserSID) != "" {
		expected, err := windowsUserSID(expectedUserSID)
		if err != nil {
			return err
		}
		if !strings.EqualFold(taskSID, expected) {
			return fmt.Errorf("Scheduled task '%s' belongs to SID %s, not expected SID %s.", taskName, taskSID, expected)
		}
	}
	logonVar, err := invokeDispatch(principal, "LogonType", dispatchPropertyGet)
	if err != nil {
		return err
	}
	if int(variantInt(logonVar)) != taskLogonInteractiveToken {
		return fmt.Errorf("Scheduled task '%s' is not an InteractiveToken task; refusing the session-bound start path.", taskName)
	}

	sessions, err := enumerateInteractiveSessions()
	if err != nil {
		return err
	}
	currentSession, err := currentProcessSessionID()
	if err != nil {
		return err
	}
	token := windows.GetCurrentProcessToken()
	currentSID, err := tokenUserSID(token)
	if err != nil {
		return err
	}
	sessionID, err := SelectInteractiveTaskSessionID(sessions, taskSID, currentSession, currentSID, activeConsoleSessionID())
	if err != nil {
		return err
	}

	enabledVar, err := invokeDispatch(task, "Enabled", dispatchPropertyGet)
	if err != nil {
		return err
	}
	wasEnabled := variantInt(enabledVar) != 0
	if !wasEnabled {
		if _, err := invokeDispatch(task, "Enabled", dispatchPropertyPut, variantBool(true)); err != nil {
			return err
		}
		defer func() {
			_, _ = invokeDispatch(task, "Enabled", dispatchPropertyPut, variantBool(false))
		}()
	}
	runningVar, err := invokeDispatch(task, "RunEx", dispatchMethod, variantEmpty(), variantInt32(taskRunUseSessionID), variantInt32(int32(sessionID)), variantEmpty())
	if err != nil {
		return fmt.Errorf("RunEx session %d: %w", sessionID, err)
	}
	if running := variantDispatch(runningVar); running != nil {
		releaseDispatch(running)
	}
	return nil
}

func tokenUserSID(token windows.Token) (string, error) {
	user, err := token.GetTokenUser()
	if err != nil {
		return "", err
	}
	return user.User.Sid.String(), nil
}

func createDispatch(progID string) (*iDispatch, error) {
	ptr, err := syscall.UTF16PtrFromString(progID)
	if err != nil {
		return nil, err
	}
	var clsid guid
	hr, _, _ := procCLSIDFromProgID.Call(uintptr(unsafe.Pointer(ptr)), uintptr(unsafe.Pointer(&clsid)))
	if hr != 0 {
		return nil, fmt.Errorf("CLSIDFromProgID %s: HRESULT 0x%X", progID, hr)
	}
	var unknown *iDispatch
	hr, _, _ = procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsid)),
		0,
		clsctxInprocServer|clsctxLocalServer,
		uintptr(unsafe.Pointer(&iidIDispatch)),
		uintptr(unsafe.Pointer(&unknown)),
	)
	if hr != 0 || unknown == nil {
		return nil, fmt.Errorf("CoCreateInstance %s: HRESULT 0x%X", progID, hr)
	}
	return unknown, nil
}

func releaseDispatch(object *iDispatch) {
	if object == nil || object.lpVtbl == nil {
		return
	}
	syscall.Syscall(object.lpVtbl.Release, 1, uintptr(unsafe.Pointer(object)), 0, 0)
}

func invokeDispatch(object *iDispatch, name string, flags uint16, args ...variant) (variant, error) {
	dispID, err := dispatchID(object, name)
	if err != nil {
		return variant{}, err
	}
	var namedDispID int32 = -3 // DISPID_PROPERTYPUT
	params := dispParams{cArgs: uint32(len(args))}
	if len(args) > 0 {
		reversed := make([]variant, len(args))
		for i := range args {
			reversed[i] = args[len(args)-1-i]
		}
		params.rgvarg = &reversed[0]
		if flags&dispatchPropertyPut != 0 {
			params.rgdispidNamedArgs = &namedDispID
			params.cNamedArgs = 1
		}
	}
	var result variant
	var iidNull guid
	hr, _, _ := syscall.Syscall9(
		object.lpVtbl.Invoke,
		9,
		uintptr(unsafe.Pointer(object)),
		uintptr(dispID),
		uintptr(unsafe.Pointer(&iidNull)),
		0,
		uintptr(flags),
		uintptr(unsafe.Pointer(&params)),
		uintptr(unsafe.Pointer(&result)),
		0,
		0,
	)
	if hr != 0 {
		freeVariants(args)
		return variant{}, fmt.Errorf("%s HRESULT 0x%X", name, hr)
	}
	freeVariants(args)
	return result, nil
}

func freeVariants(values []variant) {
	for _, value := range values {
		if value.VT == vtBstr && value.Val != 0 {
			procSysFreeString.Call(uintptr(value.Val))
		}
	}
}

func dispatchID(object *iDispatch, name string) (int32, error) {
	ptr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	names := []*uint16{ptr}
	var dispID int32
	var iidNull guid
	hr, _, _ := syscall.Syscall6(
		object.lpVtbl.GetIDsOfNames,
		6,
		uintptr(unsafe.Pointer(object)),
		uintptr(unsafe.Pointer(&iidNull)),
		uintptr(unsafe.Pointer(&names[0])),
		1,
		0,
		uintptr(unsafe.Pointer(&dispID)),
	)
	if hr != 0 {
		return 0, fmt.Errorf("GetIDsOfNames %s: HRESULT 0x%X", name, hr)
	}
	return dispID, nil
}

func variantBSTR(value string) variant {
	ptr, _ := syscall.UTF16PtrFromString(value)
	bstr, _, _ := procSysAllocString.Call(uintptr(unsafe.Pointer(ptr)))
	return variant{VT: vtBstr, Val: int64(bstr)}
}

func variantInt32(value int32) variant {
	return variant{VT: vtI4, Val: int64(value)}
}

func variantBool(value bool) variant {
	if value {
		return variant{VT: vtBool, Val: variantTrue}
	}
	return variant{VT: vtBool, Val: 0}
}

func variantEmpty() variant { return variant{VT: vtEmpty} }

func variantDispatch(value variant) *iDispatch {
	if value.VT != 9 && value.VT != 13 {
		return nil
	}
	return (*iDispatch)(unsafe.Pointer(uintptr(value.Val)))
}

func variantString(value variant) string {
	if value.VT != vtBstr || value.Val == 0 {
		return fmt.Sprint(variantInt(value))
	}
	text := windows.UTF16PtrToString((*uint16)(unsafe.Pointer(uintptr(value.Val))))
	procSysFreeString.Call(uintptr(value.Val))
	return text
}

func variantInt(value variant) int64 {
	return value.Val
}

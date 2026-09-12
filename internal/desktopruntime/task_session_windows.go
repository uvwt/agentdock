//go:build windows

package desktopruntime

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modWtsapi32                      = windows.NewLazySystemDLL("wtsapi32.dll")
	procWTSEnumerateSessionsW        = modWtsapi32.NewProc("WTSEnumerateSessionsW")
	procWTSQuerySessionInformationW  = modWtsapi32.NewProc("WTSQuerySessionInformationW")
	procWTSFreeMemory                = modWtsapi32.NewProc("WTSFreeMemory")
	procWTSGetActiveConsoleSessionId = modWtsapi32.NewProc("WTSGetActiveConsoleSessionId")
)

const (
	wtsUserName               = 5
	wtsDomainName             = 7
	taskLogonInteractiveToken = 3
	taskRunUseSessionID       = 4
)

type wtsSessionInfo struct {
	SessionID      uint32
	WinStationName *uint16
	State          int32
}

func enumerateInteractiveSessions() ([]InteractiveSession, error) {
	var buffer uintptr
	var count uint32
	ok, _, callErr := procWTSEnumerateSessionsW.Call(0, 0, 1, uintptr(unsafe.Pointer(&buffer)), uintptr(unsafe.Pointer(&count)))
	if ok == 0 {
		return nil, fmt.Errorf("WTSEnumerateSessions failed: %w", callErr)
	}
	defer procWTSFreeMemory.Call(buffer)

	sessions := make([]InteractiveSession, 0, count)
	size := unsafe.Sizeof(wtsSessionInfo{})
	for i := uint32(0); i < count; i++ {
		entry := (*wtsSessionInfo)(unsafe.Pointer(buffer + uintptr(i)*size))
		userName := querySessionText(entry.SessionID, wtsUserName)
		domain := querySessionText(entry.SessionID, wtsDomainName)
		account := userName
		if userName != "" && domain != "" {
			account = domain + `\` + userName
		}
		sid := ""
		if account != "" {
			if resolved, err := windowsUserSID(account); err == nil {
				sid = resolved
			}
		}
		sessions = append(sessions, InteractiveSession{
			SessionID: entry.SessionID,
			State:     int(entry.State),
			UserSID:   sid,
		})
	}
	return sessions, nil
}

func querySessionText(sessionID uint32, infoClass int) string {
	var buffer uintptr
	var bytesReturned uint32
	ok, _, _ := procWTSQuerySessionInformationW.Call(
		0,
		uintptr(sessionID),
		uintptr(infoClass),
		uintptr(unsafe.Pointer(&buffer)),
		uintptr(unsafe.Pointer(&bytesReturned)),
	)
	if ok == 0 || buffer == 0 || bytesReturned <= 2 {
		return ""
	}
	defer procWTSFreeMemory.Call(buffer)
	return strings.TrimRight(windows.UTF16PtrToString((*uint16)(unsafe.Pointer(buffer))), "\x00")
}

func activeConsoleSessionID() uint32 {
	id, _, _ := procWTSGetActiveConsoleSessionId.Call()
	return uint32(id)
}

func windowsUserSID(account string) (string, error) {
	sid, _, _, err := syscall.LookupSID("", account)
	if err != nil {
		if parsed, parseErr := syscall.StringToSid(account); parseErr == nil {
			return parsed.String()
		}
		return "", err
	}
	return sid.String()
}

func currentProcessSessionID() (int, error) {
	var sessionID uint32
	if err := windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &sessionID); err != nil {
		return 0, err
	}
	return int(sessionID), nil
}

//go:build windows

package tray

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/uvwt/agentdock/internal/desktopruntime"
)

const (
	wmDestroy       = 0x0002
	wmClose         = 0x0010
	wmCommand       = 0x0111
	wmTimer         = 0x0113
	wmNull          = 0x0000
	wmUser          = 0x0400
	wmTrayIcon      = wmUser + 1
	wmRButtonUp     = 0x0205
	wmLButtonDblClk = 0x0203

	nimAdd    = 0x00000000
	nimModify = 0x00000001
	nimDelete = 0x00000002

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004
	nifInfo    = 0x00000010

	niifInfo  = 0x00000001
	niifError = 0x00000003

	mfString    = 0x00000000
	mfDisabled  = 0x00000002
	mfGrayed    = 0x00000001
	mfSeparator = 0x00000800

	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100

	idiApplication = 32512
	idcArrow       = 32512
	imageIcon      = 1
	lrLoadFromFile = 0x0010
	lrDefaultSize  = 0x0040
	cfUnicodeText  = 13
	gmemMoveable   = 0x0002
	swShow         = 5

	menuStatus          = 1001
	menuCopyLocal       = 1002
	menuCopyPublic      = 1003
	menuStart           = 1004
	menuRestart         = 1005
	menuUpdate          = 1006
	menuOpenFolder      = 1007
	menuOpenDocs        = 1008
	menuExit            = 1009
	menuRefreshQuickURL = 1010
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassExW    = user32.NewProc("RegisterClassExW")
	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procPostQuitMessage     = user32.NewProc("PostQuitMessage")
	procLoadIconW           = user32.NewProc("LoadIconW")
	procLoadImageW          = user32.NewProc("LoadImageW")
	procLoadCursorW         = user32.NewProc("LoadCursorW")
	procCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	procAppendMenuW         = user32.NewProc("AppendMenuW")
	procDestroyMenu         = user32.NewProc("DestroyMenu")
	procTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procPostMessageW        = user32.NewProc("PostMessageW")
	procSetTimer            = user32.NewProc("SetTimer")
	procKillTimer           = user32.NewProc("KillTimer")
	procOpenClipboard       = user32.NewProc("OpenClipboard")
	procCloseClipboard      = user32.NewProc("CloseClipboard")
	procEmptyClipboard      = user32.NewProc("EmptyClipboard")
	procSetClipboardData    = user32.NewProc("SetClipboardData")
	procRegisterWindowMsgW  = user32.NewProc("RegisterWindowMessageW")
	procShellNotifyIconW    = shell32.NewProc("Shell_NotifyIconW")
	procShellExecuteW       = shell32.NewProc("ShellExecuteW")
	procGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
	procCreateMutexW        = kernel32.NewProc("CreateMutexW")
	procGlobalAlloc         = kernel32.NewProc("GlobalAlloc")
	procGlobalLock          = kernel32.NewProc("GlobalLock")
	procGlobalUnlock        = kernel32.NewProc("GlobalUnlock")
	procGlobalFree          = kernel32.NewProc("GlobalFree")
	procRtlMoveMemory       = kernel32.NewProc("RtlMoveMemory")

	activeTray            *trayApp
	taskbarCreatedMessage uint32
)

type point struct {
	X int32
	Y int32
}

type message struct {
	Window  windows.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Point   point
	Private uint32
}

type windowClassEx struct {
	Size            uint32
	Style           uint32
	WindowProcedure uintptr
	ClassExtra      int32
	WindowExtra     int32
	Instance        windows.Handle
	Icon            windows.Handle
	Cursor          windows.Handle
	Background      windows.Handle
	MenuName        *uint16
	ClassName       *uint16
	SmallIcon       windows.Handle
}

type notifyIconData struct {
	Size             uint32
	Window           windows.Handle
	ID               uint32
	Flags            uint32
	CallbackMessage  uint32
	Icon             windows.Handle
	Tip              [128]uint16
	State            uint32
	StateMask        uint32
	Info             [256]uint16
	VersionOrTimeout uint32
	InfoTitle        [64]uint16
	InfoFlags        uint32
	ItemGUID         windows.GUID
	BalloonIcon      windows.Handle
}

type healthResponse struct {
	OK      bool   `json:"ok"`
	Version string `json:"version"`
}

type trayState struct {
	Manifest desktopruntime.Manifest
	Healthy  bool
	Version  string
	Err      error
}

type trayApp struct {
	window       windows.Handle
	icon         windows.Handle
	manifestPath string
	httpClient   *http.Client
}

// Run starts the Windows tray application and blocks until the user exits it.
func Run() error {
	// Win32 windows and their message queues are owned by the creating OS
	// thread. Keep the Go goroutine pinned for the complete tray lifetime.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// The tray is a view/controller only. A per-user mutex prevents duplicate
	// icons without turning the tray into the AgentDock process supervisor.
	mutexName, err := windows.UTF16PtrFromString(`Local\AgentDockTray`)
	if err != nil {
		return err
	}
	mutex, _, createErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(mutexName)))
	if mutex == 0 {
		return fmt.Errorf("create tray mutex: %w", createErr)
	}
	defer windows.CloseHandle(windows.Handle(mutex))
	if errors.Is(createErr, windows.ERROR_ALREADY_EXISTS) {
		return nil
	}

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve tray executable: %w", err)
	}
	app := &trayApp{
		manifestPath: desktopruntime.PathForBinary(filepath.Join(filepath.Dir(executable), "agentdock.exe")),
		httpClient:   &http.Client{Timeout: 1500 * time.Millisecond},
	}
	activeTray = app
	defer func() { activeTray = nil }()
	return app.runMessageLoop()
}

func (app *trayApp) runMessageLoop() error {
	instance, _, err := procGetModuleHandleW.Call(0)
	if instance == 0 {
		return fmt.Errorf("get module handle: %w", err)
	}
	app.icon = loadTrayIcon(app.manifestPath)
	cursor, _, _ := procLoadCursorW.Call(0, idcArrow)

	className := windows.StringToUTF16Ptr("AgentDockTrayWindow")
	class := windowClassEx{
		Size:            uint32(unsafe.Sizeof(windowClassEx{})),
		WindowProcedure: syscall.NewCallback(windowProcedure),
		Instance:        windows.Handle(instance),
		Icon:            app.icon,
		Cursor:          windows.Handle(cursor),
		ClassName:       className,
		SmallIcon:       app.icon,
	}
	atom, _, registerErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&class)))
	if atom == 0 {
		return fmt.Errorf("register tray window class: %w", registerErr)
	}

	windowName := windows.StringToUTF16Ptr("AgentDock Tray")
	window, _, createErr := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		0,
		0, 0, 0, 0,
		0, 0,
		instance,
		0,
	)
	if window == 0 {
		return fmt.Errorf("create tray window: %w", createErr)
	}
	app.window = windows.Handle(window)

	taskbarName := windows.StringToUTF16Ptr("TaskbarCreated")
	registered, _, _ := procRegisterWindowMsgW.Call(uintptr(unsafe.Pointer(taskbarName)))
	taskbarCreatedMessage = uint32(registered)
	if err := app.addIcon(); err != nil {
		procDestroyWindow.Call(window)
		return err
	}
	procSetTimer.Call(window, 1, 30000, 0)
	app.refreshTooltip()

	var msg message
	for {
		result, _, getErr := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(result) == -1 {
			return fmt.Errorf("read tray window message: %w", getErr)
		}
		if result == 0 {
			return nil
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func loadTrayIcon(manifestPath string) windows.Handle {
	iconPath := filepath.Join(filepath.Dir(manifestPath), "bin", "agentdock.ico")
	if pointer, err := windows.UTF16PtrFromString(iconPath); err == nil {
		icon, _, _ := procLoadImageW.Call(
			0,
			uintptr(unsafe.Pointer(pointer)),
			imageIcon,
			0,
			0,
			lrLoadFromFile|lrDefaultSize,
		)
		if icon != 0 {
			return windows.Handle(icon)
		}
	}
	icon, _, _ := procLoadIconW.Call(0, idiApplication)
	return windows.Handle(icon)
}

func windowProcedure(window uintptr, message uint32, wParam, lParam uintptr) uintptr {
	app := activeTray
	if app == nil {
		result, _, _ := procDefWindowProcW.Call(window, uintptr(message), wParam, lParam)
		return result
	}
	if taskbarCreatedMessage != 0 && message == taskbarCreatedMessage {
		_ = app.addIcon()
		app.refreshTooltip()
		return 0
	}
	switch message {
	case wmTrayIcon:
		switch uint32(lParam) {
		case wmRButtonUp:
			app.showMenu()
		case wmLButtonDblClk:
			app.openDocumentation()
		}
		return 0
	case wmCommand:
		app.handleMenu(uint16(wParam & 0xffff))
		return 0
	case wmTimer:
		app.refreshTooltip()
		return 0
	case wmClose:
		procDestroyWindow.Call(window)
		return 0
	case wmDestroy:
		procKillTimer.Call(window, 1)
		app.removeIcon()
		procPostQuitMessage.Call(0)
		return 0
	}
	result, _, _ := procDefWindowProcW.Call(window, uintptr(message), wParam, lParam)
	return result
}

func (app *trayApp) addIcon() error {
	data := notifyIconData{
		Size:            uint32(unsafe.Sizeof(notifyIconData{})),
		Window:          app.window,
		ID:              1,
		Flags:           nifMessage | nifIcon | nifTip,
		CallbackMessage: wmTrayIcon,
		Icon:            app.icon,
	}
	copyUTF16(data.Tip[:], "AgentDock")
	result, _, err := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&data)))
	if result == 0 {
		return fmt.Errorf("add tray icon: %w", err)
	}
	return nil
}

func (app *trayApp) removeIcon() {
	data := notifyIconData{Size: uint32(unsafe.Sizeof(notifyIconData{})), Window: app.window, ID: 1}
	procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&data)))
}

func (app *trayApp) refreshTooltip() {
	state := app.readState()
	labels := currentTrayText()
	text := labels.TooltipStopped
	if state.Healthy {
		text = labels.TooltipRunning
		if state.Version != "" {
			text += " v" + strings.TrimPrefix(state.Version, "v")
		}
	} else if state.Err != nil {
		text = labels.TooltipUnavailable
	}
	data := notifyIconData{
		Size:   uint32(unsafe.Sizeof(notifyIconData{})),
		Window: app.window,
		ID:     1,
		Flags:  nifTip,
	}
	copyUTF16(data.Tip[:], text)
	procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&data)))
}

func (app *trayApp) showMenu() {
	state := app.readState()
	labels := currentTrayText()
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)

	status := labels.StatusStopped
	if state.Healthy {
		status = labels.StatusRunning
		if state.Version != "" {
			status += " v" + strings.TrimPrefix(state.Version, "v")
		}
	} else if state.Err != nil {
		status = labels.StatusUnavailable
	}
	appendMenu(menu, mfString|mfDisabled|mfGrayed, menuStatus, status)
	appendMenu(menu, mfSeparator, 0, "")
	appendMenu(menu, menuFlags(state.Manifest.LocalMCPURL != ""), menuCopyLocal, labels.CopyLocalMCP)
	appendMenu(menu, menuFlags(state.Manifest.PublicURL != ""), menuCopyPublic, labels.CopyPublicMCP)
	if state.Manifest.TunnelMode == "quick" {
		appendMenu(
			menu,
			menuFlags(state.Manifest.AgentDockBinary != ""),
			menuRefreshQuickURL,
			labels.RefreshQuickURL,
		)
	}
	appendMenu(menu, mfSeparator, 0, "")
	appendMenu(menu, menuFlags(!state.Healthy && state.Manifest.AgentDockBinary != ""), menuStart, labels.StartAgentDock)
	appendMenu(menu, menuFlags(state.Manifest.AgentDockBinary != ""), menuRestart, labels.RestartAgentDock)
	appendMenu(menu, menuFlags(state.Manifest.AgentDockBinary != ""), menuUpdate, labels.InstallUpdate)
	appendMenu(menu, mfSeparator, 0, "")
	appendMenu(menu, mfString, menuOpenFolder, labels.OpenRuntimeFolder)
	appendMenu(menu, mfString, menuOpenDocs, labels.OpenDocumentation)
	appendMenu(menu, mfSeparator, 0, "")
	appendMenu(menu, mfString, menuExit, labels.ExitTray)

	var cursor point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&cursor)))
	procSetForegroundWindow.Call(uintptr(app.window))
	command, _, _ := procTrackPopupMenu.Call(
		menu,
		tpmRightButton|tpmReturnCmd,
		uintptr(cursor.X),
		uintptr(cursor.Y),
		0,
		uintptr(app.window),
		0,
	)
	if command != 0 {
		app.handleMenu(uint16(command))
	}
	procPostMessageW.Call(uintptr(app.window), wmNull, 0, 0)
}

func (app *trayApp) handleMenu(command uint16) {
	state := app.readState()
	labels := currentTrayText()
	switch command {
	case menuCopyLocal:
		if err := setClipboardText(state.Manifest.LocalMCPURL); err != nil {
			app.notify("AgentDock", fmt.Sprintf(labels.CopyLocalFailed, err), true)
			return
		}
		app.notify("AgentDock", labels.CopyLocalSucceeded, false)
	case menuCopyPublic:
		if state.Manifest.PublicURL == "" {
			return
		}
		if err := setClipboardText(strings.TrimRight(state.Manifest.PublicURL, "/") + "/mcp"); err != nil {
			app.notify("AgentDock", fmt.Sprintf(labels.CopyPublicFailed, err), true)
			return
		}
		app.notify("AgentDock", labels.CopyPublicSucceeded, false)
	case menuRefreshQuickURL:
		if err := regenerateQuickTunnel(state.Manifest); err != nil {
			app.notify("AgentDock", fmt.Sprintf(labels.RefreshQuickFailed, err), true)
			return
		}
		app.notify("AgentDock", labels.RefreshQuickStarted, false)
	case menuStart:
		if err := startAgentDock(state.Manifest); err != nil {
			app.notify("AgentDock", fmt.Sprintf(labels.StartFailed, err), true)
			return
		}
		app.notify("AgentDock", labels.StartStarted, false)
	case menuRestart:
		if err := restartAgentDock(state.Manifest); err != nil {
			app.notify("AgentDock", fmt.Sprintf(labels.RestartFailed, err), true)
			return
		}
		app.notify("AgentDock", labels.RestartStarted, false)
	case menuUpdate:
		if err := launchUpdate(state.Manifest); err != nil {
			app.notify("AgentDock", fmt.Sprintf(labels.UpdateFailed, err), true)
		}
	case menuOpenFolder:
		_ = exec.Command("explorer.exe", filepath.Dir(app.manifestPath)).Start()
	case menuOpenDocs:
		app.openDocumentation()
	case menuExit:
		procDestroyWindow.Call(uintptr(app.window))
	}
}

func (app *trayApp) readState() trayState {
	manifest, err := desktopruntime.Load(app.manifestPath)
	if err != nil {
		return trayState{Err: err}
	}
	request, err := http.NewRequest(http.MethodGet, manifest.HealthURL(), nil)
	if err != nil {
		return trayState{Manifest: manifest, Err: err}
	}
	response, err := app.httpClient.Do(request)
	if err != nil {
		return trayState{Manifest: manifest, Err: err}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return trayState{Manifest: manifest, Err: fmt.Errorf("health check returned HTTP %d", response.StatusCode)}
	}
	var health healthResponse
	if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
		return trayState{Manifest: manifest, Err: err}
	}
	return trayState{Manifest: manifest, Healthy: health.OK, Version: health.Version}
}

func (app *trayApp) notify(title, body string, failed bool) {
	data := notifyIconData{
		Size:   uint32(unsafe.Sizeof(notifyIconData{})),
		Window: app.window,
		ID:     1,
		Flags:  nifInfo,
	}
	copyUTF16(data.InfoTitle[:], title)
	copyUTF16(data.Info[:], body)
	data.InfoFlags = niifInfo
	if failed {
		data.InfoFlags = niifError
	}
	procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&data)))
}

func (app *trayApp) openDocumentation() {
	_ = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", "https://uvwt.github.io/agentdock-docs/").Start()
}

func appendMenu(menu uintptr, flags uintptr, id uint16, text string) {
	var textPointer *uint16
	if text != "" {
		textPointer = windows.StringToUTF16Ptr(text)
	}
	procAppendMenuW.Call(menu, flags, uintptr(id), uintptr(unsafe.Pointer(textPointer)))
}

func menuFlags(enabled bool) uintptr {
	if enabled {
		return mfString
	}
	return mfString | mfDisabled | mfGrayed
}

func copyUTF16(destination []uint16, value string) {
	encoded := windows.StringToUTF16(value)
	if len(encoded) > len(destination) {
		encoded = encoded[:len(destination)]
		encoded[len(encoded)-1] = 0
	}
	copy(destination, encoded)
}

func setClipboardText(value string) error {
	labels := currentTrayText()
	if strings.TrimSpace(value) == "" {
		return errors.New(labels.ClipboardNoAddress)
	}
	encoded := windows.StringToUTF16(value)
	size := uintptr(len(encoded) * 2)
	memory, _, allocErr := procGlobalAlloc.Call(gmemMoveable, size)
	if memory == 0 {
		return fmt.Errorf(labels.ClipboardAllocFailed, allocErr)
	}
	transferred := false
	defer func() {
		if !transferred {
			procGlobalFree.Call(memory)
		}
	}()

	pointer, _, lockErr := procGlobalLock.Call(memory)
	if pointer == 0 {
		return fmt.Errorf(labels.ClipboardLockFailed, lockErr)
	}
	// 目标地址属于 Windows 全局内存，不把它包装成 Go 切片，避免 uintptr
	// 跨越调用边界后重新转换为 unsafe.Pointer 的生命周期风险。
	procRtlMoveMemory.Call(pointer, uintptr(unsafe.Pointer(&encoded[0])), size)
	procGlobalUnlock.Call(memory)

	opened := false
	for attempt := 0; attempt < 10; attempt++ {
		result, _, _ := procOpenClipboard.Call(0)
		if result != 0 {
			opened = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !opened {
		return errors.New(labels.ClipboardBusy)
	}
	defer procCloseClipboard.Call()
	if result, _, emptyErr := procEmptyClipboard.Call(); result == 0 {
		return fmt.Errorf(labels.ClipboardClearFailed, emptyErr)
	}
	if result, _, setErr := procSetClipboardData.Call(cfUnicodeText, memory); result == 0 {
		return fmt.Errorf(labels.ClipboardWriteFailed, setErr)
	}
	transferred = true
	return nil
}

func runtimeRootForManifest(manifest desktopruntime.Manifest) (string, error) {
	binary := strings.TrimSpace(manifest.AgentDockBinary)
	if binary == "" {
		return "", errors.New(currentTrayText().RuntimeIncomplete)
	}
	return filepath.Dir(desktopruntime.PathForBinary(binary)), nil
}

func runNativeAgentDock(manifest desktopruntime.Manifest, arguments ...string) error {
	runtimeRoot, err := runtimeRootForManifest(manifest)
	if err != nil {
		return err
	}
	binary := strings.TrimSpace(manifest.AgentDockBinary)
	if _, err := os.Stat(binary); err != nil {
		return fmt.Errorf(currentTrayText().CoreUnavailable, err)
	}
	arguments = append(arguments, "--runtime-root", runtimeRoot)
	command := exec.Command(binary, arguments...)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf(currentTrayText().NativeCommandFailed, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func regenerateQuickTunnel(manifest desktopruntime.Manifest) error {
	if manifest.TunnelMode != "quick" {
		return errors.New(currentTrayText().QuickTunnelNotActive)
	}
	return runNativeAgentDock(manifest, "tunnel", "regenerate")
}

func startAgentDock(manifest desktopruntime.Manifest) error {
	return runNativeAgentDock(manifest, "service", "start")
}

func restartAgentDock(manifest desktopruntime.Manifest) error {
	return runNativeAgentDock(manifest, "service", "restart")
}

func launchUpdate(manifest desktopruntime.Manifest) error {
	binary := strings.TrimSpace(manifest.AgentDockBinary)
	if binary == "" {
		return errors.New(currentTrayText().CoreUnavailablePlain)
	}
	if _, err := os.Stat(binary); err != nil {
		return err
	}
	verb := "open"
	if manifest.UsesScheduledTask() {
		verb = "runas"
	}
	verbPointer, _ := windows.UTF16PtrFromString(verb)
	binaryPointer, _ := windows.UTF16PtrFromString(binary)
	parametersPointer, _ := windows.UTF16PtrFromString("update")
	result, _, _ := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verbPointer)),
		uintptr(unsafe.Pointer(binaryPointer)),
		uintptr(unsafe.Pointer(parametersPointer)),
		0,
		swShow,
	)
	if result <= 32 {
		return fmt.Errorf(currentTrayText().UpdateProgramFailed, result)
	}
	return nil
}

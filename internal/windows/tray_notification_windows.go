package windows

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// systray v1.2.2 owns the icon and does not expose balloons. Attach the balloon
// to its existing icon, finding only the tray window owned by our process.
func notifyTray(title, message string) {
	user32 := windows.NewLazySystemDLL("user32.dll")
	findWindow := user32.NewProc("FindWindowExW")
	windowPID := user32.NewProc("GetWindowThreadProcessId")
	class, _ := windows.UTF16PtrFromString("SystrayClass")
	var window uintptr
	for {
		window, _, _ = findWindow.Call(0, window, uintptr(unsafe.Pointer(class)), 0)
		if window == 0 {
			return
		}
		var pid uint32
		windowPID.Call(window, uintptr(unsafe.Pointer(&pid)))
		if pid == uint32(os.Getpid()) {
			break
		}
	}
	const (
		modify         = 1
		info           = 0x10
		errorIconQuiet = 0x03 | 0x80 // NIIF_ERROR | NIIF_RESPECT_QUIET_TIME
		systrayIconID  = 100
	)
	notification := trayNotification{window: window, id: systrayIconID, flags: info, infoFlags: errorIconQuiet}
	notification.size = uint32(unsafe.Sizeof(notification))
	copy(notification.title[:], windows.StringToUTF16(title))
	copy(notification.info[:], windows.StringToUTF16(message))
	shell := windows.NewLazySystemDLL("shell32.dll")
	shell.NewProc("Shell_NotifyIconW").Call(modify, uintptr(unsafe.Pointer(&notification)))
}

// NOTIFYICONDATAW, including its pointer-sized fields for both Windows architectures.
type trayNotification struct {
	size                uint32
	window              uintptr
	id, flags, callback uint32
	icon                uintptr
	tip                 [128]uint16
	state, stateMask    uint32
	info                [256]uint16
	timeoutOrVersion    uint32
	title               [64]uint16
	infoFlags           uint32
	guid                windows.GUID
	balloonIcon         uintptr
}

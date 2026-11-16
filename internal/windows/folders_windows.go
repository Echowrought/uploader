package windows

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	win "golang.org/x/sys/windows"
)

func DataFolder() (string, error) {
	folder, err := win.KnownFolderPath(win.FOLDERID_LocalAppData, 0)
	if err != nil {
		return "", err
	}
	return filepath.Join(folder, "CWGRPGUploader"), nil
}

func DefaultSaveFolder() (string, error) {
	folder, err := win.KnownFolderPath(win.FOLDERID_Documents, 0)
	if err != nil {
		return "", err
	}
	return filepath.Join(folder, "Warcraft III", "CustomMapData", "CWG2"), nil
}

// PickFolder uses Windows PowerShell's built-in Forms dialog; no external runtime install.
func PickFolder(job *subprocessJob) (string, error) {
	script := `Add-Type -AssemblyName System.Windows.Forms; $d = New-Object System.Windows.Forms.FolderBrowserDialog; $d.Description = 'Choose your CWG RPG save folder'; $d.ShowNewFolderButton = $false; if ($d.ShowDialog() -eq 'OK') { [Console]::OutputEncoding = New-Object System.Text.UTF8Encoding; [Console]::Write($d.SelectedPath) }; $d.Dispose()`
	executable, err := exec.LookPath("powershell.exe")
	if err != nil {
		return "", fmt.Errorf("open folder picker: %w", err)
	}
	output, err := job.output([]string{executable, "-NoProfile", "-STA", "-Command", script}, 5*time.Minute)
	if err != nil {
		return "", fmt.Errorf("open folder picker: %w", err)
	}
	// Console.Write adds no newline. Whitespace may be part of the folder name.
	return pickerPath(output), nil
}

func pickerPath(output []byte) string {
	return strings.TrimPrefix(string(output), "\xef\xbb\xbf")
}

func Open(target string) error {
	operation, _ := win.UTF16PtrFromString("open")
	path, err := win.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return win.ShellExecute(0, operation, path, nil, nil, win.SW_SHOWNORMAL)
}

func Message(title, message string) error {
	titlePtr, err := win.UTF16PtrFromString(title)
	if err != nil {
		return err
	}
	messagePtr, err := win.UTF16PtrFromString(message)
	if err != nil {
		return err
	}
	box := win.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW")
	result, _, _ := box.Call(0, uintptr(unsafe.Pointer(messagePtr)), uintptr(unsafe.Pointer(titlePtr)), 0x40)
	if result == 0 {
		return errors.New("could not display Windows message box")
	}
	return nil
}

func SingleInstance() (func(), error) {
	name, _ := win.UTF16PtrFromString(`Local\CWGRPGUploader`)
	handle, err := win.CreateMutex(nil, false, name)
	if err != nil {
		if handle != 0 {
			win.CloseHandle(handle)
		}
		return nil, errors.New("CWG RPG Uploader is already running, or its instance lock is unavailable")
	}
	return func() { win.CloseHandle(handle) }, nil
}

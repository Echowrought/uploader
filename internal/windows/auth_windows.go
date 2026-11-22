package windows

import (
	"context"
	"encoding/base64"
	"errors"
	"os/exec"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"cwgrpguploader/internal/leaderboard"
)

func promptAuthentication() bool {
	title, _ := windows.UTF16PtrFromString("CWG RPG Uploader")
	message, _ := windows.UTF16PtrFromString("Uploads require a connected Battle.net account. Authenticate now?\n\nChoosing No exits the uploader.")
	const yesNoQuestion = 0x04 | 0x20
	const yes = 6
	box := windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW")
	result, _, _ := box.Call(0, uintptr(unsafe.Pointer(message)), uintptr(unsafe.Pointer(title)), yesNoQuestion)
	return result == yes
}

// Only the public browser link goes to the dialog subprocess, never either token.
func presentApproval(ctx context.Context, job *subprocessJob, link string) error {
	executable, err := exec.LookPath("powershell.exe")
	if err != nil {
		return errors.New("could not open the authentication window")
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(link))
	script := `$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$f = New-Object Windows.Forms.Form
$f.Text = 'CWG RPG Uploader - Authenticate'
$f.ClientSize = New-Object Drawing.Size(550, 115)
$f.StartPosition = 'CenterScreen'
$f.FormBorderStyle = 'FixedDialog'
$f.MaximizeBox = $false
$l = New-Object Windows.Forms.Label
$l.SetBounds(12, 12, 520, 20)
$l.Text = 'Open or copy this link, then approve upload access in the browser.'
$t = New-Object Windows.Forms.TextBox
$t.SetBounds(12, 39, 526, 24)
$t.ReadOnly = $true
$t.Text = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('` + encoded + `'))
$f.Controls.AddRange(@($l, $t))
$i = 12
foreach ($spec in @(@('Open in default browser', 'OK', 164), @('Continue without opening', 'Retry', 173), @('Cancel', 'Cancel', 85))) {
  $b = New-Object Windows.Forms.Button
  $b.SetBounds($i, 76, $spec[2], 28)
  $b.Text = $spec[0]
  $b.DialogResult = [Windows.Forms.DialogResult]$spec[1]
  if ($spec[1] -eq 'OK') { $f.AcceptButton = $b }
  if ($spec[1] -eq 'Cancel') { $f.CancelButton = $b }
  $f.Controls.Add($b)
  $i += $spec[2] + 8
}
$t.SelectAll()
[Console]::Write($f.ShowDialog().ToString())
$f.Dispose()`
	output, err := job.outputContext(ctx, []string{executable, "-NoProfile", "-STA", "-Command", script}, time.Hour)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errors.New("authentication expired; start again")
	}
	if ctx.Err() != nil {
		return leaderboard.ErrAuthCancelled
	}
	if err != nil {
		return errors.New("could not open the authentication window")
	}
	switch string(output) {
	case "OK":
		if err := Open(link); err != nil {
			return errors.New("could not open the default browser")
		}
		return nil
	case "Retry":
		return nil
	default:
		return leaderboard.ErrAuthCancelled
	}
}

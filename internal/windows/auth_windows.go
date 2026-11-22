package windows

import (
	"context"
	"encoding/base64"
	"errors"
	"os/exec"
	"time"

	"cwgrpguploader/internal/leaderboard"
)

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
$f.ClientSize = New-Object Drawing.Size(620, 204)
$f.StartPosition = 'CenterScreen'
$f.FormBorderStyle = 'FixedDialog'
$f.MaximizeBox = $false
$link = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('` + encoded + `'))
$intro = New-Object Windows.Forms.Label
$intro.SetBounds(12, 12, 596, 42)
$intro.Text = 'Connect your Battle.net account so the uploader can back up your CWG saves. Sign in through the Leaderboard and approve permission to upload saves.'
$url = New-Object Windows.Forms.Label
$url.SetBounds(12, 62, 596, 50)
$url.Font = New-Object Drawing.Font('Consolas', 9)
$url.Text = $link.Replace('?', '?' + [Environment]::NewLine)
$instructions = New-Object Windows.Forms.Label
$instructions.SetBounds(12, 118, 596, 34)
$instructions.Text = 'Copy link closes this window. Paste the link into your browser to continue; the uploader will wait for approval.'
$open = New-Object Windows.Forms.Button
$open.SetBounds(12, 164, 164, 28)
$open.Text = 'Open in default browser'
$open.DialogResult = [Windows.Forms.DialogResult]::OK
$f.AcceptButton = $open
$copy = New-Object Windows.Forms.Button
$copy.SetBounds(184, 164, 90, 28)
$copy.Text = 'Copy link'
$copy.Add_Click({
  try { [Windows.Forms.Clipboard]::SetText($link) }
  catch {
    [void][Windows.Forms.MessageBox]::Show($f, 'Could not copy the link. Try again or open it in the browser.', $f.Text)
    return
  }
  $f.DialogResult = [Windows.Forms.DialogResult]::Retry
})
$cancel = New-Object Windows.Forms.Button
$cancel.SetBounds(282, 164, 85, 28)
$cancel.Text = 'Cancel'
$cancel.DialogResult = [Windows.Forms.DialogResult]::Cancel
$f.CancelButton = $cancel
$f.Controls.AddRange(@($intro, $url, $instructions, $open, $copy, $cancel))
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

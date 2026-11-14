package windows

import (
	"os/exec"
	"testing"
	"time"
)

func TestMessageRejectsInvalidStringsWithoutOpeningDialog(t *testing.T) {
	cases := []struct {
		name    string
		title   string
		message string
	}{
		{"title contains NUL", "invalid\x00title", "message"},
		{"message contains NUL", "title", "invalid\x00message"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Message(tc.title, tc.message)

			if err == nil {
				t.Fatal("invalid dialog text accepted")
			}
		})
	}
}

func TestPickerPowerShellWritesUTF8PathVerbatim(t *testing.T) {
	job := fixtureSubprocessJob(t)
	executable, err := exec.LookPath("powershell.exe")
	if err != nil {
		t.Fatal(err)
	}
	script := `[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding; [Console]::Write('C:\Saves' + [char]0x00a0)`
	args := []string{executable, "-NoProfile", "-NonInteractive", "-Command", script}

	output, err := job.output(args, 15*time.Second)

	if err != nil || pickerPath(output) != "C:\\Saves\u00a0" {
		t.Fatalf("unexpected PowerShell output: %q, error=%v", output, err)
	}
}

func TestPickerPathPreservesActualFolderName(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   string
	}{
		{"cancelled", "", ""},
		{"BOM", "\xef\xbb\xbfC:\\Saves", `C:\Saves`},
		{"nonbreaking space", "C:\\Saves\u00a0", "C:\\Saves\u00a0"},
		{"ordinary space", "C:\\Saves ", "C:\\Saves "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			output := []byte(tc.output)

			path := pickerPath(output)

			if path != tc.want {
				t.Fatalf("path=%q, want=%q", path, tc.want)
			}
		})
	}
}

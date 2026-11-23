package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestInteractiveStartupShowsColoredPathsInABorder(t *testing.T) {
	var out bytes.Buffer
	ui := consoleUI{out: &out, interactive: true, color: true, columns: 100}
	path := `C:\Users\Player\CWGRPGUploader\uploader-token.json`

	err := ui.showPaths(consoleRow{"Credentials", path})

	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"\x1b[36m+---", "\x1b[1;36mCWG RPG Uploader\x1b[0m", "\x1b[1;36mCredentials:\x1b[0m \x1b[92m" + path + "\x1b[0m"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q from startup output", want)
		}
	}
}

func TestNarrowStartupPanelKeepsLongPathIntactOutsideTheBorder(t *testing.T) {
	var out bytes.Buffer
	ui := consoleUI{out: &out, interactive: true, columns: 36}
	path := "/home/player/" + strings.Repeat("directory/", 10) + "config.json"

	err := ui.showPaths(consoleRow{"Configuration", path})

	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "| CWG RPG Uploader |\n") || !strings.HasSuffix(out.String(), "\nConfiguration: "+path+"\n") {
		t.Fatalf("expected a small frame above an uninterrupted path: %q", out.String())
	}
}

func TestRedirectedStartupPathsRemainPlain(t *testing.T) {
	var out bytes.Buffer
	ui := consoleUI{out: &out}

	err := ui.showPaths(consoleRow{"Logs", "uploader.log"})

	if err != nil || out.String() != "Logs: uploader.log\n" {
		t.Fatalf("plain output=%q error=%v", out.String(), err)
	}
}

func TestRedirectedApprovalShowsBoxAndUnbrokenURLWithoutTerminalEscapes(t *testing.T) {
	var out bytes.Buffer
	ui := consoleUI{out: &out}
	link := "https://leaderboard.example/api/uploader/approve?request=" + strings.Repeat("a", 43)

	stop, err := ui.showApproval(link)
	if err != nil {
		t.Fatal(err)
	}
	stop()

	text := out.String()
	for _, want := range []string{"\n+---", "| Connect your Battle.net account", "\n" + link + "\n", "\nAwaiting authentication...\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in output: %q", want, text)
		}
	}
	if strings.ContainsAny(text, "\r\x1b") {
		t.Fatalf("redirected output contains terminal controls: %q", text)
	}
}

func TestInteractiveApprovalSpinnerAnimatesAndClearsItsLine(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var out bytes.Buffer
		ui := consoleUI{out: &out, interactive: true}

		stop, err := ui.startAuthenticationSpinner()
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		time.Sleep(100 * time.Millisecond)
		synctest.Wait()
		stop()

		if !strings.Contains(out.String(), "⠋ Awaiting authentication...\r⠙ Awaiting authentication...") || !strings.HasSuffix(out.String(), "\r\x1b[2K") {
			t.Fatalf("spinner did not animate and clear: %q", out.String())
		}
	})
}

func TestConsoleLogColorsTimestampAndComponentWithoutChangingFileOutput(t *testing.T) {
	const line = "2026/11/23 02:35:00 [uploader] upload started\n"
	var terminal, file bytes.Buffer
	ui := consoleUI{out: &terminal, color: true}

	n, err := io.WriteString(io.MultiWriter(&file, ui.logWriter()), line)

	const want = "\x1b[90m2026/11/23 02:35:00\x1b[0m \x1b[36m[uploader]\x1b[0m upload started\n"
	if err != nil || n != len(line) || file.String() != line || terminal.String() != want {
		t.Fatalf("write=%d,%v file=%q terminal=%q", n, err, file.String(), terminal.String())
	}
}

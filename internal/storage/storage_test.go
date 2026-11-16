package storage

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLoadConfigCreatesDefaults(t *testing.T) {
	dir := t.TempDir()
	folder := t.TempDir()

	config, firstRun, err := LoadConfig(dir, configDefault(folder))

	if err != nil {
		t.Fatal(err)
	}
	if !firstRun || !config.Automatic || config.PollSeconds != 5 || config.SaveFolder != folder || config.ProxyURL != "" {
		t.Fatalf("unexpected defaults: %+v", config)
	}
}

func TestConfigPersistsUserSettings(t *testing.T) {
	dir := t.TempDir()
	want := Config{SaveFolder: t.TempDir(), PollSeconds: 10, Automatic: false, ProxyURL: "socks5://127.0.0.1:1080"}

	saveErr := SaveConfig(dir, want)
	got, firstRun, loadErr := LoadConfig(dir, configDefault("unused"))

	if saveErr != nil || loadErr != nil {
		t.Fatalf("errors: %v, %v", saveErr, loadErr)
	}
	if firstRun || got != want {
		t.Fatalf("got %+v; want %+v", got, want)
	}
}

func TestConfigLoadingAndSavingRejectInvalidSettings(t *testing.T) {
	folder := t.TempDir()
	t.Setenv("CWG_RPG_CONFIG_TEST_RELATIVE", "relative")
	cases := []struct {
		name   string
		config Config
	}{
		{"zero interval", Config{SaveFolder: folder, PollSeconds: 0}},
		{"negative interval", Config{SaveFolder: folder, PollSeconds: -1}},
		{"excessive interval", Config{SaveFolder: folder, PollSeconds: 3601}},
		{"empty folder", Config{PollSeconds: 5}},
		{"relative folder", Config{SaveFolder: "relative", PollSeconds: 5}},
		{"relative environment path", Config{SaveFolder: "%CWG_RPG_CONFIG_TEST_RELATIVE%", PollSeconds: 5}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			data, err := json.Marshal(tc.config)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0600); err != nil {
				t.Fatal(err)
			}

			_, _, loadErr := LoadConfig(dir, configDefault("unused"))
			saveErr := SaveConfig(t.TempDir(), tc.config)

			if loadErr == nil || saveErr == nil {
				t.Fatalf("invalid settings accepted: load=%v, save=%v", loadErr, saveErr)
			}
		})
	}
}

func TestLoadConfigRejectsNullSettings(t *testing.T) {
	for _, body := range []string{
		"null", `{"save_folder":null}`, `{"poll_interval_seconds":null}`,
		`{"upload_automatically":null}`, `{"proxy_url":null}`, `{"UPLOAD_AUTOMATICALLY":null}`,
		`{"upload_notice_shown":null}`,
	} {
		t.Run(body, func(t *testing.T) {
			dir := t.TempDir()
			folder := t.TempDir()
			path := filepath.Join(dir, "config.json")
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}

			_, firstRun, loadErr := LoadConfig(dir, configDefault(folder))
			unchanged, readErr := os.ReadFile(path)

			if loadErr == nil || firstRun {
				t.Fatalf("invalid existing config accepted: firstRun=%v, error=%v", firstRun, loadErr)
			}
			if readErr != nil || string(unchanged) != body {
				t.Fatalf("invalid config was overwritten: data=%q, error=%v", unchanged, readErr)
			}
		})
	}
}

func TestLoadConfigDefaultsOmittedSettings(t *testing.T) {
	dir := t.TempDir()
	folder := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}

	config, firstRun, err := LoadConfig(dir, configDefault(folder))

	if err != nil || firstRun {
		t.Fatalf("could not load partial config: firstRun=%v, error=%v", firstRun, err)
	}
	want := Config{SaveFolder: folder, PollSeconds: 5, Automatic: true}
	if config != want {
		t.Fatalf("got %+v; want %+v", config, want)
	}
}

func TestSaveConfigRejectsInvalidCandidateWithoutReplacingPreferences(t *testing.T) {
	dir := t.TempDir()
	previous := Config{SaveFolder: t.TempDir(), PollSeconds: 5, Automatic: false}
	if err := SaveConfig(dir, previous); err != nil {
		t.Fatal(err)
	}
	candidate := previous
	candidate.SaveFolder = "relative"
	candidate.Automatic = true

	saveErr := SaveConfig(dir, candidate)
	got, _, loadErr := LoadConfig(dir, configDefault("unused"))

	if saveErr == nil || loadErr != nil {
		t.Fatalf("errors: save=%v, load=%v", saveErr, loadErr)
	}
	if got != previous {
		t.Fatalf("invalid candidate replaced preferences: %+v", got)
	}
}

func TestLoadConfigRejectsInvalidDefaultsWithoutWritingThem(t *testing.T) {
	dir := t.TempDir()

	_, _, loadErr := LoadConfig(dir, configDefault("relative"))
	_, statErr := os.Stat(filepath.Join(dir, "config.json"))

	if loadErr == nil || !os.IsNotExist(statErr) {
		t.Fatalf("invalid defaults were accepted or written: load=%v, stat=%v", loadErr, statErr)
	}
}

func TestConfigPreservesEnvironmentPaths(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CWG_RPG_CONFIG_TEST_HOME", t.TempDir())
	want := Config{SaveFolder: "%CWG_RPG_CONFIG_TEST_HOME%", PollSeconds: 5, Automatic: true}

	saveErr := SaveConfig(dir, want)
	got, _, loadErr := LoadConfig(dir, configDefault("unused"))

	if saveErr != nil || loadErr != nil {
		t.Fatalf("errors: %v, %v", saveErr, loadErr)
	}
	if got != want {
		t.Fatalf("environment path was replaced: %+v", got)
	}
}

func TestConfigIgnoresLegacyStartupPreference(t *testing.T) {
	dir := t.TempDir()
	want := Config{SaveFolder: t.TempDir(), PollSeconds: 5, Automatic: true}
	data, err := json.Marshal(struct {
		Config
		LaunchAtLogin bool `json:"launch_at_login"`
	}{want, true})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0600); err != nil {
		t.Fatal(err)
	}

	got, firstRun, loadErr := LoadConfig(dir, configDefault("unused"))

	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if firstRun || got != want {
		t.Fatalf("legacy config was not loaded: %+v", got)
	}
}

func TestExpandPathExpandsWindowsVariables(t *testing.T) {
	folder := t.TempDir()
	t.Setenv("CWG_RPG_TEST_HOME", folder)

	got, err := ExpandPath(`%CWG_RPG_TEST_HOME%\saves`)

	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(folder, "saves") {
		t.Fatalf("got %q", got)
	}
}

func TestUploadStateAtomicallyReplacesPreviousState(t *testing.T) {
	dir := t.TempDir()
	want := UploadState{SaveFolder: "folder", Destination: "server", Fingerprint: "new", UploadedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), Player: "Player", Heroes: 2}
	first := want
	first.Fingerprint = "old"

	firstErr := SaveUploadState(dir, first)
	secondErr := SaveUploadState(dir, want)
	got, loadErr := LoadUploadState(dir)

	if firstErr != nil || secondErr != nil || loadErr != nil {
		t.Fatalf("errors: %v, %v, %v", firstErr, secondErr, loadErr)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v; want %+v", got, want)
	}
}

func TestLoadUploadStateReadsSaveFolder(t *testing.T) {
	dir := t.TempDir()
	data := []byte(`{"save_folder":"saves","destination":"server","fingerprint":"existing","uploaded_at":"2025-01-01T00:00:00Z","player":"Player","heroes":2}`)
	if err := os.WriteFile(filepath.Join(dir, "state.json"), data, 0600); err != nil {
		t.Fatal(err)
	}

	want := UploadState{SaveFolder: "saves", Destination: "server", Fingerprint: "existing", UploadedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), Player: "Player", Heroes: 2}

	state, err := LoadUploadState(dir)

	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(state, want) {
		t.Fatalf("got %+v; want %+v", state, want)
	}
}

func TestLoadUploadStateAllowsMissingHistory(t *testing.T) {
	dir := t.TempDir()

	state, err := LoadUploadState(dir)

	if err != nil || state != (UploadState{}) {
		t.Fatalf("unexpected missing history: state=%+v, error=%v", state, err)
	}
}

func TestLoadUploadStateRejectsInvalidDocuments(t *testing.T) {
	for _, body := range []string{"null", "{}", "[]", "{"} {
		t.Run(body, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "state.json")
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}

			_, loadErr := LoadUploadState(dir)
			unchanged, readErr := os.ReadFile(path)

			if loadErr == nil {
				t.Fatal("invalid existing history accepted")
			}
			if readErr != nil || string(unchanged) != body {
				t.Fatalf("invalid history was overwritten: data=%q, error=%v", unchanged, readErr)
			}
		})
	}
}

func TestUploadStateLoadingAndSavingRejectInvalidHistory(t *testing.T) {
	cases := []struct {
		name       string
		invalidate func(*UploadState)
	}{
		{"missing folder", func(state *UploadState) { state.SaveFolder = "" }},
		{"missing destination", func(state *UploadState) { state.Destination = "" }},
		{"missing fingerprint", func(state *UploadState) { state.Fingerprint = "" }},
		{"missing timestamp", func(state *UploadState) { state.UploadedAt = time.Time{} }},
		{"missing player", func(state *UploadState) { state.Player = "" }},
		{"zero heroes", func(state *UploadState) { state.Heroes = 0 }},
		{"negative heroes", func(state *UploadState) { state.Heroes = -1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			previous := UploadState{SaveFolder: "saves", Destination: "server", Fingerprint: "existing", UploadedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), Player: "Player", Heroes: 2}
			candidate := previous
			tc.invalidate(&candidate)
			data, err := json.Marshal(candidate)
			if err != nil {
				t.Fatal(err)
			}
			loadDir, saveDir := t.TempDir(), t.TempDir()
			if err := os.WriteFile(filepath.Join(loadDir, "state.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := SaveUploadState(saveDir, previous); err != nil {
				t.Fatal(err)
			}

			_, loadErr := LoadUploadState(loadDir)
			saveErr := SaveUploadState(saveDir, candidate)
			got, recoveryErr := LoadUploadState(saveDir)

			if loadErr == nil || saveErr == nil {
				t.Fatalf("invalid history accepted: load=%v, save=%v", loadErr, saveErr)
			}
			if recoveryErr != nil || got != previous {
				t.Fatalf("invalid candidate replaced history: state=%+v, error=%v", got, recoveryErr)
			}
		})
	}
}

func TestLogRotatesWithoutExternalMaintenance(t *testing.T) {
	dir := t.TempDir()
	writer, err := OpenLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	writer.max = 4
	writer.backups = 2

	_, firstErr := writer.Write([]byte("1111"))
	_, secondErr := writer.Write([]byte("2222"))
	_, thirdErr := writer.Write([]byte("3333"))
	_, fourthErr := writer.Write([]byte("4444"))
	closeErr := writer.Close()
	current, readErr := os.ReadFile(filepath.Join(dir, "uploader.log"))
	previous, previousErr := os.ReadFile(filepath.Join(dir, "uploader.log.1"))
	oldest, oldestErr := os.ReadFile(filepath.Join(dir, "uploader.log.2"))
	_, extraErr := os.Stat(filepath.Join(dir, "uploader.log.3"))

	for _, err := range []error{firstErr, secondErr, thirdErr, fourthErr, closeErr, readErr, previousErr, oldestErr} {
		if err != nil {
			t.Fatal(err)
		}
	}
	if string(current) != "4444" || string(previous) != "3333" || string(oldest) != "2222" {
		t.Fatal("incorrect log rotation")
	}
	if !os.IsNotExist(extraErr) {
		t.Fatal("too many backups")
	}
}

func TestLogCapsEntriesWithoutShortWritesOrChangingInput(t *testing.T) {
	cases := []struct {
		name      string
		size      int
		wantSize  int
		truncated bool
	}{
		{"below limit", (256 << 10) - 1, (256 << 10) - 1, false},
		{"at limit", 256 << 10, 256 << 10, false},
		{"above limit", (256 << 10) + 1, 256 << 10, true},
		{"large entry", 2 << 20, 256 << 10, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writer, err := OpenLog(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Close()
			entry := bytes.Repeat([]byte("x"), tc.size)
			var console bytes.Buffer
			output := io.MultiWriter(writer, &console)

			n, writeErr := output.Write(entry)
			closeErr := writer.Close()
			logged, readErr := os.ReadFile(filepath.Join(dir, "uploader.log"))

			if writeErr != nil || closeErr != nil || readErr != nil {
				t.Fatalf("errors: write=%v, close=%v, read=%v", writeErr, closeErr, readErr)
			}
			if n != tc.size || console.Len() != tc.size {
				t.Fatalf("truncation caused a short write: n=%d, console=%d", n, console.Len())
			}
			if len(logged) != tc.wantSize || bytes.HasSuffix(logged, []byte("\n[log entry truncated]\n")) != tc.truncated {
				t.Fatalf("unexpected logged entry: length=%d, want=%d", len(logged), tc.wantSize)
			}
			if !bytes.Equal(entry, bytes.Repeat([]byte("x"), tc.size)) || !bytes.Equal(console.Bytes(), entry) {
				t.Fatal("log writer modified input or truncated console output")
			}
		})
	}
}

func TestLogTruncationDoesNotSplitUTF8(t *testing.T) {
	dir := t.TempDir()
	writer, err := OpenLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	marker := "\n[log entry truncated]\n"
	prefix := strings.Repeat("x", (256<<10)-len(marker)-1)
	entry := []byte(prefix + "€" + strings.Repeat("x", 100))

	n, writeErr := writer.Write(entry)
	closeErr := writer.Close()
	logged, readErr := os.ReadFile(filepath.Join(dir, "uploader.log"))

	if writeErr != nil || closeErr != nil || readErr != nil {
		t.Fatalf("errors: write=%v, close=%v, read=%v", writeErr, closeErr, readErr)
	}
	if n != len(entry) || string(logged) != prefix+marker {
		t.Fatalf("unexpected truncation boundary: n=%d, logged length=%d", n, len(logged))
	}
}

func TestLogDoesNotRotateWhenTruncatedEntryFits(t *testing.T) {
	dir := t.TempDir()
	writer, err := OpenLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := writer.Write([]byte("previous\n")); err != nil {
		t.Fatal(err)
	}
	entry := bytes.Repeat([]byte("x"), 2<<20)

	n, writeErr := writer.Write(entry)
	closeErr := writer.Close()
	logged, readErr := os.ReadFile(filepath.Join(dir, "uploader.log"))
	_, backupErr := os.Stat(filepath.Join(dir, "uploader.log.1"))

	if writeErr != nil || closeErr != nil || readErr != nil {
		t.Fatalf("errors: write=%v, close=%v, read=%v", writeErr, closeErr, readErr)
	}
	if n != len(entry) || len(logged) != len("previous\n")+(256<<10) || !bytes.HasPrefix(logged, []byte("previous\n")) {
		t.Fatalf("unexpected current log: n=%d, length=%d", n, len(logged))
	}
	if !os.IsNotExist(backupErr) {
		t.Fatal("entry that fits after truncation caused rotation")
	}
}

func TestLogRotatesUsingTruncatedEntrySize(t *testing.T) {
	dir := t.TempDir()
	writer, err := OpenLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	writer.max = 256 << 10
	if _, err := writer.Write([]byte("previous\n")); err != nil {
		t.Fatal(err)
	}
	entry := bytes.Repeat([]byte("x"), 2<<20)

	n, writeErr := writer.Write(entry)
	closeErr := writer.Close()
	logged, readErr := os.ReadFile(filepath.Join(dir, "uploader.log"))
	previous, previousErr := os.ReadFile(filepath.Join(dir, "uploader.log.1"))
	_, extraErr := os.Stat(filepath.Join(dir, "uploader.log.2"))

	if writeErr != nil || closeErr != nil || readErr != nil || previousErr != nil {
		t.Fatalf("errors: write=%v, close=%v, read=%v, previous=%v", writeErr, closeErr, readErr, previousErr)
	}
	if n != len(entry) || len(logged) != 256<<10 || string(previous) != "previous\n" {
		t.Fatalf("incorrect rotation: n=%d, current length=%d, previous=%q", n, len(logged), previous)
	}
	if !os.IsNotExist(extraErr) {
		t.Fatal("oversized entry caused extra rotation")
	}
}

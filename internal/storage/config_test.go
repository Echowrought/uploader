package storage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigDoesNotDiscoverUnusedDefault(t *testing.T) {
	dir := t.TempDir()
	want := Config{SaveFolder: t.TempDir(), PollSeconds: 5, Automatic: false, UploadNoticeShown: true}
	if err := SaveConfig(dir, want); err != nil {
		t.Fatal(err)
	}
	calls := 0
	defaultFolder := func() (string, error) {
		calls++
		return "", errors.New("Documents is unavailable")
	}

	got, firstRun, err := LoadConfig(dir, defaultFolder)

	if err != nil || firstRun || got != want || calls != 0 {
		t.Fatalf("got=%+v, firstRun=%v, error=%v, discovery calls=%d", got, firstRun, err, calls)
	}
}

func TestLoadConfigDiscoversOmittedDefault(t *testing.T) {
	dir := t.TempDir()
	folder := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"upload_automatically":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	defaultFolder := func() (string, error) {
		calls++
		return folder, nil
	}

	got, firstRun, err := LoadConfig(dir, defaultFolder)

	if err != nil || firstRun || got.SaveFolder != folder || got.Automatic || calls != 1 {
		t.Fatalf("got=%+v, firstRun=%v, error=%v, discovery calls=%d", got, firstRun, err, calls)
	}
}

func TestLoadConfigDoesNotReplaceExplicitlyEmptyFolder(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"save_folder":""}`), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	defaultFolder := func() (string, error) {
		calls++
		return t.TempDir(), nil
	}

	_, _, err := LoadConfig(dir, defaultFolder)

	if err == nil || calls != 0 {
		t.Fatalf("invalid folder defaulted: error=%v, discovery calls=%d", err, calls)
	}
}

func TestLoadConfigDiscoveryFailureDoesNotCreateConfig(t *testing.T) {
	dir := t.TempDir()
	cause := errors.New("Documents is unavailable")
	defaultFolder := func() (string, error) { return "", cause }

	_, _, err := LoadConfig(dir, defaultFolder)
	_, statErr := os.Stat(filepath.Join(dir, "config.json"))

	if !errors.Is(err, cause) || !os.IsNotExist(statErr) {
		t.Fatalf("discovery failure ignored or saved: error=%v, stat=%v", err, statErr)
	}
}

func configDefault(folder string) func() (string, error) {
	return func() (string, error) { return folder, nil }
}

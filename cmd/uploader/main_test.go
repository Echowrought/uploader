package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cwgrpguploader/internal/storage"
)

func TestUploadNoticePersistsOnlyAfterPresentationAndDoesNotRepeat(t *testing.T) {
	dir := t.TempDir()
	config := storage.Config{SaveFolder: t.TempDir(), PollSeconds: 5, Automatic: false}
	if err := storage.SaveConfig(dir, config); err != nil {
		t.Fatal(err)
	}
	var messages []string
	show := func(message string) error { messages = append(messages, message); return nil }

	firstErr := ensureUploadNotice(dir, &config, false, show)
	loaded, _, loadErr := storage.LoadConfig(dir, func() (string, error) { return "unused", nil })
	secondErr := ensureUploadNotice(dir, &loaded, false, show)

	if firstErr != nil || loadErr != nil || secondErr != nil {
		t.Fatalf("errors: first=%v, load=%v, second=%v", firstErr, loadErr, secondErr)
	}
	if !config.UploadNoticeShown || !loaded.UploadNoticeShown || config.Automatic || loaded.Automatic {
		t.Fatalf("notice not committed or upload preference changed: live=%+v, saved=%+v", config, loaded)
	}
	if len(messages) != 1 || !strings.Contains(messages[0], "third-party public leaderboard") {
		t.Fatalf("unexpected disclosure: %v", messages)
	}
}

func TestDryRunLeavesLiveUploadNoticePending(t *testing.T) {
	dir := t.TempDir()
	config := storage.Config{SaveFolder: t.TempDir(), PollSeconds: 5, Automatic: true}
	if err := storage.SaveConfig(dir, config); err != nil {
		t.Fatal(err)
	}
	var messages []string
	show := func(message string) error { messages = append(messages, message); return nil }

	dryErr := ensureUploadNotice(dir, &config, true, show)
	afterDry, _, loadErr := storage.LoadConfig(dir, func() (string, error) { return "unused", nil })
	liveErr := ensureUploadNotice(dir, &config, false, show)

	if dryErr != nil || loadErr != nil || liveErr != nil {
		t.Fatalf("errors: dry=%v, load=%v, live=%v", dryErr, loadErr, liveErr)
	}
	if afterDry.UploadNoticeShown || !config.UploadNoticeShown {
		t.Fatalf("dry run consumed notice or live run did not acknowledge it: dry=%+v, live=%+v", afterDry, config)
	}
	if len(messages) != 2 || !strings.HasPrefix(messages[0], "Dry run: no saves will be uploaded.") || strings.HasPrefix(messages[1], "Dry run:") {
		t.Fatalf("unexpected disclosure sequence: %v", messages)
	}
}

func TestUploadNoticePresentationFailureLeavesItPending(t *testing.T) {
	dir := t.TempDir()
	config := storage.Config{SaveFolder: t.TempDir(), PollSeconds: 5}
	if err := storage.SaveConfig(dir, config); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("console output failed")
	show := func(string) error { return cause }

	err := ensureUploadNotice(dir, &config, false, show)
	loaded, _, loadErr := storage.LoadConfig(dir, func() (string, error) { return "unused", nil })

	if !errors.Is(err, cause) || loadErr != nil || config.UploadNoticeShown || loaded.UploadNoticeShown {
		t.Fatalf("failed notice acknowledged: error=%v, load=%v, live=%+v, saved=%+v", err, loadErr, config, loaded)
	}
}

func TestUploadNoticeSaveFailureDoesNotCommitLiveConfig(t *testing.T) {
	dir := t.TempDir()
	config := storage.Config{SaveFolder: t.TempDir(), PollSeconds: 5}
	if err := os.Mkdir(filepath.Join(dir, "config.json"), 0700); err != nil {
		t.Fatal(err)
	}
	var message string
	show := func(text string) error { message = text; return nil }

	err := ensureUploadNotice(dir, &config, false, show)

	if err == nil || config.UploadNoticeShown || message == "" {
		t.Fatalf("failed save committed notice: error=%v, config=%+v, message=%q", err, config, message)
	}
}

package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cwg2uploader/internal/leaderboard"
	"cwg2uploader/internal/saves"
	"cwg2uploader/internal/storage"
)

func TestCommandPausePersistsPreference(t *testing.T) {
	state := fixtureRunState(t)
	state.app.Config.ProxyURL = "http://proxy.invalid:8080"

	commandErr := state.handleCommand(context.Background(), Command{Kind: "pause"})
	config, _, loadErr := storage.LoadConfig(state.app.Dir, func() (string, error) { return "unused", nil })
	status := publishedStatus(t, state.app)

	if commandErr != nil || loadErr != nil {
		t.Fatalf("errors: %v, %v", commandErr, loadErr)
	}
	if status.Message != "Automatic uploads off" || config.Automatic || state.app.Config.Automatic {
		t.Fatal("automatic-upload preference not updated")
	}
	if config.ProxyURL != "http://proxy.invalid:8080" {
		t.Fatal("unrelated setting lost")
	}
}

func TestCommandResumeStartsCheckingStableSaves(t *testing.T) {
	state := fixtureRunState(t)
	state.app.Config.Automatic = false

	err := state.handleCommand(context.Background(), Command{Kind: "pause"})
	status := publishedStatus(t, state.app)

	if err != nil {
		t.Fatal(err)
	}
	if !state.app.Config.Automatic || status.Message != saves.ErrSettling.Error() {
		t.Fatal("resume did not start checking save stability")
	}
}

func TestCommandPauseKeepsPreferenceWhenPersistenceFails(t *testing.T) {
	state := fixtureRunState(t)
	path := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	state.app.Dir = path

	err := state.handleCommand(context.Background(), Command{Kind: "pause"})

	if err == nil || !state.app.Config.Automatic {
		t.Fatal("failed persistence changed the preference")
	}
}

func TestCommandFolderChangePreservesAcceptedSnapshotsAndOrigin(t *testing.T) {
	application, client := fixtureApp(t)
	application.Config.Automatic = false
	state := fixtureCoordinator(t, application)
	original := application.Config.SaveFolder
	replacement := t.TempDir()
	active := fixtureFolderUpdate("active", "first save")
	active.saveFolder = original
	pending := fixtureFolderUpdate("pending", "second save")
	pending.saveFolder = original
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	activeErr := state.uploader.queue(ctx, active)
	_ = nextCoordinatorEvent[uploadStarted](t, ctx, state)
	first := nextRequest(t, ctx, client)
	pendingErr := state.uploader.queue(ctx, pending)
	state.poller.requestEmission()
	commandErr := state.handleCommand(ctx, Command{Kind: "folder", Value: replacement})
	config, _, loadErr := storage.LoadConfig(application.Dir, func() (string, error) { return "unused", nil })
	first.reply <- uploadReply{result: successfulResult()}
	firstSuccess := nextCoordinatorEvent[uploadSucceeded](t, ctx, state)
	_ = nextCoordinatorEvent[uploadStarted](t, ctx, state)
	second := nextRequest(t, ctx, client)
	second.reply <- uploadReply{result: successfulResult()}
	secondSuccess := nextCoordinatorEvent[uploadSucceeded](t, ctx, state)
	status := publishedStatus(t, application)

	if activeErr != nil || pendingErr != nil || commandErr != nil || loadErr != nil {
		t.Fatalf("errors: %v, %v, %v, %v", activeErr, pendingErr, commandErr, loadErr)
	}
	if config.SaveFolder != replacement || state.poller.saveFolder != replacement {
		t.Fatal("new folder was not selected while delivery was active")
	}
	if state.poller.forceNextEmission {
		t.Fatal("old poller request survived folder selection")
	}
	if first.payload.Files["HERO_01_123.pld"].Text != "first save" || second.payload.Files["HERO_01_123.pld"].Text != "second save" {
		t.Fatal("folder selection interrupted accepted snapshots")
	}
	if firstSuccess.saveFolder != original || secondSuccess.saveFolder != original || application.LastUpload.SaveFolder != original {
		t.Fatal("completion used the new watch folder instead of snapshot origin")
	}
	if state.view.delivery.active != nil || state.view.delivery.pending != nil || !strings.HasPrefix(status.Message, "Automatic uploads off") {
		t.Fatal("completed old-folder delivery remained active or hid the paused status")
	}
}

func TestCommandFolderChangeRetainsBackoffAndCoalescesNewSource(t *testing.T) {
	t.Parallel()
	application, client := fixtureApp(t)
	replacementApp, _ := fixtureApp(t)
	replacement := replacementApp.Config.SaveFolder
	if err := os.WriteFile(filepath.Join(replacement, "HERO_01_123.pld"), []byte("new folder save"), 0600); err != nil {
		t.Fatal(err)
	}
	state := fixtureCoordinator(t, application)
	old := fixtureFolderUpdate("old", "old save")
	old.saveFolder = application.Config.SaveFolder
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	queueErr := state.uploader.queue(ctx, old)
	_ = nextCoordinatorEvent[uploadStarted](t, ctx, state)
	first := nextRequest(t, ctx, client)
	first.reply <- uploadReply{err: &leaderboard.UploadError{Message: "retry later", Retry: true, After: 6 * time.Second}}
	retry := nextCoordinatorEvent[uploadRetryScheduled](t, ctx, state)
	commandErr := state.handleCommand(ctx, Command{Kind: "folder", Value: replacement})
	state.tick(ctx, time.Now().Add(6*time.Second))
	_ = nextCoordinatorEvent[uploadStarted](t, ctx, state)
	second := nextRequest(t, ctx, client)
	second.reply <- uploadReply{result: successfulResult()}
	_ = nextCoordinatorEvent[uploadSucceeded](t, ctx, state)

	if queueErr != nil || commandErr != nil {
		t.Fatalf("errors: %v, %v", queueErr, commandErr)
	}
	if second.startedAt.Before(retry.retryAt) {
		t.Fatal("changing folders bypassed delivery backoff")
	}
	if second.payload.Files["HERO_01_123.pld"].Text != "new folder save" {
		t.Fatal("new source did not replace older pending data")
	}
	if application.LastUpload.SaveFolder != replacement || application.LastUpload.Fingerprint != state.poller.lastEmittedFingerprint {
		t.Fatal("new source completion was not recorded correctly")
	}
}

func TestCommandFolderRejectsInvalidPathsWithoutChangingSelection(t *testing.T) {
	cases := []struct{ name, path string }{
		{"empty", ""},
		{"relative", "relative/saves"},
		{"drive relative", "C:saves"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := fixtureRunState(t)
			previous, poller := state.app.Config, state.poller
			if err := storage.SaveConfig(state.app.Dir, previous); err != nil {
				t.Fatal(err)
			}

			commandErr := state.handleCommand(context.Background(), Command{Kind: "folder", Value: tc.path})
			persisted, _, loadErr := storage.LoadConfig(state.app.Dir, func() (string, error) { return "unused", nil })

			if commandErr == nil || loadErr != nil {
				t.Fatalf("errors: %v, %v", commandErr, loadErr)
			}
			if state.app.Config != previous || persisted != previous || state.poller != poller {
				t.Fatal("invalid folder changed app configuration, disk configuration, or polling")
			}
		})
	}
}

func TestSaveConfigRejectsInvalidCandidateWithoutCommittingIt(t *testing.T) {
	state := fixtureRunState(t)
	previous := state.app.Config
	if err := storage.SaveConfig(state.app.Dir, previous); err != nil {
		t.Fatal(err)
	}
	candidate := previous
	candidate.PollSeconds = 0

	saveErr := state.saveConfig(candidate)
	persisted, _, loadErr := storage.LoadConfig(state.app.Dir, func() (string, error) { return "unused", nil })

	if saveErr == nil || loadErr != nil {
		t.Fatalf("errors: %v, %v", saveErr, loadErr)
	}
	if state.app.Config != previous || persisted != previous {
		t.Fatal("invalid candidate reached app configuration or disk")
	}
}

func TestCommandUploadNowKeepsRetryDeadline(t *testing.T) {
	t.Parallel()
	application, client := fixtureApp(t)
	state := fixtureCoordinator(t, application)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	queueErr := state.uploader.queue(ctx, fixtureFolderUpdate("old", "old save"))
	_ = nextCoordinatorEvent[uploadStarted](t, ctx, state)
	first := nextRequest(t, ctx, client)
	first.reply <- uploadReply{err: &leaderboard.UploadError{Message: "retry later", Retry: true, After: 6 * time.Second}}
	retry := nextCoordinatorEvent[uploadRetryScheduled](t, ctx, state)
	commandErr := state.handleCommand(ctx, Command{Kind: "upload"})
	requestLatched := state.poller.forceNextEmission
	state.tick(ctx, time.Now().Add(6*time.Second))
	_ = nextCoordinatorEvent[uploadStarted](t, ctx, state)
	second := nextRequest(t, ctx, client)

	if queueErr != nil || commandErr != nil {
		t.Fatalf("errors: %v, %v", queueErr, commandErr)
	}
	if !requestLatched {
		t.Fatal("explicit upload request was not latched")
	}
	if second.startedAt.Before(retry.retryAt) {
		t.Fatal("Upload now bypassed Retry-After")
	}
	if second.payload.Files["HERO_01_123.pld"].Text != "save" {
		t.Fatal("fresh snapshot was not used for the retry")
	}
}

func TestCommandUnknownDoesNotPoll(t *testing.T) {
	state := fixtureRunState(t)

	err := state.handleCommand(context.Background(), Command{Kind: "unknown"})

	if err != nil || state.view.folder.kind != pollUnknown || len(state.app.Updates) != 0 {
		t.Fatal("unknown command changed state")
	}
}

func TestPublishReplacesUnreadStatus(t *testing.T) {
	state := fixtureRunState(t)

	state.publish("first")
	state.publish("second")
	status := <-state.app.Updates

	if status.Message != "second" {
		t.Fatalf("status = %q", status.Message)
	}
}

func fixtureRunState(t *testing.T) *runState {
	t.Helper()
	application, _ := fixtureApp(t)
	return fixtureCoordinator(t, application)
}

package app

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cwgrpguploader/internal/leaderboard"
	"cwgrpguploader/internal/storage"
)

func TestPersistenceRetrySavesLatestSuccessWithAutomaticOff(t *testing.T) {
	application, client := fixtureApp(t)
	application.Config.Automatic = false
	application.Dir = filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(application.Dir, nil, 0600); err != nil {
		t.Fatal(err)
	}
	state := fixtureCoordinator(t, application)
	first := uploadSucceeded{saveFolder: application.Config.SaveFolder, fingerprint: "first", result: successfulResult(), at: time.Now()}
	latest := uploadSucceeded{saveFolder: application.Config.SaveFolder, fingerprint: "latest", result: leaderboard.Result{Player: "Latest", Heroes: 2}, at: time.Now()}

	state.handleUploadEvent(first)
	state.handleUploadEvent(latest)
	initialError := state.view.persistenceError
	removeErr := os.Remove(application.Dir)
	state.tick(context.Background(), time.Now())
	persisted, loadErr := storage.LoadUploadState(application.Dir)
	status := publishedStatus(t, application)

	if initialError == nil || removeErr != nil || loadErr != nil {
		t.Fatalf("errors: initial=%v, remove=%v, load=%v", initialError, removeErr, loadErr)
	}
	if state.view.persistenceError != nil || strings.Contains(status.Message, "Could not persist") {
		t.Fatal("recovery did not clear the persistence warning")
	}
	if persisted.Fingerprint != "latest" || persisted.SaveFolder != latest.saveFolder || !persisted.UploadedAt.Equal(latest.at) || persisted.Player != "Latest" || persisted.Heroes != 2 {
		t.Fatalf("retry persisted stale or incomplete history: %+v", persisted)
	}
	if application.Config.Automatic || len(client.requests) != 0 {
		t.Fatal("history recovery enabled detection or retried HTTP")
	}
}

func TestPersistenceRetryRecoversEvenWhenScanningFails(t *testing.T) {
	application, client := fixtureApp(t)
	application.Dir = filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(application.Dir, nil, 0600); err != nil {
		t.Fatal(err)
	}
	state := fixtureCoordinator(t, application)
	state.poller.scanner.Folder = filepath.Join(t.TempDir(), "missing")
	event := uploadSucceeded{saveFolder: application.Config.SaveFolder, fingerprint: "delivered", result: successfulResult(), at: time.Now()}

	state.handleUploadEvent(event)
	initialError := state.view.persistenceError
	removeErr := os.Remove(application.Dir)
	state.tick(context.Background(), time.Now())
	persisted, loadErr := storage.LoadUploadState(application.Dir)
	status := publishedStatus(t, application)

	if initialError == nil || removeErr != nil || loadErr != nil {
		t.Fatalf("errors: initial=%v, remove=%v, load=%v", initialError, removeErr, loadErr)
	}
	if state.view.persistenceError != nil || persisted.Fingerprint != "delivered" {
		t.Fatal("scan failure prevented history recovery")
	}
	if state.view.folder.kind != pollFailed || !strings.Contains(status.Message, "read save folder") || len(client.requests) != 0 {
		t.Fatal("recovery hid the scan failure or retried HTTP")
	}
}

func TestPersistenceRetryLogsOnlyNewOrChangedFailures(t *testing.T) {
	application, _ := fixtureApp(t)
	application.Config.Automatic = false
	var logs bytes.Buffer
	application.Logger = log.New(&logs, "", 0)
	blocked, replacement := filepath.Join(t.TempDir(), "blocked"), filepath.Join(t.TempDir(), "other")
	if err := os.WriteFile(blocked, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replacement, nil, 0600); err != nil {
		t.Fatal(err)
	}
	application.Dir = blocked
	state := fixtureCoordinator(t, application)
	event := uploadSucceeded{saveFolder: application.Config.SaveFolder, fingerprint: "delivered", result: successfulResult(), at: time.Now()}

	state.handleUploadEvent(event)
	state.tick(context.Background(), time.Now())
	state.tick(context.Background(), time.Now())
	repeatedCount := strings.Count(logs.String(), "save upload state failed:")
	application.Dir = replacement
	state.tick(context.Background(), time.Now())
	changedCount := strings.Count(logs.String(), "save upload state failed:")

	if repeatedCount != 1 || changedCount != 2 || state.view.persistenceError == nil {
		t.Fatalf("incorrect error reporting: repeated=%d, changed=%d, error=%v", repeatedCount, changedCount, state.view.persistenceError)
	}
}

func TestFolderChangeClearsExistingRejection(t *testing.T) {
	application, _ := fixtureApp(t)
	application.Config.Automatic = false
	state := fixtureCoordinator(t, application)
	replacement := t.TempDir()
	rejected := uploadRejected{saveFolder: application.Config.SaveFolder, err: errors.New("old save rejected")}

	state.handleUploadEvent(rejected)
	commandErr := state.handleCommand(context.Background(), Command{Kind: "folder", Value: replacement})
	status := publishedStatus(t, application)

	if commandErr != nil {
		t.Fatal(commandErr)
	}
	if state.view.delivery.rejected != nil || status.Message != "Automatic uploads off" {
		t.Fatal("folder selection retained the previous folder's rejection")
	}
}

func TestOldFolderTerminalFeedbackDoesNotReplaceCurrentPresentation(t *testing.T) {
	old := t.TempDir()
	cases := []struct {
		name  string
		event uploadEvent
	}{
		{"rejected", uploadRejected{saveFolder: old, err: errors.New("old save rejected")}},
		{"previewed", uploadPreviewed{saveFolder: old, fileCount: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			application, _ := fixtureApp(t)
			application.Config.Automatic = false
			application.Config.SaveFolder = old
			state := fixtureCoordinator(t, application)
			replacement := t.TempDir()
			current := uploadStarted{id: 2, saveFolder: replacement}

			commandErr := state.handleCommand(context.Background(), Command{Kind: "folder", Value: replacement})
			state.handleUploadEvent(current)
			state.handleUploadEvent(tc.event)
			status := publishedStatus(t, application)

			if commandErr != nil {
				t.Fatal(commandErr)
			}
			if state.view.delivery.active != current || status.Message != "Automatic uploads off; Uploading saves" {
				t.Fatal("an old-folder event replaced the new folder's presentation")
			}
		})
	}
}

func TestOldFolderSuccessIsPersistedWithoutReplacingCurrentPresentation(t *testing.T) {
	application, _ := fixtureApp(t)
	application.Config.Automatic = false
	state := fixtureCoordinator(t, application)
	old := application.Config.SaveFolder
	replacement := t.TempDir()
	current := uploadStarted{id: 2, saveFolder: replacement}
	success := uploadSucceeded{saveFolder: old, fingerprint: "delivered", result: successfulResult(), at: time.Now()}

	commandErr := state.handleCommand(context.Background(), Command{Kind: "folder", Value: replacement})
	state.handleUploadEvent(current)
	state.handleUploadEvent(success)
	persisted, loadErr := storage.LoadUploadState(application.Dir)
	status := publishedStatus(t, application)

	if commandErr != nil || loadErr != nil {
		t.Fatalf("errors: %v, %v", commandErr, loadErr)
	}
	if persisted.SaveFolder != old || persisted.Fingerprint != "delivered" || application.LastUpload.SaveFolder != old {
		t.Fatal("filtering the view discarded success history or changed its origin")
	}
	if state.view.delivery.active != current || !strings.Contains(status.Message, "Uploading saves") || !strings.Contains(status.Message, "Last upload:") {
		t.Fatal("old-folder success replaced current activity or lost upload history")
	}
}

func TestOldFolderRejectionCompletesPreviousFolderActivityWithoutShowingRejection(t *testing.T) {
	application, client := fixtureApp(t)
	application.Config.Automatic = false
	var logs bytes.Buffer
	application.Logger = log.New(&logs, "", 0)
	state := fixtureCoordinator(t, application)
	update := fixtureFolderUpdate("old", "save")
	update.saveFolder = application.Config.SaveFolder
	replacement := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	queueErr := state.uploader.queue(ctx, update)
	_ = nextCoordinatorEvent[uploadStarted](t, ctx, state)
	request := nextRequest(t, ctx, client)
	commandErr := state.handleCommand(ctx, Command{Kind: "folder", Value: replacement})
	afterChange := state.view.delivery.active
	messageAfterChange := publishedStatus(t, application).Message
	request.reply <- uploadReply{err: &leaderboard.UploadError{Message: "old save rejected"}}
	_ = nextCoordinatorEvent[uploadRejected](t, ctx, state)
	status := publishedStatus(t, application)

	if queueErr != nil || commandErr != nil {
		t.Fatalf("errors: %v, %v", queueErr, commandErr)
	}
	if afterChange == nil || !strings.Contains(messageAfterChange, "Uploading saves (previous folder)") || state.view.delivery.active != nil || state.view.delivery.rejected != nil || status.Message != "Automatic uploads off" {
		t.Fatal("old-folder activity was not retained until completion or its rejection leaked into the new view")
	}
	if !strings.Contains(logs.String(), "upload failed: old save rejected") {
		t.Fatal("filtering presentation suppressed the failure log")
	}
}

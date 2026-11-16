package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cwgrpguploader/internal/leaderboard"
)

func TestPendingNewFolderRemainsVisibleDuringOldFolderBackoff(t *testing.T) {
	application, client := fixtureApp(t)
	replacement, _ := fixtureApp(t)
	state := fixtureCoordinator(t, application)
	old := fixtureFolderUpdate("old", "old save")
	old.saveFolder = application.Config.SaveFolder
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	state.forwardSnapshot(ctx, old)
	_ = nextCoordinatorEvent[uploadStarted](t, ctx, state)
	request := nextRequest(t, ctx, client)
	request.reply <- uploadReply{err: &leaderboard.UploadError{Message: "retry later", Retry: true, After: time.Hour}}
	_ = nextCoordinatorEvent[uploadRetryScheduled](t, ctx, state)
	commandErr := state.handleCommand(ctx, Command{Kind: "folder", Value: replacement.Config.SaveFolder})
	now := time.Now()
	state.tick(ctx, now.Add(6*time.Second))
	state.tick(ctx, now.Add(7*time.Second))
	message := publishedStatus(t, application).Message

	if commandErr != nil {
		t.Fatal(commandErr)
	}
	if !strings.Contains(message, "No new changes; Pending upload; Retry at ") || !strings.Contains(message, "(previous folder)") {
		t.Fatalf("accepted new-folder work disappeared during backoff: %s", message)
	}
	if len(client.requests) != 0 {
		t.Fatal("new-folder work bypassed the old request's backoff")
	}
}

func TestOlderCompletionDoesNotClearRepeatedSnapshotPendingUpload(t *testing.T) {
	application, client := fixtureApp(t)
	state := fixtureCoordinator(t, application)
	update := fixtureFolderUpdate("same", "same save")
	update.saveFolder = application.Config.SaveFolder
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	state.forwardSnapshot(ctx, update)
	started := nextLifecycleEvent(t, ctx, state.uploader)
	state.forwardSnapshot(ctx, update)
	// Process the first emission's delayed start only after the second
	// identical emission has been accepted into the pending slot.
	state.handleUploadEvent(started)
	first := nextRequest(t, ctx, client)
	afterStarted := publishedStatus(t, application).Message
	first.reply <- uploadReply{result: successfulResult()}
	_ = nextCoordinatorEvent[uploadSucceeded](t, ctx, state)
	afterFirst := publishedStatus(t, application).Message
	_ = nextCoordinatorEvent[uploadStarted](t, ctx, state)
	second := nextRequest(t, ctx, client)
	second.reply <- uploadReply{result: successfulResult()}
	_ = nextCoordinatorEvent[uploadSucceeded](t, ctx, state)
	afterSecond := publishedStatus(t, application).Message

	if !strings.Contains(afterStarted, "Pending upload; Uploading saves") {
		t.Fatalf("delayed older start event erased a repeated emission: %s", afterStarted)
	}
	if !strings.Contains(afterFirst, "Pending upload") || !strings.Contains(afterFirst, "Last upload:") {
		t.Fatalf("older success erased a repeated emission: %s", afterFirst)
	}
	if strings.Contains(afterSecond, "Pending upload") || strings.Contains(afterSecond, "Uploading saves") || !strings.Contains(afterSecond, "Last upload:") {
		t.Fatalf("completed delivery remained active: %s", afterSecond)
	}
}

func TestDelayedRejectionDoesNotRejectNewerPendingEmission(t *testing.T) {
	application, client := fixtureApp(t)
	state := fixtureCoordinator(t, application)
	update := fixtureFolderUpdate("same", "save")
	update.saveFolder = application.Config.SaveFolder
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	state.forwardSnapshot(ctx, update)
	_ = nextCoordinatorEvent[uploadStarted](t, ctx, state)
	first := nextRequest(t, ctx, client)
	first.reply <- uploadReply{err: &leaderboard.UploadError{Message: "invalid old emission"}}
	rejected := nextUploaderEvent[uploadRejected](t, ctx, state.uploader)
	state.forwardSnapshot(ctx, update)
	state.handleUploadEvent(rejected)
	afterRejection := publishedStatus(t, application).Message
	_ = nextCoordinatorEvent[uploadStarted](t, ctx, state)
	second := nextRequest(t, ctx, client)
	second.reply <- uploadReply{result: successfulResult()}
	_ = nextCoordinatorEvent[uploadSucceeded](t, ctx, state)

	status := publishedStatus(t, application)
	if !strings.Contains(afterRejection, "Pending upload") || strings.Contains(afterRejection, "invalid old emission") {
		t.Fatalf("an older rejection described newer pending work: %s", afterRejection)
	}
	if strings.Contains(status.Message, "Pending upload") || !strings.Contains(status.Message, "Last upload:") {
		t.Fatalf("newer emission did not complete: %s", status.Message)
	}
}

func TestDelayedPreviewDoesNotClearNewerPendingEmission(t *testing.T) {
	application, client := fixtureApp(t)
	application.DryRun = true
	state := fixtureCoordinator(t, application)
	update := fixtureFolderUpdate("same", "save")
	update.saveFolder = application.Config.SaveFolder
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	state.forwardSnapshot(ctx, update)
	preview := nextUploaderEvent[uploadPreviewed](t, ctx, state.uploader)
	state.forwardSnapshot(ctx, update)
	state.handleUploadEvent(preview)
	afterFirst := publishedStatus(t, application)
	_ = nextCoordinatorEvent[uploadPreviewed](t, ctx, state)
	afterSecond := publishedStatus(t, application)

	if afterFirst.Message != "Pending upload" {
		t.Fatalf("older preview replaced newer pending work: %s", afterFirst.Message)
	}
	if afterSecond.Message != "Dry run: 1 eligible files; no upload or successful state written" {
		t.Fatalf("preview did not acknowledge its pending emission: %s", afterSecond.Message)
	}
	if len(client.requests) != 0 || !application.LastUpload.UploadedAt.IsZero() {
		t.Fatal("preview uploaded data or recorded a successful upload")
	}
}

func TestSuccessHistoryDoesNotHideSubsequentSettling(t *testing.T) {
	application, client := fixtureApp(t)
	state := fixtureCoordinator(t, application)
	update := fixtureFolderUpdate("first", "save")
	update.saveFolder = application.Config.SaveFolder
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	now := time.Now()

	state.tick(ctx, now)
	state.forwardSnapshot(ctx, update)
	_ = nextCoordinatorEvent[uploadStarted](t, ctx, state)
	request := nextRequest(t, ctx, client)
	request.reply <- uploadReply{result: successfulResult()}
	_ = nextCoordinatorEvent[uploadSucceeded](t, ctx, state)
	writeErr := os.WriteFile(filepath.Join(application.Config.SaveFolder, "HERO_01_123.pld"), []byte("changed save"), 0600)
	state.tick(ctx, now.Add(6*time.Second))
	status := publishedStatus(t, application)

	if writeErr != nil {
		t.Fatal(writeErr)
	}
	if !strings.Contains(status.Message, "waiting for saves to settle") || !strings.Contains(status.Message, "Last upload:") {
		t.Fatalf("success history hid new detection activity: %s", status.Message)
	}
}

func TestSuccessHistoryDoesNotHidePause(t *testing.T) {
	application, client := fixtureApp(t)
	state := fixtureCoordinator(t, application)
	update := fixtureFolderUpdate("first", "save")
	update.saveFolder = application.Config.SaveFolder
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	state.forwardSnapshot(ctx, update)
	_ = nextCoordinatorEvent[uploadStarted](t, ctx, state)
	request := nextRequest(t, ctx, client)
	request.reply <- uploadReply{result: successfulResult()}
	_ = nextCoordinatorEvent[uploadSucceeded](t, ctx, state)
	commandErr := state.handleCommand(ctx, Command{Kind: "pause"})
	status := publishedStatus(t, application)

	if commandErr != nil {
		t.Fatal(commandErr)
	}
	if !strings.Contains(status.Message, "Automatic uploads off") || !strings.Contains(status.Message, "Last upload:") {
		t.Fatalf("success history hid paused detection: %s", status.Message)
	}
}

func TestFailedQueueDoesNotClaimPendingDelivery(t *testing.T) {
	application, _ := fixtureApp(t)
	state := fixtureCoordinator(t, application)
	state.uploader.stop()

	state.forwardSnapshot(context.Background(), fixtureFolderUpdate("stopped", "save"))
	status := publishedStatus(t, application)

	if !strings.Contains(status.Message, "uploader stopped") || strings.Contains(status.Message, "Pending upload") {
		t.Fatalf("failed acceptance claimed pending delivery: %s", status.Message)
	}
}

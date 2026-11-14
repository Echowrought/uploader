package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cwg2uploader/internal/leaderboard"
	"cwg2uploader/internal/saves"
	"cwg2uploader/internal/storage"
)

func TestFolderPollerEmitsStableSnapshotOnlyOnce(t *testing.T) {
	application, _ := fixtureApp(t)
	poller := fixtureFolderPoller(application)
	now := time.Now()

	settling := poller.poll(context.Background(), now, true)
	first := poller.poll(context.Background(), now.Add(5*time.Second), true)
	repeated := poller.poll(context.Background(), now.Add(10*time.Second), true)

	if settling.kind != pollSettling || !errors.Is(settling.err, saves.ErrSettling) {
		t.Fatalf("settling event = %+v", settling)
	}
	if first.kind != pollUpdated || first.update == nil || repeated.kind != pollStable || repeated.update != nil || settling.update != nil {
		t.Fatal("unchanged snapshot emitted more than once")
	}
	if first.update.snapshot.Fingerprint == "" || poller.lastEmittedFingerprint != first.update.snapshot.Fingerprint {
		t.Fatal("emitted fingerprint not tracked")
	}
}

func TestFolderPollerForcedEmissionReemitsUnchangedSnapshot(t *testing.T) {
	application, _ := fixtureApp(t)
	poller := fixtureFolderPoller(application)
	now := time.Now()

	poller.poll(context.Background(), now, true)
	first := poller.poll(context.Background(), now.Add(5*time.Second), true)
	poller.requestEmission()
	second := poller.poll(context.Background(), now.Add(6*time.Second), false)

	if first.kind != pollUpdated || second.kind != pollUpdated || first.update == nil || second.update == nil || poller.forceNextEmission {
		t.Fatal("forced emission was not consumed")
	}
	if first.update.snapshot.Fingerprint != second.update.snapshot.Fingerprint {
		t.Fatal("unchanged data should retain its fingerprint")
	}
}

func TestFolderPollerReturnsChangedSnapshot(t *testing.T) {
	application, _ := fixtureApp(t)
	poller := fixtureFolderPoller(application)
	poller.requestEmission()
	now := time.Now()
	path := filepath.Join(application.Config.SaveFolder, "HERO_01_123.pld")

	poller.poll(context.Background(), now, true)
	first := poller.poll(context.Background(), now.Add(5*time.Second), true)
	writeErr := os.WriteFile(path, []byte("newer save data"), 0600)
	pending := poller.poll(context.Background(), now.Add(6*time.Second), true)
	second := poller.poll(context.Background(), now.Add(11*time.Second), true)

	if writeErr != nil {
		t.Fatal(writeErr)
	}
	if first.kind != pollUpdated || second.kind != pollUpdated || first.update == nil || second.update == nil || pending.kind != pollStable {
		t.Fatalf("events: %+v, %+v, %+v", first, second, pending)
	}
	if second.update.snapshot.Files["HERO_01_123.pld"].Text != "newer save data" || first.update.snapshot.Files["HERO_01_123.pld"].Text != "save" {
		t.Fatal("changed snapshot was not returned independently of the original")
	}
	if poller.forceNextEmission {
		t.Fatal("successful emission did not clear the poller's force flag")
	}
}

func TestFolderPollerAutomaticOffDoesNotEmitFurtherChanges(t *testing.T) {
	application, _ := fixtureApp(t)
	poller := fixtureFolderPoller(application)
	poller.requestEmission()
	now := time.Now()

	poller.poll(context.Background(), now, false)
	first := poller.poll(context.Background(), now.Add(5*time.Second), false)
	writeErr := os.WriteFile(filepath.Join(application.Config.SaveFolder, "HERO_01_123.pld"), []byte("changed save"), 0600)
	disabled := poller.poll(context.Background(), now.Add(6*time.Second), false)

	if writeErr != nil {
		t.Fatal(writeErr)
	}
	if first.kind != pollUpdated || first.update == nil || disabled.kind != pollDisabled {
		t.Fatalf("events: %+v, %+v", first, disabled)
	}
	if disabled.update != nil || poller.lastEmittedFingerprint != first.update.snapshot.Fingerprint {
		t.Fatal("manual emission enabled subsequent automatic polling")
	}
}

func TestFolderPollerCancellationDoesNotRecordEmission(t *testing.T) {
	application, _ := fixtureApp(t)
	poller := fixtureFolderPoller(application)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	event := poller.poll(ctx, time.Now(), true)

	if event.kind != pollCancelled || !errors.Is(event.err, context.Canceled) {
		t.Fatalf("cancelled poll = %+v", event)
	}
	if poller.lastEmittedFingerprint != "" || event.update != nil {
		t.Fatal("cancelled poll recorded an event")
	}
}

func TestCoordinatorDetectsChangesDuringAnActiveUpload(t *testing.T) {
	application, client := fixtureApp(t)
	state := fixtureCoordinator(t, application)
	now := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	state.tick(ctx, now)
	state.tick(ctx, now.Add(5*time.Second))
	firstFingerprint := state.poller.lastEmittedFingerprint
	_ = nextCoordinatorEvent[uploadStarted](t, ctx, state)
	first := nextRequest(t, ctx, client)
	writeErr := os.WriteFile(filepath.Join(application.Config.SaveFolder, "HERO_01_123.pld"), []byte("changed during upload"), 0600)
	state.tick(ctx, now.Add(6*time.Second))
	state.tick(ctx, now.Add(11*time.Second))
	first.reply <- uploadReply{result: successfulResult()}
	_ = nextCoordinatorEvent[uploadSucceeded](t, ctx, state)
	_ = nextCoordinatorEvent[uploadStarted](t, ctx, state)
	second := nextRequest(t, ctx, client)
	second.reply <- uploadReply{result: successfulResult()}
	latest := nextCoordinatorEvent[uploadSucceeded](t, ctx, state)

	if writeErr != nil {
		t.Fatal(writeErr)
	}
	if second.payload.Files["HERO_01_123.pld"].Text != "changed during upload" {
		t.Fatal("active upload prevented folder change detection")
	}
	if latest.fingerprint == firstFingerprint || application.LastUpload.Fingerprint != latest.fingerprint {
		t.Fatal("changed snapshot was not acknowledged")
	}
}

func TestCoordinatorDeliversNewSnapshotBeforeRetry(t *testing.T) {
	t.Parallel()
	application, client := fixtureApp(t)
	state := fixtureCoordinator(t, application)
	now := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	state.tick(ctx, now)
	state.tick(ctx, now.Add(5*time.Second))
	_ = nextCoordinatorEvent[uploadStarted](t, ctx, state)
	first := nextRequest(t, ctx, client)
	first.reply <- uploadReply{err: &leaderboard.UploadError{Message: "retry later", Retry: true, After: 6 * time.Second}}
	retry := nextCoordinatorEvent[uploadRetryScheduled](t, ctx, state)
	writeErr := os.WriteFile(filepath.Join(application.Config.SaveFolder, "HERO_01_123.pld"), []byte("latest data"), 0600)
	state.tick(ctx, now.Add(6*time.Second))
	state.tick(ctx, now.Add(11*time.Second))
	_ = nextCoordinatorEvent[uploadStarted](t, ctx, state)
	second := nextRequest(t, ctx, client)

	if writeErr != nil {
		t.Fatal(writeErr)
	}
	if second.payload.Files["HERO_01_123.pld"].Text != "latest data" || second.startedAt.Before(retry.retryAt) {
		t.Fatal("retry used stale data or bypassed Retry-After")
	}
}

func TestFolderPollerEmitsAgainAfterRestart(t *testing.T) {
	application, _ := fixtureApp(t)
	first := fixtureFolderPoller(application)
	restarted := fixtureFolderPoller(application)
	now := time.Now()

	first.poll(context.Background(), now, true)
	before := first.poll(context.Background(), now.Add(5*time.Second), true)
	restarted.poll(context.Background(), now, true)
	after := restarted.poll(context.Background(), now.Add(5*time.Second), true)

	if before.kind != pollUpdated || after.kind != pollUpdated || before.update == nil || after.update == nil {
		t.Fatalf("events: %+v, %+v", before, after)
	}
	if before.update.snapshot.Fingerprint != after.update.snapshot.Fingerprint {
		t.Fatal("restart suppressed an emitted but unacknowledged snapshot")
	}
}

func TestRunStateSeedsPollerFromMatchingSuccessfulUpload(t *testing.T) {
	cases := []struct {
		name, folderSuffix, destination string
		dryRun                          bool
		want                            string
	}{
		{"matching success", "", leaderboard.BaseURL, false, "uploaded"},
		{"different folder", "other", leaderboard.BaseURL, false, ""},
		{"different destination", "", "https://other.invalid", false, ""},
		{"dry run still emits", "", leaderboard.BaseURL, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			application, _ := fixtureApp(t)
			application.DryRun = tc.dryRun
			application.LastUpload = storage.UploadState{SaveFolder: filepath.Join(application.Config.SaveFolder, tc.folderSuffix), Destination: tc.destination, Fingerprint: "uploaded"}

			state := fixtureCoordinator(t, application)

			if state.poller.lastEmittedFingerprint != tc.want {
				t.Fatalf("baseline = %q; want %q", state.poller.lastEmittedFingerprint, tc.want)
			}
		})
	}
}

func TestCoordinatorAutomaticOffFinishesQueuedWorkAndRetries(t *testing.T) {
	t.Parallel()
	application, client := fixtureApp(t)
	application.Config.Automatic = false
	state := fixtureCoordinator(t, application)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	queueErr := state.uploader.queue(ctx, fixtureFolderUpdate("pending", "save"))
	_ = nextCoordinatorEvent[uploadStarted](t, ctx, state)
	first := nextRequest(t, ctx, client)
	first.reply <- uploadReply{err: &leaderboard.UploadError{Message: "temporary failure", Retry: true}}
	_ = nextCoordinatorEvent[uploadRetryScheduled](t, ctx, state)
	_ = nextCoordinatorEvent[uploadStarted](t, ctx, state)
	second := nextRequest(t, ctx, client)
	second.reply <- uploadReply{result: successfulResult()}
	_ = nextCoordinatorEvent[uploadSucceeded](t, ctx, state)

	if queueErr != nil {
		t.Fatal(queueErr)
	}
	if application.Config.Automatic || application.LastUpload.Fingerprint != "pending" {
		t.Fatal("accepted work did not finish independently of automatic polling")
	}
	if state.poller.lastEmittedFingerprint != "" {
		t.Fatal("retry required another folder event")
	}
}

func TestCommandUploadLatchesEmissionWhileAutomaticOff(t *testing.T) {
	state := fixtureRunState(t)
	state.app.Config.Automatic = false

	err := state.handleCommand(context.Background(), Command{Kind: "upload"})
	status := publishedStatus(t, state.app)

	if err != nil {
		t.Fatal(err)
	}
	if !state.poller.forceNextEmission || state.app.Config.Automatic {
		t.Fatal("upload request was not latched independently of automatic polling")
	}
	if status.Message != saves.ErrSettling.Error() {
		t.Fatal("requested snapshot bypassed settling")
	}
}

func TestTickPreservesForcedEmissionAcrossSettlingTicks(t *testing.T) {
	cases := []struct {
		name      string
		automatic bool
	}{{"automatic uploads on", true}, {"automatic uploads off", false}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			application, client := fixtureApp(t)
			state := fixtureCoordinator(t, application)
			application.Config.Automatic = tc.automatic
			snapshot := readSnapshot(t, application.Config.SaveFolder)
			state.poller.lastEmittedFingerprint = snapshot.Fingerprint
			now := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			commandErr := state.handleCommand(ctx, Command{Kind: "upload"})
			state.tick(ctx, now.Add(time.Second))
			state.tick(ctx, now.Add(2*time.Second))
			latchedBeforeSettling := state.poller.forceNextEmission
			beforeSettling := len(client.requests)
			state.tick(ctx, now.Add(6*time.Second))
			started := nextCoordinatorEvent[uploadStarted](t, ctx, state)
			request := nextRequest(t, ctx, client)
			if commandErr != nil || !latchedBeforeSettling || state.poller.forceNextEmission {
				t.Fatal("force request was lost or not consumed after settling")
			}
			if beforeSettling != 0 || started.id != state.nextWorkID || started.saveFolder != application.Config.SaveFolder || request.payload.Files["HERO_01_123.pld"].Text != "save" {
				t.Fatal("forced snapshot was not emitted and uploaded after settling")
			}
			if application.Config.Automatic != tc.automatic {
				t.Fatal("explicit request changed the preference")
			}
		})
	}
}

func TestTickAutomaticOffDoesNotReadSaveFolder(t *testing.T) {
	state := fixtureRunState(t)
	state.app.Config.Automatic = false
	state.poller.scanner.Folder = filepath.Join(t.TempDir(), "missing")

	state.tick(context.Background(), time.Now())
	status := publishedStatus(t, state.app)

	if status.Message != "Automatic uploads off" {
		t.Fatal("disabled polling accessed the save folder")
	}
}

func fixtureFolderPoller(application *App) *folderPoller {
	return newFolderPoller(application.Config.SaveFolder, "", componentLogger(application.Logger, "poller"))
}

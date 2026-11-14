package app

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
	"time"

	"cwg2uploader/internal/leaderboard"
)

func TestWorkLogContextPersistsAcrossRetriesAndChangesForNewEmissions(t *testing.T) {
	t.Parallel()
	application, client := fixtureApp(t)
	var logs bytes.Buffer
	application.Logger = log.New(&logs, "", 0)
	state := fixtureCoordinator(t, application)
	update := fixtureFolderUpdate("same", "save")
	update.saveFolder = application.Config.SaveFolder
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	state.forwardSnapshot(ctx, update)
	_ = nextCoordinatorEvent[uploadStarted](t, ctx, state)
	first := nextRequest(t, ctx, client)
	first.reply <- uploadReply{err: &leaderboard.UploadError{Message: "temporary failure", Retry: true, Body: "retry\nlater"}}
	_ = nextCoordinatorEvent[uploadRetryScheduled](t, ctx, state)
	_ = nextCoordinatorEvent[uploadStarted](t, ctx, state)
	retry := nextRequest(t, ctx, client)
	retry.reply <- uploadReply{result: successfulResult()}
	_ = nextCoordinatorEvent[uploadSucceeded](t, ctx, state)
	state.forwardSnapshot(ctx, update)
	_ = nextCoordinatorEvent[uploadStarted](t, ctx, state)
	second := nextRequest(t, ctx, client)
	second.reply <- uploadReply{result: successfulResult()}
	_ = nextCoordinatorEvent[uploadSucceeded](t, ctx, state)
	state.shutdownUploader()
	output := logs.String()

	for _, want := range []string{
		"[app] work_id=1 snapshot accepted: 1 eligible files",
		"[uploader] work_id=1 snapshot queued: 1 eligible files",
		"[uploader] work_id=1 upload failed: temporary failure",
		`[uploader] work_id=1 Upload error response body: "retry\nlater"`,
		"[uploader] work_id=1 retry scheduled at ",
		"[uploader] work_id=1 upload succeeded: 1 heroes",
		"[app] work_id=2 snapshot accepted: 1 eligible files",
		"[uploader] work_id=2 snapshot queued: 1 eligible files",
		"[uploader] work_id=2 upload started: 1 eligible files",
		"[uploader] work_id=2 upload succeeded: 1 heroes",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("missing log context %q in:\n%s", want, output)
		}
	}
	if count := strings.Count(output, "[uploader] work_id=1 upload started:"); count != 2 {
		t.Errorf("original attempt and retry should share work_id=1; got %d attempts", count)
	}
}

func TestWorkLogContextIncludesDryRunPreview(t *testing.T) {
	application, _ := fixtureApp(t)
	application.DryRun = true
	var logs bytes.Buffer
	application.Logger = log.New(&logs, "", 0)
	state := fixtureCoordinator(t, application)
	update := fixtureFolderUpdate("preview", "save")
	update.saveFolder = application.Config.SaveFolder
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	state.forwardSnapshot(ctx, update)
	_ = nextCoordinatorEvent[uploadPreviewed](t, ctx, state)
	state.shutdownUploader()

	if !strings.Contains(logs.String(), "[uploader] work_id=1 dry-run snapshot prepared: 1 eligible files") {
		t.Fatalf("preview log lost its work context:\n%s", logs.String())
	}
}

func TestWorkLogContextIncludesCancelledUpload(t *testing.T) {
	application, client := fixtureApp(t)
	var logs bytes.Buffer
	application.Logger = log.New(&logs, "", 0)
	state := fixtureCoordinator(t, application)
	update := fixtureFolderUpdate("cancelled", "save")
	update.saveFolder = application.Config.SaveFolder
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	state.forwardSnapshot(ctx, update)
	_ = nextCoordinatorEvent[uploadStarted](t, ctx, state)
	_ = nextRequest(t, ctx, client)
	state.shutdownUploader()

	if !strings.Contains(logs.String(), "[uploader] work_id=1 upload stopped: context canceled") {
		t.Fatalf("cancellation log lost its work context:\n%s", logs.String())
	}
}

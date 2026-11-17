package app

import (
	"context"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"cwgrpguploader/internal/storage"
)

func TestFolderChangeDiscardsUnfulfilledManualRequest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		application.Config.Automatic = false
		replacement := fixtureSaveFolder(t)
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		_ = awaitStatus(t, ctx, application, "Automatic uploads off")
		uploadErr := sendCommand(t, ctx, application, Command{Kind: "upload"})
		_ = awaitStatus(t, ctx, application, "waiting for saves to settle")
		folderErr := sendCommand(t, ctx, application, Command{Kind: "folder", Value: replacement})
		time.Sleep(7 * time.Second)
		synctest.Wait()
		status := <-application.Updates
		cancel()
		<-done

		if uploadErr != nil || folderErr != nil {
			t.Fatalf("commands: %v %v", uploadErr, folderErr)
		}
		if len(client.requests) != 0 || status.Config.Automatic || status.Config.SaveFolder != replacement || status.Detection != "Automatic uploads off" || status.Upload != "" {
			t.Fatalf("old manual intent escaped into the new folder: %+v; requests=%d", status, len(client.requests))
		}
	})
}

func TestFailedQueueDoesNotClaimPendingDelivery(t *testing.T) {
	application, client := fixtureApp(t)
	state := newRunState(t.Context(), application)
	t.Cleanup(state.shutdownUploader)
	state.uploader.stop()
	update := fixtureFolderUpdate(11, "ready", "ready save")
	update.saveFolder = application.Config.SaveFolder

	state.forwardSnapshot(t.Context(), update)
	status := <-application.Updates

	if status.Upload != "" || !strings.Contains(status.Message, "uploader stopped") || len(client.requests) != 0 {
		t.Fatalf("failed queue claimed delivery or lost its error: %+v", status)
	}
}

func TestForwardSnapshotDoesNotWaitForFutureEmission(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		state := newRunState(ctx, application)
		t.Cleanup(state.shutdownUploader)
		ready := fixtureFolderUpdate(11, "ready", "already ready")
		ready.saveFolder = application.Config.SaveFolder

		state.poller.requestEmission()
		state.forwardSnapshot(ctx, ready)
		_ = nextUploaderEvent[uploadStarted](t, ctx, state.uploader)
		request := nextRequest(t, ctx, client)
		request.reply <- uploadReply{result: successfulResult()}
		success := nextUploaderEvent[uploadSucceeded](t, ctx, state.uploader)

		if request.payload.Files["HERO_01_123.pld"].Text != "already ready" || success.fingerprint != ready.snapshot.Fingerprint {
			t.Fatal("future emission prevented delivery of already-ready work")
		}
	})
}

func TestOldFolderSuccessIsPersistedWithoutReplacingCurrentPresentation(t *testing.T) {
	application, _ := fixtureApp(t)
	oldFolder := application.Config.SaveFolder
	replacement := fixtureSaveFolder(t)
	state := newRunState(t.Context(), application)
	t.Cleanup(state.shutdownUploader)
	confirmedAt := time.Date(2026, 1, 1, 12, 34, 56, 0, time.UTC)
	success := uploadSucceeded{id: 11, saveFolder: oldFolder, fingerprint: "old confirmed snapshot", at: confirmedAt, result: successfulResult()}

	folderErr := state.changeFolder(replacement)
	state.handleUploadEvent(uploadStarted{id: 22, saveFolder: replacement})
	state.handleUploadEvent(success)
	status := <-application.Updates
	persisted, loadErr := storage.LoadUploadState(application.Dir)

	if folderErr != nil || loadErr != nil {
		t.Fatalf("errors: %v %v", folderErr, loadErr)
	}
	if status.Upload != "Uploading saves" || status.Config.SaveFolder != replacement {
		t.Fatalf("old success replaced newer activity or selection: %+v", status)
	}
	if persisted != status.History || persisted.SaveFolder != oldFolder || persisted.Fingerprint != success.fingerprint || !persisted.UploadedAt.Equal(confirmedAt) || persisted.Player != success.result.Player || persisted.Heroes != success.result.Heroes {
		t.Fatalf("old-folder confirmation was lost or given the wrong origin: %+v", persisted)
	}
}

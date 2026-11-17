package app

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"cwgrpguploader/internal/leaderboard"
	"cwgrpguploader/internal/storage"
)

func TestAppDoesNotUseUploadHistoryAsDetectionObservationTime(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		snapshot := readSnapshot(t, application.Config.SaveFolder)
		started := time.Now()
		application.LastUpload = storage.UploadState{SaveFolder: application.Config.SaveFolder, Destination: leaderboard.BaseURL, Fingerprint: snapshot.Fingerprint, UploadedAt: started.Add(-24 * time.Hour)}
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		status := awaitStatus(t, ctx, application, "No new changes")
		cancel()
		<-done

		if status.DetectionSince.Before(started) || status.DetectionSince.IsZero() {
			t.Fatalf("claimed observations before this session: %v", status.DetectionSince)
		}
		if len(client.requests) != 0 {
			t.Fatal("recording an observation time caused a duplicate upload")
		}
	})
}

func TestAppRestartsDetectionTimeAfterFolderChange(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		application, _ := fixtureApp(t)
		replacement := fixtureSaveFolder(t)
		application.DryRun = true
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
		defer cancel()

		done := startApp(ctx, application)
		initial := awaitStatus(t, ctx, application, "No new changes")
		time.Sleep(65 * time.Second)
		commandErr := sendCommand(t, ctx, application, Command{Kind: "folder", Value: replacement})
		time.Sleep(7 * time.Second)
		synctest.Wait()
		after := awaitStatus(t, ctx, application, "No new changes")
		cancel()
		<-done

		if commandErr != nil {
			t.Fatal(commandErr)
		}
		if after.Config.SaveFolder != replacement || !after.DetectionSince.After(initial.DetectionSince) {
			t.Fatalf("new folder retained old observation time: before=%v after=%v folder=%q", initial.DetectionSince, after.DetectionSince, after.Config.SaveFolder)
		}
	})
}

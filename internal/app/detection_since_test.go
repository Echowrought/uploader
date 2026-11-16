package app

import (
	"context"
	"os"
	"path/filepath"
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
		application.LastUpload = storage.UploadState{
			SaveFolder: application.Config.SaveFolder, Destination: leaderboard.BaseURL,
			Fingerprint: snapshot.Fingerprint, UploadedAt: started.Add(-24 * time.Hour),
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
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

func TestAppRetainsDetectionTimeAcrossManualUploadOfUnchangedSaves(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		application, _ := fixtureApp(t)
		application.DryRun = true
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		done := startApp(ctx, application)
		initial := awaitStatus(t, ctx, application, "No new changes")
		time.Sleep(65 * time.Second)
		application.Commands <- Command{Kind: "upload"}
		time.Sleep(2 * time.Second)
		synctest.Wait()
		after := awaitStatus(t, ctx, application, "No new changes")
		cancel()
		<-done

		if initial.DetectionSince.IsZero() || !after.DetectionSince.Equal(initial.DetectionSince) {
			t.Fatalf("delivery request reset unchanged observation: before=%v after=%v", initial.DetectionSince, after.DetectionSince)
		}
	})
}

func TestAppRestartsDetectionTimeWhenSettledContentsChange(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		application, _ := fixtureApp(t)
		application.DryRun = true
		path := filepath.Join(application.Config.SaveFolder, "HERO_01_123.pld")
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		done := startApp(ctx, application)
		initial := awaitStatus(t, ctx, application, "No new changes")
		time.Sleep(65 * time.Second)
		writeErr := os.WriteFile(path, []byte("new settled hero contents"), 0600)
		time.Sleep(7 * time.Second)
		synctest.Wait()
		after := awaitStatus(t, ctx, application, "No new changes")
		cancel()
		<-done

		if writeErr != nil {
			t.Fatal(writeErr)
		}
		if !after.DetectionSince.After(initial.DetectionSince) {
			t.Fatalf("new contents kept the old observation time: before=%v after=%v", initial.DetectionSince, after.DetectionSince)
		}
	})
}

func TestAppRestartsDetectionTimeAfterFolderChange(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		application, _ := fixtureApp(t)
		replacement, _ := fixtureApp(t)
		application.DryRun = true
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		done := startApp(ctx, application)
		initial := awaitStatus(t, ctx, application, "No new changes")
		time.Sleep(65 * time.Second)
		application.Commands <- Command{Kind: "folder", Value: replacement.Config.SaveFolder}
		time.Sleep(7 * time.Second)
		synctest.Wait()
		after := awaitStatus(t, ctx, application, "No new changes")
		cancel()
		<-done

		if after.Config.SaveFolder != replacement.Config.SaveFolder || !after.DetectionSince.After(initial.DetectionSince) {
			t.Fatalf("new folder retained the old observation time: before=%v after=%v folder=%q", initial.DetectionSince, after.DetectionSince, after.Config.SaveFolder)
		}
	})
}

func TestAppRestartsDetectionTimeAfterScanFailure(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		application, _ := fixtureApp(t)
		application.DryRun = true
		account := filepath.Join(application.Config.SaveFolder, "ACCOUNT_player_123.pld")
		contents, err := os.ReadFile(account)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		done := startApp(ctx, application)
		initial := awaitStatus(t, ctx, application, "No new changes")
		removeErr := os.Remove(account)
		broken := awaitStatus(t, ctx, application, "ACCOUNT_")
		time.Sleep(65 * time.Second)
		writeErr := os.WriteFile(account, contents, 0600)
		time.Sleep(7 * time.Second)
		synctest.Wait()
		after := awaitStatus(t, ctx, application, "No new changes")
		cancel()
		<-done

		if removeErr != nil || writeErr != nil {
			t.Fatalf("remove=%v restore=%v", removeErr, writeErr)
		}
		if !broken.DetectionSince.IsZero() || !after.DetectionSince.After(initial.DetectionSince) {
			t.Fatalf("claimed continuous observations through a scan failure: before=%v failed=%v after=%v", initial.DetectionSince, broken.DetectionSince, after.DetectionSince)
		}
	})
}

func TestAppRestartsDetectionTimeAfterMonitoringWasPaused(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		application, _ := fixtureApp(t)
		application.DryRun = true
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		done := startApp(ctx, application)
		initial := awaitStatus(t, ctx, application, "No new changes")
		application.Commands <- Command{Kind: "pause"}
		paused := awaitStatus(t, ctx, application, "Automatic uploads off")
		time.Sleep(65 * time.Second)
		application.Commands <- Command{Kind: "pause"}
		after := awaitStatus(t, ctx, application, "No new changes")
		cancel()
		<-done

		if !paused.DetectionSince.IsZero() || !after.DetectionSince.After(initial.DetectionSince) {
			t.Fatalf("claimed continuous observations through a pause: before=%v paused=%v after=%v", initial.DetectionSince, paused.DetectionSince, after.DetectionSince)
		}
	})
}

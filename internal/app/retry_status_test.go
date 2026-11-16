package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"cwgrpguploader/internal/leaderboard"
)

func TestAppDistinguishesRetriesFromTheFirstAttemptAndCompletedWork(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		first := nextRequest(t, ctx, client)
		initial := awaitStatus(t, ctx, application, "Uploading saves")
		first.reply <- uploadReply{err: &leaderboard.UploadError{Message: "offline", Retry: true}}
		waiting := awaitStatus(t, ctx, application, "Retry at")
		second := nextRequest(t, ctx, client)
		retrying := awaitStatus(t, ctx, application, "Retrying saves")
		second.reply <- uploadReply{err: &leaderboard.UploadError{Message: "still offline", Retry: true}}
		_ = awaitStatus(t, ctx, application, "Retry at")
		third := nextRequest(t, ctx, client)
		retryingAgain := awaitStatus(t, ctx, application, "Retrying saves")
		third.reply <- uploadReply{result: successfulResult()}
		completed := awaitStatus(t, ctx, application, "successfully")
		cancel()
		<-done

		if initial.Menu(time.Now()).Upload.Title != "Upload: Uploading saves" {
			t.Fatalf("first attempt incorrectly warned: %s", initial.Upload)
		}
		if !strings.HasPrefix(waiting.Upload, "Retry at ") ||
			retrying.Menu(time.Now()).Upload.Title != "Upload: Retrying saves" ||
			retryingAgain.Menu(time.Now()).Upload.Title != "Upload: Retrying saves" {
			t.Fatalf("retry warning missing: waiting=%q retry=%q next=%q", waiting.Upload, retrying.Upload, retryingAgain.Upload)
		}
		if completed.Menu(time.Now()).Upload.Title != "Upload: Idle" {
			t.Fatalf("warning survived success: %s", completed.Upload)
		}
	})
}

func TestAppDoesNotMarkNewSnapshotFirstAttemptAsRetry(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		path := filepath.Join(application.Config.SaveFolder, "HERO_01_123.pld")

		done := startApp(ctx, application)
		first := nextRequest(t, ctx, client)
		first.reply <- uploadReply{err: &leaderboard.UploadError{Message: "offline", Retry: true, After: 15 * time.Second}}
		waiting := awaitStatus(t, ctx, application, "Retry at")
		writeErr := os.WriteFile(path, []byte("new hero snapshot"), 0600)
		next := nextRequest(t, ctx, client)
		started := awaitStatus(t, ctx, application, "Uploading saves")
		next.reply <- uploadReply{result: successfulResult()}
		_ = awaitStatus(t, ctx, application, "successfully")
		cancel()
		<-done

		if writeErr != nil {
			t.Fatal(writeErr)
		}
		if !strings.HasPrefix(waiting.Upload, "Retry at ") {
			t.Fatalf("failed snapshot backoff was not marked: %s", waiting.Upload)
		}
		if next.payload.Files["HERO_01_123.pld"].Text != "new hero snapshot" {
			t.Fatal("new snapshot did not supersede failed work")
		}
		if started.Menu(time.Now()).Upload.Title != "Upload: Uploading saves" {
			t.Fatalf("new snapshot inherited an old retry warning: %s", started.Upload)
		}
	})
}

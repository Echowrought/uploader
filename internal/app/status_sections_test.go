package app

import (
	"context"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"cwgrpguploader/internal/leaderboard"
	"cwgrpguploader/internal/storage"
)

func TestAppPublishesPausedDetectionSeparatelyFromSuccessfulHistory(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		application.Config.Automatic = false
		application.LastUpload = storage.UploadState{UploadedAt: time.Now(), Player: "Player", Heroes: 2, SaveFolder: "previous saves"}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		status := awaitStatus(t, ctx, application, "Automatic uploads off")
		cancel()
		<-done
		menu := status.Menu(time.Now())

		if menu.Detection.Title != "Detection: Automatic uploads off" || menu.Upload.Title != "Upload: Idle" {
			t.Fatalf("unexpected activity: %+v", menu)
		}
		if !strings.Contains(menu.History.Title, "Today, ") || !strings.Contains(menu.History.Details, "previous saves") || status.History.Heroes != 2 {
			t.Fatalf("confirmed history lost: %+v", menu.History)
		}
		if len(client.requests) != 0 {
			t.Fatal("paused detection submitted new work")
		}
	})
}

func TestAppPublishesUnchangedDetectionWhileUploadWaitsForRetry(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		request := nextRequest(t, ctx, client)
		request.reply <- uploadReply{err: &leaderboard.UploadError{Message: "offline", Retry: true, After: time.Minute}}
		_ = awaitStatus(t, ctx, application, "Retry at")
		time.Sleep(time.Second)
		synctest.Wait()
		status := awaitStatus(t, ctx, application, "No new changes")
		cancel()
		<-done
		menu := status.Menu(time.Now())

		if !strings.HasPrefix(menu.Detection.Title, "Detection: No changes since ") || !strings.Contains(menu.Upload.Title, "Retry at") || menu.History.Title != "Last success: Never" {
			t.Fatalf("independent status was combined or lost: %+v", menu)
		}
	})
}

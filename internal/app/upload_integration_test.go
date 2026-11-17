package app

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"cwgrpguploader/internal/leaderboard"
	"cwgrpguploader/internal/storage"
)

func TestAppCoalescesPendingSavesWhileStatusIsUnread(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
		defer cancel()
		path := filepath.Join(application.Config.SaveFolder, "HERO_01_123.pld")

		done := startApp(ctx, application)
		first := nextRequest(t, ctx, client)
		writeErr := os.WriteFile(path, []byte("middle save"), 0600)
		time.Sleep(7 * time.Second)
		synctest.Wait()
		latestErr := os.WriteFile(path, []byte("latest save"), 0600)
		time.Sleep(7 * time.Second)
		synctest.Wait()
		// Neither accepted snapshots nor HTTP completion requires a status consumer.
		first.reply <- uploadReply{result: successfulResult()}
		second := nextRequest(t, ctx, client)
		second.reply <- uploadReply{result: successfulResult()}
		time.Sleep(2 * time.Second)
		synctest.Wait()
		status := awaitStatus(t, ctx, application, "successfully")
		cancel()
		<-done
		persisted, loadErr := storage.LoadUploadState(application.Dir)

		if writeErr != nil || latestErr != nil || loadErr != nil {
			t.Fatalf("write=%v latest=%v load=%v", writeErr, latestErr, loadErr)
		}
		if first.payload.Files["HERO_01_123.pld"].Text != "save" || second.payload.Files["HERO_01_123.pld"].Text != "latest save" || len(client.requests) != 0 {
			t.Fatal("pending snapshots were lost, mutated, or not coalesced")
		}
		assertUploadHistoryEqual(t, persisted, status.History)
		if persisted.Fingerprint != readSnapshot(t, application.Config.SaveFolder).Fingerprint || status.Upload != "" {
			t.Fatal("latest delivery was not recorded or remained active")
		}
	})
}

func TestAppAcceptsConcurrentManualCommandsWhileUploading(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
		defer cancel()
		results := make(chan error, 16)
		var callers sync.WaitGroup

		done := startApp(ctx, application)
		first := nextRequest(t, ctx, client)
		for range 16 {
			callers.Go(func() {
				result := make(chan error, 1)
				application.Commands <- Command{Kind: "upload", Result: result}
				results <- <-result
			})
		}
		callers.Wait()
		close(results)
		first.reply <- uploadReply{result: successfulResult()}
		second := nextRequest(t, ctx, client)
		second.reply <- uploadReply{result: successfulResult()}
		_ = awaitStatus(t, ctx, application, "successfully")
		time.Sleep(2 * time.Second)
		synctest.Wait()
		cancel()
		<-done

		for err := range results {
			if err != nil {
				t.Fatal(err)
			}
		}
		if !reflect.DeepEqual(first.payload, second.payload) || len(client.requests) != 0 {
			t.Fatal("concurrent manual requests corrupted or failed to coalesce work")
		}
	})
}

func TestAppManualUploadDoesNotBypassExactRetryDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()

		done := startApp(ctx, application)
		first := nextRequest(t, ctx, client)
		deadline := time.Now().Add(15 * time.Second)
		first.reply <- uploadReply{err: retryAfterError(15 * time.Second)}
		_ = awaitStatus(t, ctx, application, "Retry at")
		commandErr := sendCommand(t, ctx, application, Command{Kind: "upload"})
		time.Sleep(time.Until(deadline) - time.Nanosecond)
		synctest.Wait()
		early := len(client.requests)
		time.Sleep(time.Nanosecond)
		second := nextRequest(t, ctx, client)
		cancel()
		<-done

		if commandErr != nil {
			t.Fatal(commandErr)
		}
		if early != 0 || !second.startedAt.Equal(deadline) {
			t.Fatalf("retry timing: early requests=%d started=%s deadline=%s", early, second.startedAt, deadline)
		}
	})
}

func TestAppReportsRejectedUpload(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		request := nextRequest(t, ctx, client)
		request.reply <- uploadReply{err: &leaderboard.UploadError{Message: "invalid save"}}
		status := awaitStatus(t, ctx, application, "invalid save")
		cancel()
		<-done

		if !strings.Contains(status.Upload, "invalid save") {
			t.Fatalf("rejection missing from upload status: %q", status.Upload)
		}
	})
}

func TestAppManualUploadReemitsRejectedUnchangedSaves(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		first := nextRequest(t, ctx, client)
		first.reply <- uploadReply{err: &leaderboard.UploadError{Message: "invalid save"}}
		_ = awaitStatus(t, ctx, application, "invalid save")
		err := sendCommand(t, ctx, application, Command{Kind: "upload"})
		second := nextRequest(t, ctx, client)
		second.reply <- uploadReply{result: successfulResult()}
		status := awaitStatus(t, ctx, application, "successfully")
		cancel()
		<-done

		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(first.payload, second.payload) || status.History.UploadedAt.IsZero() || status.Upload != "" {
			t.Fatal("manual retry did not deliver unchanged rejected data")
		}
	})
}

func TestAppShutdownInterruptsRetryWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()

		done := startApp(ctx, application)
		first := nextRequest(t, ctx, client)
		first.reply <- uploadReply{err: retryAfterError(time.Hour)}
		_ = awaitStatus(t, ctx, application, "Retry at")
		stopped := time.Now()
		cancel()
		<-done

		if !time.Now().Equal(stopped) || len(client.requests) != 0 {
			t.Fatal("shutdown waited for backoff or started another request")
		}
	})
}

func TestAppShutdownJoinsClientCleanup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, _ := fixtureApp(t)
		started, cleaning, release, cleaned := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
		var once sync.Once
		finish := func() { once.Do(func() { close(release) }) }
		defer finish()
		application.Client = uploadFunc(func(ctx context.Context, _ leaderboard.UploadRequest) (leaderboard.Result, error) {
			close(started)
			<-ctx.Done()
			close(cleaning)
			<-release
			close(cleaned)
			return leaderboard.Result{}, ctx.Err()
		})
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		awaitSignal(t, ctx, started)
		cancel()
		<-cleaning
		synctest.Wait()
		exitedEarly := isClosed(done)
		finish()
		<-done

		if exitedEarly || !isClosed(cleaned) {
			t.Fatal("App.Run returned before its client finished cleanup")
		}
	})
}

func retryAfterError(after time.Duration) error {
	return &leaderboard.UploadError{Message: "retry later", Retry: true, After: after}
}

func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

type uploadFunc func(context.Context, leaderboard.UploadRequest) (leaderboard.Result, error)

func (f uploadFunc) Upload(ctx context.Context, request leaderboard.UploadRequest) (leaderboard.Result, error) {
	return f(ctx, request)
}

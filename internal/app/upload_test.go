package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"reflect"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"cwgrpguploader/internal/leaderboard"
	"cwgrpguploader/internal/saves"
)

func TestUploaderAutomaticallyStartsLatestPendingSnapshot(t *testing.T) {
	ctx, uploader, client := fixtureUploader(t)
	firstUpdate := fixtureFolderUpdate(11, "first", "first save")
	latestUpdate := fixtureFolderUpdate(33, "latest", "latest save")

	firstErr := uploader.queue(ctx, firstUpdate)
	started := nextUploaderEvent[uploadStarted](t, ctx, uploader)
	first := nextRequest(t, ctx, client)
	middleErr := uploader.queue(ctx, fixtureFolderUpdate(22, "middle", "middle save"))
	latestErr := uploader.queue(ctx, latestUpdate)
	first.reply <- uploadReply{result: successfulResult()}
	firstSuccess := nextUploaderEvent[uploadSucceeded](t, ctx, uploader)
	_ = nextUploaderEvent[uploadStarted](t, ctx, uploader)
	second := nextRequest(t, ctx, client)
	second.reply <- uploadReply{result: successfulResult()}
	latestSuccess := nextUploaderEvent[uploadSucceeded](t, ctx, uploader)

	if firstErr != nil || middleErr != nil || latestErr != nil {
		t.Fatalf("queue errors: %v %v %v", firstErr, middleErr, latestErr)
	}
	if started.id != firstUpdate.id || started.saveFolder != firstUpdate.saveFolder || firstSuccess.id != firstUpdate.id || firstSuccess.fingerprint != firstUpdate.snapshot.Fingerprint || latestSuccess.id != latestUpdate.id || latestSuccess.fingerprint != latestUpdate.snapshot.Fingerprint || latestSuccess.saveFolder != latestUpdate.saveFolder {
		t.Fatal("lifecycle events lost the accepted work's identity or origin")
	}
	if !reflect.DeepEqual(firstSuccess.result, successfulResult()) || firstSuccess.at.IsZero() || latestSuccess.at.IsZero() {
		t.Fatal("completion metadata lost its result or confirmation time")
	}
	if first.payload.Files["HERO_01_123.pld"].Text != "first save" || second.payload.Files["HERO_01_123.pld"].Text != "latest save" {
		t.Fatal("pending data was not coalesced or an active request was mutated")
	}
}

func TestUploaderPreservesSnapshotWhenNormalizingRequest(t *testing.T) {
	ctx, uploader, client := fixtureUploader(t)
	update := fixtureFolderUpdate(11, "original", "\ufeffhero\ufeffdata")
	update.snapshot.Files["ACCOUNT_player_123.pld"] = saves.File{Text: "account\ufeffdata", Mtime: 1700000000456}
	wantSnapshot := saves.Snapshot{Fingerprint: "original", Files: map[string]saves.File{
		"HERO_01_123.pld":        {Text: "\ufeffhero\ufeffdata", Mtime: 1700000000123},
		"ACCOUNT_player_123.pld": {Text: "account\ufeffdata", Mtime: 1700000000456},
	}}
	wantRequest := leaderboard.UploadRequest{Files: map[string]leaderboard.UploadFile{
		"HERO_01_123.pld":        {Text: "hero\ufeffdata", Mtime: 1700000000123},
		"ACCOUNT_player_123.pld": {Text: "account\ufeffdata", Mtime: 1700000000456},
	}}

	queueErr := uploader.queue(ctx, update)
	_ = nextUploaderEvent[uploadStarted](t, ctx, uploader)
	request := nextRequest(t, ctx, client)
	request.reply <- uploadReply{result: successfulResult()}
	_ = nextUploaderEvent[uploadSucceeded](t, ctx, uploader)

	if queueErr != nil {
		t.Fatal(queueErr)
	}
	if !reflect.DeepEqual(request.payload, wantRequest) {
		t.Fatalf("request = %+v; want %+v", request.payload, wantRequest)
	}
	if !reflect.DeepEqual(update.snapshot, wantSnapshot) {
		t.Fatalf("normalization mutated the submitted snapshot: %+v", update.snapshot)
	}
}

func TestUploaderQueueDoesNotWaitForEventDelivery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, uploader, client := fixtureUploader(t)

		firstErr := uploader.queue(ctx, fixtureFolderUpdate(11, "first", "first save"))
		_ = nextUploaderEvent[uploadStarted](t, ctx, uploader)
		first := nextRequest(t, ctx, client)
		first.reply <- uploadReply{result: successfulResult()}
		synctest.Wait() // The success event is deliberately left unread.
		middleErr := uploader.queue(ctx, fixtureFolderUpdate(22, "middle", "middle save"))
		latestErr := uploader.queue(ctx, fixtureFolderUpdate(33, "latest", "latest save"))
		firstSuccess := nextUploaderEvent[uploadSucceeded](t, ctx, uploader)
		_ = nextUploaderEvent[uploadStarted](t, ctx, uploader)
		second := nextRequest(t, ctx, client)
		second.reply <- uploadReply{result: successfulResult()}
		latestSuccess := nextUploaderEvent[uploadSucceeded](t, ctx, uploader)

		if firstErr != nil || middleErr != nil || latestErr != nil {
			t.Fatalf("queue errors: %v %v %v", firstErr, middleErr, latestErr)
		}
		if firstSuccess.fingerprint != "first" || latestSuccess.fingerprint != "latest" || second.payload.Files["HERO_01_123.pld"].Text != "latest save" {
			t.Fatal("event backpressure lost completion or prevented coalescing")
		}
	})
}

func TestUploaderQueueIsSafeForConcurrentCallers(t *testing.T) {
	ctx, uploader, client := fixtureUploader(t)
	results := make(chan error, 32)
	var callers sync.WaitGroup

	initialErr := uploader.queue(ctx, fixtureFolderUpdate(11, "active", "active save"))
	_ = nextUploaderEvent[uploadStarted](t, ctx, uploader)
	first := nextRequest(t, ctx, client)
	for range 32 {
		callers.Go(func() { results <- uploader.queue(ctx, fixtureFolderUpdate(22, "concurrent", "queued save")) })
	}
	callers.Wait()
	close(results)
	latestErr := uploader.queue(ctx, fixtureFolderUpdate(33, "latest", "latest save"))
	first.reply <- uploadReply{result: successfulResult()}
	_ = nextUploaderEvent[uploadSucceeded](t, ctx, uploader)
	_ = nextUploaderEvent[uploadStarted](t, ctx, uploader)
	second := nextRequest(t, ctx, client)
	second.reply <- uploadReply{result: successfulResult()}
	latest := nextUploaderEvent[uploadSucceeded](t, ctx, uploader)

	if initialErr != nil || latestErr != nil {
		t.Fatalf("queue errors: %v %v", initialErr, latestErr)
	}
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if latest.fingerprint != "latest" || second.payload.Files["HERO_01_123.pld"].Text != "latest save" {
		t.Fatal("concurrent calls corrupted pending work")
	}
}

func TestUploaderDryRunReturnsPreviewWithoutCallingClient(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	client := &fakeUploader{requests: make(chan uploadRequest, 8)}
	uploader := newSnapshotUploader(ctx, client, true, log.New(io.Discard, "", 0))
	t.Cleanup(func() { closeTestUploader(uploader) })
	update := fixtureFolderUpdate(11, "preview", "hero")
	update.snapshot.Files["ACCOUNT_player_123.pld"] = saves.File{Text: "account", Mtime: 1700000000456}

	queueErr := uploader.queue(ctx, update)
	preview := nextUploaderEvent[uploadPreviewed](t, ctx, uploader)
	closeTestUploader(uploader)

	if queueErr != nil {
		t.Fatal(queueErr)
	}
	if preview.id != update.id || preview.saveFolder != update.saveFolder || preview.fileCount != 2 || len(client.requests) != 0 {
		t.Fatal("preview lost metadata or called the network client")
	}
}

func TestUploaderRetriesOnItsOwnTimer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, uploader, client := fixtureUploader(t)
		update := fixtureFolderUpdate(11, "retry", "save")

		queueErr := uploader.queue(ctx, update)
		initial := nextUploaderEvent[uploadStarted](t, ctx, uploader)
		first := nextRequest(t, ctx, client)
		deadline := time.Now().Add(6 * time.Second)
		first.reply <- uploadReply{err: retryAfterError(6 * time.Second)}
		retry := nextUploaderEvent[uploadRetryScheduled](t, ctx, uploader)
		started := nextUploaderEvent[uploadStarted](t, ctx, uploader)
		second := nextRequest(t, ctx, client)
		second.reply <- uploadReply{result: successfulResult()}
		success := nextUploaderEvent[uploadSucceeded](t, ctx, uploader)

		if queueErr != nil {
			t.Fatal(queueErr)
		}
		if !retry.retryAt.Equal(deadline) || !second.startedAt.Equal(deadline) || !reflect.DeepEqual(first.payload, second.payload) {
			t.Fatal("retry needed another queue call, changed its payload, or ignored Retry-After")
		}
		if initial.retry || !started.retry || started.id != update.id || retry.id != update.id || retry.saveFolder != update.saveFolder || success.fingerprint != update.snapshot.Fingerprint {
			t.Fatal("retry lifecycle metadata did not identify the failed work")
		}
	})
}

func TestUploaderRetryUsesNewerSnapshotQueuedDuringRequest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, uploader, client := fixtureUploader(t)
		old := fixtureFolderUpdate(11, "old", "old save")
		replacement := fixtureFolderUpdate(22, "new", "new save")
		replacement.saveFolder = "replacement folder"

		firstErr := uploader.queue(ctx, old)
		_ = nextUploaderEvent[uploadStarted](t, ctx, uploader)
		first := nextRequest(t, ctx, client)
		replacementErr := uploader.queue(ctx, replacement)
		first.reply <- uploadReply{err: retryAfterError(6 * time.Second)}
		retry := nextUploaderEvent[uploadRetryScheduled](t, ctx, uploader)
		started := nextUploaderEvent[uploadStarted](t, ctx, uploader)
		second := nextRequest(t, ctx, client)
		second.reply <- uploadReply{result: successfulResult()}
		success := nextUploaderEvent[uploadSucceeded](t, ctx, uploader)

		if firstErr != nil || replacementErr != nil {
			t.Fatalf("queue errors: %v %v", firstErr, replacementErr)
		}
		if second.payload.Files["HERO_01_123.pld"].Text != "new save" || second.startedAt.Before(retry.retryAt) {
			t.Fatal("replacement lost to stale failed data or bypassed backoff")
		}
		if retry.id != old.id || retry.saveFolder != old.saveFolder || started.retry || started.id != replacement.id || success.id != replacement.id || success.saveFolder != replacement.saveFolder || success.fingerprint != replacement.snapshot.Fingerprint {
			t.Fatal("replacement inherited failed-work identity or a retry marker")
		}
	})
}

func TestUploaderRetryUsesNewerSnapshotQueuedDuringBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, uploader, client := fixtureUploader(t)
		replacement := fixtureFolderUpdate(22, "new", "new save")

		firstErr := uploader.queue(ctx, fixtureFolderUpdate(11, "old", "old save"))
		_ = nextUploaderEvent[uploadStarted](t, ctx, uploader)
		first := nextRequest(t, ctx, client)
		first.reply <- uploadReply{err: retryAfterError(6 * time.Second)}
		retry := nextUploaderEvent[uploadRetryScheduled](t, ctx, uploader)
		replacementErr := uploader.queue(ctx, replacement)
		started := nextUploaderEvent[uploadStarted](t, ctx, uploader)
		second := nextRequest(t, ctx, client)
		second.reply <- uploadReply{result: successfulResult()}
		success := nextUploaderEvent[uploadSucceeded](t, ctx, uploader)

		if firstErr != nil || replacementErr != nil {
			t.Fatalf("queue errors: %v %v", firstErr, replacementErr)
		}
		if second.payload.Files["HERO_01_123.pld"].Text != "new save" || second.startedAt.Before(retry.retryAt) || started.retry || success.fingerprint != replacement.snapshot.Fingerprint {
			t.Fatal("work accepted during backoff was lost, sent early, or marked as a repeat attempt")
		}
	})
}

func TestUploaderRejectsPermanentAndUnknownFailuresWithoutRetry(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{{"permanent", &leaderboard.UploadError{Message: "invalid save"}}, {"unknown", errors.New("unexpected failure")}} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, uploader, client := fixtureUploader(t)
				update := fixtureFolderUpdate(11, "rejected", "save")

				queueErr := uploader.queue(ctx, update)
				_ = nextUploaderEvent[uploadStarted](t, ctx, uploader)
				first := nextRequest(t, ctx, client)
				first.reply <- uploadReply{err: tc.err}
				rejected := nextUploaderEvent[uploadRejected](t, ctx, uploader)
				time.Sleep(20 * time.Second)
				synctest.Wait()
				closeTestUploader(uploader)

				if queueErr != nil {
					t.Fatal(queueErr)
				}
				if rejected.id != update.id || rejected.saveFolder != update.saveFolder || !errors.Is(rejected.err, tc.err) || len(client.requests) != 0 {
					t.Fatal("terminal failure lost its metadata or was retried automatically")
				}
			})
		})
	}
}

func TestUploaderReemittedRejectedSnapshotIsAccepted(t *testing.T) {
	ctx, uploader, client := fixtureUploader(t)
	update := fixtureFolderUpdate(11, "same", "save")

	firstErr := uploader.queue(ctx, update)
	_ = nextUploaderEvent[uploadStarted](t, ctx, uploader)
	first := nextRequest(t, ctx, client)
	first.reply <- uploadReply{err: &leaderboard.UploadError{Message: "invalid save"}}
	_ = nextUploaderEvent[uploadRejected](t, ctx, uploader)
	secondErr := uploader.queue(ctx, update)
	_ = nextUploaderEvent[uploadStarted](t, ctx, uploader)
	second := nextRequest(t, ctx, client)
	second.reply <- uploadReply{result: successfulResult()}
	success := nextUploaderEvent[uploadSucceeded](t, ctx, uploader)

	if firstErr != nil || secondErr != nil {
		t.Fatalf("queue errors: %v %v", firstErr, secondErr)
	}
	if !reflect.DeepEqual(first.payload, second.payload) || success.fingerprint != update.snapshot.Fingerprint {
		t.Fatal("accepted repeated work was deduplicated or changed")
	}
}

func TestUploaderSupersededFailureDoesNotRejectNewSnapshot(t *testing.T) {
	ctx, uploader, client := fixtureUploader(t)
	replacement := fixtureFolderUpdate(22, "new", "new save")

	firstErr := uploader.queue(ctx, fixtureFolderUpdate(11, "old", "old save"))
	_ = nextUploaderEvent[uploadStarted](t, ctx, uploader)
	first := nextRequest(t, ctx, client)
	replacementErr := uploader.queue(ctx, replacement)
	first.reply <- uploadReply{err: &leaderboard.UploadError{Message: "invalid old snapshot"}}
	started := nextUploaderEvent[uploadStarted](t, ctx, uploader)
	second := nextRequest(t, ctx, client)
	second.reply <- uploadReply{result: successfulResult()}
	success := nextUploaderEvent[uploadSucceeded](t, ctx, uploader)

	if firstErr != nil || replacementErr != nil {
		t.Fatalf("queue errors: %v %v", firstErr, replacementErr)
	}
	if started.id != replacement.id || second.payload.Files["HERO_01_123.pld"].Text != "new save" || success.fingerprint != replacement.snapshot.Fingerprint {
		t.Fatal("superseded failure rejected or prevented newer pending work")
	}
}

func TestUploaderCancelledQueueDoesNotSubmitWork(t *testing.T) {
	ctx, uploader, client := fixtureUploader(t)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()

	cancelledErr := uploader.queue(cancelled, fixtureFolderUpdate(11, "cancelled", "do not send"))
	acceptedErr := uploader.queue(ctx, fixtureFolderUpdate(22, "accepted", "send"))
	_ = nextUploaderEvent[uploadStarted](t, ctx, uploader)
	request := nextRequest(t, ctx, client)

	if !errors.Is(cancelledErr, context.Canceled) || acceptedErr != nil {
		t.Fatalf("queue errors: %v %v", cancelledErr, acceptedErr)
	}
	if request.payload.Files["HERO_01_123.pld"].Text != "send" {
		t.Fatal("cancelled work replaced accepted data")
	}
}

func TestUploaderStoppedQueueDoesNotSubmitWork(t *testing.T) {
	ctx, uploader, client := fixtureUploader(t)

	closeTestUploader(uploader)
	err := uploader.queue(ctx, fixtureFolderUpdate(11, "late", "do not send"))

	if !errors.Is(err, errUploaderStopped) || len(client.requests) != 0 {
		t.Fatalf("stopped worker accepted a new upload: %v", err)
	}
}

func TestUploaderStopInterruptsRetryWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, uploader, client := fixtureUploader(t)

		queueErr := uploader.queue(ctx, fixtureFolderUpdate(11, "retry", "save"))
		_ = nextUploaderEvent[uploadStarted](t, ctx, uploader)
		first := nextRequest(t, ctx, client)
		first.reply <- uploadReply{err: retryAfterError(time.Hour)}
		_ = nextUploaderEvent[uploadRetryScheduled](t, ctx, uploader)
		stopped := time.Now()
		closeTestUploader(uploader)

		if queueErr != nil {
			t.Fatal(queueErr)
		}
		if !time.Now().Equal(stopped) || len(client.requests) != 0 {
			t.Fatal("stop waited for backoff or started another request")
		}
	})
}

func TestUploaderShutdownPreservesUndeliveredSuccess(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started, release, returning := make(chan struct{}), make(chan struct{}), make(chan struct{})
		client := uploadFunc(func(ctx context.Context, _ leaderboard.UploadRequest) (leaderboard.Result, error) {
			close(started)
			select {
			case <-release:
				close(returning)
				return successfulResult(), nil
			case <-ctx.Done():
				return leaderboard.Result{}, ctx.Err()
			}
		})
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		uploader := newSnapshotUploader(ctx, client, false, log.New(io.Discard, "", 0))
		t.Cleanup(func() { closeTestUploader(uploader) })
		update := fixtureFolderUpdate(11, "delivered", "save")

		queueErr := uploader.queue(ctx, update)
		_ = nextUploaderEvent[uploadStarted](t, ctx, uploader)
		awaitSignal(t, ctx, started)
		close(release)
		awaitSignal(t, ctx, returning)
		synctest.Wait() // Leave successful completion undelivered until shutdown.
		uploader.stop()
		var successes []uploadSucceeded
		for event := range uploader.events {
			if success, ok := event.(uploadSucceeded); ok {
				successes = append(successes, success)
			}
		}
		uploader.wait()

		if queueErr != nil {
			t.Fatal(queueErr)
		}
		if len(successes) != 1 || successes[0].fingerprint != update.snapshot.Fingerprint || !reflect.DeepEqual(successes[0].result, successfulResult()) {
			t.Fatal("shutdown dropped or duplicated the confirmed success")
		}
	})
}

func TestUploaderStopWaitsForClientToFinish(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started, cleaning, release, cleaned := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
		var once sync.Once
		finish := func() { once.Do(func() { close(release) }) }
		client := uploadFunc(func(ctx context.Context, _ leaderboard.UploadRequest) (leaderboard.Result, error) {
			close(started)
			<-ctx.Done()
			close(cleaning)
			<-release
			close(cleaned)
			return leaderboard.Result{}, ctx.Err()
		})
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		uploader := newSnapshotUploader(ctx, client, false, log.New(io.Discard, "", 0))
		t.Cleanup(func() { finish(); closeTestUploader(uploader) })
		finished := make(chan struct{})
		drained := make(chan struct{})

		queueErr := uploader.queue(ctx, fixtureFolderUpdate(11, "active", "save"))
		_ = nextUploaderEvent[uploadStarted](t, ctx, uploader)
		awaitSignal(t, ctx, started)
		uploader.stop()
		<-cleaning
		go func() {
			for range uploader.events {
			}
			close(drained)
		}()
		go func() { uploader.wait(); close(finished) }()
		synctest.Wait()
		finishedEarly := isClosed(finished)
		finish()
		<-finished
		<-drained

		if queueErr != nil {
			t.Fatal(queueErr)
		}
		if finishedEarly || !isClosed(cleaned) {
			t.Fatal("wait returned before the network client completed cleanup")
		}
	})
}

func TestRetryDelayPolicy(t *testing.T) {
	transient := &leaderboard.UploadError{Message: "offline", Retry: true}
	for _, tc := range []struct {
		name     string
		err      error
		attempt  int
		retry    bool
		min, max time.Duration
	}{
		{"permanent", &leaderboard.UploadError{Message: "invalid save"}, 0, false, 0, 0},
		{"unknown", errors.New("unexpected failure"), 0, false, 0, 0},
		{"initial backoff", transient, 0, true, 2500 * time.Millisecond, 5 * time.Second},
		{"second backoff", transient, 1, true, 5 * time.Second, 10 * time.Second},
		{"third backoff", transient, 2, true, 10 * time.Second, 20 * time.Second},
		{"fourth backoff", transient, 3, true, 20 * time.Second, 40 * time.Second},
		{"fifth backoff", transient, 4, true, 40 * time.Second, 80 * time.Second},
		{"sixth backoff", transient, 5, true, 80 * time.Second, 160 * time.Second},
		{"cap reached", transient, 6, true, 150 * time.Second, 5 * time.Minute},
		{"cap retained", transient, 7, true, 150 * time.Second, 5 * time.Minute},
		{"large attempt count", transient, 100, true, 150 * time.Second, 5 * time.Minute},
		{"Retry-After wins", retryAfterError(time.Hour), 0, true, time.Hour, time.Hour},
		{"backoff wins", retryAfterError(time.Second), 1, true, 5 * time.Second, 10 * time.Second},
		{"wrapped Retry-After", fmt.Errorf("wrapped: %w", retryAfterError(time.Hour)), 0, true, time.Hour, time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			delay, retry := retryDelay(tc.err, tc.attempt)

			if retry != tc.retry || delay < tc.min || delay > tc.max {
				t.Fatalf("delay=%s retry=%t; want retry=%t delay in [%s, %s]", delay, retry, tc.retry, tc.min, tc.max)
			}
		})
	}
}

func fixtureFolderUpdate(id uint64, fingerprint, text string) folderUpdate {
	return folderUpdate{id: id, saveFolder: "saves", snapshot: saves.Snapshot{Files: map[string]saves.File{"HERO_01_123.pld": {Text: text, Mtime: 1700000000123}}, Fingerprint: fingerprint}}
}

func fixtureUploader(t *testing.T) (context.Context, *snapshotUploader, *fakeUploader) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	client := &fakeUploader{requests: make(chan uploadRequest, 8)}
	uploader := newSnapshotUploader(ctx, client, false, log.New(io.Discard, "", 0))
	t.Cleanup(func() { closeTestUploader(uploader); cancel() })
	return ctx, uploader, client
}

func closeTestUploader(uploader *snapshotUploader) {
	uploader.stop()
	for range uploader.events {
	}
	uploader.wait()
}

func nextUploaderEvent[T uploadEvent](t *testing.T, ctx context.Context, uploader *snapshotUploader) T {
	t.Helper()
	var zero T
	select {
	case event, open := <-uploader.events:
		if !open {
			t.Fatalf("uploader stopped before expected event %T", zero)
			return zero
		}
		matching, ok := event.(T)
		if !ok {
			t.Fatalf("event=%T; want %T", event, zero)
			return zero
		}
		return matching
	case <-ctx.Done():
		t.Fatalf("timed out waiting for uploader event %T", zero)
		return zero
	}
}

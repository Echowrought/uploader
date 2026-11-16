package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"cwgrpguploader/internal/leaderboard"
	"cwgrpguploader/internal/saves"
)

func TestUploaderAutomaticallyStartsLatestPendingSnapshot(t *testing.T) {
	ctx, uploader, application, client := fixtureUploader(t)

	firstErr := uploader.queue(ctx, fixtureFolderUpdate("first", "first save"))
	started := nextUploaderEvent[uploadStarted](t, ctx, uploader)
	first := nextRequest(t, ctx, client)
	middleErr := uploader.queue(ctx, fixtureFolderUpdate("middle", "middle save"))
	latestErr := uploader.queue(ctx, fixtureFolderUpdate("latest", "latest save"))
	first.reply <- uploadReply{result: successfulResult()}
	firstSuccess := nextUploaderEvent[uploadSucceeded](t, ctx, uploader)
	_ = nextUploaderEvent[uploadStarted](t, ctx, uploader)
	second := nextRequest(t, ctx, client)
	second.reply <- uploadReply{result: successfulResult()}
	secondSuccess := nextUploaderEvent[uploadSucceeded](t, ctx, uploader)

	if firstErr != nil || middleErr != nil || latestErr != nil {
		t.Fatalf("queue errors: %v, %v, %v", firstErr, middleErr, latestErr)
	}
	if started.saveFolder != "saves" || started.id != firstSuccess.id {
		t.Fatal("started event identified the wrong snapshot")
	}
	if firstSuccess.fingerprint != "first" || secondSuccess.fingerprint != "latest" || firstSuccess.saveFolder != "saves" || secondSuccess.saveFolder != "saves" {
		t.Fatal("completion events acknowledged the wrong snapshots")
	}
	if !reflect.DeepEqual(firstSuccess.result, successfulResult()) || firstSuccess.at.IsZero() || secondSuccess.at.IsZero() {
		t.Fatal("completion events lost their result or completion time")
	}
	if second.payload.Files["HERO_01_123.pld"].Text != "latest save" {
		t.Fatal("pending data was not coalesced")
	}
	if application.LastUpload.Fingerprint != "" {
		t.Fatal("uploader mutated coordinator state")
	}
	_, stateErr := os.Stat(filepath.Join(application.Dir, "state.json"))
	if !os.IsNotExist(stateErr) {
		t.Fatal("uploader performed persistence")
	}
}

func TestUploaderQueueDoesNotWaitForEventDelivery(t *testing.T) {
	ctx, uploader, _, client := fixtureUploader(t)

	firstErr := uploader.queue(ctx, fixtureFolderUpdate("first", "first save"))
	_ = nextUploaderEvent[uploadStarted](t, ctx, uploader)
	first := nextRequest(t, ctx, client)
	first.reply <- uploadReply{result: successfulResult()}
	// Leave success unread: the worker must deliver it before taking more work.
	middleErr := uploader.queue(ctx, fixtureFolderUpdate("middle", "middle save"))
	latestErr := uploader.queue(ctx, fixtureFolderUpdate("latest", "latest save"))
	firstSuccess := nextUploaderEvent[uploadSucceeded](t, ctx, uploader)
	_ = nextUploaderEvent[uploadStarted](t, ctx, uploader)
	second := nextRequest(t, ctx, client)
	second.reply <- uploadReply{result: successfulResult()}
	latestSuccess := nextUploaderEvent[uploadSucceeded](t, ctx, uploader)

	if firstErr != nil || middleErr != nil || latestErr != nil {
		t.Fatalf("queue errors: %v, %v, %v", firstErr, middleErr, latestErr)
	}
	if firstSuccess.fingerprint != "first" || latestSuccess.fingerprint != "latest" {
		t.Fatal("event backpressure lost or reordered snapshot completions")
	}
	if second.payload.Files["HERO_01_123.pld"].Text != "latest save" {
		t.Fatal("event backpressure prevented pending-snapshot coalescing")
	}
}

func TestUploaderQueueIsSafeForConcurrentCallers(t *testing.T) {
	ctx, uploader, _, client := fixtureUploader(t)
	errorsSeen := make(chan error, 32)
	var callers sync.WaitGroup

	initialErr := uploader.queue(ctx, fixtureFolderUpdate("active", "active save"))
	_ = nextUploaderEvent[uploadStarted](t, ctx, uploader)
	first := nextRequest(t, ctx, client)
	for i := 0; i < 32; i++ {
		callers.Add(1)
		go func() {
			defer callers.Done()
			errorsSeen <- uploader.queue(ctx, fixtureFolderUpdate("concurrent", "queued save"))
		}()
	}
	callers.Wait()
	close(errorsSeen)
	latestErr := uploader.queue(ctx, fixtureFolderUpdate("latest", "latest save"))
	first.reply <- uploadReply{result: successfulResult()}
	_ = nextUploaderEvent[uploadSucceeded](t, ctx, uploader)
	_ = nextUploaderEvent[uploadStarted](t, ctx, uploader)
	second := nextRequest(t, ctx, client)
	second.reply <- uploadReply{result: successfulResult()}
	latest := nextUploaderEvent[uploadSucceeded](t, ctx, uploader)

	if initialErr != nil || latestErr != nil {
		t.Fatalf("queue errors: %v, %v", initialErr, latestErr)
	}
	for err := range errorsSeen {
		if err != nil {
			t.Fatal(err)
		}
	}
	if latest.fingerprint != "latest" || second.payload.Files["HERO_01_123.pld"].Text != "latest save" {
		t.Fatal("concurrent callers corrupted pending work")
	}
}

func TestUploaderRetriesOnItsOwnTimer(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ctx, uploader, _, client := fixtureUploader(t)

		queueErr := uploader.queue(ctx, fixtureFolderUpdate("retry", "save"))
		_ = nextUploaderEvent[uploadStarted](t, ctx, uploader)
		first := nextRequest(t, ctx, client)
		first.reply <- uploadReply{err: &leaderboard.UploadError{Message: "retry later", Retry: true, After: 6 * time.Second}}
		retry := nextUploaderEvent[uploadRetryScheduled](t, ctx, uploader)
		_ = nextUploaderEvent[uploadStarted](t, ctx, uploader)
		second := nextRequest(t, ctx, client)
		second.reply <- uploadReply{result: successfulResult()}
		success := nextUploaderEvent[uploadSucceeded](t, ctx, uploader)

		if queueErr != nil {
			t.Fatal(queueErr)
		}
		if retry.saveFolder != "saves" || retry.id != success.id {
			t.Fatal("retry event did not identify its failed attempt")
		}
		if second.startedAt.Before(retry.retryAt) {
			t.Fatal("retry started before Retry-After elapsed")
		}
		if !reflect.DeepEqual(first.payload, second.payload) || success.fingerprint != "retry" {
			t.Fatal("retry required a new event or changed the data")
		}
	})
}

func TestUploaderRetryUsesNewerSnapshotQueuedDuringRequest(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ctx, uploader, _, client := fixtureUploader(t)

		old := fixtureFolderUpdate("old", "old save")
		old.id = 1
		replacement := fixtureFolderUpdate("new", "new save")
		replacement.id = 2

		firstErr := uploader.queue(ctx, old)
		_ = nextUploaderEvent[uploadStarted](t, ctx, uploader)
		first := nextRequest(t, ctx, client)
		newErr := uploader.queue(ctx, replacement)
		first.reply <- uploadReply{err: &leaderboard.UploadError{Message: "temporary failure", Retry: true}}
		retry := nextUploaderEvent[uploadRetryScheduled](t, ctx, uploader)
		_ = nextUploaderEvent[uploadStarted](t, ctx, uploader)
		second := nextRequest(t, ctx, client)
		second.reply <- uploadReply{result: successfulResult()}
		success := nextUploaderEvent[uploadSucceeded](t, ctx, uploader)

		if firstErr != nil || newErr != nil {
			t.Fatalf("queue errors: %v, %v", firstErr, newErr)
		}
		if second.payload.Files["HERO_01_123.pld"].Text != "new save" || success.fingerprint != "new" {
			t.Fatal("failure restored old data over a newer pending snapshot")
		}
		if retry.saveFolder != "saves" || retry.id != old.id || success.id != replacement.id {
			t.Fatal("retry event identified pending data instead of the failed attempt")
		}
		if second.startedAt.Before(retry.retryAt) {
			t.Fatal("new pending data bypassed retry backoff")
		}
	})
}

func TestUploaderReemittedRejectedSnapshotIsAccepted(t *testing.T) {
	ctx, uploader, _, client := fixtureUploader(t)
	update := fixtureFolderUpdate("same", "save")

	firstErr := uploader.queue(ctx, update)
	_ = nextUploaderEvent[uploadStarted](t, ctx, uploader)
	first := nextRequest(t, ctx, client)
	first.reply <- uploadReply{err: &leaderboard.UploadError{Message: "invalid save"}}
	rejected := nextUploaderEvent[uploadRejected](t, ctx, uploader)
	secondErr := uploader.queue(ctx, update)
	_ = nextUploaderEvent[uploadStarted](t, ctx, uploader)
	second := nextRequest(t, ctx, client)
	second.reply <- uploadReply{result: successfulResult()}
	success := nextUploaderEvent[uploadSucceeded](t, ctx, uploader)

	if firstErr != nil || secondErr != nil {
		t.Fatalf("queue errors: %v, %v", firstErr, secondErr)
	}
	if rejected.saveFolder != "saves" || rejected.id != update.id || rejected.err.Error() != "invalid save" || success.fingerprint != "same" {
		t.Fatal("uploader deduplicated a trusted event")
	}
}

func TestUploaderSupersededFailureDoesNotRejectNewSnapshot(t *testing.T) {
	ctx, uploader, _, client := fixtureUploader(t)

	old := fixtureFolderUpdate("old", "old save")
	old.id = 1
	replacement := fixtureFolderUpdate("new", "new save")
	replacement.id = 2

	firstErr := uploader.queue(ctx, old)
	_ = nextUploaderEvent[uploadStarted](t, ctx, uploader)
	first := nextRequest(t, ctx, client)
	secondErr := uploader.queue(ctx, replacement)
	first.reply <- uploadReply{err: &leaderboard.UploadError{Message: "invalid old snapshot"}}
	afterFailure, startedOK := nextLifecycleEvent(t, ctx, uploader).(uploadStarted)
	second := nextRequest(t, ctx, client)
	second.reply <- uploadReply{result: successfulResult()}
	success := nextUploaderEvent[uploadSucceeded](t, ctx, uploader)

	if firstErr != nil || secondErr != nil {
		t.Fatalf("queue errors: %v, %v", firstErr, secondErr)
	}
	if !startedOK || afterFailure.id != replacement.id {
		t.Fatal("old failure was rendered as a rejection of newer pending data")
	}
	if second.payload.Files["HERO_01_123.pld"].Text != "new save" || success.fingerprint != "new" {
		t.Fatal("new snapshot did not finish")
	}
}

func TestRetryWaitHonorsExactDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		events := make(chan uploadEvent, 1)
		finished := make(chan bool, 1)
		logger := log.New(io.Discard, "", 0)
		start := time.Now()
		const delay = 6 * time.Second

		go func() { finished <- waitForRetry(ctx, events, logger, uploadRetryScheduled{}, delay) }()
		retry := (<-events).(uploadRetryScheduled)
		time.Sleep(delay - time.Nanosecond)
		synctest.Wait()
		finishedEarly := len(finished) != 0
		time.Sleep(time.Nanosecond)
		completed := <-finished
		elapsed := time.Since(start)

		if finishedEarly || !completed || elapsed != delay {
			t.Fatalf("retry timing: early=%v, completed=%v, elapsed=%s", finishedEarly, completed, elapsed)
		}
		if !retry.retryAt.Equal(start.Add(delay)) {
			t.Fatalf("retry deadline=%s, want=%s", retry.retryAt, start.Add(delay))
		}
	})
}

func TestRetryDelay(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		attempt  int
		retry    bool
		min, max time.Duration
	}{
		{"permanent", &leaderboard.UploadError{Message: "invalid"}, 0, false, 0, 0},
		{"unknown", errors.New("unexpected failure"), 0, false, 0, 0},
		{"initial backoff", &leaderboard.UploadError{Retry: true}, 0, true, 2500 * time.Millisecond, 5 * time.Second},
		{"increased backoff", &leaderboard.UploadError{Retry: true}, 1, true, 5 * time.Second, 10 * time.Second},
		{"capped backoff", &leaderboard.UploadError{Retry: true}, 100, true, 150 * time.Second, 5 * time.Minute},
		{"retry after", &leaderboard.UploadError{Retry: true, After: time.Hour}, 0, true, time.Hour, time.Hour},
		{"wrapped failure", fmt.Errorf("wrapped: %w", &leaderboard.UploadError{Retry: true, After: time.Hour}), 0, true, time.Hour, time.Hour},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			delay, retry := retryDelay(tc.err, tc.attempt)

			if retry != tc.retry || delay < tc.min || delay > tc.max {
				t.Fatalf("retryDelay = (%s, %v); expected retry=%v, delay in [%s, %s]", delay, retry, tc.retry, tc.min, tc.max)
			}
		})
	}
}

func TestUploaderCancelledQueueDoesNotSubmitWork(t *testing.T) {
	ctx, uploader, _, client := fixtureUploader(t)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()

	cancelledErr := uploader.queue(cancelled, fixtureFolderUpdate("cancelled", "do not send"))
	acceptedErr := uploader.queue(ctx, fixtureFolderUpdate("accepted", "send"))
	_ = nextUploaderEvent[uploadStarted](t, ctx, uploader)
	request := nextRequest(t, ctx, client)

	if !errors.Is(cancelledErr, context.Canceled) || acceptedErr != nil {
		t.Fatalf("errors: %v, %v", cancelledErr, acceptedErr)
	}
	if request.payload.Files["HERO_01_123.pld"].Text != "send" {
		t.Fatal("cancelled queue request was submitted")
	}
}

func TestUploaderStopInterruptsRetryWait(t *testing.T) {
	ctx, uploader, _, client := fixtureUploader(t)

	queueErr := uploader.queue(ctx, fixtureFolderUpdate("retry", "save"))
	_ = nextUploaderEvent[uploadStarted](t, ctx, uploader)
	first := nextRequest(t, ctx, client)
	first.reply <- uploadReply{err: &leaderboard.UploadError{Message: "retry later", Retry: true, After: time.Hour}}
	_ = nextUploaderEvent[uploadRetryScheduled](t, ctx, uploader)
	uploader.stop()
	for range uploader.events {
	}
	uploader.wait()

	if queueErr != nil {
		t.Fatal(queueErr)
	}
	if len(client.requests) != 0 {
		t.Fatal("shutdown started another retry")
	}
}

func TestUploaderShutdownPreservesUndeliveredSuccess(t *testing.T) {
	application, _ := fixtureApp(t)
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	uploader := newSnapshotUploader(ctx, client, false, componentLogger(application.Logger, "uploader"))
	t.Cleanup(func() {
		uploader.stop()
		for range uploader.events {
		}
		uploader.wait()
	})

	queueErr := uploader.queue(ctx, fixtureFolderUpdate("delivered", "save"))
	_ = nextUploaderEvent[uploadStarted](t, ctx, uploader)
	awaitSignal(t, ctx, started)
	close(release)
	awaitSignal(t, ctx, returning)
	uploader.stop()
	var successes []uploadSucceeded
	for event := range uploader.events {
		if success, ok := event.(uploadSucceeded); ok {
			successes = append(successes, success)
		}
	}
	uploader.wait()
	stoppedErr := uploader.queue(ctx, fixtureFolderUpdate("late", "do not send"))

	if queueErr != nil {
		t.Fatal(queueErr)
	}
	if len(successes) != 1 || successes[0].fingerprint != "delivered" {
		t.Fatal("shutdown dropped or duplicated success")
	}
	if !errors.Is(stoppedErr, errUploaderStopped) {
		t.Fatalf("queue after shutdown: %v", stoppedErr)
	}
}

func TestUploaderStopWaitsForClientToFinish(t *testing.T) {
	application, _ := fixtureApp(t)
	started, cleaning, release, cleaned := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	finishCleanup := func() { releaseOnce.Do(func() { close(release) }) }
	client := uploadFunc(func(ctx context.Context, _ leaderboard.UploadRequest) (leaderboard.Result, error) {
		close(started)
		<-ctx.Done()
		close(cleaning)
		<-release
		close(cleaned)
		return leaderboard.Result{}, ctx.Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	uploader := newSnapshotUploader(ctx, client, false, componentLogger(application.Logger, "uploader"))
	t.Cleanup(func() {
		finishCleanup()
		uploader.stop()
		for range uploader.events {
		}
		uploader.wait()
	})

	queueErr := uploader.queue(ctx, fixtureFolderUpdate("active", "save"))
	_ = nextUploaderEvent[uploadStarted](t, ctx, uploader)
	awaitSignal(t, ctx, started)
	uploader.stop()
	awaitSignal(t, ctx, cleaning)
	finishedEarly := false
	select {
	case <-uploader.done:
		finishedEarly = true
	default:
	}
	finishCleanup()
	for range uploader.events {
	}
	uploader.wait()

	if queueErr != nil {
		t.Fatal(queueErr)
	}
	if finishedEarly {
		t.Fatal("uploader exited before the client finished")
	}
	select {
	case <-cleaned:
	default:
		t.Fatal("client cleanup was not joined")
	}
}

func fixtureFolderUpdate(fingerprint, text string) folderUpdate {
	return folderUpdate{saveFolder: "saves", snapshot: saves.Snapshot{Files: map[string]saves.File{"HERO_01_123.pld": {Text: text, Mtime: 1700000000123}}, Fingerprint: fingerprint}}
}

func nextLifecycleEvent(t *testing.T, ctx context.Context, uploader *snapshotUploader) uploadEvent {
	t.Helper()
	select {
	case event, open := <-uploader.events:
		if !open {
			t.Fatal("uploader stopped before expected event")
		}
		return event
	case <-ctx.Done():
		t.Fatal("timed out waiting for lifecycle event")
		return nil
	}
}

func fixtureUploader(t *testing.T) (context.Context, *snapshotUploader, *App, *fakeUploader) {
	t.Helper()
	application, client := fixtureApp(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	uploader := newSnapshotUploader(ctx, client, application.DryRun, componentLogger(application.Logger, "uploader"))
	t.Cleanup(func() {
		uploader.stop()
		for range uploader.events {
		}
		uploader.wait()
		cancel()
	})
	return ctx, uploader, application, client
}

func nextUploaderEvent[T uploadEvent](t *testing.T, ctx context.Context, uploader *snapshotUploader) T {
	t.Helper()
	var zero T
	for {
		select {
		case event, open := <-uploader.events:
			if !open {
				t.Fatalf("uploader stopped before expected event %T", zero)
				return zero
			}
			if matching, ok := event.(T); ok {
				return matching
			}
		case <-ctx.Done():
			t.Fatalf("timed out waiting for upload event %T", zero)
			return zero
		}
	}
}

type uploadFunc func(context.Context, leaderboard.UploadRequest) (leaderboard.Result, error)

func (f uploadFunc) Upload(ctx context.Context, request leaderboard.UploadRequest) (leaderboard.Result, error) {
	return f(ctx, request)
}

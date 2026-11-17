package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"cwgrpguploader/internal/leaderboard"
	"cwgrpguploader/internal/saves"
	"cwgrpguploader/internal/storage"
)

func TestAppPersistsSuccessfulSnapshot(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		snapshot := readSnapshot(t, application.Config.SaveFolder)
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		request := nextRequest(t, ctx, client)
		request.reply <- uploadReply{result: successfulResult()}
		status := awaitStatus(t, ctx, application, "successfully")
		cancel()
		<-done
		persisted, err := storage.LoadUploadState(application.Dir)

		if err != nil {
			t.Fatal(err)
		}
		if persisted.Fingerprint != snapshot.Fingerprint || persisted.Heroes != 1 || persisted.Player != "Player" || status.History != persisted || status.History.UploadedAt.IsZero() {
			t.Fatalf("incorrect persisted/published history: %+v, %+v", persisted, status.History)
		}
	})
}

func TestAppDoesNotPersistRejectedSnapshot(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		request := nextRequest(t, ctx, client)
		request.reply <- uploadReply{err: &leaderboard.UploadError{Message: "invalid save"}}
		synctest.Wait()
		cancel()
		<-done
		persisted, err := storage.LoadUploadState(application.Dir)

		if err != nil {
			t.Fatal(err)
		}
		if persisted != (storage.UploadState{}) {
			t.Fatalf("rejected upload recorded as successful history: %+v", persisted)
		}
	})
}

func TestAppSkipsSnapshotAlreadyUploadedBeforeRestart(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		application.LastUpload = storage.UploadState{SaveFolder: application.Config.SaveFolder, Destination: leaderboard.BaseURL, Fingerprint: readSnapshot(t, application.Config.SaveFolder).Fingerprint}
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		_ = awaitStatus(t, ctx, application, "No new changes")
		time.Sleep(5 * time.Second)
		synctest.Wait()
		cancel()
		<-done

		if len(client.requests) != 0 {
			t.Fatal("unchanged previously delivered snapshot uploaded again")
		}
	})
}

func TestAppExplicitUploadRetriesWithAutomaticUploadsOff(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		snapshot := readSnapshot(t, application.Config.SaveFolder)
		application.Config.Automatic = false
		application.LastUpload = storage.UploadState{SaveFolder: application.Config.SaveFolder, Destination: leaderboard.BaseURL, Fingerprint: snapshot.Fingerprint}
		ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		_ = awaitStatus(t, ctx, application, "Automatic uploads off")
		commandErr := sendCommand(t, ctx, application, Command{Kind: "upload"})
		first := nextRequest(t, ctx, client)
		first.reply <- uploadReply{err: retryAfterError(6 * time.Second)}
		second := nextRequest(t, ctx, client)
		second.reply <- uploadReply{result: successfulResult()}
		status := awaitStatus(t, ctx, application, "successfully")
		cancel()
		<-done

		if commandErr != nil {
			t.Fatal(commandErr)
		}
		if status.Config.Automatic || status.History.UploadedAt.IsZero() || status.History.Fingerprint != snapshot.Fingerprint || !reflect.DeepEqual(first.payload, second.payload) {
			t.Fatal("manual delivery/retry changed settings or failed to deliver identical data")
		}
	})
}

func TestAppLogsQuotedErrorResponseBody(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		var logs bytes.Buffer
		application.Logger = log.New(&logs, "", 0)
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		request := nextRequest(t, ctx, client)
		request.reply <- uploadReply{err: &leaderboard.UploadError{Message: "HTTP 408", Body: "timeout\ntry again\x1b"}}
		status := awaitStatus(t, ctx, application, "HTTP 408")
		cancel()
		<-done

		if !strings.Contains(logs.String(), `Upload error response body: "timeout\ntry again\x1b"`) || strings.Contains(status.Message, "try again") {
			t.Fatalf("error body not safely separated from status: %s", logs.String())
		}
	})
}

func TestAppDryRunDoesNotUploadOrPersistState(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		application.DryRun = true
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		status := awaitStatus(t, ctx, application, "Dry run: 2 eligible files; no upload or successful state written")
		cancel()
		<-done
		_, err := os.Stat(filepath.Join(application.Dir, "state.json"))

		if !os.IsNotExist(err) || len(client.requests) != 0 || !status.History.UploadedAt.IsZero() {
			t.Fatal("dry run uploaded or persisted successful state")
		}
	})
}

func TestAppCancelsInFlightUploadOnShutdown(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		_ = nextRequest(t, ctx, client)
		cancel()
		<-done
		persisted, err := storage.LoadUploadState(application.Dir)

		if err != nil {
			t.Fatal(err)
		}
		if persisted.Fingerprint != "" {
			t.Fatal("cancelled upload persisted as successful")
		}
	})
}

func TestAppDetectsChangesMadeDuringUpload(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		first := nextRequest(t, ctx, client)
		writeErr := os.WriteFile(filepath.Join(application.Config.SaveFolder, "HERO_01_123.pld"), []byte("new save contents"), 0600)
		first.reply <- uploadReply{result: successfulResult()}
		_ = awaitStatus(t, ctx, application, "successfully")
		second := nextRequest(t, ctx, client)
		second.reply <- uploadReply{result: successfulResult()}
		_ = awaitStatus(t, ctx, application, "successfully")
		cancel()
		<-done

		if writeErr != nil {
			t.Fatal(writeErr)
		}
		if first.payload.Files["HERO_01_123.pld"].Text != "save" || second.payload.Files["HERO_01_123.pld"].Text != "new save contents" {
			t.Fatal("changed save was not independently delivered")
		}
	})
}

func TestPollingFailureIsLoggedWhileUploadIsActive(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		var logs bytes.Buffer
		application.Logger = log.New(&logs, "", 0)
		snapshot := readSnapshot(t, application.Config.SaveFolder)
		ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		request := nextRequest(t, ctx, client)
		removeErr := os.RemoveAll(application.Config.SaveFolder)
		active := awaitStatus(t, ctx, application, "read save folder")
		request.reply <- uploadReply{result: successfulResult()}
		status := awaitStatus(t, ctx, application, "successfully")
		cancel()
		<-done

		if removeErr != nil {
			t.Fatal(removeErr)
		}
		if !strings.Contains(logs.String(), "[poller] save scan failed:") || !strings.Contains(active.Upload, "Uploading saves") || !strings.Contains(status.Detection, "read save folder") || status.History.Fingerprint != snapshot.Fingerprint {
			t.Fatal("scan failure hid active delivery, its outcome, or diagnostics")
		}
	})
}

func TestAppShutdownPersistsSuccessfulDelivery(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		application, _ := fixtureApp(t)
		started, release, returning := make(chan struct{}), make(chan struct{}), make(chan struct{})
		application.Client = uploadFunc(func(ctx context.Context, _ leaderboard.UploadRequest) (leaderboard.Result, error) {
			close(started)
			select {
			case <-release:
				close(returning)
				return successfulResult(), nil
			case <-ctx.Done():
				return leaderboard.Result{}, ctx.Err()
			}
		})
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		awaitSignal(t, ctx, started)
		close(release)
		awaitSignal(t, ctx, returning)
		cancel()
		<-done
		persisted, err := storage.LoadUploadState(application.Dir)

		if err != nil {
			t.Fatal(err)
		}
		if persisted.Fingerprint == "" || persisted.Heroes != 1 {
			t.Fatal("shutdown dropped confirmed success")
		}
	})
}

func TestAppSendsFileTextAndUnixMillisecondTimestamps(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		hero := filepath.Join(application.Config.SaveFolder, "HERO_01_123.pld")
		account := filepath.Join(application.Config.SaveFolder, "ACCOUNT_player_123.pld")
		if err := os.WriteFile(hero, []byte("hero"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(account, []byte("account"), 0600); err != nil {
			t.Fatal(err)
		}
		heroAt, accountAt := time.UnixMilli(1700000000123), time.UnixMilli(1700000000456)
		if err := os.Chtimes(hero, heroAt, heroAt); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(account, accountAt, accountAt); err != nil {
			t.Fatal(err)
		}
		want := leaderboard.UploadRequest{Files: map[string]leaderboard.UploadFile{"HERO_01_123.pld": {Text: "hero", Mtime: 1700000000123}, "ACCOUNT_player_123.pld": {Text: "account", Mtime: 1700000000456}}}
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		request := nextRequest(t, ctx, client)
		cancel()
		<-done

		if !reflect.DeepEqual(request.payload, want) {
			t.Fatalf("request=%+v want=%+v", request.payload, want)
		}
	})
}

func TestAppStripsOnlyLeadingBOMAndPersistsOriginalFingerprint(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		text := "\ufeffhero\ufeffdata"
		if err := os.WriteFile(filepath.Join(application.Config.SaveFolder, "HERO_01_123.pld"), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		original := readSnapshot(t, application.Config.SaveFolder)
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		request := nextRequest(t, ctx, client)
		request.reply <- uploadReply{result: successfulResult()}
		status := awaitStatus(t, ctx, application, "successfully")
		cancel()
		<-done

		if request.payload.Files["HERO_01_123.pld"].Text != "hero\ufeffdata" || status.History.Fingerprint != original.Fingerprint {
			t.Fatal("protocol conversion changed BOM normalization or the original fingerprint")
		}
	})
}

type uploadReply struct {
	result leaderboard.Result
	err    error
}
type uploadRequest struct {
	payload   leaderboard.UploadRequest
	reply     chan uploadReply
	startedAt time.Time
}
type fakeUploader struct{ requests chan uploadRequest }

func (f *fakeUploader) Upload(ctx context.Context, payload leaderboard.UploadRequest) (leaderboard.Result, error) {
	request := uploadRequest{payload: payload, reply: make(chan uploadReply, 1), startedAt: time.Now()}
	select {
	case f.requests <- request:
	case <-ctx.Done():
		return leaderboard.Result{}, ctx.Err()
	}
	select {
	case reply := <-request.reply:
		return reply.result, reply.err
	case <-ctx.Done():
		return leaderboard.Result{}, ctx.Err()
	}
}
func fixtureApp(t *testing.T) (*App, *fakeUploader) {
	t.Helper()
	folder := fixtureSaveFolder(t)
	client := &fakeUploader{requests: make(chan uploadRequest, 8)}
	return &App{Dir: t.TempDir(), Config: storage.Config{SaveFolder: folder, PollSeconds: 1, Automatic: true}, Client: client, Logger: log.New(io.Discard, "", 0), Commands: make(chan Command, 8), Updates: make(chan Status, 1)}, client
}
func fixtureSaveFolder(t *testing.T) string {
	t.Helper()
	folder := t.TempDir()
	for _, name := range []string{"HERO_01_123.pld", "ACCOUNT_player_123.pld"} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte("save"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return folder
}

func readSnapshot(t *testing.T, folder string) saves.Snapshot {
	t.Helper()
	scanner := saves.NewScanner(folder, 0, saves.DefaultLimits())
	_, _ = scanner.Scan(context.Background(), time.Now())
	snapshot, err := scanner.Scan(context.Background(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
func startApp(ctx context.Context, application *App) <-chan struct{} {
	done := make(chan struct{})
	go func() { defer close(done); application.Run(ctx) }()
	return done
}
func nextRequest(t *testing.T, ctx context.Context, client *fakeUploader) uploadRequest {
	t.Helper()
	select {
	case request := <-client.requests:
		return request
	case <-ctx.Done():
		t.Fatal("timed out waiting for upload")
		return uploadRequest{}
	}
}
func awaitSignal(t *testing.T, ctx context.Context, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-ctx.Done():
		t.Fatal("timed out waiting for signal")
	}
}
func awaitStatus(t *testing.T, ctx context.Context, application *App, substring string) Status {
	t.Helper()
	for {
		select {
		case status := <-application.Updates:
			if strings.Contains(status.Message, substring) {
				return status
			}
		case <-ctx.Done():
			t.Fatalf("timed out waiting for status %q", substring)
			return Status{}
		}
	}
}
func successfulResult() leaderboard.Result {
	return leaderboard.Result{Player: "Player", Heroes: 1, UserHash: json.RawMessage(`"123"`)}
}

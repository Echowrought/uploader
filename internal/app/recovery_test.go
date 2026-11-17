package app

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"cwgrpguploader/internal/leaderboard"
	"cwgrpguploader/internal/storage"
)

func TestAppReportsConfirmedUploadEvenWhenHistoryCannotBeSaved(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		application.Dir = filepath.Join(t.TempDir(), "blocked")
		if err := os.WriteFile(application.Dir, nil, 0600); err != nil {
			t.Fatal(err)
		}
		var logs bytes.Buffer
		application.Logger = log.New(&logs, "", 0)
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		first := nextRequest(t, ctx, client)
		first.reply <- uploadReply{result: successfulResult()}
		status := awaitStatus(t, ctx, application, "Could not persist")
		cancel()
		<-done

		if status.History.Fingerprint == "" || status.History.UploadedAt.IsZero() || status.HistoryError == "" || !strings.Contains(status.Menu(time.Now()).History.Title, "history not saved") || !strings.Contains(logs.String(), "[app] save upload state failed:") {
			t.Fatal("confirmed remote delivery was conflated with a local history-write failure")
		}
	})
}

func TestPersistenceRetrySavesLatestSuccessWithAutomaticOff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		application.ConfigDir = t.TempDir()
		application.Dir = filepath.Join(t.TempDir(), "blocked")
		if err := os.WriteFile(application.Dir, nil, 0600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		first := nextRequest(t, ctx, client)
		first.reply <- uploadReply{result: successfulResult()}
		_ = awaitStatus(t, ctx, application, "Could not persist")
		pauseErr := sendCommand(t, ctx, application, Command{Kind: "pause"})
		requestErr := sendCommand(t, ctx, application, Command{Kind: "upload"})
		second := nextRequest(t, ctx, client)
		second.reply <- uploadReply{result: leaderboard.Result{Player: "Latest", Heroes: 2}}
		time.Sleep(time.Second)
		synctest.Wait()
		failed := awaitStatus(t, ctx, application, "Could not persist")
		removeErr := os.Remove(application.Dir)
		time.Sleep(2 * time.Second)
		synctest.Wait()
		recovered := awaitStatus(t, ctx, application, "Automatic uploads off")
		cancel()
		<-done
		persisted, loadErr := storage.LoadUploadState(application.Dir)

		if pauseErr != nil || requestErr != nil || removeErr != nil || loadErr != nil {
			t.Fatalf("errors: %v %v %v %v", pauseErr, requestErr, removeErr, loadErr)
		}
		if failed.History.Player != "Latest" || failed.HistoryError == "" || persisted != failed.History || persisted.Heroes != 2 || recovered.HistoryError != "" || recovered.Config.Automatic || len(client.requests) != 0 {
			t.Fatalf("recovery lost latest history or retried HTTP: failed=%+v recovered=%+v persisted=%+v", failed, recovered, persisted)
		}
	})
}

func TestPersistenceRetryRecoversEvenWhenScanningFails(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		application.Dir = filepath.Join(t.TempDir(), "blocked")
		if err := os.WriteFile(application.Dir, nil, 0600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		first := nextRequest(t, ctx, client)
		first.reply <- uploadReply{result: successfulResult()}
		failed := awaitStatus(t, ctx, application, "Could not persist")
		removeFolderErr := os.RemoveAll(application.Config.SaveFolder)
		removeErr := os.Remove(application.Dir)
		time.Sleep(2 * time.Second)
		synctest.Wait()
		status := awaitStatus(t, ctx, application, "read save folder")
		cancel()
		<-done
		persisted, loadErr := storage.LoadUploadState(application.Dir)

		if removeFolderErr != nil || removeErr != nil || loadErr != nil {
			t.Fatalf("errors: %v %v %v", removeFolderErr, removeErr, loadErr)
		}
		if status.HistoryError != "" || persisted != failed.History || len(client.requests) != 0 || !strings.Contains(status.Detection, "read save folder") {
			t.Fatal("scan failure hid diagnostics, prevented history recovery, or caused HTTP replay")
		}
	})
}

func TestPersistenceRetryLogsOnlyNewOrChangedFailures(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		application.ConfigDir = t.TempDir()
		application.Dir = filepath.Join(t.TempDir(), "blocked")
		if err := os.WriteFile(application.Dir, nil, 0600); err != nil {
			t.Fatal(err)
		}
		var logs bytes.Buffer
		application.Logger = log.New(&logs, "", 0)
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		first := nextRequest(t, ctx, client)
		first.reply <- uploadReply{result: successfulResult()}
		initial := awaitStatus(t, ctx, application, "Could not persist")
		pauseErr := sendCommand(t, ctx, application, Command{Kind: "pause"})
		time.Sleep(3 * time.Second)
		synctest.Wait()
		repeated := strings.Count(logs.String(), "save upload state failed:")
		removeErr := os.Remove(application.Dir)
		mkdirErr := os.MkdirAll(filepath.Join(application.Dir, "state.json"), 0700)
		time.Sleep(2 * time.Second)
		synctest.Wait()
		changed := awaitStatus(t, ctx, application, "Could not persist")
		cancel()
		<-done

		if pauseErr != nil || removeErr != nil || mkdirErr != nil {
			t.Fatalf("errors: %v %v %v", pauseErr, removeErr, mkdirErr)
		}
		if repeated != 1 || strings.Count(logs.String(), "save upload state failed:") < 2 || changed.HistoryError == "" || changed.HistoryError == initial.HistoryError {
			t.Fatalf("history error logging was not deduplicated by actual failure: %s", logs.String())
		}
	})
}

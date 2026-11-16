package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"cwgrpguploader/internal/storage"
)

func TestCommandFailureSurvivesUnreadStatusesAndLaterUploadActivity(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		blocked := filepath.Join(t.TempDir(), "not-a-directory")
		if err := os.WriteFile(blocked, nil, 0600); err != nil {
			t.Fatal(err)
		}
		application.ConfigDir = blocked
		result := make(chan error, 1)
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		first := nextRequest(t, ctx, client)
		writeErr := os.WriteFile(filepath.Join(application.Config.SaveFolder, "HERO_01_123.pld"), []byte("new pending hero"), 0600)
		time.Sleep(7 * time.Second)
		synctest.Wait()
		application.Commands <- Command{Kind: "pause", Result: result}
		synctest.Wait() // Leave the failure buffered; do not read status or result.
		first.reply <- uploadReply{result: successfulResult()}
		second := nextRequest(t, ctx, client)
		time.Sleep(time.Second)
		synctest.Wait()
		latest := awaitStatus(t, ctx, application, "Uploading saves")
		var commandErr error
		received := false
		select {
		case commandErr = <-result:
			received = true
		case <-ctx.Done():
		}
		second.reply <- uploadReply{result: successfulResult()}
		_ = awaitStatus(t, ctx, application, "successfully")
		cancel()
		<-done

		if writeErr != nil {
			t.Fatal(writeErr)
		}
		if !received || commandErr == nil || !strings.Contains(commandErr.Error(), "Save config") {
			t.Fatalf("command result lost after upload/status activity: received=%t error=%v", received, commandErr)
		}
		if strings.Contains(latest.Message, "Save config") {
			t.Fatal("test did not replace the transient status error")
		}
		if second.payload.Files["HERO_01_123.pld"].Text != "new pending hero" || !latest.Config.Automatic {
			t.Fatal("failed settings change interrupted delivery or changed live preferences")
		}
	})
}

func TestIdenticalCommandFailuresEachHaveTheirOwnResult(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		application, _ := fixtureApp(t)
		application.Config.Automatic = false
		blocked := filepath.Join(t.TempDir(), "not-a-directory")
		if err := os.WriteFile(blocked, nil, 0600); err != nil {
			t.Fatal(err)
		}
		application.ConfigDir = blocked
		first, second := make(chan error, 1), make(chan error, 1)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		application.Commands <- Command{Kind: "pause", Result: first}
		application.Commands <- Command{Kind: "pause", Result: second}
		synctest.Wait()
		cancel()
		<-done
		var firstErr, secondErr error
		select {
		case firstErr = <-first:
		default:
		}
		select {
		case secondErr = <-second:
		default:
		}

		if firstErr == nil || secondErr == nil || firstErr.Error() != secondErr.Error() {
			t.Fatalf("identical failures were dropped or deduplicated: first=%v second=%v", firstErr, secondErr)
		}
	})
}

func TestCommandResultReportsSuccessfulApplication(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		application, _ := fixtureApp(t)
		result := make(chan error, 1)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		application.Commands <- Command{Kind: "pause", Result: result}
		var err error
		received := false
		select {
		case err = <-result:
			received = true
		case <-ctx.Done():
		}
		status := awaitStatus(t, ctx, application, "Automatic uploads off")
		cancel()
		<-done

		if !received || err != nil || status.Config.Automatic {
			t.Fatalf("successful command was not acknowledged: error=%v automatic=%t", err, status.Config.Automatic)
		}
	})
}

func TestShutdownDoesNotWaitForCommandResultConsumer(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		application, _ := fixtureApp(t)
		application.Config.Automatic = false
		// Even a caller that supplies an unbuffered result must not prevent quit.
		result := make(chan error)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		application.Commands <- Command{Kind: "pause", Result: result}
		synctest.Wait()
		cancel()
		<-done

		config, _, err := storage.LoadConfig(application.Dir, func() (string, error) { return "unused", nil })
		if err != nil || !config.Automatic {
			t.Fatalf("command was not applied before abandoning its result: config=%+v error=%v", config, err)
		}
	})
}

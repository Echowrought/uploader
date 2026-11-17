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

func TestCommandPausePersistsPreference(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, _ := fixtureApp(t)
		application.Config.ProxyURL = "http://proxy.invalid:8080"
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		commandErr := sendCommand(t, ctx, application, Command{Kind: "pause"})
		status := awaitStatus(t, ctx, application, "Automatic uploads off")
		cancel()
		<-done
		config, _, loadErr := storage.LoadConfig(application.Dir, func() (string, error) { return "unused", nil })

		if commandErr != nil || loadErr != nil {
			t.Fatalf("command=%v load=%v", commandErr, loadErr)
		}
		if config.Automatic || status.Config.Automatic || config.ProxyURL != "http://proxy.invalid:8080" {
			t.Fatalf("incorrect persisted settings: %+v", config)
		}
	})
}

func TestCommandResumeStartsCheckingStableSaves(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		application.Config.Automatic = false
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		_ = awaitStatus(t, ctx, application, "Automatic uploads off")
		resumed := time.Now()
		err := sendCommand(t, ctx, application, Command{Kind: "pause"})
		request := nextRequest(t, ctx, client)
		cancel()
		<-done

		if err != nil {
			t.Fatal(err)
		}
		if !application.Config.Automatic || request.startedAt.Before(resumed.Add(5*time.Second)) {
			t.Fatal("resume skipped settling or did not resume detection")
		}
	})
}

func TestCommandFolderRejectsInvalidPathsWithoutChangingSelection(t *testing.T) {
	for _, path := range []string{"", "relative/saves", "C:saves"} {
		t.Run(path, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				application, client := fixtureApp(t)
				previous := application.Config
				if err := storage.SaveConfig(application.Dir, previous); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
				defer cancel()

				done := startApp(ctx, application)
				commandErr := sendCommand(t, ctx, application, Command{Kind: "folder", Value: path})
				request := nextRequest(t, ctx, client)
				request.reply <- uploadReply{result: successfulResult()}
				status := awaitStatus(t, ctx, application, "successfully")
				cancel()
				<-done
				persisted, _, loadErr := storage.LoadConfig(application.Dir, func() (string, error) { return "unused", nil })

				if commandErr == nil || loadErr != nil {
					t.Fatalf("command=%v load=%v", commandErr, loadErr)
				}
				if status.Config != previous || persisted != previous || status.History.SaveFolder != previous.SaveFolder || request.payload.Files["HERO_01_123.pld"].Text != "save" {
					t.Fatal("invalid folder changed settings or interrupted the original folder's upload")
				}
			})
		})
	}
}

func TestFailedFolderConfigWritePreservesPendingDelivery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		original := application.Config.SaveFolder
		blocked := filepath.Join(t.TempDir(), "blocked")
		if err := os.WriteFile(blocked, nil, 0600); err != nil {
			t.Fatal(err)
		}
		application.ConfigDir = blocked
		ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		first := nextRequest(t, ctx, client)
		deadline := time.Now().Add(15 * time.Second)
		first.reply <- uploadReply{err: retryAfterError(15 * time.Second)}
		_ = awaitStatus(t, ctx, application, "Retry at")
		writeErr := os.WriteFile(filepath.Join(original, "HERO_01_123.pld"), []byte("new pending hero"), 0600)
		pending := awaitStatus(t, ctx, application, "Pending upload")
		expected := readSnapshot(t, original)
		commandErr := sendCommand(t, ctx, application, Command{Kind: "folder", Value: t.TempDir()})
		second := nextRequest(t, ctx, client)
		second.reply <- uploadReply{result: successfulResult()}
		status := awaitStatus(t, ctx, application, "successfully")
		cancel()
		<-done
		persisted, loadErr := storage.LoadUploadState(application.Dir)

		if commandErr == nil || writeErr != nil || loadErr != nil {
			t.Fatalf("command=%v write=%v load=%v", commandErr, writeErr, loadErr)
		}
		if !strings.Contains(pending.Upload, "Retry at") || first.payload.Files["HERO_01_123.pld"].Text != "save" || second.payload.Files["HERO_01_123.pld"].Text != "new pending hero" {
			t.Fatal("newer pending saves did not survive the failed folder change during backoff")
		}
		if status.Config.SaveFolder != original || persisted.SaveFolder != original || persisted.Fingerprint != expected.Fingerprint || second.startedAt.Before(deadline) {
			t.Fatal("failed folder change lost accepted work, origin, or backoff")
		}
	})
}

func TestCommandUnknownLeavesPausedStatusAndConfigurationUnchanged(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		application.Config.Automatic = false
		original := application.Config
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		before := awaitStatus(t, ctx, application, "Automatic uploads off")
		err := sendCommand(t, ctx, application, Command{Kind: "unknown"})
		synctest.Wait()
		latest := before
		select {
		case latest = <-application.Updates:
		default:
		}
		cancel()
		<-done
		_, configErr := os.Stat(filepath.Join(application.Dir, "config.json"))

		if err != nil || len(client.requests) != 0 || application.Config != original || !os.IsNotExist(configErr) {
			t.Fatal("unknown command changed configuration or submitted work")
		}
		if latest.Config != original || latest.Detection != before.Detection || latest.Upload != before.Upload || latest.History != before.History {
			t.Fatal("unknown command changed the displayed settings, activity, or history")
		}
	})
}

func TestAppPublishesLatestStatusWithoutWaitingForReader(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, _ := fixtureApp(t)
		application.Config.Automatic = false
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		_ = awaitStatus(t, ctx, application, "Automatic uploads off")
		firstErr := sendCommand(t, ctx, application, Command{Kind: "pause"})
		secondErr := sendCommand(t, ctx, application, Command{Kind: "pause"})
		synctest.Wait()
		// Read exactly once, before another tick can mask a stale update. The
		// unread resumed state must have been replaced by the final pause.
		var latest Status
		select {
		case latest = <-application.Updates:
		default:
			t.Fatal("commands did not publish a status update")
		}
		cancel()
		<-done

		if firstErr != nil || secondErr != nil || latest.Config.Automatic || !strings.Contains(latest.Message, "Automatic uploads off") {
			t.Fatal("unread status updates blocked commands or retained stale settings")
		}
	})
}

func sendCommand(t *testing.T, ctx context.Context, application *App, command Command) error {
	t.Helper()
	result := make(chan error, 1)
	command.Result = result
	select {
	case application.Commands <- command:
	case <-ctx.Done():
		t.Fatal("timed out sending command")
	}
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		t.Fatal("timed out waiting for command result")
		return ctx.Err()
	}
}

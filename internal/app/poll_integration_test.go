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

func TestAppDoesNotReuseHistoryFromAnotherFolderOrDestination(t *testing.T) {
	for _, tc := range []struct{ name, folder, destination string }{
		{"other folder", "other", leaderboard.BaseURL},
		{"other destination", "", "https://other.invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				application, client := fixtureApp(t)
				snapshot := readSnapshot(t, application.Config.SaveFolder)
				application.LastUpload = storage.UploadState{SaveFolder: filepath.Join(application.Config.SaveFolder, tc.folder), Destination: tc.destination, Fingerprint: snapshot.Fingerprint}
				ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
				defer cancel()

				done := startApp(ctx, application)
				request := nextRequest(t, ctx, client)
				request.reply <- uploadReply{result: successfulResult()}
				status := awaitStatus(t, ctx, application, "successfully")
				cancel()
				<-done

				if request.payload.Files["HERO_01_123.pld"].Text != "save" || status.History.SaveFolder != application.Config.SaveFolder || status.History.Destination != leaderboard.BaseURL {
					t.Fatalf("unrelated history suppressed or misidentified delivery: %+v", status)
				}
			})
		})
	}
}

func TestAppDryRunPreviewsSavesEvenWithMatchingHistory(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		original := storage.UploadState{SaveFolder: application.Config.SaveFolder, Destination: leaderboard.BaseURL, Fingerprint: readSnapshot(t, application.Config.SaveFolder).Fingerprint}
		application.LastUpload, application.DryRun = original, true
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		status := awaitStatus(t, ctx, application, "Dry run: 2 eligible files")
		cancel()
		<-done
		_, stateErr := os.Stat(filepath.Join(application.Dir, "state.json"))

		if len(client.requests) != 0 || status.History != original || !os.IsNotExist(stateErr) {
			t.Fatal("dry run reused delivery history instead of previewing, uploaded, or wrote history")
		}
	})
}

func TestAppManualUploadWaitsForSettling(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		application.Config.Automatic = false
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		_ = awaitStatus(t, ctx, application, "Automatic uploads off")
		requested := time.Now()
		commandErr := sendCommand(t, ctx, application, Command{Kind: "upload"})
		request := nextRequest(t, ctx, client)
		cancel()
		<-done

		if commandErr != nil {
			t.Fatal(commandErr)
		}
		if request.startedAt.Before(requested.Add(5 * time.Second)) {
			t.Fatal("manual upload bypassed the save-settling period")
		}
	})
}

func TestAppManualUploadDoesNotEnableAutomaticDetection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		application.Config.Automatic = false
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		_ = awaitStatus(t, ctx, application, "Automatic uploads off")
		commandErr := sendCommand(t, ctx, application, Command{Kind: "upload"})
		first := nextRequest(t, ctx, client)
		first.reply <- uploadReply{result: successfulResult()}
		_ = awaitStatus(t, ctx, application, "successfully")
		writeErr := os.WriteFile(filepath.Join(application.Config.SaveFolder, "HERO_01_123.pld"), []byte("do not send automatically"), 0600)
		time.Sleep(7 * time.Second)
		synctest.Wait()
		status := awaitStatus(t, ctx, application, "Automatic uploads off")
		cancel()
		<-done

		if commandErr != nil || writeErr != nil {
			t.Fatalf("command=%v write=%v", commandErr, writeErr)
		}
		if len(client.requests) != 0 || status.Config.Automatic {
			t.Fatal("manual upload enabled ongoing automatic detection")
		}
	})
}

func TestAppCancelledBeforeStartDoesNotSubmit(t *testing.T) {
	application, client := fixtureApp(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	application.Run(ctx)
	_, err := os.Stat(filepath.Join(application.Dir, "state.json"))

	if len(client.requests) != 0 || !os.IsNotExist(err) {
		t.Fatal("cancelled app uploaded or persisted success")
	}
}

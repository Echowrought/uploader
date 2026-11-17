package app

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"cwgrpguploader/internal/leaderboard"
	"cwgrpguploader/internal/storage"
)

func TestPendingNewFolderRemainsVisibleDuringOldFolderBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		replacement, _ := fixtureApp(t)
		if err := os.WriteFile(filepath.Join(replacement.Config.SaveFolder, "HERO_01_123.pld"), []byte("replacement hero"), 0600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()

		done := startApp(ctx, application)
		first := nextRequest(t, ctx, client)
		deadline := time.Now().Add(20 * time.Second)
		first.reply <- uploadReply{err: retryAfterError(20 * time.Second)}
		_ = awaitStatus(t, ctx, application, "Retry at")
		err := sendCommand(t, ctx, application, Command{Kind: "folder", Value: replacement.Config.SaveFolder})
		time.Sleep(7 * time.Second)
		synctest.Wait()
		pending := awaitStatus(t, ctx, application, "Pending upload")
		early := len(client.requests)
		second := nextRequest(t, ctx, client)
		second.reply <- uploadReply{result: successfulResult()}
		status := awaitStatus(t, ctx, application, "successfully")
		cancel()
		<-done
		persisted, loadErr := storage.LoadUploadState(application.Dir)

		if err != nil || loadErr != nil {
			t.Fatalf("command=%v load=%v", err, loadErr)
		}
		menu := pending.Menu(time.Now())
		if !strings.Contains(menu.Upload.Title, "Pending upload") || !strings.Contains(menu.Upload.Title, "Retry at") || !strings.Contains(menu.Upload.Details, "previous folder") || early != 0 {
			t.Fatalf("pending/backoff presentation lost: %+v", menu)
		}
		assertUploadHistoryEqual(t, persisted, status.History)
		if second.startedAt.Before(deadline) || second.payload.Files["HERO_01_123.pld"].Text != "replacement hero" || persisted.SaveFolder != replacement.Config.SaveFolder {
			t.Fatal("new folder bypassed backoff, failed to replace work, or lost history origin")
		}
	})
}

func TestManualRequestRemainsPendingUntilIdenticalOlderUploadCompletes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		first := nextRequest(t, ctx, client)
		err := sendCommand(t, ctx, application, Command{Kind: "upload"})
		pending := awaitStatus(t, ctx, application, "Pending upload")
		first.reply <- uploadReply{result: successfulResult()}
		second := nextRequest(t, ctx, client)
		// The second call is held open: older success must not mark it completed.
		active := awaitStatus(t, ctx, application, "Last upload:")
		second.reply <- uploadReply{result: successfulResult()}
		time.Sleep(time.Second)
		synctest.Wait()
		final := awaitStatus(t, ctx, application, "successfully")
		cancel()
		<-done

		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(pending.Upload, "Pending upload") || !strings.Contains(pending.Upload, "Uploading saves") || !strings.Contains(active.Upload, "Uploading saves") || active.History.UploadedAt.IsZero() || final.Upload != "" || !reflect.DeepEqual(first.payload, second.payload) {
			t.Fatal("older completion cleared newer identical accepted work or lost history")
		}
	})
}

func TestSupersededRejectionDoesNotDescribeNewPendingSaves(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		first := nextRequest(t, ctx, client)
		writeErr := os.WriteFile(filepath.Join(application.Config.SaveFolder, "HERO_01_123.pld"), []byte("new save"), 0600)
		time.Sleep(7 * time.Second)
		synctest.Wait()
		first.reply <- uploadReply{err: &leaderboard.UploadError{Message: "invalid old emission"}}
		second := nextRequest(t, ctx, client)
		time.Sleep(time.Second)
		synctest.Wait()
		active := awaitStatus(t, ctx, application, "Uploading saves")
		second.reply <- uploadReply{result: successfulResult()}
		final := awaitStatus(t, ctx, application, "successfully")
		cancel()
		<-done

		if writeErr != nil {
			t.Fatal(writeErr)
		}
		if strings.Contains(active.Message, "invalid old emission") || second.payload.Files["HERO_01_123.pld"].Text != "new save" || final.Upload != "" {
			t.Fatal("superseded rejection hid or rejected newer accepted work")
		}
	})
}

func TestFolderChangePreservesAcceptedOldFolderWorkAndHistory(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		old := application.Config.SaveFolder
		replacement := t.TempDir()
		ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		first := nextRequest(t, ctx, client)
		writeErr := os.WriteFile(filepath.Join(old, "HERO_01_123.pld"), []byte("pending old folder"), 0600)
		time.Sleep(7 * time.Second)
		synctest.Wait()
		pauseErr := sendCommand(t, ctx, application, Command{Kind: "pause"})
		folderErr := sendCommand(t, ctx, application, Command{Kind: "folder", Value: replacement})
		changed := awaitStatus(t, ctx, application, "previous folder")
		first.reply <- uploadReply{result: successfulResult()}
		second := nextRequest(t, ctx, client)
		time.Sleep(time.Second)
		synctest.Wait()
		active := awaitStatus(t, ctx, application, "Last upload:")
		second.reply <- uploadReply{result: successfulResult()}
		time.Sleep(time.Second)
		synctest.Wait()
		final := awaitStatus(t, ctx, application, "successfully")
		cancel()
		<-done
		persisted, loadErr := storage.LoadUploadState(application.Dir)
		config, _, configErr := storage.LoadConfig(application.Dir, func() (string, error) { return "unused", nil })

		if writeErr != nil || pauseErr != nil || folderErr != nil || loadErr != nil || configErr != nil {
			t.Fatalf("errors: %v %v %v %v %v", writeErr, pauseErr, folderErr, loadErr, configErr)
		}
		if !strings.Contains(changed.Upload, "previous folder") || !strings.Contains(active.Upload, "Uploading saves (previous folder)") || active.History.SaveFolder != old || final.Upload != "" || final.Detection != "Automatic uploads off" {
			t.Fatal("folder selection hid accepted work or old success replaced current presentation")
		}
		if second.payload.Files["HERO_01_123.pld"].Text != "pending old folder" || persisted.SaveFolder != old || persisted.Fingerprint != readSnapshot(t, old).Fingerprint || config.SaveFolder != replacement {
			t.Fatal("accepted old-folder work changed origin or was interrupted")
		}
	})
}

func TestOldFolderRejectionDoesNotReplaceCurrentDetection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		first := nextRequest(t, ctx, client)
		err := sendCommand(t, ctx, application, Command{Kind: "folder", Value: t.TempDir()})
		changed := awaitStatus(t, ctx, application, "previous folder")
		first.reply <- uploadReply{err: &leaderboard.UploadError{Message: "old save rejected"}}
		time.Sleep(time.Second)
		synctest.Wait()
		status := awaitStatus(t, ctx, application, "waiting for HERO_")
		cancel()
		<-done

		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(changed.Upload, "previous folder") || status.Upload != "" || strings.Contains(status.Message, "old save rejected") || status.History.Fingerprint != "" {
			t.Fatal("old-folder rejection leaked into current presentation or remained active")
		}
	})
}

func TestFolderChangeClearsExistingRejection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		replacement, _ := fixtureApp(t)
		ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		first := nextRequest(t, ctx, client)
		first.reply <- uploadReply{err: &leaderboard.UploadError{Message: "old rejection"}}
		_ = awaitStatus(t, ctx, application, "old rejection")
		folderErr := sendCommand(t, ctx, application, Command{Kind: "folder", Value: replacement.Config.SaveFolder})
		settling := awaitStatus(t, ctx, application, "waiting for saves to settle")
		cancel()
		<-done

		if folderErr != nil {
			t.Fatal(folderErr)
		}
		if settling.Config.SaveFolder != replacement.Config.SaveFolder || strings.Contains(settling.Message, "old rejection") || settling.Upload != "" {
			t.Fatal("folder selection retained the previous folder's rejection")
		}
	})
}

func TestSuccessHistoryDoesNotHidePause(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		request := nextRequest(t, ctx, client)
		request.reply <- uploadReply{result: successfulResult()}
		completed := awaitStatus(t, ctx, application, "successfully")
		pauseErr := sendCommand(t, ctx, application, Command{Kind: "pause"})
		paused := awaitStatus(t, ctx, application, "Automatic uploads off")
		cancel()
		<-done

		if pauseErr != nil {
			t.Fatal(pauseErr)
		}
		if paused.Detection != "Automatic uploads off" || paused.History.UploadedAt.IsZero() || paused.History != completed.History || !strings.Contains(paused.Message, "Last upload:") {
			t.Fatal("paused detection and confirmed history were not both retained")
		}
	})
}

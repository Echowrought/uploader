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

func TestLinuxAppDoesNotDeduplicateDifferentCaseSaveFolders(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		snapshot := readSnapshot(t, application.Config.SaveFolder)
		root := t.TempDir()
		previous, selected := filepath.Join(root, "Saves"), filepath.Join(root, "saves")
		if err := os.Mkdir(selected, 0700); err != nil {
			t.Fatal(err)
		}
		for name, file := range snapshot.Files {
			if err := os.WriteFile(filepath.Join(selected, name), []byte(file.Text), 0600); err != nil {
				t.Fatal(err)
			}
		}
		application.Config.SaveFolder = selected
		application.LastUpload = storage.UploadState{SaveFolder: previous, Destination: leaderboard.BaseURL, Fingerprint: snapshot.Fingerprint}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		request := nextRequest(t, ctx, client)
		request.reply <- uploadReply{result: successfulResult()}
		_ = awaitStatus(t, ctx, application, "successfully")
		cancel()
		<-done
		history, err := storage.LoadUploadState(application.Dir)

		if err != nil || history.SaveFolder != selected || history.Fingerprint != snapshot.Fingerprint {
			t.Fatalf("history=%+v error=%v", history, err)
		}
	})
}

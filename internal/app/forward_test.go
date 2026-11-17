package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"
)

func TestAcceptedOldFolderDeliveryDoesNotConsumeManualRequestForSettlingNewFolder(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		replacement, _ := fixtureApp(t)
		if err := os.WriteFile(filepath.Join(replacement.Config.SaveFolder, "HERO_01_123.pld"), []byte("fresh replacement save"), 0600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		first := nextRequest(t, ctx, client)
		pauseErr := sendCommand(t, ctx, application, Command{Kind: "pause"})
		folderErr := sendCommand(t, ctx, application, Command{Kind: "folder", Value: replacement.Config.SaveFolder})
		requestedAt := time.Now()
		uploadErr := sendCommand(t, ctx, application, Command{Kind: "upload"})
		first.reply <- uploadReply{result: successfulResult()}
		second := nextRequest(t, ctx, client)
		second.reply <- uploadReply{result: successfulResult()}
		time.Sleep(time.Second)
		synctest.Wait()
		status := awaitStatus(t, ctx, application, "successfully")
		cancel()
		<-done

		if pauseErr != nil || folderErr != nil || uploadErr != nil {
			t.Fatalf("commands: %v %v %v", pauseErr, folderErr, uploadErr)
		}
		if first.payload.Files["HERO_01_123.pld"].Text != "save" || second.payload.Files["HERO_01_123.pld"].Text != "fresh replacement save" || second.startedAt.Before(requestedAt.Add(5*time.Second)) || status.Config.Automatic || status.History.SaveFolder != replacement.Config.SaveFolder {
			t.Fatal("old delivery consumed the fresh manual request, bypassed settling, or changed its origin/preferences")
		}
	})
}

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
)

func TestAppReportsSupplementaryWarningsDuringRetryWithoutReemittingCore(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		var logs bytes.Buffer
		application.Logger = log.New(&logs, "", 0)
		path := filepath.Join(application.Config.SaveFolder, "MISC_bad.pld")
		ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		first := nextRequest(t, ctx, client)
		first.reply <- uploadReply{err: retryAfterError(time.Hour)}
		_ = awaitStatus(t, ctx, application, "Retry at")
		writeErr := os.WriteFile(path, []byte{0xff}, 0600)
		time.Sleep(7 * time.Second)
		synctest.Wait()
		invalid := awaitStatus(t, ctx, application, "Save warnings: 1")
		time.Sleep(3 * time.Second)
		synctest.Wait()
		removeErr := os.Remove(path)
		time.Sleep(2 * time.Second)
		synctest.Wait()
		recovered := awaitStatus(t, ctx, application, "Retry at")
		cancel()
		<-done

		if writeErr != nil || removeErr != nil {
			t.Fatalf("write=%v remove=%v", writeErr, removeErr)
		}
		menu := invalid.Menu(time.Now())
		if !strings.Contains(menu.Detection.Title, "Save warnings: 1") || !strings.Contains(menu.Upload.Title, "Retry at") || recovered.DetectionWarnings != 0 || len(client.requests) != 0 {
			t.Fatal("warning hid delivery, did not recover, or reemitted unchanged core saves")
		}
		if strings.Count(logs.String(), "snapshot accepted:") != 1 || strings.Count(logs.String(), "save warnings:") != 1 || !strings.Contains(logs.String(), "MISC_bad.pld") || !strings.Contains(logs.String(), "UTF-8") {
			t.Fatalf("warnings/unchanged core were not deduplicated: %s", logs.String())
		}
	})
}

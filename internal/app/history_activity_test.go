package app

import (
	"context"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"cwgrpguploader/internal/leaderboard"
	"cwgrpguploader/internal/storage"
)

func TestAppHistoryDoesNotHideInitialSettling(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		history := storage.UploadState{SaveFolder: application.Config.SaveFolder, Destination: leaderboard.BaseURL, Fingerprint: readSnapshot(t, application.Config.SaveFolder).Fingerprint, UploadedAt: time.Now().Add(-time.Hour), Heroes: 1, Player: "Player"}
		application.LastUpload = history
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		status := awaitStatus(t, ctx, application, "waiting for saves to settle")
		cancel()
		<-done

		if !strings.Contains(status.Detection, "waiting for saves to settle") || !strings.Contains(status.Message, "Last upload:") || status.History != history || len(client.requests) != 0 {
			t.Fatal("historical success hid settling or bypassed it")
		}
	})
}

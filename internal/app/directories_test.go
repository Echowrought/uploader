package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"cwgrpguploader/internal/storage"
)

func TestAppKeepsSettingsSeparateFromUploadHistory(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		application.ConfigDir = t.TempDir()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		request := nextRequest(t, ctx, client)
		request.reply <- uploadReply{result: successfulResult()}
		_ = awaitStatus(t, ctx, application, "successfully")
		application.Commands <- Command{Kind: "pause"}
		_ = awaitStatus(t, ctx, application, "Automatic uploads off")
		cancel()
		<-done
		config, _, configErr := storage.LoadConfig(application.ConfigDir, nil)
		history, historyErr := storage.LoadUploadState(application.Dir)
		_, misplacedConfig := os.Stat(filepath.Join(application.Dir, "config.json"))
		_, misplacedHistory := os.Stat(filepath.Join(application.ConfigDir, "state.json"))

		if configErr != nil || historyErr != nil || config.Automatic || history.Heroes != 1 {
			t.Fatalf("config=%+v history=%+v errors=%v, %v", config, history, configErr, historyErr)
		}
		if !os.IsNotExist(misplacedConfig) || !os.IsNotExist(misplacedHistory) {
			t.Fatalf("misplaced config=%v history=%v", misplacedConfig, misplacedHistory)
		}
	})
}

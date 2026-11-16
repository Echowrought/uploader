package app

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cwgrpguploader/internal/storage"
)

func TestPollerReportsSupplementaryOmissionsWithoutReemittingCore(t *testing.T) {
	application, _ := fixtureApp(t)
	var logs bytes.Buffer
	poller := newFolderPoller(application.Config.SaveFolder, "", log.New(&logs, "", 0))
	now := time.Now()
	path := filepath.Join(application.Config.SaveFolder, "MISC_bad.pld")

	poller.poll(context.Background(), now, true)
	first := poller.poll(context.Background(), now.Add(5*time.Second), true)
	writeErr := os.WriteFile(path, []byte{0xff}, 0600)
	initialWarning := poller.poll(context.Background(), now.Add(6*time.Second), true)
	invalid := poller.poll(context.Background(), now.Add(11*time.Second), true)
	repeated := poller.poll(context.Background(), now.Add(16*time.Second), true)
	removeErr := os.Remove(path)
	recovered := poller.poll(context.Background(), now.Add(17*time.Second), true)

	if writeErr != nil || removeErr != nil {
		t.Fatalf("errors: %v, %v", writeErr, removeErr)
	}
	if first.kind != pollUpdated || initialWarning.kind != pollStable || invalid.kind != pollStable || repeated.kind != pollStable || recovered.kind != pollStable {
		t.Fatal("supplementary failures prevented delivery or reemitted unchanged core data")
	}
	if len(invalid.warnings) != 1 || !strings.Contains(invalid.warnings[0], "MISC_bad.pld") || !strings.Contains(invalid.warnings[0], "UTF-8") || len(recovered.warnings) != 0 {
		t.Fatal("omission diagnostics were missing or did not recover")
	}
	if strings.Count(logs.String(), "save warnings:") != 1 || !strings.Contains(logs.String(), "MISC_bad.pld") {
		t.Fatalf("unchanged warnings were not deduplicated: %s", logs.String())
	}
}

func TestRenderSupplementaryWarningsAlongsideDelivery(t *testing.T) {
	deadline := time.Date(2026, 1, 1, 12, 34, 56, 0, time.UTC)
	view := statusView{
		folder:   pollEvent{kind: pollStable, warnings: []string{"skip supplementary save MISC_bad.pld: invalid UTF-8"}},
		delivery: deliveryView{active: uploadRetryScheduled{retryAt: deadline}},
	}

	message := renderStatus(view, storage.UploadState{})

	if message != "No new changes; Retry at 12:34:56; Save warnings: 1 (see logs)" {
		t.Fatal(message)
	}
}

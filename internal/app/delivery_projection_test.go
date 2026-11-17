package app

import (
	"errors"
	"strings"
	"testing"

	"cwgrpguploader/internal/storage"
)

func TestOlderCompletionDoesNotClearRepeatedSnapshotPendingUpload(t *testing.T) {
	first := fixtureFolderUpdate(11, "same", "same save")
	repeated := fixtureFolderUpdate(22, "same", "same save")
	view := statusView{saveFolder: first.saveFolder}

	view.delivery.accept(first)
	view.delivery.accept(repeated)
	view.delivery.observe(uploadStarted{id: first.id, saveFolder: first.saveFolder}, view.saveFolder)
	started := renderStatus(view, storage.UploadState{})
	view.delivery.observe(uploadSucceeded{id: first.id, saveFolder: first.saveFolder}, view.saveFolder)
	completed := renderStatus(view, storage.UploadState{})
	view.delivery.observe(uploadStarted{id: repeated.id, saveFolder: repeated.saveFolder}, view.saveFolder)
	acknowledged := renderStatus(view, storage.UploadState{})

	if !strings.Contains(started, "Pending upload") || !strings.Contains(started, "Uploading saves") {
		t.Fatalf("older start acknowledged newer pending work: %s", started)
	}
	if completed != "Pending upload" {
		t.Fatalf("older completion acknowledged newer pending work: %s", completed)
	}
	if acknowledged != "Uploading saves" {
		t.Fatalf("newer start did not acknowledge its own pending work: %s", acknowledged)
	}
}

func TestDelayedRejectionDoesNotRejectNewerPendingEmission(t *testing.T) {
	first := fixtureFolderUpdate(11, "first", "first save")
	newer := fixtureFolderUpdate(22, "newer", "newer save")
	view := statusView{saveFolder: first.saveFolder}
	// The event has already left the worker; replacing its queue cannot retract it.
	delayed := uploadRejected{id: first.id, saveFolder: first.saveFolder, err: errors.New("old rejection")}

	view.delivery.accept(first)
	view.delivery.observe(uploadStarted{id: first.id, saveFolder: first.saveFolder}, view.saveFolder)
	view.delivery.accept(newer)
	view.delivery.observe(delayed, view.saveFolder)
	message := renderStatus(view, storage.UploadState{})

	if message != "Pending upload" {
		t.Fatalf("delayed rejection replaced newer pending delivery: %s", message)
	}
}

func TestDelayedPreviewDoesNotClearNewerPendingEmission(t *testing.T) {
	first := fixtureFolderUpdate(11, "first", "first save")
	newer := fixtureFolderUpdate(22, "newer", "newer save")
	view := statusView{saveFolder: first.saveFolder}
	delayed := uploadPreviewed{id: first.id, saveFolder: first.saveFolder, fileCount: 1}

	view.delivery.accept(first)
	view.delivery.accept(newer)
	view.delivery.observe(delayed, view.saveFolder)
	delayedMessage := renderStatus(view, storage.UploadState{})
	view.delivery.observe(uploadPreviewed{id: newer.id, saveFolder: newer.saveFolder, fileCount: 2}, view.saveFolder)
	currentMessage := renderStatus(view, storage.UploadState{})

	if delayedMessage != "Pending upload" {
		t.Fatalf("delayed preview replaced newer pending delivery: %s", delayedMessage)
	}
	if currentMessage != "Dry run: 2 eligible files; no upload or successful state written" {
		t.Fatalf("current preview did not acknowledge its own delivery: %s", currentMessage)
	}
}

func TestOldFolderTerminalFeedbackDoesNotReplaceCurrentPresentation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		event uploadEvent
	}{
		{"rejection", uploadRejected{id: 11, saveFolder: "old", err: errors.New("old rejection")}},
		{"preview", uploadPreviewed{id: 11, saveFolder: "old", fileCount: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := statusView{saveFolder: "new"}

			view.delivery.observe(uploadStarted{id: 22, saveFolder: "new"}, view.saveFolder)
			view.delivery.observe(tc.event, view.saveFolder)
			message := renderStatus(view, storage.UploadState{})

			if message != "Uploading saves" {
				t.Fatalf("old-folder terminal feedback replaced current activity: %s", message)
			}
		})
	}
}

package app

import "cwgrpguploader/internal/storage"

// deliveryView projects accepted work and worker transitions, not scheduling.
// Pending acceptance is recorded synchronously: worker events may be delayed by
// an active request or backoff. IDs distinguish repeated emissions of identical
// snapshots so an older completion cannot clear a newer pending upload.
type deliveryView struct {
	pending  *deliveryIdentity
	active   uploadEvent
	rejected *uploadRejected
	preview  *uploadPreviewed
}

type deliveryIdentity struct {
	id         uint64
	saveFolder string
}

func (v *deliveryView) accept(update folderUpdate) {
	v.pending = &deliveryIdentity{id: update.id, saveFolder: update.saveFolder}
	v.rejected = nil
	v.preview = nil
}

func (v *deliveryView) observe(event uploadEvent, saveFolder string) {
	// Any lifecycle event acknowledges that the worker took this emission.
	if v.pending != nil && v.pending.id == event.workID() {
		v.pending = nil
	}
	switch event.(type) {
	case uploadStarted, uploadRetryScheduled:
		v.active = event
	case uploadSucceeded, uploadRejected, uploadPreviewed:
		if v.active != nil && v.active.workID() == event.workID() {
			v.active = nil
		}
	}
	// Activity is global because accepted old-folder work still occupies the
	// worker. Terminal feedback belongs only to the selected folder and must
	// not describe a newer pending emission as rejected or previewed.
	if !storage.SameFolder(event.sourceFolder(), saveFolder) || (v.pending != nil && v.pending.id > event.workID()) {
		return
	}
	switch event := event.(type) {
	case uploadStarted:
		v.rejected = nil
		v.preview = nil
	case uploadRejected:
		v.rejected = &event
	case uploadPreviewed:
		v.rejected = nil
		v.preview = &event
	}
}

func (v *deliveryView) changeFolder() {
	v.rejected = nil
	v.preview = nil
}

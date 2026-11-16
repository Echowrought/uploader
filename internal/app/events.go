package app

import (
	"time"

	"cwgrpguploader/internal/leaderboard"
	"cwgrpguploader/internal/saves"
)

// Events describe observations and delivery transitions, never UI text.
type folderUpdate struct {
	id         uint64
	saveFolder string
	snapshot   saves.Snapshot
}

type pollEventKind uint8

const (
	pollUnknown pollEventKind = iota
	pollDisabled
	pollSettling
	pollStable
	pollUpdated
	pollFailed
	pollCancelled
)

type pollEvent struct {
	kind     pollEventKind
	update   *folderUpdate
	err      error
	warnings []string
}

// uploadEvent is implemented by value events containing only lifecycle metadata,
// never snapshots or file contents. A nil event represents no observation yet.
type uploadEvent interface {
	isUploadEvent()
	sourceFolder() string
	workID() uint64
}

type uploadStarted struct {
	id         uint64
	saveFolder string
}

// Identity describes the failed attempt, not necessarily the next one. Backoff
// still applies to delivery as a whole; newer snapshots can replace failed work.
type uploadRetryScheduled struct {
	id         uint64
	saveFolder string
	retryAt    time.Time
}

type uploadRejected struct {
	id         uint64
	saveFolder string
	err        error
}

type uploadSucceeded struct {
	id          uint64
	saveFolder  string
	fingerprint string
	result      leaderboard.Result
	at          time.Time
}

type uploadPreviewed struct {
	id         uint64
	saveFolder string
	fileCount  int
}

func (uploadStarted) isUploadEvent()        {}
func (uploadRetryScheduled) isUploadEvent() {}
func (uploadRejected) isUploadEvent()       {}
func (uploadSucceeded) isUploadEvent()      {}
func (uploadPreviewed) isUploadEvent()      {}

func (e uploadStarted) sourceFolder() string        { return e.saveFolder }
func (e uploadRetryScheduled) sourceFolder() string { return e.saveFolder }
func (e uploadRejected) sourceFolder() string       { return e.saveFolder }
func (e uploadSucceeded) sourceFolder() string      { return e.saveFolder }
func (e uploadPreviewed) sourceFolder() string      { return e.saveFolder }

func (e uploadStarted) workID() uint64        { return e.id }
func (e uploadRetryScheduled) workID() uint64 { return e.id }
func (e uploadRejected) workID() uint64       { return e.id }
func (e uploadSucceeded) workID() uint64      { return e.id }
func (e uploadPreviewed) workID() uint64      { return e.id }

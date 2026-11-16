package app

import (
	"time"

	"cwgrpguploader/internal/storage"
)

type Status struct {
	Message           string
	Detection         string
	DetectionSince    time.Time
	DetectionWarnings int
	Upload            string
	History           storage.UploadState
	HistoryError      string
	Config            storage.Config
}

type Command struct {
	Kind  string
	Value string
	// Optional one-shot result. Callers supply a fresh channel buffered to one
	// so a slow UI cannot block scanning or delivery. Nil means no reply needed.
	Result chan<- error
}

func (s *runState) refreshStatus() {
	s.publish(renderStatus(s.view, s.app.LastUpload))
}

func (s *runState) reportCommandError(err error) {
	s.logger.Printf("command failed: %v", err)
	s.view.commandError = err
	s.refreshStatus()
}

func (s *runState) publish(message string) {
	detection, upload := renderActivity(s.view)
	status := Status{
		Message: message, Detection: detection, Upload: upload,
		Config: s.app.Config, History: s.app.LastUpload,
		DetectionSince:    s.view.folder.unchangedSince,
		DetectionWarnings: len(s.view.folder.warnings),
	}
	if s.view.persistenceError != nil {
		status.HistoryError = s.view.persistenceError.Error()
	}
	select {
	case s.app.Updates <- status:
	default:
		// Replace an unread update rather than blocking the event loop.
		select {
		case <-s.app.Updates:
		default:
		}
		select {
		case s.app.Updates <- status:
		default:
		}
	}
}

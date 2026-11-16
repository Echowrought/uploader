package app

import (
	"fmt"
	"time"
)

// StatusLine keeps compact menu text separate from the untruncated click detail.
type StatusLine struct {
	Title   string
	Details string
}

type MenuStatus struct {
	Detection StatusLine
	Upload    StatusLine
	History   StatusLine
}

// Menu presents one published status using the viewer's local calendar. It does
// not own scheduling or delivery state; the coordinator's view remains authoritative.
func (status Status) Menu(now time.Time) MenuStatus {
	detection := status.Detection
	if detection == "" {
		detection = "Starting"
	}
	detectionDetails := detection
	if !status.DetectionSince.IsZero() {
		at := status.DetectionSince.In(now.Location())
		since := at.Format("15:04")
		year, month, day := now.Date()
		if at.Year() != year || at.Month() != month || at.Day() != day {
			since = at.Format("2006-01-02, 15:04")
		}
		detection = "No changes since " + since
		detectionDetails = "Current settled saves have been observed unchanged since " + at.Format("2006-01-02 15:04:05 -0700") +
			".\nThis observation is limited to the current monitoring session; it is not the time of the last upload or an in-game save."
	}
	if status.DetectionWarnings > 0 {
		warning := fmt.Sprintf("Save warnings: %d (see logs)", status.DetectionWarnings)
		detection += "; " + warning
		detectionDetails += "\n\n" + warning
	}
	upload := status.Upload
	if upload == "" {
		upload = "Idle"
	}
	folder := "\n\nSave folder: " + status.Config.SaveFolder
	menu := MenuStatus{
		Detection: StatusLine{Title: "Detection: " + detection, Details: detectionDetails + folder},
		Upload:    StatusLine{Title: "Upload: " + upload, Details: upload + folder},
		History:   StatusLine{Title: "Last success: Never", Details: "No successful upload has been confirmed yet."},
	}
	if !status.History.UploadedAt.IsZero() {
		at := status.History.UploadedAt.In(now.Location())
		date := at.Format("2006-01-02, 15:04")
		year, month, day := now.Date()
		if at.Year() == year && at.Month() == month && at.Day() == day {
			date = "Today, " + at.Format("15:04")
		}
		menu.History.Title = "Last success: " + date
		menu.History.Details = fmt.Sprintf("Last confirmed upload: %s\nPlayer: %s\nHeroes: %d\nSave folder: %s\nDestination: %s",
			at.Format("2006-01-02 15:04:05 -0700"), status.History.Player, status.History.Heroes,
			status.History.SaveFolder, status.History.Destination)
	}
	if status.HistoryError != "" {
		menu.History.Title += " (history not saved)"
		menu.History.Details += "\n\nCould not save upload history: " + status.HistoryError +
			"\nThe upload succeeded, but restarting before history is saved may submit the same saves again."
	}
	return menu
}

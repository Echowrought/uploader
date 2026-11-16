package app

import (
	"fmt"
	"strings"

	"cwgrpguploader/internal/storage"
)

type statusView struct {
	saveFolder       string
	folder           pollEvent
	delivery         deliveryView
	persistenceError error
	commandError     error
}

// renderStatus composes independent facts. Detection never claims delivery is
// complete, and historical success never replaces current activity or errors.
func renderStatus(view statusView, history storage.UploadState) string {
	var parts []string
	if view.commandError != nil {
		parts = append(parts, view.commandError.Error())
	}
	// Keep the dry-run completion prefix used by the console entry point.
	if preview := view.delivery.preview; preview != nil {
		parts = append(parts, fmt.Sprintf("Dry run: %d eligible files; no upload or successful state written", preview.fileCount))
	}
	if detection := renderDetection(view.folder); detection != "" {
		parts = append(parts, detection)
	}
	parts = append(parts, renderDelivery(view.delivery, view.saveFolder)...)
	if count := len(view.folder.warnings); count > 0 {
		parts = append(parts, fmt.Sprintf("Save warnings: %d (see logs)", count))
	}
	if !history.UploadedAt.IsZero() {
		parts = append(parts, fmt.Sprintf("Last upload: %d heroes successfully uploaded at %s", history.Heroes, history.UploadedAt.Format("2006-01-02 15:04:05")))
	}
	if view.persistenceError != nil {
		parts = append(parts, "Could not persist successful state: "+view.persistenceError.Error())
	}
	if len(parts) == 0 {
		return "Starting; automatic uploads send saves to the public CWG RPG leaderboard"
	}
	return strings.Join(parts, "; ")
}

// renderActivity projects the existing view into independent UI sections.
// The combined message remains available to console/profiling consumers.
func renderActivity(view statusView) (detection, upload string) {
	detection = renderDetection(view.folder)
	if view.folder.kind == pollUpdated {
		detection = "Saves ready"
	}
	parts := renderDelivery(view.delivery, view.saveFolder)
	if preview := view.delivery.preview; preview != nil {
		parts = append([]string{fmt.Sprintf("Dry run: %d eligible files; no upload or successful state written", preview.fileCount)}, parts...)
	}
	return detection, strings.Join(parts, "; ")
}

func renderDetection(event pollEvent) string {
	switch event.kind {
	case pollDisabled:
		return "Automatic uploads off"
	case pollSettling, pollFailed:
		return event.err.Error()
	case pollStable:
		return "No new changes"
	default:
		return ""
	}
}

func renderDelivery(view deliveryView, saveFolder string) []string {
	var parts []string
	if view.pending != nil {
		parts = append(parts, "Pending upload"+sourceSuffix(view.pending.saveFolder, saveFolder))
	}
	if view.active != nil {
		suffix := sourceSuffix(view.active.sourceFolder(), saveFolder)
		switch event := view.active.(type) {
		case uploadStarted:
			message := "Uploading saves"
			if event.retry {
				message = "Retrying saves"
			}
			parts = append(parts, message+suffix)
		case uploadRetryScheduled:
			parts = append(parts, "Retry at "+event.retryAt.Format("15:04:05")+suffix)
		}
	}
	if rejected := view.rejected; rejected != nil {
		parts = append(parts, rejected.err.Error()+"; -save ingame or choose Upload now")
	}
	return parts
}

func sourceSuffix(source, selected string) string {
	if storage.SameFolder(source, selected) {
		return ""
	}
	return " (previous folder)"
}

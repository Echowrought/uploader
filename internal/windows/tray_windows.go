package windows

import (
	"context"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf16"

	"cwgrpguploader/internal/app"
	"cwgrpguploader/internal/leaderboard"
	"cwgrpguploader/internal/storage"
	"github.com/getlantern/systray"
)

func RunTray(ctx context.Context, cancel context.CancelFunc, commands chan<- app.Command, updates <-chan app.Status, config storage.Config, dir string) error {
	job, err := newSubprocessJob()
	if err != nil {
		return err
	}
	defer job.Close()
	systray.Run(func() {
		systray.SetIcon(trayIcon)
		systray.SetTooltip("CWG RPG Uploader")
		statusItem := systray.AddMenuItem("Starting...", "Show full status")
		lastItem := systray.AddMenuItem("Last upload: never", "")
		lastItem.Disable()
		systray.AddSeparator()
		automaticUploads := systray.AddMenuItemCheckbox("Automatic uploads", "Send new saves automatically; queued uploads and retries continue when off", config.Automatic)
		upload := systray.AddMenuItem("Upload now", "Upload current saves even with Automatic uploads off")
		folder := systray.AddMenuItem("Choose save folder...", "")
		startupEnabled, startupErr := StartupEnabled()
		startup := systray.AddMenuItemCheckbox("Launch at Windows sign-in", "Register this executable; Windows startup policy may override it", startupEnabled)
		board := systray.AddMenuItem("Open leaderboard", "")
		logs := systray.AddMenuItem("Open logs", "")
		settings := systray.AddMenuItem("Open config.json (restart to reload)", "")
		systray.AddSeparator()
		quit := systray.AddMenuItem("Quit", "")
		go func() {
			latest := app.Status{Message: "Starting", Config: config}
			picked := make(chan folderResult, 1)
			picking := false
			send := func(command app.Command) {
				select {
				case commands <- command:
				case <-ctx.Done():
				}
			}
			showError := func(err error) {
				if err != nil {
					go Message("CWG RPG Uploader", err.Error())
				}
			}
			showError(startupErr)
			for {
				select {
				case <-ctx.Done():
					systray.Quit()
					return
				case <-quit.ClickedCh:
					cancel()
					systray.Quit()
					return
				case update := <-updates:
					latest = update
					statusItem.SetTitle(display(update.Message, 90))
					systray.SetTooltip(tooltip("CWG RPG: " + update.Message))
					if !update.LastUpload.IsZero() {
						lastItem.SetTitle("Last upload: " + update.LastUpload.Format("2006-01-02 15:04:05"))
					}
					if update.Config.Automatic {
						automaticUploads.Check()
					} else {
						automaticUploads.Uncheck()
					}
				case <-statusItem.ClickedCh:
					text := latest.Message + "\n\nSave folder: " + latest.Config.SaveFolder
					go Message("CWG RPG Uploader status", text)
				case <-automaticUploads.ClickedCh:
					send(app.Command{Kind: "pause"})
				case <-upload.ClickedCh:
					send(app.Command{Kind: "upload"})
				case <-startup.ClickedCh:
					// Query again so external registration changes are respected.
					enabled, err := StartupEnabled()
					if err != nil {
						showError(err)
						continue
					}
					if err := SetStartup(!enabled); err != nil {
						showError(err)
						continue
					}
					if enabled {
						startup.Uncheck()
					} else {
						startup.Check()
					}
				case <-folder.ClickedCh:
					if !picking {
						picking = true
						folder.Disable()
						go func() { path, err := PickFolder(job); picked <- folderResult{path, err} }()
					}
				case result := <-picked:
					picking = false
					folder.Enable()
					showError(result.err)
					if result.path != "" {
						send(app.Command{Kind: "folder", Value: result.path})
					}
				case <-board.ClickedCh:
					showError(Open(leaderboard.BaseURL + "/#leaderboard"))
				case <-logs.ClickedCh:
					showError(Open(dir))
				case <-settings.ClickedCh:
					showError(Open(filepath.Join(dir, "config.json")))
				}
			}
		}()
	}, cancel)
	return nil
}

type folderResult struct {
	path string
	err  error
}

func display(text string, limit int) string {
	text = cleanDisplayText(text)
	runes := []rune(text)
	if len(runes) > limit {
		return string(runes[:limit-1]) + "…"
	}
	return text
}

// Windows reserves 128 UTF-16 units for the tooltip, including its terminator.
func tooltip(text string) string {
	text = cleanDisplayText(text)
	const limit = 127
	units := 0
	for _, r := range text {
		units += utf16.RuneLen(r)
	}
	if units <= limit {
		return text
	}
	units = 0
	for end, r := range text {
		units += utf16.RuneLen(r)
		if units > limit-1 { // Reserve one unit for the ellipsis.
			return text[:end] + "…"
		}
	}
	return text
}

func cleanDisplayText(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, text)
}

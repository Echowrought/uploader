package windows

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"

	"cwgrpguploader/internal/app"
	"cwgrpguploader/internal/leaderboard"
	"cwgrpguploader/internal/storage"
	"github.com/getlantern/systray"
)

func RunTray(ctx context.Context, cancel context.CancelFunc, commands chan<- app.Command, updates <-chan app.Status, config storage.Config, dir string, account *leaderboard.Account, authenticator *leaderboard.Authenticator, windowsSignInLaunch bool) error {
	job, err := newSubprocessJob()
	if err != nil {
		return err
	}
	defer job.Close()
	startupFailure := make(chan error, 1)
	systray.Run(func() {
		systray.SetIcon(trayIcon)
		systray.SetTooltip("CWG RPG Uploader")
		detectionItem := systray.AddMenuItem("Detection: Starting", "Show detection details")
		uploadItem := systray.AddMenuItem("Upload: Idle", "Show upload details")
		historyItem := systray.AddMenuItem("Last success: Never", "Show confirmed upload history")
		systray.AddSeparator()
		automaticUploads := systray.AddMenuItemCheckbox("Automatic uploads", "Send new saves automatically; queued uploads and retries continue when off", config.Automatic)
		upload := systray.AddMenuItem("Upload now", "Upload current saves even with Automatic uploads off")
		systray.AddSeparator()
		accountItem := systray.AddMenuItem(display(account.Status(), 90), "Leaderboard account")
		accountItem.Disable()
		authenticate := systray.AddMenuItem("Authenticate...", "Connect or reconnect to the Leaderboard")
		cancelAuth := systray.AddMenuItem("Cancel authentication", "Stop the pending authentication")
		cancelAuth.Hide()
		disconnect := systray.AddMenuItem("Disconnect", "Remove the local upload credential")
		systray.AddSeparator()
		board := systray.AddMenuItem("Open leaderboard", "")
		settings := systray.AddMenuItem("Settings", "")
		folder := settings.AddSubMenuItem("Choose save folder...", "")
		startupRefreshErr := RefreshStartup()
		startupEnabled, startupErr := StartupEnabled()
		startup := settings.AddSubMenuItemCheckbox("Launch at Windows sign-in", "Register this executable; Windows startup policy may override it", startupEnabled)
		configuration := settings.AddSubMenuItem("Open config.json (restart to reload)", "")
		logs := settings.AddSubMenuItem("Open logs", "")
		quit := systray.AddMenuItem("Quit", "")
		go func() {
			latest := app.Status{Message: "Starting", Config: config}
			picked := make(chan folderResult, 1)
			picking := false
			authDone := make(chan error, 1)
			var stopAuth context.CancelFunc
			startAuth := func() {
				if stopAuth != nil || ctx.Err() != nil {
					return
				}
				authCtx, cancelAttempt := context.WithCancel(ctx)
				stopAuth = cancelAttempt
				authenticate.Disable()
				disconnect.Disable()
				cancelAuth.Show()
				accountItem.SetTitle("Authentication pending")
				go func() {
					authDone <- authenticator.Authenticate(authCtx, func(ctx context.Context, link string) error {
						return presentApproval(ctx, job, link)
					})
				}()
			}
			send := func(command app.Command) {
				result := make(chan error, 1)
				command.Result = result
				select {
				case commands <- command:
					go func() {
						select {
						case err := <-result:
							if err != nil {
								Message("CWG RPG Uploader", err.Error())
							}
						case <-ctx.Done():
						}
					}()
				case <-ctx.Done():
				}
			}
			showError := func(err error) {
				if err != nil {
					go Message("CWG RPG Uploader", err.Error())
				}
			}
			if !windowsSignInLaunch {
				showError(errors.Join(startupRefreshErr, startupErr))
			}
			var startupApproval <-chan bool
			startupAuth := account.UploadBlock() != ""
			if startupAuth {
				decision := make(chan bool, 1)
				startupApproval = decision
				authenticate.Disable()
				go func() { decision <- promptAuthentication() }()
			}
			badged := false
			refresh := func() {
				menu := latest.Menu(time.Now())
				problem := automaticUploadProblem(latest.Config.Automatic, account)
				blocked := problem != ""
				if blocked != badged {
					badged = blocked
					if blocked {
						systray.SetIcon(errorTrayIcon())
						if startupApproval == nil {
							notifyTray("CWG RPG Uploader", "Backups not uploading — "+problem)
						}
					} else {
						systray.SetIcon(trayIcon)
					}
				}
				detectionItem.SetTitle(display(menu.Detection.Title, 90))
				historyItem.SetTitle(display(menu.History.Title, 90))
				if blocked {
					uploadItem.SetTitle("Backups not uploading")
					systray.SetTooltip(tooltip("Backups not uploading — " + problem))
				} else {
					uploadItem.SetTitle(display(menu.Upload.Title, 90))
					systray.SetTooltip(tooltip("CWG RPG: " + menu.Detection.Title + "; " + menu.Upload.Title + "; " + menu.History.Title))
				}
			}
			refresh()
			failStartup := func(err error) {
				startupFailure <- err
				cancel()
				systray.Quit()
			}
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
					if stopAuth == nil {
						accountItem.SetTitle(display(account.Status(), 90))
					}
					refresh()
					if update.Config.Automatic {
						automaticUploads.Check()
					} else {
						automaticUploads.Uncheck()
					}
				case <-detectionItem.ClickedCh:
					go Message("CWG RPG Uploader — Detection", latest.Menu(time.Now()).Detection.Details)
				case <-uploadItem.ClickedCh:
					if problem := automaticUploadProblem(latest.Config.Automatic, account); problem != "" {
						go Message("CWG RPG Uploader — Upload", "Backups not uploading — "+problem)
					} else {
						go Message("CWG RPG Uploader — Upload", latest.Menu(time.Now()).Upload.Details)
					}
				case <-historyItem.ClickedCh:
					go Message("CWG RPG Uploader — Last success", latest.Menu(time.Now()).History.Details)
				case <-automaticUploads.ClickedCh:
					send(app.Command{Kind: "pause"})
				case <-upload.ClickedCh:
					send(app.Command{Kind: "upload"})
				case approved := <-startupApproval:
					startupApproval = nil
					authenticate.Enable()
					if !approved {
						failStartup(leaderboard.ErrAuthCancelled)
						return
					}
					startAuth()
				case <-authenticate.ClickedCh:
					if startupApproval == nil {
						startAuth()
					}
				case <-cancelAuth.ClickedCh:
					if stopAuth != nil {
						stopAuth()
					}
				case err := <-authDone:
					if startupAuth && err != nil {
						failStartup(err)
						return
					}
					startupAuth = false
					stopAuth()
					stopAuth = nil
					cancelAuth.Hide()
					authenticate.Enable()
					disconnect.Enable()
					accountItem.SetTitle(display(account.Status(), 90))
					refresh()
					if err == nil && latest.Config.Automatic {
						send(app.Command{Kind: "upload"})
					} else if !errors.Is(err, context.Canceled) && !errors.Is(err, leaderboard.ErrAuthCancelled) {
						showError(err)
					}
				case <-disconnect.ClickedCh:
					if stopAuth == nil && !account.MissingCredential() {
						if err := account.Disconnect(); err != nil {
							showError(err)
							continue
						}
						accountItem.SetTitle(display(account.Status(), 90))
						refresh()
					}
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
				case <-configuration.ClickedCh:
					showError(Open(filepath.Join(dir, "config.json")))
				}
			}
		}()
	}, cancel)
	select {
	case err := <-startupFailure:
		return err
	default:
		return nil
	}
}

func automaticUploadProblem(automatic bool, account *leaderboard.Account) string {
	if !automatic {
		return ""
	}
	return account.UploadBlock()
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

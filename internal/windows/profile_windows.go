package windows

import (
	"context"

	"cwg2uploader/internal/app"
	"github.com/getlantern/systray"
)

// RunProfileTray uses the normal icon/tooltip rendering without upload,
// configuration, folder-picker, browser, or sign-in registration actions.
// Only the separate profiling command references this tray.
func RunProfileTray(ctx context.Context, cancel context.CancelFunc, updates <-chan app.Status) {
	systray.Run(func() {
		systray.SetIcon(trayIcon())
		systray.SetTooltip("CWG2 profiling: uploads disabled")
		status := systray.AddMenuItem("Profiling: starting...", "Uploads are disabled")
		status.Disable()
		quit := systray.AddMenuItem("Stop profiling", "Exit early; artifacts may be incomplete")
		go func() {
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
					status.SetTitle(display("Profiling: "+update.Message, 90))
					systray.SetTooltip(tooltip("CWG2 profiling (no uploads): " + update.Message))
				}
			}
		}()
	}, cancel)
}

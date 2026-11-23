package main

import (
	"context"

	"cwgrpguploader/internal/app"
	"cwgrpguploader/internal/leaderboard"
	ui "cwgrpguploader/internal/windows"
)

func dataFolders() (string, string, error) {
	dir, err := ui.DataFolder()
	return dir, dir, err
}

func singleInstance(_ string) (func(), error) { return ui.SingleInstance() }
func showMessage(title, message string) error { return ui.Message(title, message) }

func authenticateTray(ctx context.Context, account *leaderboard.Account, authenticator *leaderboard.Authenticator) error {
	return ui.Authenticate(ctx, account, authenticator)
}

func runTray(ctx context.Context, cancel context.CancelFunc, application *app.App, dir string, account *leaderboard.Account, authenticator *leaderboard.Authenticator, windowsSignInLaunch bool) error {
	return ui.RunTray(ctx, cancel, application.Commands, application.Updates, application.Config, dir, account, authenticator, windowsSignInLaunch)
}

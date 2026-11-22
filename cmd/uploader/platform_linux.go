package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"cwgrpguploader/internal/app"
	"cwgrpguploader/internal/leaderboard"
	platform "cwgrpguploader/internal/linux"
)

func dataFolders() (string, string, error)      { return platform.DataFolders() }
func singleInstance(dir string) (func(), error) { return platform.SingleInstance(dir) }
func showMessage(title, message string) error {
	_, err := fmt.Fprintln(os.Stderr, title+": "+message)
	return err
}

func runTray(context.Context, context.CancelFunc, *app.App, string, *leaderboard.Account, *leaderboard.Authenticator) error {
	return errors.New("Linux supports console mode only")
}

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"cwgrpguploader/internal/app"
	"cwgrpguploader/internal/leaderboard"
	"cwgrpguploader/internal/storage"
)

func main() {
	autostart := flag.Bool("autostart", false, "launch the Windows tray without an authentication prompt")
	authenticate := flag.Bool("authenticate", false, "connect to the Leaderboard without scanning or uploading saves (console)")
	dryRun := flag.Bool("dry-run", false, "inspect one stable snapshot without uploading; use the console binary for terminal output")
	var saveFolder string
	flag.Func("save-folder", "absolute CWG save folder to use and remember (required for unconfigured console builds)", func(value string) error {
		if !filepath.IsAbs(value) {
			return fmt.Errorf("save-folder must be an absolute path")
		}
		saveFolder = value
		return nil
	})
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [--dry-run | --authenticate] [--save-folder PATH]\n", os.Args[0])
		if consoleBuild {
			fmt.Fprintln(flag.CommandLine.Output(), "Console build: normal runs upload according to configuration, without the tray.")
		} else {
			fmt.Fprintln(flag.CommandLine.Output(), "Tray build: normal runs use the Windows notification area.")
		}
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "Unexpected positional arguments; use --save-folder PATH")
		os.Exit(2)
	}
	if *authenticate && (!consoleBuild || *dryRun || saveFolder != "") {
		message := "Use --authenticate alone with the console executable."
		if consoleBuild {
			fmt.Fprintln(os.Stderr, message)
		} else {
			showMessage("CWG RPG Uploader", message)
		}
		os.Exit(2)
	}
	console := consoleBuild || *dryRun
	var err error
	if *authenticate {
		err = runAuthentication()
	} else {
		err = run(console, *dryRun, *autostart, saveFolder)
	}
	if err != nil {
		if console {
			fmt.Fprintln(os.Stderr, err)
		} else {
			showMessage("CWG RPG Uploader", err.Error())
		}
		os.Exit(1)
	}
}

func run(console, dryRun, autostart bool, saveFolder string) error {
	configDir, stateDir, err := dataFolders()
	if err != nil {
		return err
	}
	if console {
		if _, err := fmt.Fprintf(os.Stdout, "Configuration: %s\nLogs: %s\n", filepath.Join(configDir, "config.json"), filepath.Join(stateDir, "uploader.log")); err != nil {
			return err
		}
	}
	release, err := singleInstance(stateDir)
	if err != nil {
		return err
	}
	defer release()
	config, _, err := storage.LoadConfig(configDir, func() (string, error) {
		if saveFolder != "" {
			return saveFolder, nil
		}
		return defaultSaveFolder()
	})
	if err != nil {
		return err
	}
	if saveFolder != "" {
		config.SaveFolder = saveFolder
		if err := storage.SaveConfig(configDir, config); err != nil {
			return err
		}
	}
	httpClient, err := leaderboard.NewHTTPClient(config.ProxyURL)
	if err != nil {
		return err
	}
	defer httpClient.CloseIdleConnections()
	account, credentialErr := leaderboard.OpenAccount(configDir)
	client := leaderboard.NewClient(httpClient, account)
	writer, err := storage.OpenLog(stateDir)
	if err != nil {
		return err
	}
	defer writer.Close()
	var output io.Writer = writer
	if console {
		output = io.MultiWriter(writer, os.Stdout)
	}
	logger := log.New(output, "", log.Ldate|log.Ltime)
	if !dryRun {
		if credentialErr != nil {
			logger.Printf("Could not load uploader credential: %v", credentialErr)
		}
		if console {
			fmt.Fprintln(os.Stdout, account.Status())
			if account.MissingCredential() {
				fmt.Fprintln(os.Stdout, "To enable uploads, stop the uploader and run it with --authenticate.")
			}
		}
	}
	lastUpload, err := storage.LoadUploadState(stateDir)
	if err != nil {
		return fmt.Errorf("load state.json (fix or remove it before restarting): %w", err)
	}
	showNotice := func(message string) error {
		if console {
			_, err := fmt.Fprintln(os.Stdout, message)
			return err
		}
		return showMessage("CWG RPG Uploader", message)
	}
	if err := ensureUploadNotice(configDir, &config, dryRun, showNotice); err != nil {
		return err
	}
	if dryRun {
		// Inspect even if automatic uploads are paused; do not persist this override.
		config.Automatic = true
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	application := &app.App{
		Dir: stateDir, ConfigDir: configDir, Config: config, LastUpload: lastUpload, DryRun: dryRun,
		Client: client,
		Logger: logger, Commands: make(chan app.Command, 16), Updates: make(chan app.Status, 1),
	}
	done := make(chan struct{})
	go func() { defer close(done); application.Run(ctx) }()
	if console {
		return runConsole(ctx, cancel, application, done, dryRun)
	}
	err = runTray(ctx, cancel, application, configDir, account, leaderboard.NewAuthenticator(client), autostart)
	cancel()
	<-done
	return err
}

func runAuthentication() error {
	configDir, stateDir, err := dataFolders()
	if err != nil {
		return err
	}
	release, err := singleInstance(stateDir)
	if err != nil {
		return err
	}
	defer release()
	proxyURL, err := storage.LoadProxyURL(configDir)
	if err != nil {
		return err
	}
	httpClient, err := leaderboard.NewHTTPClient(proxyURL)
	if err != nil {
		return err
	}
	defer httpClient.CloseIdleConnections()
	account, _ := leaderboard.OpenAccount(configDir)
	authenticator := leaderboard.NewAuthenticator(leaderboard.NewClient(httpClient, account))
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := authenticator.Authenticate(ctx, func(_ context.Context, link string) error {
		_, err := fmt.Fprintln(os.Stdout, "Open this link to approve upload access:\n"+link)
		return err
	}); err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, account.Status())
	return err
}

func runConsole(ctx context.Context, cancel context.CancelFunc, application *app.App, done <-chan struct{}, dryRun bool) error {
	for {
		select {
		case <-ctx.Done():
			<-done
			return nil
		case status := <-application.Updates:
			if dryRun && strings.HasPrefix(status.Message, "Dry run:") {
				fmt.Fprintln(os.Stdout, status.Message)
				cancel()
				<-done
				return nil
			}
		}
	}
}

func ensureUploadNotice(dir string, config *storage.Config, dryRun bool, show func(string) error) error {
	if config.UploadNoticeShown {
		return nil
	}
	message := "This app sends CWG RPG saves to a third-party public leaderboard. Existing saves upload after you connect a Leaderboard account if Automatic uploads is enabled. It never modifies game saves.\n\nYou can change Automatic uploads and the save folder in config.json, or through the Windows tray. Already queued uploads and retries continue when Automatic uploads is off."
	message += fmt.Sprintf("\n\nAutomatic uploads configured: %t\nConfiguration directory: %s", config.Automatic, dir)
	if dryRun {
		message = "Dry run: no saves will be uploaded. This notice will appear again before the first normal run.\n\n" + message
	}
	if err := show(message); err != nil {
		return fmt.Errorf("show upload notice: %w", err)
	}
	if dryRun {
		return nil
	}
	next := *config
	next.UploadNoticeShown = true
	if err := storage.SaveConfig(dir, next); err != nil {
		return fmt.Errorf("save upload notice acknowledgement: %w", err)
	}
	*config = next
	return nil
}

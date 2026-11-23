package main

import (
	"context"
	"errors"
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
	windowsSignInLaunch := flag.Bool("windows-sign-in-launch", false, "Internal: marks this invocation as a Windows sign-in launch.")
	nonInteractive := flag.Bool("non-interactive", false, "use plain console output and do not start authentication")
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
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [--dry-run | --authenticate] [--non-interactive] [--save-folder PATH]\n", os.Args[0])
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
	if *nonInteractive && (*authenticate || (!consoleBuild && !*dryRun)) {
		fmt.Fprintln(os.Stderr, "Use --non-interactive with the console executable or --dry-run, not with --authenticate.")
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
		err = run(console, *dryRun, *windowsSignInLaunch, *nonInteractive, saveFolder)
	}
	if err != nil {
		if console {
			fmt.Fprintln(os.Stderr, err)
		} else if !errors.Is(err, leaderboard.ErrAuthCancelled) {
			showMessage("CWG RPG Uploader", err.Error())
		}
		os.Exit(1)
	}
}

func run(console, dryRun, windowsSignInLaunch, nonInteractive bool, saveFolder string) error {
	configDir, stateDir, err := dataFolders()
	if err != nil {
		return err
	}
	var ui consoleUI
	if console {
		var restore func()
		ui, restore = openConsoleUI(os.Stdout, nonInteractive)
		defer restore()
		if err := ui.showPaths(
			consoleRow{"Configuration", filepath.Join(configDir, "config.json")},
			consoleRow{"Logs", filepath.Join(stateDir, "uploader.log")},
			consoleRow{"Credentials", storage.CredentialPath(configDir)},
		); err != nil {
			return err
		}
	}
	release, err := singleInstance(stateDir)
	if err != nil {
		return err
	}
	defer release()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	account, credentialErr := leaderboard.OpenAccount(configDir)
	if console && !dryRun {
		if problem := account.UploadBlock(); problem != "" {
			if nonInteractive {
				return fmt.Errorf("%s. Run with --authenticate before retrying --non-interactive", problem)
			}
			if err := authenticateConsole(ctx, configDir, account, ui); err != nil {
				return err
			}
		}
		fmt.Fprintln(os.Stdout, account.Status())
	}
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
	client := leaderboard.NewClient(httpClient, account)
	writer, err := storage.OpenLog(stateDir)
	if err != nil {
		return err
	}
	defer writer.Close()
	var output io.Writer = writer
	if console {
		output = io.MultiWriter(writer, ui.logWriter())
	}
	logger := log.New(output, "", log.Ldate|log.Ltime)
	if !console && credentialErr != nil {
		logger.Printf("Could not load uploader credential: %v", credentialErr)
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
	if err := ctx.Err(); err != nil {
		return err
	}
	authenticator := leaderboard.NewAuthenticator(client)
	if !console && account.UploadBlock() != "" {
		if err := authenticateTray(ctx, account, authenticator); err != nil {
			return err
		}
	}
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
	err = runTray(ctx, cancel, application, configDir, account, authenticator, windowsSignInLaunch)
	cancel()
	<-done
	return err
}

func runAuthentication() error {
	configDir, stateDir, err := dataFolders()
	if err != nil {
		return err
	}
	ui, restore := openConsoleUI(os.Stdout, false)
	defer restore()
	if err := ui.showPaths(consoleRow{"Credentials", storage.CredentialPath(configDir)}); err != nil {
		return err
	}
	release, err := singleInstance(stateDir)
	if err != nil {
		return err
	}
	defer release()
	account, _ := leaderboard.OpenAccount(configDir)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := authenticateConsole(ctx, configDir, account, ui); err != nil {
		return err
	}
	_, err = fmt.Fprintln(ui.out, account.Status())
	return err
}

func authenticateConsole(ctx context.Context, configDir string, account *leaderboard.Account, ui consoleUI) error {
	proxyURL, err := storage.LoadProxyURL(configDir)
	if err != nil {
		return err
	}
	httpClient, err := leaderboard.NewHTTPClient(proxyURL)
	if err != nil {
		return err
	}
	defer httpClient.CloseIdleConnections()
	authenticator := leaderboard.NewAuthenticator(leaderboard.NewClient(httpClient, account))
	stopSpinner := func() {}
	defer func() { stopSpinner() }()
	credential, err := authenticator.Authenticate(ctx, func(_ context.Context, link string) error {
		stop, err := ui.showApproval(link)
		if err != nil {
			return err
		}
		stopSpinner = stop
		return nil
	})
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return account.SetCredential(credential)
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
	message := "This app sends CWG RPG saves to a third-party public leaderboard. After you connect a Battle.net account, existing saves upload if Automatic uploads is enabled. It never modifies game saves.\n\nYou can change Automatic uploads and the save folder in config.json, or through the Windows tray. Already queued uploads and retries continue when Automatic uploads is off."
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

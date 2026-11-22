// Command profile runs the real scanner/coordinator with uploads disabled.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"syscall"
	"time"

	"cwgrpguploader/internal/app"
	"cwgrpguploader/internal/leaderboard"
	"cwgrpguploader/internal/storage"
	ui "cwgrpguploader/internal/windows"
)

func main() {
	// This command alone uses denser heap sampling; release defaults stay intact.
	runtime.MemProfileRate = 16 << 10
	folder := flag.String("save-folder", "", "absolute save folder to inspect (required; read only)")
	output := flag.String("output", "profiles", "parent directory for a new run-* artifact directory")
	duration := flag.Duration("duration", 30*time.Second, "time to scan before capturing heap snapshots and exiting")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, *folder, *output, *duration); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, folder, output string, duration time.Duration) error {
	if duration <= 0 {
		return errors.New("duration must be positive")
	}
	application, err := newApplication(folder)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(output, 0700); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(output, "run-")
	if err != nil {
		return err
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, "Upload-disabled profiling artifacts:", dir)
	writer, err := os.Create(filepath.Join(dir, "app.log"))
	if err != nil {
		return err
	}
	defer writer.Close()
	application.Dir = dir
	application.Logger = log.New(writer, "", log.Ldate|log.Ltime)
	info, _ := debug.ReadBuildInfo()
	if err := writeJSON(dir, "build.json", struct {
		BuildInfo      *debug.BuildInfo
		SaveFolder     string
		Duration       time.Duration
		MemProfileRate int
		GODEBUG        string
	}{info, folder, duration, runtime.MemProfileRate, os.Getenv("GODEBUG")}); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); application.Run(ctx) }()
	captured := make(chan error, 1)
	go func() {
		captured <- capture(ctx, dir, duration)
		cancel()
	}()
	ui.RunProfileTray(ctx, cancel, application.Updates)
	cancel()
	<-done
	err = <-captured
	if err == context.Canceled {
		return nil // Quitting early leaves partial diagnostics, not a failed upload.
	}
	return err
}

func newApplication(folder string) (*app.App, error) {
	config := storage.Config{SaveFolder: folder, PollSeconds: 5, Automatic: true}
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("save-folder: %w", err)
	}
	client, err := leaderboard.NewHTTPClient("")
	if err != nil {
		return nil, err
	}
	// Retain the normal idle transport and linked TLS implementation for memory
	// comparisons, but never delegate a request to it, even if DryRun regresses.
	return &app.App{
		Config: config, DryRun: true,
		Client: blockedUploader{client},
		Logger: log.New(io.Discard, "", 0), Updates: make(chan app.Status, 1),
		// No Commands channel: the diagnostic tray cannot mutate configuration.
	}, nil
}

var errNetworkingDisabled = errors.New("networking is disabled in the profiling executable")

type blockedUploader struct{ idleClient *http.Client }

func (blockedUploader) Upload(context.Context, leaderboard.UploadRequest) (leaderboard.Result, error) {
	return leaderboard.Result{}, errNetworkingDisabled
}

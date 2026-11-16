package app

import (
	"context"
	"log"
	"path/filepath"
	"time"

	"cwgrpguploader/internal/leaderboard"
	"cwgrpguploader/internal/storage"
)

type Uploader interface {
	Upload(context.Context, leaderboard.UploadRequest) (leaderboard.Result, error)
}

type App struct {
	Dir        string // Successful-upload history and logs.
	ConfigDir  string // Empty means settings share Dir, as on Windows.
	Config     storage.Config
	LastUpload storage.UploadState
	Client     Uploader
	Logger     *log.Logger
	DryRun     bool
	Commands   chan Command
	Updates    chan Status
}

// Run owns coordination and presentation. The uploader owns a separate loop;
// only lifecycle events cross back into the coordinator.
func (a *App) Run(ctx context.Context) {
	state := newRunState(ctx, a)
	ticker := time.NewTicker(time.Duration(a.Config.PollSeconds) * time.Second)
	defer ticker.Stop()
	defer state.shutdownUploader()

	state.logger.Print("started")
	state.refreshStatus()
	state.tick(ctx, time.Now())
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			state.tick(ctx, now)
		case event, open := <-state.uploader.events:
			if !open {
				return
			}
			state.handleUploadEvent(event)
		case command := <-a.Commands:
			if err := state.handleCommand(ctx, command); err != nil {
				state.reportCommandError(err)
			}
		}
	}
}

func newRunState(ctx context.Context, a *App) *runState {
	state := &runState{app: a, logger: componentLogger(a.Logger, "app")}
	state.resetPoller(filepath.Clean(a.Config.SaveFolder))
	state.uploader = newSnapshotUploader(ctx, a.Client, a.DryRun, componentLogger(a.Logger, "uploader"))
	return state
}

func (s *runState) resetPoller(saveFolder string) {
	baseline := ""
	lastUpload := s.app.LastUpload
	if !s.app.DryRun && lastUpload.Destination == leaderboard.BaseURL && storage.SameFolder(lastUpload.SaveFolder, saveFolder) {
		baseline = lastUpload.Fingerprint
	}
	s.poller = newFolderPoller(saveFolder, baseline, componentLogger(s.app.Logger, "poller"))
	s.view.saveFolder = saveFolder
	s.view.folder = pollEvent{}
	s.view.delivery.changeFolder()
}

func (s *runState) tick(ctx context.Context, now time.Time) {
	if ctx.Err() != nil {
		return
	}
	if s.view.persistenceError != nil {
		s.persistLastUpload()
		s.refreshStatus()
	}
	observation := s.poller.poll(ctx, now, s.app.Config.Automatic)
	if observation.kind == pollCancelled {
		return
	}
	s.view.folder = observation
	s.view.commandError = nil
	if observation.update != nil {
		s.forwardSnapshot(ctx, *observation.update)
	} else {
		s.refreshStatus()
	}
}

func (s *runState) forwardSnapshot(ctx context.Context, update folderUpdate) {
	s.nextWorkID++
	update.id = s.nextWorkID
	if err := s.uploader.queue(ctx, update); err != nil {
		if ctx.Err() == nil {
			s.reportCommandError(err)
		}
		return
	}
	s.logger.Printf("work_id=%d snapshot accepted: %d eligible files", update.id, len(update.snapshot.Files))
	s.view.delivery.accept(update)
	s.refreshStatus()
}

func (s *runState) shutdownUploader() {
	s.uploader.stop()
	// Lifecycle events are reliable: drain successes before shutting down, even
	// though intermediate UI status messages may be coalesced.
	for event := range s.uploader.events {
		s.handleUploadEvent(event)
	}
	s.uploader.wait()
}

func (s *runState) handleUploadEvent(event uploadEvent) {
	// Accepted work keeps its origin even after the watched folder changes.
	if success, ok := event.(uploadSucceeded); ok {
		s.recordUpload(success)
	}
	s.view.delivery.observe(event, s.poller.saveFolder)
	if storage.SameFolder(event.sourceFolder(), s.poller.saveFolder) {
		s.view.commandError = nil
	}
	s.refreshStatus()
}

func (s *runState) recordUpload(event uploadSucceeded) {
	lastUpload := storage.UploadState{
		SaveFolder:  event.saveFolder,
		Destination: leaderboard.BaseURL,
		Fingerprint: event.fingerprint,
		UploadedAt:  event.at,
		Player:      event.result.Player,
		Heroes:      event.result.Heroes,
	}
	s.app.LastUpload = lastUpload
	s.persistLastUpload()
}

func (s *runState) persistLastUpload() {
	previous := s.view.persistenceError
	err := storage.SaveUploadState(s.app.Dir, s.app.LastUpload)
	s.view.persistenceError = err
	if err != nil && (previous == nil || err.Error() != previous.Error()) {
		s.logger.Printf("save upload state failed: %v", err)
	}
}

type runState struct {
	app        *App
	logger     *log.Logger
	poller     *folderPoller
	uploader   *snapshotUploader
	view       statusView
	nextWorkID uint64
}

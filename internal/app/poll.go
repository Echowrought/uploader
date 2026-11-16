package app

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"cwgrpguploader/internal/saves"
)

// folderPoller owns detection and event deduplication, not delivery state.
type folderPoller struct {
	saveFolder             string
	scanner                *saves.Scanner
	logger                 *log.Logger
	lastEmittedFingerprint string
	unchangedSince         time.Time
	forceNextEmission      bool
	lastError              string
	lastWarning            string
}

func (p *folderPoller) poll(ctx context.Context, now time.Time, automatic bool) pollEvent {
	if err := ctx.Err(); err != nil {
		return pollEvent{kind: pollCancelled, err: err}
	}
	if !automatic && !p.forceNextEmission {
		p.unchangedSince = time.Time{}
		return pollEvent{kind: pollDisabled}
	}
	snapshot, err := p.scanner.Scan(ctx, now)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return pollEvent{kind: pollCancelled, err: err}
	}
	if errors.Is(err, saves.ErrSettling) {
		p.unchangedSince = time.Time{}
		return pollEvent{kind: pollSettling, err: err}
	}
	if err != nil {
		p.unchangedSince = time.Time{}
		if err.Error() != p.lastError {
			p.logger.Printf("save scan failed: %v", err)
			p.lastError = err.Error()
		}
		return pollEvent{kind: pollFailed, err: err}
	}
	p.lastError = ""
	warning := strings.Join(snapshot.Warnings, "; ")
	if warning != "" && warning != p.lastWarning {
		p.logger.Printf("save warnings: %s", warning)
	}
	p.lastWarning = warning
	// This is an observation time, not a filesystem mtime or upload time.
	// A loaded delivery baseline cannot establish observations before startup.
	if p.unchangedSince.IsZero() || snapshot.Fingerprint != p.lastEmittedFingerprint {
		p.unchangedSince = now
	}
	if snapshot.Fingerprint == p.lastEmittedFingerprint && !p.forceNextEmission {
		return pollEvent{kind: pollStable, warnings: snapshot.Warnings, unchangedSince: p.unchangedSince}
	}
	if err := ctx.Err(); err != nil {
		return pollEvent{kind: pollCancelled, err: err}
	}
	p.lastEmittedFingerprint = snapshot.Fingerprint
	p.forceNextEmission = false
	p.logger.Printf("snapshot emitted: %d eligible files", len(snapshot.Files))
	return pollEvent{kind: pollUpdated, update: &folderUpdate{saveFolder: p.saveFolder, snapshot: snapshot}, warnings: snapshot.Warnings}
}

func (p *folderPoller) requestEmission() {
	p.forceNextEmission = true
}

func newFolderPoller(saveFolder, baseline string, logger *log.Logger) *folderPoller {
	return &folderPoller{
		saveFolder:             saveFolder,
		scanner:                saves.NewScanner(saveFolder, 5*time.Second, saves.DefaultLimits()),
		logger:                 logger,
		lastEmittedFingerprint: baseline,
	}
}

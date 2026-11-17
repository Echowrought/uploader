package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"cwgrpguploader/internal/saves"
)

func TestFolderPollerUsesProductionFileLimit(t *testing.T) {
	folder := fixtureSaveFolder(t)
	text := strings.Repeat("x", 65)
	if err := os.WriteFile(filepath.Join(folder, "HERO_01_123.pld"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	poller := newFolderPoller(folder, "", log.New(io.Discard, "", 0))
	now := time.Now()

	poller.poll(t.Context(), now, true)
	event := poller.poll(t.Context(), now.Add(5*time.Second), true)

	if event.kind != pollUpdated || event.update == nil || event.update.snapshot.Files["HERO_01_123.pld"].Text != text {
		t.Fatalf("production poller rejected valid save contents: %+v", event)
	}
}

func TestFolderPollerEmitsStableSnapshotOnlyOnce(t *testing.T) {
	_, poller := fixtureFolderPoller(t)
	now := time.Now()

	settling := poller.poll(t.Context(), now, true)
	first := poller.poll(t.Context(), now.Add(5*time.Second), true)
	repeated := poller.poll(t.Context(), now.Add(10*time.Second), true)

	if settling.kind != pollSettling || !errors.Is(settling.err, saves.ErrSettling) || settling.update != nil {
		t.Fatalf("unsettled observation: %+v", settling)
	}
	if first.kind != pollUpdated || first.update == nil || first.update.snapshot.Fingerprint == "" || repeated.kind != pollStable || repeated.update != nil {
		t.Fatal("settled snapshot was missing or emitted more than once")
	}
}

func TestFolderPollerSkipsMatchingBaseline(t *testing.T) {
	folder := fixtureSaveFolder(t)
	baseline := readSnapshot(t, folder)
	poller := newFolderPoller(folder, baseline.Fingerprint, log.New(io.Discard, "", 0))
	now := time.Now()

	poller.poll(t.Context(), now, true)
	observation := poller.poll(t.Context(), now.Add(5*time.Second), true)

	if observation.kind != pollStable || observation.update != nil || !observation.unchangedSince.Equal(now.Add(5*time.Second)) {
		t.Fatalf("baseline was reemitted or claimed observations before the session: %+v", observation)
	}
}

func TestFolderPollerForcedEmissionReemitsUnchangedSnapshot(t *testing.T) {
	_, poller := fixtureFolderPoller(t)
	now := time.Now()

	poller.poll(t.Context(), now, true)
	first := poller.poll(t.Context(), now.Add(5*time.Second), true)
	poller.requestEmission()
	second := poller.poll(t.Context(), now.Add(6*time.Second), false)

	if first.kind != pollUpdated || second.kind != pollUpdated || first.update == nil || second.update == nil {
		t.Fatal("manual request did not reemit settled saves")
	}
	if first.update.snapshot.Fingerprint != second.update.snapshot.Fingerprint {
		t.Fatal("unchanged contents acquired a different fingerprint")
	}
}

func TestFolderPollerRetainsForcedRequestUntilSavesSettle(t *testing.T) {
	for _, automatic := range []bool{true, false} {
		t.Run(strconv.FormatBool(automatic), func(t *testing.T) {
			folder := fixtureSaveFolder(t)
			baseline := readSnapshot(t, folder)
			poller := newFolderPoller(folder, baseline.Fingerprint, log.New(io.Discard, "", 0))
			now := time.Now()

			poller.requestEmission()
			first := poller.poll(t.Context(), now, automatic)
			early := poller.poll(t.Context(), now.Add(4*time.Second), automatic)
			settled := poller.poll(t.Context(), now.Add(5*time.Second), automatic)

			if first.kind != pollSettling || early.kind != pollSettling || first.update != nil || early.update != nil || settled.kind != pollUpdated || settled.update == nil {
				t.Fatal("manual request was lost during settling or emitted prematurely")
			}
		})
	}
}

func TestFolderPollerReturnsChangedSnapshot(t *testing.T) {
	folder, poller := fixtureFolderPoller(t)
	now := time.Now()

	poller.poll(t.Context(), now, true)
	first := poller.poll(t.Context(), now.Add(5*time.Second), true)
	writeErr := os.WriteFile(filepath.Join(folder, "HERO_01_123.pld"), []byte("newer save data"), 0600)
	pending := poller.poll(t.Context(), now.Add(6*time.Second), true)
	second := poller.poll(t.Context(), now.Add(11*time.Second), true)

	if writeErr != nil {
		t.Fatal(writeErr)
	}
	if first.update == nil || second.update == nil || pending.kind != pollStable || pending.update != nil {
		t.Fatal("changed contents did not settle independently")
	}
	if first.update.snapshot.Files["HERO_01_123.pld"].Text != "save" || second.update.snapshot.Files["HERO_01_123.pld"].Text != "newer save data" || first.update.snapshot.Fingerprint == second.update.snapshot.Fingerprint {
		t.Fatal("changed snapshot was lost or mutated the previous emission")
	}
}

func TestFolderPollerTimestampChangeDoesNotEmitSnapshot(t *testing.T) {
	folder, poller := fixtureFolderPoller(t)
	now := time.Now()
	at := now.Add(time.Hour)

	poller.poll(t.Context(), now, true)
	first := poller.poll(t.Context(), now.Add(5*time.Second), true)
	touchErr := os.Chtimes(filepath.Join(folder, "HERO_01_123.pld"), at, at)
	repeated := poller.poll(t.Context(), now.Add(10*time.Second), true)

	if touchErr != nil {
		t.Fatal(touchErr)
	}
	if first.update == nil || repeated.kind != pollStable || repeated.update != nil {
		t.Fatal("timestamp-only change reemitted unchanged contents")
	}
}

func TestFolderPollerAutomaticOffDoesNotEmitFurtherChanges(t *testing.T) {
	folder, poller := fixtureFolderPoller(t)
	now := time.Now()

	poller.requestEmission()
	poller.poll(t.Context(), now, false)
	first := poller.poll(t.Context(), now.Add(5*time.Second), false)
	writeErr := os.WriteFile(filepath.Join(folder, "HERO_01_123.pld"), []byte("changed save"), 0600)
	disabled := poller.poll(t.Context(), now.Add(6*time.Second), false)
	later := poller.poll(t.Context(), now.Add(12*time.Second), false)

	if writeErr != nil {
		t.Fatal(writeErr)
	}
	if first.kind != pollUpdated || first.update == nil || disabled.kind != pollDisabled || disabled.update != nil || later.kind != pollDisabled || later.update != nil {
		t.Fatal("one manual emission enabled further automatic detection")
	}
}

func TestFolderPollerAutomaticOffDoesNotReadMissingFolder(t *testing.T) {
	poller := newFolderPoller(filepath.Join(t.TempDir(), "missing"), "", log.New(io.Discard, "", 0))

	observation := poller.poll(t.Context(), time.Now(), false)

	if observation.kind != pollDisabled || observation.err != nil || observation.update != nil {
		t.Fatalf("disabled poll attempted to read a missing folder: %+v", observation)
	}
}

func TestFolderPollerCancellationDoesNotEmitSnapshot(t *testing.T) {
	_, poller := fixtureFolderPoller(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	now := time.Now()

	cancelled := poller.poll(ctx, now, true)
	poller.poll(t.Context(), now, true)
	recovered := poller.poll(t.Context(), now.Add(5*time.Second), true)

	if cancelled.kind != pollCancelled || !errors.Is(cancelled.err, context.Canceled) || cancelled.update != nil {
		t.Fatalf("cancelled poll: %+v", cancelled)
	}
	if recovered.kind != pollUpdated || recovered.update == nil {
		t.Fatal("cancelled observation suppressed the subsequent valid snapshot")
	}
}

func TestFolderPollerEmitsAgainAfterRestart(t *testing.T) {
	folder, first := fixtureFolderPoller(t)
	restarted := newFolderPoller(folder, "", log.New(io.Discard, "", 0))
	now := time.Now()

	first.poll(t.Context(), now, true)
	before := first.poll(t.Context(), now.Add(5*time.Second), true)
	restarted.poll(t.Context(), now, true)
	after := restarted.poll(t.Context(), now.Add(5*time.Second), true)

	if before.update == nil || after.update == nil || before.update.snapshot.Fingerprint != after.update.snapshot.Fingerprint {
		t.Fatal("a new session suppressed previously emitted but unacknowledged contents")
	}
}

func TestFolderPollerReportsWarningsWithoutReemittingCore(t *testing.T) {
	folder := fixtureSaveFolder(t)
	var logs bytes.Buffer
	poller := newFolderPoller(folder, "", log.New(&logs, "", 0))
	now := time.Now()
	path := filepath.Join(folder, "MISC_bad.pld")

	poller.poll(t.Context(), now, true)
	first := poller.poll(t.Context(), now.Add(5*time.Second), true)
	writeErr := os.WriteFile(path, []byte{0xff}, 0600)
	invalid := poller.poll(t.Context(), now.Add(6*time.Second), true)
	repeated := poller.poll(t.Context(), now.Add(12*time.Second), true)
	removeErr := os.Remove(path)
	recovered := poller.poll(t.Context(), now.Add(13*time.Second), true)

	if writeErr != nil || removeErr != nil {
		t.Fatalf("write=%v remove=%v", writeErr, removeErr)
	}
	if first.update == nil || invalid.kind != pollStable || invalid.update != nil || repeated.kind != pollStable || repeated.update != nil || recovered.kind != pollStable || recovered.update != nil || len(invalid.warnings) != 1 || len(recovered.warnings) != 0 {
		t.Fatal("supplementary warning reemitted core data or did not recover")
	}
	if strings.Count(logs.String(), "save warnings:") != 1 || !strings.Contains(logs.String(), "MISC_bad.pld") || !strings.Contains(logs.String(), "UTF-8") {
		t.Fatalf("warning diagnostics were lost or repeatedly logged: %s", logs.String())
	}
}

func TestFolderPollerManualEmissionRetainsObservationTime(t *testing.T) {
	_, poller := fixtureFolderPoller(t)
	now := time.Now()

	poller.poll(t.Context(), now, true)
	poller.poll(t.Context(), now.Add(5*time.Second), true)
	initial := poller.poll(t.Context(), now.Add(6*time.Second), true)
	poller.requestEmission()
	forced := poller.poll(t.Context(), now.Add(time.Minute), true)
	after := poller.poll(t.Context(), now.Add(61*time.Second), true)

	if forced.update == nil || initial.unchangedSince.IsZero() || !after.unchangedSince.Equal(initial.unchangedSince) {
		t.Fatal("requesting unchanged data restarted its observation window")
	}
}

func TestFolderPollerNewContentsRestartObservationTime(t *testing.T) {
	folder, poller := fixtureFolderPoller(t)
	now := time.Now()

	poller.poll(t.Context(), now, true)
	poller.poll(t.Context(), now.Add(5*time.Second), true)
	initial := poller.poll(t.Context(), now.Add(6*time.Second), true)
	writeErr := os.WriteFile(filepath.Join(folder, "HERO_01_123.pld"), []byte("new contents"), 0600)
	poller.poll(t.Context(), now.Add(10*time.Second), true)
	poller.poll(t.Context(), now.Add(15*time.Second), true)
	after := poller.poll(t.Context(), now.Add(16*time.Second), true)

	if writeErr != nil {
		t.Fatal(writeErr)
	}
	if initial.unchangedSince.IsZero() || !after.unchangedSince.Equal(now.Add(15*time.Second)) {
		t.Fatal("new settled contents retained the previous observation window")
	}
}

func TestFolderPollerPauseRestartsObservationTime(t *testing.T) {
	_, poller := fixtureFolderPoller(t)
	now := time.Now()

	poller.poll(t.Context(), now, true)
	poller.poll(t.Context(), now.Add(5*time.Second), true)
	initial := poller.poll(t.Context(), now.Add(6*time.Second), true)
	paused := poller.poll(t.Context(), now.Add(10*time.Second), false)
	resumed := poller.poll(t.Context(), now.Add(20*time.Second), true)

	if initial.unchangedSince.IsZero() || !paused.unchangedSince.IsZero() || !resumed.unchangedSince.Equal(now.Add(20*time.Second)) {
		t.Fatal("observations incorrectly spanned a monitoring pause")
	}
}

func TestFolderPollerScanFailureRestartsObservationTime(t *testing.T) {
	folder, poller := fixtureFolderPoller(t)
	now := time.Now()
	path := filepath.Join(folder, "ACCOUNT_player_123.pld")

	poller.poll(t.Context(), now, true)
	poller.poll(t.Context(), now.Add(5*time.Second), true)
	initial := poller.poll(t.Context(), now.Add(6*time.Second), true)
	removeErr := os.Remove(path)
	failed := poller.poll(t.Context(), now.Add(10*time.Second), true)
	writeErr := os.WriteFile(path, []byte("save"), 0600)
	poller.poll(t.Context(), now.Add(20*time.Second), true)
	recovered := poller.poll(t.Context(), now.Add(25*time.Second), true)

	if removeErr != nil || writeErr != nil {
		t.Fatalf("remove=%v restore=%v", removeErr, writeErr)
	}
	if initial.unchangedSince.IsZero() || failed.kind != pollFailed || !failed.unchangedSince.IsZero() || !recovered.unchangedSince.Equal(now.Add(25*time.Second)) {
		t.Fatal("observations incorrectly spanned a failed scan")
	}
}

func fixtureFolderPoller(t *testing.T) (string, *folderPoller) {
	t.Helper()
	folder := fixtureSaveFolder(t)
	return folder, newFolderPoller(folder, "", log.New(io.Discard, "", 0))
}

package saves

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScannerRestartsInterruptedObservationOfSameContents(t *testing.T) {
	folder := fixtureFolder(t)
	path := filepath.Join(folder, "HERO_01_123.pld")
	scanner := NewScanner(folder, 5*time.Second, DefaultLimits())
	now := time.Now()

	_, _ = scanner.Scan(t.Context(), now)
	writeFixture(t, path, "candidate")
	_, candidateErr := scanner.Scan(t.Context(), now.Add(time.Second))
	writeFixture(t, path, "\xff")
	_, interruptedErr := scanner.Scan(t.Context(), now.Add(2*time.Second))
	writeFixture(t, path, "candidate")
	recovered, recoveredErr := scanner.Scan(t.Context(), now.Add(10*time.Second))
	_, earlyErr := scanner.Scan(t.Context(), now.Add(14*time.Second))
	ready, readyErr := scanner.Scan(t.Context(), now.Add(15*time.Second))

	if !errors.Is(candidateErr, ErrSettling) || interruptedErr == nil || !strings.Contains(interruptedErr.Error(), "UTF-8") {
		t.Fatalf("candidate/interruption errors: %v %v", candidateErr, interruptedErr)
	}
	if !errors.Is(recoveredErr, ErrSettling) || recovered.Files != nil || !errors.Is(earlyErr, ErrSettling) {
		t.Fatalf("interrupted time counted toward settlement: %v %v", recoveredErr, earlyErr)
	}
	if readyErr != nil || ready.Files["HERO_01_123.pld"].Text != "candidate" {
		t.Fatalf("candidate did not settle after a fresh quiet period: %+v %v", ready, readyErr)
	}
}

func TestScannerEssentialOverflowRetainsFallbackForLaterReadFailure(t *testing.T) {
	folder := fixtureFolder(t)
	path := filepath.Join(folder, "HERO_01_123.pld")
	writeFixture(t, filepath.Join(folder, "HERO_extra.pld"), "four")
	limits := smallLimits()
	limits.SnapshotBytes = 30
	scanner := NewScanner(folder, 0, limits)
	now := time.Now()

	_, _ = scanner.Scan(t.Context(), now)
	first, firstErr := scanner.Scan(t.Context(), now)
	writeFixture(t, path, strings.Repeat("x", 15))
	_, _ = scanner.Scan(t.Context(), now.Add(time.Second))
	blocked, overflowErr := scanner.Scan(t.Context(), now.Add(time.Second))
	writeFixture(t, path, "\xff")
	fallback, fallbackErr := scanner.Scan(t.Context(), now.Add(2*time.Second))

	if firstErr != nil || overflowErr == nil || !strings.Contains(overflowErr.Error(), "30 bytes") || blocked.Files != nil {
		t.Fatalf("overflow did not block the new essential set: %v %v", firstErr, overflowErr)
	}
	if fallbackErr != nil || fallback.Fingerprint != first.Fingerprint || fallback.Files["HERO_01_123.pld"].Text != "hero data!!" || len(fallback.Warnings) != 1 || !strings.Contains(fallback.Warnings[0], "UTF-8") {
		t.Fatalf("overflow discarded the prior usable payload: %+v %v", fallback, fallbackErr)
	}
}

func TestScannerOmittedSupplementaryCandidateEntersWhenCapacityFrees(t *testing.T) {
	folder := fixtureFolder(t)
	extra := filepath.Join(folder, "HERO_extra.pld")
	optional := filepath.Join(folder, "MISC.pld")
	writeFixture(t, optional, "old")
	limits := smallLimits()
	limits.SaveFiles = 3
	scanner := NewScanner(folder, 5*time.Second, limits)
	now := time.Now()

	_, _ = scanner.Scan(t.Context(), now)
	initial, initialErr := scanner.Scan(t.Context(), now.Add(5*time.Second))
	writeFixture(t, extra, "extra hero")
	writeFixture(t, optional, "optional")
	_, _ = scanner.Scan(t.Context(), now.Add(6*time.Second))
	full, fullErr := scanner.Scan(t.Context(), now.Add(11*time.Second))
	removeErr := os.Remove(extra)
	recovered, recoveredErr := scanner.Scan(t.Context(), now.Add(12*time.Second))
	_, includedWhileFull := full.Files["MISC.pld"]

	if initialErr != nil || fullErr != nil || removeErr != nil || recoveredErr != nil {
		t.Fatalf("errors: %v %v %v %v", initialErr, fullErr, removeErr, recoveredErr)
	}
	if initial.Files["MISC.pld"].Text != "old" {
		t.Fatalf("initial supplementary version was not admitted: %+v", initial)
	}
	if includedWhileFull || len(full.Files) != 3 || len(full.Warnings) != 1 {
		t.Fatalf("supplementary candidate displaced essential saves: %+v", full)
	}
	if recovered.Files["MISC.pld"].Text != "optional" || len(recovered.Files) != 3 || len(recovered.Warnings) != 0 {
		t.Fatalf("omission forgot the already-observed candidate's quiet period: %+v", recovered)
	}
}

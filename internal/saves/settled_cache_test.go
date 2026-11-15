package saves

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScannerSupplementaryUpdatesUseLastSettledVersion(t *testing.T) {
	folder := fixtureFolder(t)
	path := filepath.Join(folder, "MISC_player.pld")
	writeFixture(t, path, "old misc")
	scanner := NewScanner(folder, 5*time.Second, DefaultLimits())
	now := time.Now()

	_, _ = scanner.Scan(context.Background(), now)
	first, firstErr := scanner.Scan(context.Background(), now.Add(5*time.Second))
	writeFixture(t, path, "new misc")
	pending, pendingErr := scanner.Scan(context.Background(), now.Add(6*time.Second))
	latest, latestErr := scanner.Scan(context.Background(), now.Add(11*time.Second))

	if firstErr != nil || pendingErr != nil || latestErr != nil {
		t.Fatalf("scan errors: %v, %v, %v", firstErr, pendingErr, latestErr)
	}
	if first.Files["MISC_player.pld"].Text != "old misc" || pending.Files["MISC_player.pld"].Text != "old misc" || first.Fingerprint != pending.Fingerprint || len(pending.Warnings) != 0 {
		t.Fatal("unsettled supplementary data displaced or mutated its settled version")
	}
	if latest.Files["MISC_player.pld"].Text != "new misc" || latest.Fingerprint == first.Fingerprint {
		t.Fatal("supplementary version did not advance after settling")
	}
}

func TestScannerInvalidSupplementaryCandidateKeepsSettledVersion(t *testing.T) {
	folder := fixtureFolder(t)
	path := filepath.Join(folder, "MISC_player.pld")
	writeFixture(t, path, "valid misc")
	scanner := NewScanner(folder, 0, DefaultLimits())
	now := time.Now()

	_, _ = scanner.Scan(context.Background(), now)
	first, firstErr := scanner.Scan(context.Background(), now)
	writeFixture(t, path, "\xff")
	fallback, fallbackErr := scanner.Scan(context.Background(), now)

	if firstErr != nil || fallbackErr != nil {
		t.Fatalf("scan errors: %v, %v", firstErr, fallbackErr)
	}
	if fallback.Files["MISC_player.pld"].Text != "valid misc" || fallback.Fingerprint != first.Fingerprint || len(fallback.Warnings) != 1 || !strings.Contains(fallback.Warnings[0], "UTF-8") {
		t.Fatal("invalid candidate displaced cached supplementary data or lacked a diagnostic")
	}
}

func TestScannerOversizedEssentialCandidateKeepsSettledVersion(t *testing.T) {
	folder := fixtureFolder(t)
	limits := smallLimits()
	scanner := NewScanner(folder, 0, limits)
	now := time.Now()

	_, _ = scanner.Scan(context.Background(), now)
	first, firstErr := scanner.Scan(context.Background(), now)
	writeSizedFixture(t, filepath.Join(folder, "HERO_01_123.pld"), limits.FileBytes+1)
	fallback, fallbackErr := scanner.Scan(context.Background(), now)

	if firstErr != nil || fallbackErr != nil {
		t.Fatalf("scan errors: %v, %v", firstErr, fallbackErr)
	}
	if fallback.Files["HERO_01_123.pld"].Text != "hero data!!" || fallback.Fingerprint != first.Fingerprint || len(fallback.Warnings) != 1 || !strings.Contains(fallback.Warnings[0], "64 bytes") {
		t.Fatal("on-disk candidate limits prevented use of a valid settled essential version")
	}
	assertSettledCacheWithinLimits(t, scanner, limits)
}

func TestScannerBudgetsSettledEssentialVersionsRatherThanCandidates(t *testing.T) {
	folder := fixtureFolder(t)
	for i := 2; i <= 4; i++ {
		writeFixture(t, filepath.Join(folder, fmt.Sprintf("HERO_%02d_123.pld", i)), "old hero")
	}
	limits := smallLimits()
	scanner := NewScanner(folder, 5*time.Second, limits)
	now := time.Now()

	_, _ = scanner.Scan(context.Background(), now)
	first, firstErr := scanner.Scan(context.Background(), now.Add(5*time.Second))
	writeSizedFixture(t, filepath.Join(folder, "ACCOUNT_player_123.pld"), limits.FileBytes)
	for i := 1; i <= 4; i++ {
		writeSizedFixture(t, filepath.Join(folder, fmt.Sprintf("HERO_%02d_123.pld", i)), limits.FileBytes)
	}
	pending, pendingErr := scanner.Scan(context.Background(), now.Add(6*time.Second))
	overflow, overflowErr := scanner.Scan(context.Background(), now.Add(11*time.Second))
	writeFixture(t, filepath.Join(folder, "HERO_04_123.pld"), "")
	_, _ = scanner.Scan(context.Background(), now.Add(12*time.Second))
	recovered, recoveredErr := scanner.Scan(context.Background(), now.Add(17*time.Second))

	if firstErr != nil || pendingErr != nil || overflowErr == nil || !strings.Contains(overflowErr.Error(), "256 bytes") || recoveredErr != nil {
		t.Fatalf("scan errors: %v, %v, %v, %v", firstErr, pendingErr, overflowErr, recoveredErr)
	}
	if pending.Fingerprint != first.Fingerprint || overflow.Files != nil {
		t.Fatal("candidate sizes blocked valid cached data or an over-limit settled set produced a snapshot")
	}
	if len(recovered.Files) != 5 || int64(len(recovered.Files["HERO_03_123.pld"].Text)) != limits.FileBytes || recovered.Files["HERO_04_123.pld"].Text != "" {
		t.Fatal("settled shrink did not free capacity for essential growth in the same scan")
	}
	assertSettledCacheWithinLimits(t, scanner, limits)
}

func assertSettledCacheWithinLimits(t *testing.T, scanner *Scanner, limits Limits) {
	t.Helper()
	var bytes int64
	count := 0
	for _, state := range scanner.settlements {
		if state.settled != nil {
			bytes += settledSize(state.settled)
			count++
		}
	}
	if bytes > limits.SnapshotBytes || count > limits.SaveFiles {
		t.Fatalf("cache exceeds payload limits: %d bytes, %d versions", bytes, count)
	}
}

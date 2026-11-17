package saves

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScannerEssentialReplacementReusesSnapshotCapacity(t *testing.T) {
	folder := fixtureFolder(t)
	limits := smallLimits()
	limits.SnapshotBytes = 24
	scanner := NewScanner(folder, 0, limits)
	now := time.Now()

	_, _ = scanner.Scan(context.Background(), now)
	first, firstErr := scanner.Scan(context.Background(), now)
	writeFixture(t, filepath.Join(folder, "HERO_01_123.pld"), "twelve bytes")
	_, _ = scanner.Scan(context.Background(), now)
	second, secondErr := scanner.Scan(context.Background(), now)

	if firstErr != nil || secondErr != nil {
		t.Fatalf("errors: %v %v", firstErr, secondErr)
	}
	if second.Files["HERO_01_123.pld"].Text != "twelve bytes" || len(second.Warnings) != 0 || first.Files["HERO_01_123.pld"].Text != "hero data!!" {
		t.Fatal("essential replacement could not reuse capacity or mutated the previous snapshot")
	}
}

func TestScannerEssentialOverflowRecoversWithoutRestartingObservedCandidate(t *testing.T) {
	folder := fixtureFolder(t)
	extra := filepath.Join(folder, "HERO_extra.pld")
	writeFixture(t, extra, "four")
	limits := smallLimits()
	limits.SnapshotBytes = 30
	scanner := NewScanner(folder, 5*time.Second, limits)
	now := time.Now()

	_, _ = scanner.Scan(context.Background(), now)
	first, firstErr := scanner.Scan(context.Background(), now.Add(5*time.Second))
	writeFixture(t, filepath.Join(folder, "HERO_01_123.pld"), strings.Repeat("x", 15))
	_, _ = scanner.Scan(context.Background(), now.Add(6*time.Second))
	blocked, overflowErr := scanner.Scan(context.Background(), now.Add(11*time.Second))
	removeErr := os.Remove(extra)
	recovered, recoveredErr := scanner.Scan(context.Background(), now.Add(12*time.Second))

	if firstErr != nil || overflowErr == nil || !strings.Contains(overflowErr.Error(), "30 bytes") || removeErr != nil || recoveredErr != nil {
		t.Fatalf("errors: %v %v %v %v", firstErr, overflowErr, removeErr, recoveredErr)
	}
	if blocked.Files != nil || recovered.Files["HERO_01_123.pld"].Text != strings.Repeat("x", 15) || first.Files["HERO_01_123.pld"].Text != "hero data!!" {
		t.Fatal("overflow published partial data, lost candidate observations, or mutated an older snapshot")
	}
}

func TestScannerSupplementaryGrowthFallsBackToFittingSettledVersion(t *testing.T) {
	folder := fixtureFolder(t)
	path := filepath.Join(folder, "MISC.pld")
	writeFixture(t, path, "old")
	limits := smallLimits()
	limits.SnapshotBytes = 30
	scanner := NewScanner(folder, 0, limits)
	now := time.Now()

	_, _ = scanner.Scan(context.Background(), now)
	first, firstErr := scanner.Scan(context.Background(), now)
	writeFixture(t, path, "too large")
	_, _ = scanner.Scan(context.Background(), now)
	fallback, fallbackErr := scanner.Scan(context.Background(), now)

	if firstErr != nil || fallbackErr != nil {
		t.Fatalf("errors: %v %v", firstErr, fallbackErr)
	}
	if fallback.Files["MISC.pld"].Text != "old" || fallback.Fingerprint != first.Fingerprint || len(fallback.Warnings) != 1 || !strings.Contains(fallback.Warnings[0], `use settled save "MISC.pld"`) {
		t.Fatalf("supplementary growth displaced its fitting fallback: %+v", fallback)
	}
}

func TestScannerEssentialGrowthOmitsSupplementaryVersionsThatNoLongerFit(t *testing.T) {
	folder := fixtureFolder(t)
	path := filepath.Join(folder, "MISC.pld")
	writeFixture(t, path, "old")
	limits := smallLimits()
	limits.SnapshotBytes = 30
	scanner := NewScanner(folder, 0, limits)
	now := time.Now()

	_, _ = scanner.Scan(context.Background(), now)
	first, firstErr := scanner.Scan(context.Background(), now)
	writeFixture(t, filepath.Join(folder, "HERO_01_123.pld"), strings.Repeat("x", 17))
	writeFixture(t, path, "larger")
	_, _ = scanner.Scan(context.Background(), now)
	second, secondErr := scanner.Scan(context.Background(), now)
	_, included := second.Files["MISC.pld"]

	if firstErr != nil || secondErr != nil {
		t.Fatalf("errors: %v %v", firstErr, secondErr)
	}
	if included || second.Files["HERO_01_123.pld"].Text != strings.Repeat("x", 17) || len(second.Warnings) != 1 || !strings.Contains(second.Warnings[0], `skip supplementary save "MISC.pld"`) || first.Files["MISC.pld"].Text != "old" {
		t.Fatal("unfitting supplementary versions blocked essential growth or mutated previous data")
	}
}

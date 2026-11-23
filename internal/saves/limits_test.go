package saves

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultLimitsPreserveProductionSafetyBudgets(t *testing.T) {
	want := Limits{DirectoryEntries: 4096, SaveFiles: 1024, FileBytes: 8 << 20, SnapshotBytes: 32 << 20}

	got := DefaultLimits()

	if got != want {
		t.Fatalf("default limits=%+v, want=%+v", got, want)
	}
}

func TestScannerCopiesPolicyAndKeepsInstancesIndependent(t *testing.T) {
	folder := fixtureFolder(t)
	limits := smallLimits()
	scanner := NewScanner(folder, 0, limits)
	limits.FileBytes = 4
	restricted := NewScanner(folder, 0, limits)
	now := time.Now()

	_, _ = scanner.Scan(context.Background(), now)
	snapshot, err := scanner.Scan(context.Background(), now)
	blocked, restrictedErr := restricted.Scan(context.Background(), now)

	if err != nil || len(snapshot.Files) != 2 {
		t.Fatalf("changing another policy affected this scanner: snapshot=%+v, error=%v", snapshot, err)
	}
	if restrictedErr == nil || !strings.Contains(restrictedErr.Error(), "4 bytes safety limit") || blocked.Files != nil {
		t.Fatalf("restricted scanner did not use its own policy: %v", restrictedErr)
	}
}

func TestScannerRejectsInvalidPolicyBeforeFilesystemAccess(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Limits)
	}{
		{"zero directory count", func(l *Limits) { l.DirectoryEntries = 0 }},
		{"negative file count", func(l *Limits) { l.SaveFiles = -1 }},
		{"zero file bytes", func(l *Limits) { l.FileBytes = 0 }},
		{"negative snapshot bytes", func(l *Limits) { l.SnapshotBytes = -1 }},
		{"overflowing read sentinel", func(l *Limits) { l.FileBytes = 1<<63 - 1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			limits := smallLimits()
			tc.change(&limits)
			scanner := NewScanner(filepath.Join(t.TempDir(), "missing"), 0, limits)

			snapshot, err := scanner.Scan(context.Background(), time.Now())

			if err == nil || !strings.Contains(err.Error(), "scanner limits") || snapshot.Files != nil {
				t.Fatalf("invalid policy reached filesystem access: %v", err)
			}
		})
	}
}

func TestScannerAcceptsConfiguredFileSizeLimitAndRetainsItForOversizedReplacement(t *testing.T) {
	folder := fixtureFolder(t)
	limits := smallLimits()
	path := filepath.Join(folder, "HERO_01_123.pld")
	text := strings.Repeat("x", int(limits.FileBytes))
	writeFixture(t, path, text)
	scanner := NewScanner(folder, 0, limits)
	now := time.Now()

	_, _ = scanner.Scan(context.Background(), now)
	first, firstErr := scanner.Scan(context.Background(), now)
	writeFixture(t, path, text+"x")
	fallback, fallbackErr := scanner.Scan(context.Background(), now)

	if firstErr != nil || fallbackErr != nil {
		t.Fatalf("scan errors: %v, %v", firstErr, fallbackErr)
	}
	if first.Files["HERO_01_123.pld"].Text != text || fallback.Fingerprint != first.Fingerprint || len(fallback.Warnings) != 1 || !strings.Contains(fallback.Warnings[0], "64 bytes") {
		t.Fatal("exact-size file was rejected, truncated, or displaced by its oversized replacement")
	}
}

func TestDefaultScannerReadsFullSizeSaveWithoutTruncation(t *testing.T) {
	folder := fixtureFolder(t)
	limits := DefaultLimits()
	writeSizedFixture(t, filepath.Join(folder, "HERO_01_123.pld"), limits.FileBytes)
	scanner := NewScanner(folder, 0, limits)
	now := time.Now()

	_, _ = scanner.Scan(context.Background(), now)
	snapshot, err := scanner.Scan(context.Background(), now)

	if err != nil || int64(len(snapshot.Files["HERO_01_123.pld"].Text)) != limits.FileBytes || snapshot.Fingerprint == "" {
		t.Fatalf("full-size save was rejected or truncated: %v", err)
	}
}

func TestScannerRejectsDirectoryPastLimitInSecondBatch(t *testing.T) {
	folder := fixtureFolder(t)
	limits := smallLimits()
	limits.DirectoryEntries = directoryBatchSize
	for i := 0; i < limits.DirectoryEntries-fixtureEntryCount+1; i++ {
		writeFixture(t, filepath.Join(folder, fmt.Sprintf("notes_%04d.txt", i)), "")
	}
	scanner := NewScanner(folder, 0, limits)

	snapshot, err := scanner.Scan(context.Background(), time.Now())

	if err == nil || !strings.Contains(err.Error(), "directory entries safety limit") || snapshot.Files != nil || snapshot.Fingerprint != "" {
		t.Fatalf("over-limit directory published partial data: %v", err)
	}
}

func smallLimits() Limits {
	return Limits{DirectoryEntries: 16, SaveFiles: 8, FileBytes: 64, SnapshotBytes: 256}
}

package saves

import (
	"context"
	"errors"
	"fmt"
	"os"
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

func TestReadFileEnforcesConfiguredSizeBoundary(t *testing.T) {
	cases := []struct {
		name    string
		size    int
		want    string
		wantErr error
	}{
		{"exact limit", 64, strings.Repeat("x", 64), nil},
		{"one over limit", 65, "", ErrSettling},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "save.pld")
			writeFixture(t, path, strings.Repeat("x", tc.size))
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			expected := metadata{Size: info.Size(), Modified: info.ModTime()}

			text, readErr := readFile(context.Background(), path, expected, 64)

			if text != tc.want || !errors.Is(readErr, tc.wantErr) {
				t.Fatalf("read size=%d: text length=%d, error=%v", tc.size, len(text), readErr)
			}
		})
	}
}

func TestDefaultScannerAcceptsFullSizeFileAndRejectsOversizedReplacement(t *testing.T) {
	folder := fixtureFolder(t)
	limits := DefaultLimits()
	path := filepath.Join(folder, "HERO_01_123.pld")
	writeSizedFixture(t, path, limits.FileBytes)
	scanner := NewScanner(folder, 0, limits)
	now := time.Now()

	_, _ = scanner.Scan(context.Background(), now)
	first, firstErr := scanner.Scan(context.Background(), now)
	writeSizedFixture(t, path, limits.FileBytes+1)
	fallback, fallbackErr := scanner.Scan(context.Background(), now)

	if firstErr != nil || fallbackErr != nil || len(first.Files["HERO_01_123.pld"].Text) != 8<<20 {
		t.Fatalf("production-sized file failed: first=%v, replacement=%v", firstErr, fallbackErr)
	}
	if first.Fingerprint != fallback.Fingerprint || len(fallback.Warnings) != 1 || !strings.Contains(fallback.Warnings[0], "8 MiB safety limit") {
		t.Fatalf("oversized replacement displaced the production-sized cache: %v", fallback.Warnings)
	}
	assertSettledCacheWithinLimits(t, scanner, limits)
}

func TestScannerDirectoryLimitFailureAfterEarlierBatchReturnsNoSnapshot(t *testing.T) {
	folder := fixtureFolder(t)
	limits := smallLimits()
	limits.DirectoryEntries = directoryBatchSize
	for i := 0; i < limits.DirectoryEntries-3; i++ {
		writeFixture(t, filepath.Join(folder, fmt.Sprintf("notes_%04d.txt", i)), "")
	}
	scanner := NewScanner(folder, 0, limits)

	snapshot, err := scanner.Scan(context.Background(), time.Now())

	if err == nil || !strings.Contains(err.Error(), "directory entries safety limit") || snapshot.Files != nil || snapshot.Fingerprint != "" {
		t.Fatalf("later directory batch published partial data: %v", err)
	}
}

func smallLimits() Limits {
	return Limits{DirectoryEntries: 16, SaveFiles: 8, FileBytes: 64, SnapshotBytes: 256}
}

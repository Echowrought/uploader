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

func TestScannerWaitsForQuietPeriod(t *testing.T) {
	folder := fixtureFolder(t)
	scanner := NewScanner(folder, 5*time.Second, DefaultLimits())
	now := time.Now()

	_, firstErr := scanner.Scan(context.Background(), now)
	_, earlyErr := scanner.Scan(context.Background(), now.Add(4*time.Second))
	snapshot, settledErr := scanner.Scan(context.Background(), now.Add(5*time.Second))

	if !errors.Is(firstErr, ErrSettling) || !errors.Is(earlyErr, ErrSettling) {
		t.Fatal("uploaded before quiet period")
	}
	if settledErr != nil {
		t.Fatal(settledErr)
	}
	if len(snapshot.Files) != 2 {
		t.Fatalf("got %d eligible files", len(snapshot.Files))
	}
	if snapshot.Files["HERO_01_123.pld"].Mtime != 1700000000123 {
		t.Fatal("mtime must use Unix milliseconds")
	}
}

func TestScannerDetectsContentChangesWithUnchangedMetadata(t *testing.T) {
	folder := fixtureFolder(t)
	scanner := NewScanner(folder, 5*time.Second, DefaultLimits())
	now := time.Now()
	_, firstErr := scanner.Scan(context.Background(), now)
	path := filepath.Join(folder, "HERO_01_123.pld")

	writeFixture(t, path, "different!!")
	changed, changedErr := scanner.Scan(context.Background(), now.Add(5*time.Second))
	_, earlyErr := scanner.Scan(context.Background(), now.Add(9*time.Second))
	second, settledErr := scanner.Scan(context.Background(), now.Add(10*time.Second))

	if !errors.Is(firstErr, ErrSettling) || !errors.Is(changedErr, ErrSettling) || !errors.Is(earlyErr, ErrSettling) || settledErr != nil {
		t.Fatalf("scan errors: %v, %v, %v, %v", firstErr, changedErr, earlyErr, settledErr)
	}
	if changed.Files != nil || second.Files["HERO_01_123.pld"].Text != "different!!" {
		t.Fatal("same-metadata content change bypassed initial settling or was missed")
	}
}

func TestScannerResetsQuietPeriodOnChange(t *testing.T) {
	folder := fixtureFolder(t)
	scanner := NewScanner(folder, 5*time.Second, DefaultLimits())
	now := time.Now()
	_, _ = scanner.Scan(context.Background(), now)

	writeFixture(t, filepath.Join(folder, "HERO_01_123.pld"), "longer replacement save")
	_, changedErr := scanner.Scan(context.Background(), now.Add(5*time.Second))
	_, settledErr := scanner.Scan(context.Background(), now.Add(10*time.Second))

	if !errors.Is(changedErr, ErrSettling) {
		t.Fatal("change did not restart quiet period")
	}
	if settledErr != nil {
		t.Fatal(settledErr)
	}
}

func TestScannerCoreReversionRestartsQuietPeriod(t *testing.T) {
	folder := fixtureFolder(t)
	scanner := NewScanner(folder, 5*time.Second, DefaultLimits())
	path := filepath.Join(folder, "HERO_01_123.pld")
	now := time.Now()

	_, _ = scanner.Scan(context.Background(), now)
	writeFixture(t, path, "different!!")
	_, changedErr := scanner.Scan(context.Background(), now.Add(4*time.Second))
	writeFixture(t, path, "hero data!!")
	_, revertedErr := scanner.Scan(context.Background(), now.Add(5*time.Second))
	_, earlyErr := scanner.Scan(context.Background(), now.Add(9*time.Second))
	snapshot, settledErr := scanner.Scan(context.Background(), now.Add(10*time.Second))

	if !errors.Is(changedErr, ErrSettling) || !errors.Is(revertedErr, ErrSettling) || !errors.Is(earlyErr, ErrSettling) || settledErr != nil {
		t.Fatalf("scan errors: %v, %v, %v, %v", changedErr, revertedErr, earlyErr, settledErr)
	}
	if snapshot.Files["HERO_01_123.pld"].Text != "hero data!!" {
		t.Fatal("settling reused the first observation instead of the latest core read")
	}
}

func TestScannerReadFailureKeepsLastSettledVersionUntilRecoverySettles(t *testing.T) {
	folder := fixtureFolder(t)
	scanner := NewScanner(folder, 5*time.Second, DefaultLimits())
	path := filepath.Join(folder, "HERO_01_123.pld")
	now := time.Now()

	_, _ = scanner.Scan(context.Background(), now)
	_, firstErr := scanner.Scan(context.Background(), now.Add(5*time.Second))
	writeFixture(t, path, "\xffero data!!")
	failed, failedErr := scanner.Scan(context.Background(), now.Add(6*time.Second))
	writeFixture(t, path, "recovered current save")
	pending, pendingErr := scanner.Scan(context.Background(), now.Add(7*time.Second))
	snapshot, recoveredErr := scanner.Scan(context.Background(), now.Add(12*time.Second))

	if firstErr != nil || failedErr != nil || pendingErr != nil || recoveredErr != nil {
		t.Fatalf("scan errors: %v, %v, %v, %v", firstErr, failedErr, pendingErr, recoveredErr)
	}
	if failed.Files["HERO_01_123.pld"].Text != "hero data!!" || len(failed.Warnings) != 1 || !strings.Contains(failed.Warnings[0], "UTF-8") || pending.Files["HERO_01_123.pld"].Text != "hero data!!" {
		t.Fatal("failed or unsettled replacement displaced the last settled essential version")
	}
	if snapshot.Files["HERO_01_123.pld"].Text != "recovered current save" {
		t.Fatal("recovered contents did not replace the cache after settling")
	}
}

func TestScannerRequiresAccount(t *testing.T) {
	folder := fixtureFolder(t)
	if err := os.Remove(filepath.Join(folder, "ACCOUNT_player_123.pld")); err != nil {
		t.Fatal(err)
	}
	scanner := NewScanner(folder, 0, DefaultLimits())

	_, _ = scanner.Scan(context.Background(), time.Now())
	_, err := scanner.Scan(context.Background(), time.Now())

	if err == nil || errors.Is(err, ErrSettling) {
		t.Fatal("missing account was not rejected")
	}
}

func TestScannerIncludesMultipleAccounts(t *testing.T) {
	folder := fixtureFolder(t)
	writeFixture(t, filepath.Join(folder, "ACCOUNT_other_456.pld"), "account")
	scanner := NewScanner(folder, 0, DefaultLimits())
	now := time.Now()

	_, _ = scanner.Scan(context.Background(), now)
	snapshot, err := scanner.Scan(context.Background(), now)

	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Files) != 3 || snapshot.Files["ACCOUNT_player_123.pld"].Text != "account data" || snapshot.Files["ACCOUNT_other_456.pld"].Text != "account" {
		t.Fatal("snapshot did not include both accounts and the hero")
	}
}

func TestScannerFingerprintIsDeterministic(t *testing.T) {
	folder := fixtureFolder(t)
	scanner := NewScanner(folder, 0, DefaultLimits())
	_, _ = scanner.Scan(context.Background(), time.Now())

	first, firstErr := scanner.Scan(context.Background(), time.Now())
	second, secondErr := scanner.Scan(context.Background(), time.Now())

	if firstErr != nil || secondErr != nil {
		t.Fatalf("scan errors: %v, %v", firstErr, secondErr)
	}
	if first.Fingerprint != second.Fingerprint || first.Fingerprint == "" {
		t.Fatal("unstable fingerprint")
	}
}

func TestScannerFingerprintIgnoresModificationTime(t *testing.T) {
	folder := fixtureFolder(t)
	scanner := NewScanner(folder, 5*time.Second, DefaultLimits())
	now := time.Now()
	path := filepath.Join(folder, "HERO_01_123.pld")
	timestamp := time.UnixMilli(1800000000456)

	_, _ = scanner.Scan(context.Background(), now)
	first, firstErr := scanner.Scan(context.Background(), now.Add(5*time.Second))
	touchErr := os.Chtimes(path, timestamp, timestamp)
	second, secondErr := scanner.Scan(context.Background(), now.Add(6*time.Second))

	if firstErr != nil || touchErr != nil || secondErr != nil {
		t.Fatalf("timestamp-only change restarted content settling: %v, %v, %v", firstErr, touchErr, secondErr)
	}
	if first.Fingerprint != second.Fingerprint {
		t.Fatal("timestamp-only change altered the fingerprint")
	}
	if second.Files["HERO_01_123.pld"].Mtime != timestamp.UnixMilli() {
		t.Fatal("upload payload must retain the updated modification time")
	}
}

func TestScannerReadsMultipleDirectoryBatches(t *testing.T) {
	folder := fixtureFolder(t)
	for i := directoryBatchSize; i > 0; i-- {
		writeFixture(t, filepath.Join(folder, fmt.Sprintf("HERO_%04d.pld", i)), "save")
	}
	scanner := NewScanner(folder, 0, DefaultLimits())
	now := time.Now()

	_, firstErr := scanner.Scan(context.Background(), now)
	snapshot, err := scanner.Scan(context.Background(), now)

	if !errors.Is(firstErr, ErrSettling) || err != nil {
		t.Fatalf("scan errors: %v, %v", firstErr, err)
	}
	if len(snapshot.Files) != directoryBatchSize+2 {
		t.Fatalf("got %d files; want %d", len(snapshot.Files), directoryBatchSize+2)
	}
}

func TestInventorySortsMetadataAcrossBatches(t *testing.T) {
	folder := t.TempDir()
	for i := directoryBatchSize + 1; i > 0; i-- {
		writeFixture(t, filepath.Join(folder, fmt.Sprintf("HERO_%04d.pld", i)), "save")
	}

	entries, err := inventory(context.Background(), folder, DefaultLimits().DirectoryEntries)

	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != directoryBatchSize+1 {
		t.Fatalf("got %d entries", len(entries))
	}
	for i, entry := range entries {
		if entry.Name != fmt.Sprintf("HERO_%04d.pld", i+1) {
			t.Fatalf("entry %d is out of order: %s", i, entry.Name)
		}
	}
}

func TestInventoryAcceptsDirectoryEntryLimit(t *testing.T) {
	folder := t.TempDir()
	limits := smallLimits()
	for i := 0; i < limits.DirectoryEntries; i++ {
		writeFixture(t, filepath.Join(folder, fmt.Sprintf("notes_%04d.txt", i)), "")
	}

	entries, err := inventory(context.Background(), folder, limits.DirectoryEntries)

	if err != nil || len(entries) != 0 {
		t.Fatalf("inventory = %v, %v", entries, err)
	}
}

func TestScannerRejectsTooManyDirectoryEntriesWithoutPartialSnapshot(t *testing.T) {
	folder := fixtureFolder(t)
	limits := smallLimits()
	for i := 0; i < limits.DirectoryEntries-3; i++ {
		writeFixture(t, filepath.Join(folder, fmt.Sprintf("notes_%04d.txt", i)), "")
	}
	scanner := NewScanner(folder, 0, limits)

	snapshot, err := scanner.Scan(context.Background(), time.Now())

	if err == nil || !strings.Contains(err.Error(), "directory entries safety limit") {
		t.Fatalf("scan error = %v", err)
	}
	if snapshot.Files != nil || snapshot.Fingerprint != "" {
		t.Fatal("directory limit returned a partial snapshot")
	}
}

func TestInventoryAcceptsEligibleFileLimit(t *testing.T) {
	folder := t.TempDir()
	limits := smallLimits()
	for i := 0; i < limits.SaveFiles; i++ {
		writeFixture(t, filepath.Join(folder, fmt.Sprintf("HERO_%04d.pld", i)), "")
	}

	entries, err := inventory(context.Background(), folder, limits.DirectoryEntries)

	if err != nil || len(entries) != limits.SaveFiles {
		t.Fatalf("inventory count = %d, error = %v", len(entries), err)
	}
}

func TestScannerRejectsTooManyEligibleFilesWithoutPartialSnapshot(t *testing.T) {
	folder := fixtureFolder(t)
	limits := smallLimits()
	for i := 0; i < limits.SaveFiles-1; i++ {
		writeFixture(t, filepath.Join(folder, fmt.Sprintf("HERO_%04d.pld", i)), "")
	}
	scanner := NewScanner(folder, 0, limits)

	snapshot, err := scanner.Scan(context.Background(), time.Now())

	if err == nil || !strings.Contains(err.Error(), "eligible files safety limit") {
		t.Fatalf("scan error = %v", err)
	}
	if snapshot.Files != nil || snapshot.Fingerprint != "" {
		t.Fatal("eligible file limit returned a partial snapshot")
	}
}

func TestScannerCancellationDoesNotReadMissingFolder(t *testing.T) {
	scanner := NewScanner(filepath.Join(t.TempDir(), "missing"), 0, DefaultLimits())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	snapshot, err := scanner.Scan(ctx, time.Now())

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("scan error = %v; want cancellation", err)
	}
	if snapshot.Files != nil || snapshot.Fingerprint != "" {
		t.Fatal("cancelled scan returned a snapshot")
	}
}

func TestScannerPreservesDeadlineExceeded(t *testing.T) {
	scanner := NewScanner(fixtureFolder(t), 0, DefaultLimits())
	ctx, cancel := context.WithDeadline(context.Background(), time.Time{})
	defer cancel()

	_, err := scanner.Scan(ctx, time.Now())

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("scan error = %v; want deadline exceeded", err)
	}
}

func TestScannerRecoversAfterCancellation(t *testing.T) {
	scanner := NewScanner(fixtureFolder(t), 0, DefaultLimits())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	now := time.Now()

	_, cancelledErr := scanner.Scan(ctx, now)
	_, settlingErr := scanner.Scan(context.Background(), now)
	snapshot, err := scanner.Scan(context.Background(), now)

	if !errors.Is(cancelledErr, context.Canceled) || !errors.Is(settlingErr, ErrSettling) || err != nil {
		t.Fatalf("scan errors: %v, %v, %v", cancelledErr, settlingErr, err)
	}
	if len(snapshot.Files) != 2 || snapshot.Fingerprint == "" {
		t.Fatal("scan did not recover after cancellation")
	}
}

func TestInventoryCancellationDoesNotReadMissingFolder(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "missing")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	entries, err := inventory(ctx, folder, DefaultLimits().DirectoryEntries)

	if !errors.Is(err, context.Canceled) || entries != nil {
		t.Fatalf("inventory = %v, %v; want cancellation", entries, err)
	}
}

func TestReadFileCancellationDoesNotReadMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.pld")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	text, err := readFile(ctx, path, metadata{}, DefaultLimits().FileBytes)

	if !errors.Is(err, context.Canceled) || text != "" {
		t.Fatalf("read = %q, %v; want cancellation", text, err)
	}
}

func TestScannerPreservesBOMAndFingerprintsOriginalText(t *testing.T) {
	folder := fixtureFolder(t)
	scanner := NewScanner(folder, 0, DefaultLimits())
	now := time.Now()
	_, _ = scanner.Scan(context.Background(), now)
	first, firstErr := scanner.Scan(context.Background(), now)
	text := "\ufeffhero data!!"

	writeFixture(t, filepath.Join(folder, "HERO_01_123.pld"), text)
	_, _ = scanner.Scan(context.Background(), now)
	second, secondErr := scanner.Scan(context.Background(), now)

	if firstErr != nil || secondErr != nil {
		t.Fatalf("scan errors: %v, %v", firstErr, secondErr)
	}
	if second.Files["HERO_01_123.pld"].Text != text || first.Fingerprint == second.Fingerprint {
		t.Fatal("scanner normalized save contents or fingerprinted normalized text")
	}
}

func TestEligibleFiltersFiles(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"HERO_01_123.pld", true}, {"account_player_123.PLD", true},
		{"ENTITLEMENTS_player_123_1.pld", true}, {"SETTINGS_123.pld", true}, {"LEGACY_player_123.pld", true},
		{"ACCOUNT_VERIFY_123.pld", true}, {"HERO_01_123.txt", false}, {"private_something.pld", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := eligible(tc.name)
			if got != tc.want {
				t.Fatalf("eligible = %v; want %v", got, tc.want)
			}
		})
	}
}

func fixtureFolder(t *testing.T) string {
	t.Helper()
	folder := t.TempDir()
	writeFixture(t, filepath.Join(folder, "HERO_01_123.pld"), "hero data!!")
	writeFixture(t, filepath.Join(folder, "ACCOUNT_player_123.pld"), "account data")
	writeFixture(t, filepath.Join(folder, "PRIVATE_DATA.pld"), "never upload")
	writeFixture(t, filepath.Join(folder, "notes.txt"), "never upload")
	return folder
}

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	timestamp := time.UnixMilli(1700000000123)
	if err := os.Chtimes(path, timestamp, timestamp); err != nil {
		t.Fatal(err)
	}
}

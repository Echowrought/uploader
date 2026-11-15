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

func TestScannerSupplementaryChangesDoNotRestartEssentialQuietPeriod(t *testing.T) {
	folder := fixtureFolder(t)
	path := filepath.Join(folder, "MISC_player.pld")
	writeFixture(t, path, "initial")
	scanner := NewScanner(folder, 5*time.Second, DefaultLimits())
	now := time.Now()

	_, firstErr := scanner.Scan(context.Background(), now)
	writeFixture(t, path, "changed settings")
	_, earlyErr := scanner.Scan(context.Background(), now.Add(4*time.Second))
	writeFixture(t, path, "changed settings again")
	snapshot, scanErr := scanner.Scan(context.Background(), now.Add(5*time.Second))

	if !errors.Is(firstErr, ErrSettling) || !errors.Is(earlyErr, ErrSettling) || scanErr != nil {
		t.Fatalf("scan errors: %v, %v, %v", firstErr, earlyErr, scanErr)
	}
	if len(snapshot.Files) != 2 || len(snapshot.Warnings) != 1 {
		t.Fatal("supplementary churn blocked essential data or included an unsettled version")
	}
}

func TestScannerNewSupplementaryFilesSettleWithoutBlockingEssentialData(t *testing.T) {
	folder := fixtureFolder(t)
	scanner := NewScanner(folder, 5*time.Second, DefaultLimits())
	now := time.Now()
	_, _ = scanner.Scan(context.Background(), now)
	_, settledErr := scanner.Scan(context.Background(), now.Add(5*time.Second))
	if settledErr != nil {
		t.Fatal(settledErr)
	}

	writeFixture(t, filepath.Join(folder, "MISC_first.pld"), "first misc")
	writeFixture(t, filepath.Join(folder, "MISC_second.pld"), "second misc")
	pending, pendingErr := scanner.Scan(context.Background(), now.Add(6*time.Second))
	snapshot, settledErr := scanner.Scan(context.Background(), now.Add(11*time.Second))

	if pendingErr != nil || settledErr != nil {
		t.Fatalf("scan errors: %v, %v", pendingErr, settledErr)
	}
	if len(pending.Files) != 2 || len(pending.Warnings) != 2 {
		t.Fatal("unsettled supplementary files entered or blocked the essential snapshot")
	}
	if len(snapshot.Files) != 4 || snapshot.Files["MISC_first.pld"].Text != "first misc" || snapshot.Files["MISC_second.pld"].Text != "second misc" || len(snapshot.Warnings) != 0 {
		t.Fatal("settled supplementary versions did not enter the snapshot")
	}
}

func TestScannerSupplementaryTimestampChangeDoesNotChangeFingerprint(t *testing.T) {
	folder := fixtureFolder(t)
	path := filepath.Join(folder, "MISC_player.pld")
	writeFixture(t, path, "misc")
	scanner := NewScanner(folder, 5*time.Second, DefaultLimits())
	now := time.Now()
	_, _ = scanner.Scan(context.Background(), now)
	first, firstErr := scanner.Scan(context.Background(), now.Add(5*time.Second))

	timestamp := time.UnixMilli(1800000000456)
	touchErr := os.Chtimes(path, timestamp, timestamp)
	second, secondErr := scanner.Scan(context.Background(), now.Add(6*time.Second))

	if firstErr != nil || touchErr != nil || secondErr != nil {
		t.Fatalf("errors: %v, %v, %v", firstErr, touchErr, secondErr)
	}
	if first.Fingerprint != second.Fingerprint || len(second.Warnings) != 0 || second.Files["MISC_player.pld"].Mtime != timestamp.UnixMilli() {
		t.Fatal("timestamp-only change delayed or omitted supplementary data")
	}
}

func TestScannerIncludesVerificationAndUnknownPLDFiles(t *testing.T) {
	folder := fixtureFolder(t)
	writeFixture(t, filepath.Join(folder, "ACCOUNT_VERIFY_123.pld"), "verification")
	writeFixture(t, filepath.Join(folder, "MISC_player.pld"), "miscellaneous")
	scanner := NewScanner(folder, 0, DefaultLimits())
	now := time.Now()

	_, _ = scanner.Scan(context.Background(), now)
	snapshot, err := scanner.Scan(context.Background(), now)

	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Files) != 4 || snapshot.Files["ACCOUNT_VERIFY_123.pld"].Text != "verification" || snapshot.Files["MISC_player.pld"].Text != "miscellaneous" || len(snapshot.Warnings) != 0 {
		t.Fatal("verification or unknown non-private PLD data was excluded")
	}
	if _, included := snapshot.Files["PRIVATE_DATA.pld"]; included {
		t.Fatal("private data was uploaded")
	}
}

func TestScannerVerificationDoesNotReplaceRequiredAccount(t *testing.T) {
	folder := fixtureFolder(t)
	if err := os.Remove(filepath.Join(folder, "ACCOUNT_player_123.pld")); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(folder, "ACCOUNT_VERIFY_123.pld"), "verification")
	scanner := NewScanner(folder, 0, DefaultLimits())

	snapshot, err := scanner.Scan(context.Background(), time.Now())

	if err == nil || !strings.Contains(err.Error(), "waiting for HERO_ and ACCOUNT_") || snapshot.Files != nil {
		t.Fatalf("verification was treated as a real account: %v", err)
	}
}

func TestScannerSkipsInvalidSupplementaryUTF8WithoutChangingCoreFingerprint(t *testing.T) {
	folder := fixtureFolder(t)
	scanner := NewScanner(folder, 0, DefaultLimits())
	now := time.Now()
	_, _ = scanner.Scan(context.Background(), now)
	core, err := scanner.Scan(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}

	writeFixture(t, filepath.Join(folder, "MISC_bad.pld"), "\xff")
	snapshot, scanErr := scanner.Scan(context.Background(), now)

	if scanErr != nil {
		t.Fatal(scanErr)
	}
	if len(snapshot.Files) != 2 || snapshot.Fingerprint != core.Fingerprint || len(snapshot.Warnings) != 1 || !strings.Contains(snapshot.Warnings[0], "MISC_bad.pld") || !strings.Contains(snapshot.Warnings[0], "UTF-8") {
		t.Fatalf("bad supplementary data blocked or changed essential data: %+v", snapshot.Warnings)
	}
}

func TestScannerSupplementaryFileRecoversWithoutMetadataChange(t *testing.T) {
	folder := fixtureFolder(t)
	path := filepath.Join(folder, "MISC_bad.pld")
	writeFixture(t, path, "\xff")
	scanner := NewScanner(folder, 0, DefaultLimits())
	now := time.Now()

	_, _ = scanner.Scan(context.Background(), now)
	omitted, omittedErr := scanner.Scan(context.Background(), now)
	writeFixture(t, path, "x")
	_, pendingErr := scanner.Scan(context.Background(), now)
	recovered, recoveredErr := scanner.Scan(context.Background(), now)

	if omittedErr != nil || pendingErr != nil || recoveredErr != nil {
		t.Fatalf("errors: %v, %v, %v", omittedErr, pendingErr, recoveredErr)
	}
	if len(omitted.Warnings) != 1 || len(recovered.Warnings) != 0 || recovered.Files["MISC_bad.pld"].Text != "x" || recovered.Fingerprint == omitted.Fingerprint {
		t.Fatal("supplementary content recovery was missed with unchanged metadata")
	}
}

func TestScannerRejectsInvalidEssentialUTF8WithFilename(t *testing.T) {
	folder := fixtureFolder(t)
	writeFixture(t, filepath.Join(folder, "HERO_01_123.pld"), "\xff")
	scanner := NewScanner(folder, 0, DefaultLimits())
	now := time.Now()

	_, _ = scanner.Scan(context.Background(), now)
	snapshot, err := scanner.Scan(context.Background(), now)

	if err == nil || !strings.Contains(err.Error(), "HERO_01_123.pld") || !strings.Contains(err.Error(), "UTF-8") || snapshot.Files != nil {
		t.Fatalf("essential failure was omitted, unnamed, or returned partial data: %v", err)
	}
}

func TestScannerChecksRequiredNamesBeforeReadingContents(t *testing.T) {
	folder := fixtureFolder(t)
	if err := os.Remove(filepath.Join(folder, "ACCOUNT_player_123.pld")); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(folder, "HERO_01_123.pld"), "\xff")
	scanner := NewScanner(folder, 0, DefaultLimits())

	_, err := scanner.Scan(context.Background(), time.Now())

	if err == nil || !strings.Contains(err.Error(), "waiting for HERO_ and ACCOUNT_") {
		t.Fatalf("scanner read invalid contents before checking the required set: %v", err)
	}
}

func TestScannerSkipsOversizedSupplementaryFile(t *testing.T) {
	folder := fixtureFolder(t)
	limits := smallLimits()
	writeSizedFixture(t, filepath.Join(folder, "AAAA_misc.pld"), limits.FileBytes+1)
	scanner := NewScanner(folder, 0, limits)
	now := time.Now()

	_, _ = scanner.Scan(context.Background(), now)
	snapshot, err := scanner.Scan(context.Background(), now)

	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Files) != 2 || len(snapshot.Warnings) != 1 || !strings.Contains(snapshot.Warnings[0], "AAAA_misc.pld") || !strings.Contains(snapshot.Warnings[0], "64 bytes") {
		t.Fatalf("oversized supplementary data blocked essential saves: %+v", snapshot.Warnings)
	}
}

func TestScannerRejectsOversizedEssentialFileBeforeSettling(t *testing.T) {
	folder := fixtureFolder(t)
	limits := smallLimits()
	writeSizedFixture(t, filepath.Join(folder, "HERO_01_123.pld"), limits.FileBytes+1)
	scanner := NewScanner(folder, 0, limits)

	snapshot, err := scanner.Scan(context.Background(), time.Now())

	if err == nil || !strings.Contains(err.Error(), "HERO_01_123.pld") || !strings.Contains(err.Error(), "64 bytes") || snapshot.Files != nil {
		t.Fatalf("oversized essential file was admitted: %v", err)
	}
}

func TestScannerReservesByteBudgetForEssentialFiles(t *testing.T) {
	folder := fixtureFolder(t)
	limits := smallLimits()
	writeSizedFixture(t, filepath.Join(folder, "ACCOUNT_player_123.pld"), limits.FileBytes)
	for i := 1; i <= 3; i++ {
		writeSizedFixture(t, filepath.Join(folder, fmt.Sprintf("HERO_%02d_123.pld", i)), limits.FileBytes)
	}
	writeFixture(t, filepath.Join(folder, "AAAA_misc.pld"), "optional")
	scanner := NewScanner(folder, 0, limits)
	now := time.Now()

	_, _ = scanner.Scan(context.Background(), now)
	snapshot, err := scanner.Scan(context.Background(), now)

	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Files) != 4 || len(snapshot.Warnings) != 1 || !strings.Contains(snapshot.Warnings[0], "AAAA_misc.pld") {
		t.Fatal("alphabetically earlier supplementary data displaced essential saves")
	}
	if int64(len(snapshot.Files["ACCOUNT_player_123.pld"].Text)) != limits.FileBytes || int64(len(snapshot.Files["HERO_03_123.pld"].Text)) != limits.FileBytes {
		t.Fatal("essential data was truncated to fit supplementary data")
	}
}

func TestScannerRejectsSettledEssentialByteOverflow(t *testing.T) {
	folder := fixtureFolder(t)
	limits := smallLimits()
	writeSizedFixture(t, filepath.Join(folder, "ACCOUNT_player_123.pld"), limits.FileBytes)
	for i := 1; i <= 4; i++ {
		writeSizedFixture(t, filepath.Join(folder, fmt.Sprintf("HERO_%02d_123.pld", i)), limits.FileBytes)
	}
	scanner := NewScanner(folder, 0, limits)

	_, _ = scanner.Scan(context.Background(), time.Now())
	snapshot, err := scanner.Scan(context.Background(), time.Now())

	if err == nil || !strings.Contains(err.Error(), "256 bytes") || snapshot.Files != nil {
		t.Fatalf("settled essential byte overflow returned a partial snapshot: %v", err)
	}
	var cached int64
	for _, state := range scanner.settlements {
		cached += settledSize(state.settled)
	}
	if cached > limits.SnapshotBytes {
		t.Fatal("over-limit essential versions exceeded the retained cache budget")
	}
}

func TestScannerReservesFileBudgetForEssentialFiles(t *testing.T) {
	folder := fixtureFolder(t)
	limits := smallLimits()
	for i := 0; i < limits.SaveFiles-1; i++ {
		writeFixture(t, filepath.Join(folder, fmt.Sprintf("AAAA_%04d.pld", i)), "")
	}
	scanner := NewScanner(folder, 0, limits)
	now := time.Now()

	_, _ = scanner.Scan(context.Background(), now)
	snapshot, err := scanner.Scan(context.Background(), now)

	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Files) != limits.SaveFiles || len(snapshot.Warnings) != 1 || snapshot.Files["HERO_01_123.pld"].Text != "hero data!!" || snapshot.Files["ACCOUNT_player_123.pld"].Text != "account data" {
		t.Fatal("supplementary file count blocked or displaced essential saves")
	}
	assertSettledCacheWithinLimits(t, scanner, limits)
}

func TestScannerNewEssentialVersionEvictsSupplementaryCacheFirst(t *testing.T) {
	folder := fixtureFolder(t)
	limits := smallLimits()
	for i := 0; i < limits.SaveFiles-1; i++ {
		writeFixture(t, filepath.Join(folder, fmt.Sprintf("AAAA_%04d.pld", i)), "")
	}
	scanner := NewScanner(folder, 0, limits)
	now := time.Now()
	_, _ = scanner.Scan(context.Background(), now)
	first, firstErr := scanner.Scan(context.Background(), now)

	writeFixture(t, filepath.Join(folder, "HERO_02_123.pld"), "new hero")
	blocked, addedErr := scanner.Scan(context.Background(), now)
	second, secondErr := scanner.Scan(context.Background(), now)

	if firstErr != nil || !errors.Is(addedErr, ErrSettling) || secondErr != nil {
		t.Fatalf("scan errors: %v, %v, %v", firstErr, addedErr, secondErr)
	}
	if blocked.Files != nil || len(second.Files) != limits.SaveFiles || second.Files["HERO_02_123.pld"].Text != "new hero" || len(second.Warnings) != 2 {
		t.Fatal("cached supplementary versions displaced a new essential version")
	}
	if _, exists := first.Files[fmt.Sprintf("AAAA_%04d.pld", limits.SaveFiles-3)]; !exists {
		t.Fatal("cache eviction mutated an older snapshot")
	}
	assertSettledCacheWithinLimits(t, scanner, limits)
}

func writeSizedFixture(t *testing.T, path string, size int64) {
	t.Helper()
	writeFixture(t, path, "")
	if err := os.Truncate(path, size); err != nil {
		t.Fatal(err)
	}
}

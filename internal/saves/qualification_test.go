package saves

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScannerCombinesLastSettledVersionsAndKeepsTheirModificationTimes(t *testing.T) {
	folder := fixtureFolder(t)
	hero, account := filepath.Join(folder, "HERO_01_123.pld"), filepath.Join(folder, "ACCOUNT_player_123.pld")
	writeFixture(t, hero, "123")
	writeFixture(t, account, "ABC")
	setFixtureTime(t, account)
	scanner := NewScanner(folder, 5*time.Second, DefaultLimits())
	now := time.Now()
	updatedTime := time.UnixMilli(1800000000456)

	_, _ = scanner.Scan(context.Background(), now)
	first, firstErr := scanner.Scan(context.Background(), now.Add(5*time.Second))
	writeFixture(t, hero, "456")
	pending, pendingErr := scanner.Scan(context.Background(), now.Add(6*time.Second))
	writeFixture(t, account, "DEF")
	touchErr := os.Chtimes(account, updatedTime, updatedTime)
	_, _ = scanner.Scan(context.Background(), now.Add(10*time.Second))
	second, secondErr := scanner.Scan(context.Background(), now.Add(11*time.Second))
	third, thirdErr := scanner.Scan(context.Background(), now.Add(15*time.Second))

	if firstErr != nil || pendingErr != nil || touchErr != nil || secondErr != nil || thirdErr != nil {
		t.Fatalf("errors: %v, %v, %v, %v, %v", firstErr, pendingErr, touchErr, secondErr, thirdErr)
	}
	if first.Files["HERO_01_123.pld"].Text != "123" || first.Files["ACCOUNT_player_123.pld"].Text != "ABC" || pending.Fingerprint != first.Fingerprint {
		t.Fatal("unsettled content leaked into a snapshot or an older snapshot was mutated")
	}
	if second.Files["HERO_01_123.pld"].Text != "456" || second.Files["ACCOUNT_player_123.pld"].Text != "ABC" || second.Files["ACCOUNT_player_123.pld"].Mtime != 1700000000123 {
		t.Fatal("snapshot did not combine f1's new settled version with f2's previous settled version")
	}
	if third.Files["ACCOUNT_player_123.pld"].Text != "DEF" || third.Files["ACCOUNT_player_123.pld"].Mtime != updatedTime.UnixMilli() || third.Fingerprint == second.Fingerprint {
		t.Fatal("f2's newly settled contents and timestamp did not replace its cache")
	}
}

func TestScannerEssentialFilesSettleWithoutOverlappingQuietPeriods(t *testing.T) {
	folder := fixtureFolder(t)
	scanner := NewScanner(folder, 5*time.Second, DefaultLimits())
	hero, account := filepath.Join(folder, "HERO_01_123.pld"), filepath.Join(folder, "ACCOUNT_player_123.pld")
	now := time.Now()

	_, _ = scanner.Scan(context.Background(), now)
	writeFixture(t, account, "new account")
	_, accountErr := scanner.Scan(context.Background(), now.Add(4*time.Second))
	_, heroErr := scanner.Scan(context.Background(), now.Add(5*time.Second))
	writeFixture(t, hero, "latest hero")
	blocked, blockedErr := scanner.Scan(context.Background(), now.Add(6*time.Second))
	snapshot, readyErr := scanner.Scan(context.Background(), now.Add(9*time.Second))

	if !errors.Is(accountErr, ErrSettling) || !errors.Is(heroErr, ErrSettling) || !errors.Is(blockedErr, ErrSettling) || readyErr != nil {
		t.Fatalf("scan errors: %v, %v, %v, %v", accountErr, heroErr, blockedErr, readyErr)
	}
	if blocked.Files != nil || len(snapshot.Files) != 2 || snapshot.Files["HERO_01_123.pld"].Text != "hero data!!" || snapshot.Files["ACCOUNT_player_123.pld"].Text != "new account" {
		t.Fatal("initial gate omitted an essential file or required overlapping quiet periods")
	}
}

func TestScannerNewEssentialFileReopensGateWithoutLosingSettledVersions(t *testing.T) {
	folder := fixtureFolder(t)
	scanner := NewScanner(folder, 5*time.Second, DefaultLimits())
	now := time.Now()
	_, _ = scanner.Scan(context.Background(), now)
	_, firstErr := scanner.Scan(context.Background(), now.Add(5*time.Second))

	writeFixture(t, filepath.Join(folder, "HERO_02_123.pld"), "new hero")
	blocked, addedErr := scanner.Scan(context.Background(), now.Add(6*time.Second))
	writeFixture(t, filepath.Join(folder, "HERO_01_123.pld"), "updated existing hero")
	_, earlyErr := scanner.Scan(context.Background(), now.Add(10*time.Second))
	snapshot, readyErr := scanner.Scan(context.Background(), now.Add(11*time.Second))

	if firstErr != nil || !errors.Is(addedErr, ErrSettling) || !errors.Is(earlyErr, ErrSettling) || readyErr != nil {
		t.Fatalf("scan errors: %v, %v, %v, %v", firstErr, addedErr, earlyErr, readyErr)
	}
	if blocked.Files != nil || len(snapshot.Files) != 3 || snapshot.Files["HERO_01_123.pld"].Text != "hero data!!" || snapshot.Files["HERO_02_123.pld"].Text != "new hero" {
		t.Fatal("new essential file bypassed the gate or unsettled bytes replaced a cached version")
	}
}

func TestScannerUnsettledReadFailureDoesNotBlockOtherFilesFromSettling(t *testing.T) {
	folder := fixtureFolder(t)
	scanner := NewScanner(folder, 5*time.Second, DefaultLimits())
	hero, account := filepath.Join(folder, "HERO_01_123.pld"), filepath.Join(folder, "ACCOUNT_player_123.pld")
	now := time.Now()

	_, _ = scanner.Scan(context.Background(), now)
	writeFixture(t, account, "\xff")
	_, failedErr := scanner.Scan(context.Background(), now.Add(5*time.Second))
	writeFixture(t, account, "recovered account")
	_, recoveredErr := scanner.Scan(context.Background(), now.Add(6*time.Second))
	writeFixture(t, hero, "recent hero change")
	_, earlyErr := scanner.Scan(context.Background(), now.Add(10*time.Second))
	snapshot, readyErr := scanner.Scan(context.Background(), now.Add(11*time.Second))

	if failedErr == nil || errors.Is(failedErr, ErrSettling) || !errors.Is(recoveredErr, ErrSettling) || !errors.Is(earlyErr, ErrSettling) || readyErr != nil {
		t.Fatalf("scan errors: %v, %v, %v, %v", failedErr, recoveredErr, earlyErr, readyErr)
	}
	if snapshot.Files["HERO_01_123.pld"].Text != "hero data!!" || snapshot.Files["ACCOUNT_player_123.pld"].Text != "recovered account" {
		t.Fatal("a failed candidate prevented another file's settled version from being cached")
	}
}

func TestScannerReappearingEssentialFilenameMustSettleAgain(t *testing.T) {
	folder := fixtureFolder(t)
	scanner := NewScanner(folder, 5*time.Second, DefaultLimits())
	path := filepath.Join(folder, "HERO_01_123.pld")
	now := time.Now()
	_, _ = scanner.Scan(context.Background(), now)
	_, firstErr := scanner.Scan(context.Background(), now.Add(5*time.Second))

	removeErr := os.Remove(path)
	_, missingErr := scanner.Scan(context.Background(), now.Add(6*time.Second))
	writeFixture(t, path, "hero data!!")
	blocked, returnedErr := scanner.Scan(context.Background(), now.Add(7*time.Second))
	snapshot, readyErr := scanner.Scan(context.Background(), now.Add(12*time.Second))

	if firstErr != nil || removeErr != nil || missingErr == nil || !errors.Is(returnedErr, ErrSettling) || readyErr != nil {
		t.Fatalf("errors: %v, %v, %v, %v, %v", firstErr, removeErr, missingErr, returnedErr, readyErr)
	}
	if blocked.Files != nil || snapshot.Files["HERO_01_123.pld"].Text != "hero data!!" {
		t.Fatal("a reappearing essential filename inherited a removed cache")
	}
}

func TestScannerRemovedUnsettledFileNoLongerBlocksGate(t *testing.T) {
	folder := fixtureFolder(t)
	scanner := NewScanner(folder, 5*time.Second, DefaultLimits())
	path := filepath.Join(folder, "HERO_02_123.pld")
	now := time.Now()
	_, _ = scanner.Scan(context.Background(), now)
	_, firstErr := scanner.Scan(context.Background(), now.Add(5*time.Second))

	writeFixture(t, path, "new hero")
	_, addedErr := scanner.Scan(context.Background(), now.Add(6*time.Second))
	removeErr := os.Remove(path)
	snapshot, readyErr := scanner.Scan(context.Background(), now.Add(7*time.Second))

	if firstErr != nil || !errors.Is(addedErr, ErrSettling) || removeErr != nil || readyErr != nil {
		t.Fatalf("errors: %v, %v, %v, %v", firstErr, addedErr, removeErr, readyErr)
	}
	if len(snapshot.Files) != 2 {
		t.Fatal("a removed file still blocked the current essential set")
	}
}

func TestNewScannerDoesNotInheritSettledVersions(t *testing.T) {
	folder := fixtureFolder(t)
	first := NewScanner(folder, 5*time.Second, DefaultLimits())
	second := NewScanner(folder, 5*time.Second, DefaultLimits())
	now := time.Now()

	_, _ = first.Scan(context.Background(), now)
	_, firstErr := first.Scan(context.Background(), now.Add(5*time.Second))
	blocked, restartedErr := second.Scan(context.Background(), now.Add(6*time.Second))
	snapshot, readyErr := second.Scan(context.Background(), now.Add(11*time.Second))

	if firstErr != nil || !errors.Is(restartedErr, ErrSettling) || readyErr != nil {
		t.Fatalf("scan errors: %v, %v, %v", firstErr, restartedErr, readyErr)
	}
	if blocked.Files != nil || len(snapshot.Files) != 2 {
		t.Fatal("a fresh scanner bypassed initial settlement")
	}
}

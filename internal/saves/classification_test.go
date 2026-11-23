package saves

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestEssentialClassifiesKnownCategories(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"HERO_01_123.pld", true},
		{"ACCOUNT_player_123.pld", true},
		{"ACCOUNT_VERIFY_123.pld", true},
		{"ENTITLEMENTS_player_123.pld", true},
		{"SETTINGS_player.pld", true},
		{"LEGACY_player.pld", true},
		{"hero_01_123.PLD", true},
		{"account_player_123.PLD", true},
		{"account_verify_123.PLD", true},
		{"entitlements_player_123.PLD", true},
		{"settings_player.PLD", true},
		{"legacy_player.PLD", true},
		{"MISC_player.pld", false},
		{"SETTINGSOTHER_player.pld", false},
		{"PRIVATE_DATA.pld", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := essential(tc.name)

			if got != tc.want {
				t.Fatalf("essential(%q) = %v; want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestSelectionOrdersScrambledInventoryWithinEachCategory(t *testing.T) {
	entries := []metadata{
		{Name: "MISC_z.pld"}, {Name: "HERO_z.pld"},
		{Name: "MISC_a.pld"}, {Name: "ACCOUNT_player.pld"},
		{Name: "MISC_m.pld"}, {Name: "HERO_a.pld"},
	}

	core, extra := splitFiles(entries)

	wantCore := []metadata{{Name: "ACCOUNT_player.pld"}, {Name: "HERO_a.pld"}, {Name: "HERO_z.pld"}}
	wantExtra := []metadata{{Name: "MISC_a.pld"}, {Name: "MISC_m.pld"}, {Name: "MISC_z.pld"}}
	if !reflect.DeepEqual(core, wantCore) || !reflect.DeepEqual(extra, wantExtra) {
		t.Fatalf("core=%v extra=%v; selection depends on enumeration order", core, extra)
	}
}

func TestScannerKnownCategoryWithoutSettledVersionBlocksSnapshot(t *testing.T) {
	// Classification is tested for every category above. Exercise the scanner's
	// use of that classification with one mixed-case, non-hero example.
	const name = "account_verify_extra.PLD"
	folder := fixtureFolder(t)
	scanner := NewScanner(folder, 5*time.Second, DefaultLimits())
	now := time.Now()

	_, _ = scanner.Scan(context.Background(), now)
	_, initialErr := scanner.Scan(context.Background(), now.Add(5*time.Second))
	writeFixture(t, filepath.Join(folder, name), "new essential data")
	blocked, pendingErr := scanner.Scan(context.Background(), now.Add(6*time.Second))
	snapshot, settledErr := scanner.Scan(context.Background(), now.Add(11*time.Second))

	if initialErr != nil || !errors.Is(pendingErr, ErrSettling) || settledErr != nil {
		t.Fatalf("scan errors: %v, %v, %v", initialErr, pendingErr, settledErr)
	}
	if blocked.Files != nil || blocked.Fingerprint != "" {
		t.Fatal("unsettled essential category was omitted from a partial snapshot")
	}
	if len(snapshot.Files) != 3 || snapshot.Files[name].Text != "new essential data" || len(snapshot.Warnings) != 0 {
		t.Fatalf("settled essential category was not included: %+v", snapshot)
	}
}

func TestScannerInvalidKnownCategoryWithoutCacheBlocksSnapshot(t *testing.T) {
	for _, name := range []string{
		"ACCOUNT_VERIFY_123.pld",
		"ENTITLEMENTS_player_123.pld",
		"SETTINGS_player.pld",
		"LEGACY_player.pld",
	} {
		t.Run(name, func(t *testing.T) {
			folder := fixtureFolder(t)
			writeFixture(t, filepath.Join(folder, name), "\xff")
			scanner := NewScanner(folder, 0, DefaultLimits())

			snapshot, err := scanner.Scan(context.Background(), time.Now())

			if err == nil || !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), "UTF-8") {
				t.Fatalf("invalid essential category did not produce a named read error: %v", err)
			}
			if snapshot.Files != nil || snapshot.Fingerprint != "" {
				t.Fatal("invalid essential category was omitted from a partial snapshot")
			}
		})
	}
}

func TestScannerInvalidKnownCategoryRetainsSettledVersion(t *testing.T) {
	for _, name := range []string{
		"ACCOUNT_VERIFY_123.pld",
		"ENTITLEMENTS_player_123.pld",
		"SETTINGS_player.pld",
		"LEGACY_player.pld",
	} {
		t.Run(name, func(t *testing.T) {
			folder := fixtureFolder(t)
			path := filepath.Join(folder, name)
			writeFixture(t, path, "settled essential data")
			scanner := NewScanner(folder, 0, DefaultLimits())
			now := time.Now()

			_, _ = scanner.Scan(context.Background(), now)
			first, initialErr := scanner.Scan(context.Background(), now)
			writeFixture(t, path, "\xff")
			fallback, fallbackErr := scanner.Scan(context.Background(), now)

			if initialErr != nil || fallbackErr != nil {
				t.Fatalf("scan errors: %v, %v", initialErr, fallbackErr)
			}
			if len(fallback.Files) != 3 || fallback.Files[name].Text != "settled essential data" || fallback.Fingerprint != first.Fingerprint {
				t.Fatal("invalid candidate displaced or omitted the settled essential version")
			}
			if len(fallback.Warnings) != 1 || !strings.Contains(fallback.Warnings[0], name) || !strings.Contains(fallback.Warnings[0], "use settled save") {
				t.Fatalf("missing essential fallback diagnostic: %v", fallback.Warnings)
			}
		})
	}
}

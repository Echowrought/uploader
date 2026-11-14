package saves

import (
	"crypto/sha256"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSettleCandidateTransitions(t *testing.T) {
	start := time.Unix(1700000000, 0)
	old := &File{Text: "old", Mtime: 1}
	current := File{Text: "new", Mtime: 2}
	hash := sha256.Sum256([]byte(current.Text))
	oldHash := sha256.Sum256([]byte(old.Text))
	cases := []struct {
		name     string
		previous fileSettlement
		now      time.Time
		want     fileSettlement
	}{
		{
			name: "first observation starts the timer",
			now:  start,
			want: fileSettlement{hash: hash, since: start, observed: true},
		},
		{
			name:     "matching candidate waits for quiet period",
			previous: fileSettlement{hash: hash, since: start, observed: true},
			now:      start.Add(4 * time.Second),
			want:     fileSettlement{hash: hash, since: start, observed: true},
		},
		{
			name:     "matching candidate settles at quiet boundary",
			previous: fileSettlement{hash: hash, since: start, observed: true},
			now:      start.Add(5 * time.Second),
			want:     fileSettlement{hash: hash, since: start, observed: true, settled: &current},
		},
		{
			name:     "changed candidate restarts timer and retains old version",
			previous: fileSettlement{hash: oldHash, since: start, observed: true, settled: old},
			now:      start.Add(5 * time.Second),
			want:     fileSettlement{hash: hash, since: start.Add(5 * time.Second), observed: true, settled: old},
		},
		{
			name:     "interrupted observation restarts timer",
			previous: fileSettlement{hash: hash, since: start, observed: false, settled: old},
			now:      start.Add(5 * time.Second),
			want:     fileSettlement{hash: hash, since: start.Add(5 * time.Second), observed: true, settled: old},
		},
		{
			name:     "settled replacement uses current metadata",
			previous: fileSettlement{hash: hash, since: start, observed: true, settled: old},
			now:      start.Add(5 * time.Second),
			want:     fileSettlement{hash: hash, since: start, observed: true, settled: &current},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := settle(tc.previous, current, tc.now, 5*time.Second)

			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("settlement = %+v; want %+v", got, tc.want)
			}
		})
	}
}

func TestSettleKnownVersionRefreshesMetadataWithoutMutatingPreviousVersion(t *testing.T) {
	start := time.Unix(1700000000, 0)
	old := &File{Text: "settled", Mtime: 1}
	previous := fileSettlement{settled: old}
	current := File{Text: old.Text, Mtime: 2}

	next := settle(previous, current, start, 5*time.Second)

	if next.settled == nil || *next.settled != current || !next.observed || next.since != start || next.hash != sha256.Sum256([]byte(current.Text)) {
		t.Fatalf("known version was not immediately refreshed: %+v", next)
	}
	if *old != (File{Text: "settled", Mtime: 1}) {
		t.Fatal("refresh mutated the previous settled version")
	}
}

func TestAdmissionEssentialReplacementUsesReservedCapacity(t *testing.T) {
	previous := fileSettlement{settled: &File{Text: "old", Mtime: 1}}
	candidate := fileSettlement{observed: true, settled: &File{Text: "next", Mtime: 2}}
	result := scanResult{states: make(map[string]fileSettlement), files: make(map[string]File), bytes: maxSnapshotBytes - 1}

	err := result.admitEssential("HERO_1.pld", previous, candidate, nil)

	if err != nil {
		t.Fatal(err)
	}
	if result.bytes != maxSnapshotBytes || result.files["HERO_1.pld"] != *candidate.settled || result.states["HERO_1.pld"] != candidate || len(result.warnings) != 0 {
		t.Fatal("essential replacement did not reuse its reserved capacity")
	}
}

func TestAdmissionEssentialOverflowRetainsPreviousVersionAndCandidateObservation(t *testing.T) {
	previous := fileSettlement{settled: &File{Text: "old", Mtime: 1}}
	candidate := fileSettlement{hash: sha256.Sum256([]byte("next")), observed: true, settled: &File{Text: "next", Mtime: 2}}
	result := scanResult{states: make(map[string]fileSettlement), files: make(map[string]File), bytes: maxSnapshotBytes}

	err := result.admitEssential("HERO_1.pld", previous, candidate, nil)
	retained := result.states["HERO_1.pld"]

	if err == nil || !strings.Contains(err.Error(), "32 MiB") {
		t.Fatalf("essential overflow did not block publication: %v", err)
	}
	if result.bytes != maxSnapshotBytes || result.files["HERO_1.pld"] != *previous.settled || retained.settled != previous.settled || retained.hash != candidate.hash || !retained.observed {
		t.Fatal("overflow lost the old payload or candidate observation, or exceeded capacity")
	}
}

func TestAdmissionEssentialFailedCandidateRetainsVersionWithWarning(t *testing.T) {
	previous := fileSettlement{settled: &File{Text: "old", Mtime: 1}}
	result := scanResult{states: make(map[string]fileSettlement), files: make(map[string]File), bytes: 3}

	err := result.admitEssential("HERO_1.pld", previous, previous, errors.New("read failed"))

	if err != nil || result.files["HERO_1.pld"] != *previous.settled || result.bytes != 3 {
		t.Fatal("candidate failure displaced a usable essential version")
	}
	if len(result.warnings) != 1 || result.warnings[0] != `use settled save "HERO_1.pld": read failed` {
		t.Fatalf("missing essential fallback diagnostic: %v", result.warnings)
	}
}

func TestAdmissionSupplementaryReplacementFallsBackWhenPreviousVersionFits(t *testing.T) {
	previous := fileSettlement{settled: &File{Text: "old", Mtime: 1}}
	candidate := fileSettlement{observed: true, settled: &File{Text: "larger", Mtime: 2}}
	result := scanResult{states: make(map[string]fileSettlement), files: make(map[string]File), bytes: maxSnapshotBytes - 4}

	result.admitSupplementary("MISC.pld", previous, candidate, nil)

	if result.bytes != maxSnapshotBytes-1 || result.files["MISC.pld"] != *previous.settled || result.states["MISC.pld"].settled != previous.settled {
		t.Fatal("oversized supplementary replacement did not fall back to the fitting version")
	}
	if len(result.warnings) != 1 || !strings.Contains(result.warnings[0], `use settled save "MISC.pld"`) {
		t.Fatalf("missing supplementary fallback diagnostic: %v", result.warnings)
	}
}

func TestAdmissionSupplementaryEvictsVersionsWhenNeitherFits(t *testing.T) {
	previous := fileSettlement{settled: &File{Text: "old", Mtime: 1}}
	candidate := fileSettlement{observed: true, settled: &File{Text: "larger", Mtime: 2}}
	result := scanResult{states: make(map[string]fileSettlement), files: make(map[string]File), bytes: maxSnapshotBytes - 2}

	result.admitSupplementary("MISC.pld", previous, candidate, nil)
	_, included := result.files["MISC.pld"]

	if included || result.states["MISC.pld"].settled != nil || !result.states["MISC.pld"].observed || result.bytes != maxSnapshotBytes-2 {
		t.Fatal("unfitting optional payload was retained or its observation was lost")
	}
	if len(result.warnings) != 1 || !strings.Contains(result.warnings[0], `skip supplementary save "MISC.pld"`) {
		t.Fatalf("missing supplementary omission diagnostic: %v", result.warnings)
	}
}

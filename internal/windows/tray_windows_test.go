package windows

import (
	"strings"
	"testing"
	"unicode/utf16"

	"cwgrpguploader/internal/leaderboard"
	"cwgrpguploader/internal/storage"
)

func TestAccountMenuOnlyOffersAuthenticationWhenNeeded(t *testing.T) {
	account, err := leaderboard.OpenAccount(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	missing, canConnect := accountMenu(account, false)
	pending, canStartAgain := accountMenu(account, true)
	saveErr := account.SetCredential(storage.Credential{Token: strings.Repeat("A", 43), BattleTag: "Tester#1234"})
	connected, canReconnect := accountMenu(account, false)

	if saveErr != nil {
		t.Fatal(saveErr)
	}
	if missing != "Authenticate..." || !canConnect || pending != "Authentication pending" || canStartAgain || connected != "Connected as Tester#1234" || canReconnect {
		t.Fatalf("missing=%q (%t), pending=%q (%t), connected=%q (%t)", missing, canConnect, pending, canStartAgain, connected, canReconnect)
	}
}

func TestAuthenticationErrorBadgeTracksWhetherAutomaticUploadsAreBlocked(t *testing.T) {
	account, err := leaderboard.OpenAccount(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	missing := automaticUploadProblem(true, account)
	paused := automaticUploadProblem(false, account)
	saveErr := account.SetCredential(storage.Credential{Token: strings.Repeat("A", 43), BattleTag: "Tester#1234"})
	connected := automaticUploadProblem(true, account)

	if missing == "" || paused != "" || saveErr != nil || connected != "" {
		t.Fatalf("missing=%q paused=%q save=%v connected=%q", missing, paused, saveErr, connected)
	}
}

func TestTooltipFitsWindowsUTF16Buffer(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{"short", "Ready", "Ready"},
		{"controls", "one\nline\x00", "one line "},
		{"ASCII at limit", strings.Repeat("x", 127), strings.Repeat("x", 127)},
		{"ASCII over limit", strings.Repeat("x", 128), strings.Repeat("x", 126) + "…"},
		{"BMP at limit", strings.Repeat("界", 127), strings.Repeat("界", 127)},
		{"surrogate pairs at limit", strings.Repeat("😀", 63) + "x", strings.Repeat("😀", 63) + "x"},
		{"surrogate pairs over limit", strings.Repeat("😀", 64), strings.Repeat("😀", 63) + "…"},
		{"mixed boundary", "x" + strings.Repeat("😀", 64), "x" + strings.Repeat("😀", 62) + "…"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text := tc.text

			got := tooltip(text)

			if got != tc.want {
				t.Fatalf("tooltip=%q, want=%q", got, tc.want)
			}
			if units := len(utf16.Encode([]rune(got))); units > 127 {
				t.Fatalf("tooltip exceeds Windows buffer: %d UTF-16 units", units)
			}
		})
	}
}

func TestMenuDisplayKeepsRuneBasedLimit(t *testing.T) {
	text := strings.Repeat("😀", 91)

	got := display(text, 90)

	if got != strings.Repeat("😀", 89)+"…" {
		t.Fatalf("unexpected menu title: %q", got)
	}
}

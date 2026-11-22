package leaderboard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAccountWithUnreadableCredentialDoesNotRequestStartupPrompt(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "uploader-token.json"), 0700); err != nil {
		t.Fatal(err)
	}

	account, err := OpenAccount(dir)

	if err == nil || account.MissingCredential() || account.Status() != "Reconnect required" {
		t.Fatalf("load error=%v, missing=%t, status=%q", err, account.MissingCredential(), account.Status())
	}
}

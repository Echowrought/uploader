package leaderboard

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cwgrpguploader/internal/storage"
)

func TestOpenAccountWithNoCredentialRequiresAuthentication(t *testing.T) {
	dir := t.TempDir()

	account, err := OpenAccount(dir)
	token, uploadErr := account.uploadToken()

	if err != nil || !errors.Is(uploadErr, ErrAuthenticationRequired) || token != "" || !account.MissingCredential() {
		t.Fatalf("open=%v, upload=%v, missing=%t", err, uploadErr, account.MissingCredential())
	}
}

func TestDisconnectRemovesCredentialAndBlocksUploads(t *testing.T) {
	dir := t.TempDir()
	saveTestCredential(t, dir)
	account, err := OpenAccount(dir)
	if err != nil {
		t.Fatal(err)
	}

	disconnectErr := account.Disconnect()
	_, uploadErr := account.uploadToken()
	restored, openErr := OpenAccount(dir)

	if disconnectErr != nil || !errors.Is(uploadErr, ErrAuthenticationRequired) || openErr != nil || !restored.MissingCredential() {
		t.Fatalf("disconnect=%v, upload=%v, open=%v, missing=%t", disconnectErr, uploadErr, openErr, restored.MissingCredential())
	}
}

func TestFailedCredentialSavePreservesConnectedAccount(t *testing.T) {
	dir := t.TempDir()
	existing := saveTestCredential(t, dir)
	account, err := OpenAccount(dir)
	if err != nil {
		t.Fatal(err)
	}
	path := storage.CredentialPath(dir)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	replacement := storage.Credential{Token: strings.Repeat("B", 43), BattleTag: "Other#1234"}

	saveErr := account.SetCredential(replacement)
	token, uploadErr := account.uploadToken()

	if saveErr == nil || uploadErr != nil || token != existing.Token || account.Status() != "Connected as Tester#1234" {
		t.Fatalf("save=%v upload=%v status=%q", saveErr, uploadErr, account.Status())
	}
}

func TestOpenAccountWithInvalidCredentialBlocksUploads(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "uploader-token.json"), 0700); err != nil {
		t.Fatal(err)
	}

	account, err := OpenAccount(dir)

	_, uploadErr := account.uploadToken()

	if err == nil || !errors.Is(uploadErr, ErrAuthenticationRejected) || account.MissingCredential() || account.Status() != "Reconnect required" {
		t.Fatalf("open=%v, upload=%v, missing=%t, status=%q", err, uploadErr, account.MissingCredential(), account.Status())
	}
}

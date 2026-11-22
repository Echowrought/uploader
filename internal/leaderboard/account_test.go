package leaderboard

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
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

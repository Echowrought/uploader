package storage

import (
	"errors"
	"os"
	"testing"
)

func TestLoadCredentialReportsMissingFile(t *testing.T) {
	dir := t.TempDir()

	credential, err := LoadCredential(dir)

	if !errors.Is(err, os.ErrNotExist) || credential != (Credential{}) {
		t.Fatalf("missing credential: error=%v, empty=%t", err, credential == (Credential{}))
	}
}

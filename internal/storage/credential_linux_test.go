package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCredentialFileIsOwnerOnlyAndRejectsBroaderPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, credentialFile)
	credential := Credential{Token: strings.Repeat("A", 43), BattleTag: "Tester#1234"}

	if err := SaveCredential(dir, credential); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	saved, loadErr := LoadCredential(dir)
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	_, unsafeErr := LoadCredential(dir)

	if info.Mode().Perm() != 0600 || saved != credential || loadErr != nil || unsafeErr == nil {
		t.Fatalf("mode=%o, load=%v, unsafe load=%v", info.Mode().Perm(), loadErr, unsafeErr)
	}
}

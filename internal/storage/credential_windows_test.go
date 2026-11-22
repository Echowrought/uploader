package storage

import (
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestCredentialReplacementRetainsProtectedUserOnlyDACL(t *testing.T) {
	dir := t.TempDir()
	first := Credential{Token: strings.Repeat("A", 43), BattleTag: "Tester#1234"}
	second := Credential{Token: strings.Repeat("B", 43), BattleTag: "Tester#1234"}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	want, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCredential(dir, first); err != nil {
		t.Fatal(err)
	}

	saveErr := SaveCredential(dir, second)
	sd, aclErr := windows.GetNamedSecurityInfo(filepath.Join(dir, credentialFile), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	got, loadErr := LoadCredential(dir)

	if saveErr != nil || aclErr != nil || loadErr != nil {
		t.Fatalf("save=%v, ACL=%v, load=%v", saveErr, aclErr, loadErr)
	}
	control, _, err := sd.Control()
	_, gotACE, _ := strings.Cut(sd.String(), "(")
	_, wantACE, _ := strings.Cut(want.String(), "(")
	if got != second || err != nil || control&windows.SE_DACL_PROTECTED == 0 || gotACE != wantACE {
		t.Fatalf("credential did not rotate with the protected user-only DACL: %s", sd)
	}
}

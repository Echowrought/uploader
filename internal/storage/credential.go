package storage

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

const credentialFile = "uploader-token.json"

var (
	uploadToken = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	battleTag   = regexp.MustCompile(`^[^#\s]{1,24}#[0-9]{3,8}$`)
)

type Credential struct {
	Token     string `json:"token"`
	BattleTag string `json:"battletag"`
}

func (c Credential) Valid() bool {
	return uploadToken.MatchString(c.Token) && battleTag.MatchString(c.BattleTag) && strings.IndexFunc(c.BattleTag, unicode.IsControl) == -1
}

func LoadCredential(dir string) (Credential, error) {
	path := filepath.Join(dir, credentialFile)
	info, err := os.Lstat(path)
	if err != nil {
		return Credential{}, err
	}
	if !info.Mode().IsRegular() || !privateCredentialMode(info.Mode()) {
		return Credential{}, errors.New("credential file is not a private regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return Credential{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return Credential{}, err
	}
	var credential Credential
	if len(data) > 4096 || json.Unmarshal(data, &credential) != nil || !credential.Valid() {
		return Credential{}, errors.New("credential file is invalid")
	}
	return credential, nil
}

func SaveCredential(dir string, credential Credential) error {
	if !credential.Valid() {
		return errors.New("invalid uploader credential")
	}
	return writeJSON(filepath.Join(dir, credentialFile), credential, protectCredential)
}

func RemoveCredential(dir string) error {
	err := os.Remove(filepath.Join(dir, credentialFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

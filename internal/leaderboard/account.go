package leaderboard

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"cwgrpguploader/internal/storage"
)

var ErrAuthenticationRejected = errors.New("authentication rejected; reconnect with Authenticate or --authenticate")

type Account struct {
	dir        string
	mu         sync.Mutex
	credential storage.Credential
	rejected   bool
}

func OpenAccount(dir string) (*Account, error) {
	credential, err := storage.LoadCredential(dir)
	if errors.Is(err, os.ErrNotExist) {
		return &Account{dir: dir}, nil
	}
	return &Account{dir: dir, credential: credential, rejected: err != nil}, err
}

// MissingCredential distinguishes a new connection from a credential that failed.
func (a *Account) MissingCredential() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return !a.rejected && !a.credential.Valid()
}

// UploadBlock describes the authentication blocker, if any, without checking the network.
func (a *Account) UploadBlock() string {
	if _, err := a.uploadToken(); err == ErrAuthenticationRejected {
		return "Reconnect required"

	}
	return ""
}

// Status never returns secrets and does not make network requests.
func (a *Account) Status() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case a.rejected:
		return "Reconnect required"
	case a.credential.Valid():
		return "Connected as " + a.credential.BattleTag
	default:
		return "Not connected"
	}
}

// SetCredential publishes the new credential only after it has been saved.
func (a *Account) SetCredential(credential storage.Credential) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := storage.SaveCredential(a.dir, credential); err != nil {
		return fmt.Errorf("save uploader credential: %w", err)
	}
	a.credential, a.rejected = credential, false
	return nil
}

// Disconnect removes the local credential only; the server's unlink behavior is unchanged.
func (a *Account) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := storage.RemoveCredential(a.dir); err != nil {
		return fmt.Errorf("remove uploader credential: %w", err)
	}
	a.credential, a.rejected = storage.Credential{}, false
	return nil
}

func (a *Account) uploadToken() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.rejected {
		return "", ErrAuthenticationRejected
	}
	if !a.credential.Valid() {
		return "", nil // No stored credential: send an anonymous upload.
	}
	return a.credential.Token, nil
}

func (a *Account) reject(token string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.credential.Token == token {
		a.rejected = true
	}
}

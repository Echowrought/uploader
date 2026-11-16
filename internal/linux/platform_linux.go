// Package linux owns Linux console paths and process-instance locking.
package linux

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// DataFolders follows XDG paths; Wine/Proton save discovery is deliberately not guessed.
func DataFolders() (configDir, stateDir string, err error) {
	config, err := os.UserConfigDir()
	if err != nil {
		return "", "", err
	}
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", "", err
		}
		state = filepath.Join(home, ".local", "state")
	}
	if !filepath.IsAbs(config) || !filepath.IsAbs(state) {
		return "", "", errors.New("XDG configuration and state directories must be absolute paths")
	}
	return filepath.Join(config, "cwg-rpg-uploader"), filepath.Join(state, "cwg-rpg-uploader"), nil
}

// SingleInstance retains the lock file: unlinking it could let another process
// lock a different inode. Closing the descriptor, including on process exit,
// releases the lock. Go opens the descriptor close-on-exec.
func SingleInstance(stateDir string) (func(), error) {
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(stateDir, "instance.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open instance lock: %w", err)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("CWG RPG Uploader is already running, or its instance lock is unavailable: %w", err)
	}
	return func() { file.Close() }, nil
}

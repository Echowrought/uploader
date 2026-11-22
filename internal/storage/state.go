package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type UploadState struct {
	SaveFolder  string    `json:"save_folder"`
	Destination string    `json:"destination"`
	Fingerprint string    `json:"fingerprint"`
	UploadedAt  time.Time `json:"uploaded_at"`
	Player      string    `json:"player"`
	Heroes      int       `json:"heroes"`
}

func LoadUploadState(dir string) (UploadState, error) {
	var state UploadState
	data, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, fmt.Errorf("invalid state.json: %w", err)
	}
	if err := state.Validate(); err != nil {
		return state, fmt.Errorf("invalid state.json: %w", err)
	}
	return state, nil
}

func SaveUploadState(dir string, state UploadState) error {
	if err := state.Validate(); err != nil {
		return err
	}
	return atomicJSON(filepath.Join(dir, "state.json"), state)
}

func (state UploadState) Validate() error {
	if state.SaveFolder == "" || state.Destination == "" || state.Fingerprint == "" {
		return errors.New("upload state requires a save folder, destination, and fingerprint")
	}
	if state.UploadedAt.IsZero() {
		return errors.New("upload state requires an upload timestamp")
	}
	if state.Player == "" || state.Heroes < 0 {
		return errors.New("upload state requires a player and at least one hero")
	}
	return nil
}

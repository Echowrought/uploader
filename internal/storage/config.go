package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	SaveFolder        string `json:"save_folder"`
	PollSeconds       int    `json:"poll_interval_seconds"`
	Automatic         bool   `json:"upload_automatically"`
	ProxyURL          string `json:"proxy_url"`
	UploadNoticeShown bool   `json:"upload_notice_shown"`
}

func LoadConfig(dir string, defaultFolder func() (string, error)) (Config, bool, error) {
	config, firstRun, hasFolder, err := readConfig(dir)
	if err != nil {
		return config, false, err
	}
	if !hasFolder {
		config.SaveFolder, err = defaultFolder()
		if err != nil {
			return config, firstRun, fmt.Errorf("discover default save folder: %w", err)
		}
	}
	if firstRun {
		return config, true, SaveConfig(dir, config)
	}
	return config, false, config.Validate()
}

// LoadProxyURL is used by explicit authentication, which does not need a save folder.
func LoadProxyURL(dir string) (string, error) {
	config, _, _, err := readConfig(dir)
	return config.ProxyURL, err
}

func readConfig(dir string) (Config, bool, bool, error) {
	config := Config{PollSeconds: 5, Automatic: true}
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if errors.Is(err, os.ErrNotExist) {
		return config, true, false, nil
	}
	if err != nil {
		return config, false, false, err
	}
	hasFolder, err := decodeConfig(data, &config)
	if err != nil {
		return config, false, false, fmt.Errorf("invalid config.json: %w", err)
	}
	return config, false, hasFolder, nil
}

func SaveConfig(dir string, config Config) error {
	if err := config.Validate(); err != nil {
		return err
	}
	return atomicJSON(filepath.Join(dir, "config.json"), config)
}

// Validate checks a candidate without changing it or writing anything.
// Save paths are literal absolute paths, without variable substitution.
func (config Config) Validate() error {
	if config.PollSeconds < 1 || config.PollSeconds > 3600 {
		return errors.New("poll_interval_seconds must be between 1 and 3600")
	}
	if !filepath.IsAbs(config.SaveFolder) {
		return errors.New("save_folder must be an absolute path")
	}
	return nil
}

func decodeConfig(data []byte, config *Config) (bool, error) {
	// encoding/json treats null scalar values as no-ops. Preserve defaults for
	// omitted settings, but reject explicit nulls instead of silently defaulting.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return false, err
	}
	if fields == nil {
		return false, errors.New("configuration must be a JSON object, not null")
	}
	hasFolder := false
	for name, value := range fields {
		if strings.EqualFold(name, "save_folder") {
			hasFolder = true
		}
		if string(value) != "null" {
			continue
		}
		for _, field := range []string{"save_folder", "poll_interval_seconds", "upload_automatically", "proxy_url", "upload_notice_shown"} {
			if strings.EqualFold(name, field) {
				return false, fmt.Errorf("%s must not be null", field)
			}
		}
	}
	return hasFolder, json.Unmarshal(data, config)
}

func atomicJSON(path string, value any) error {
	return writeJSON(path, value, nil)
}

func writeJSON(path string, value any, protect func(*os.File) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".write-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if protect != nil {
		if err := protect(file); err != nil {
			file.Close()
			return err
		}
	}
	if _, err = file.Write(append(data, '\n')); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

package windows

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const startupKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
const startupValueName = "CWG2Uploader"

// StartupEnabled reports whether our registration points to this executable.
// Windows startup policy may independently prevent registered apps from running.
func StartupEnabled() (bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, startupKeyPath, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer key.Close()
	executable, err := os.Executable()
	if err != nil {
		return false, err
	}
	return startupEnabled(key, executable)
}

func SetStartup(enabled bool) error {
	var executable string
	if enabled {
		var err error
		executable, err = os.Executable()
		if err != nil {
			return err
		}
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, startupKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return setStartup(key, executable, enabled)
}

func startupEnabled(key registry.Key, executable string) (bool, error) {
	command, _, err := key.GetStringValue(startupValueName)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(command), `"`+executable+`"`), nil
}

func setStartup(key registry.Key, executable string, enabled bool) error {
	if !enabled {
		err := key.DeleteValue(startupValueName)
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
	return key.SetStringValue(startupValueName, `"`+executable+`"`)
}

package windows

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const startupKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
const startupValueName = "CWGRPGUploader"

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

// RefreshStartup adds the launch marker to older registrations owned by this executable.
func RefreshStartup() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, startupKeyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer key.Close()
	return refreshStartup(key, executable)
}

func refreshStartup(key registry.Key, executable string) error {
	command, _, err := key.GetStringValue(startupValueName)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if strings.EqualFold(strings.TrimSpace(command), `"`+executable+`"`) {
		return setStartup(key, executable, true)
	}
	return nil
}

func startupEnabled(key registry.Key, executable string) (bool, error) {
	command, _, err := key.GetStringValue(startupValueName)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	command = strings.TrimSpace(command)
	return strings.EqualFold(command, `"`+executable+`"`) || strings.EqualFold(command, startupCommand(executable)), nil
}

func setStartup(key registry.Key, executable string, enabled bool) error {
	if !enabled {
		err := key.DeleteValue(startupValueName)
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
	return key.SetStringValue(startupValueName, startupCommand(executable))
}

func startupCommand(executable string) string { return `"` + executable + `" --autostart` }

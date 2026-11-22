package windows

import (
	"crypto/rand"
	"testing"

	"golang.org/x/sys/windows/registry"
)

func TestStartupRegistrationMatchesCurrentExecutable(t *testing.T) {
	key := fixtureStartupKey(t)
	executable := `C:\Program Files\CWG RPG Uploader\uploader.exe`

	setErr := setStartup(key, executable, true)
	enabled, readErr := startupEnabled(key, executable)
	command, _, commandErr := key.GetStringValue("CWGRPGUploader")

	if setErr != nil || readErr != nil || commandErr != nil {
		t.Fatalf("errors: %v, %v, %v", setErr, readErr, commandErr)
	}
	if !enabled || command != `"C:\Program Files\CWG RPG Uploader\uploader.exe" --windows-sign-in-launch` {
		t.Fatalf("incorrect registration: enabled=%v, command=%q", enabled, command)
	}
}

func TestStartupRegistrationComparesWindowsPathsCaseInsensitively(t *testing.T) {
	key := fixtureStartupKey(t)
	if err := key.SetStringValue("CWGRPGUploader", `"C:\CWGRPG\Uploader.exe"`); err != nil {
		t.Fatal(err)
	}

	enabled, err := startupEnabled(key, `c:\cwgrpg\uploader.exe`)

	if err != nil || !enabled {
		t.Fatalf("enabled=%v, err=%v", enabled, err)
	}
}

func TestStartupRegistrationForAnotherExecutableIsNotEnabled(t *testing.T) {
	key := fixtureStartupKey(t)
	if err := key.SetStringValue("CWGRPGUploader", `"C:\Old Location\uploader.exe"`); err != nil {
		t.Fatal(err)
	}

	enabled, err := startupEnabled(key, `C:\New Location\uploader.exe`)

	if err != nil || enabled {
		t.Fatalf("enabled=%v, err=%v", enabled, err)
	}
}

func TestMissingStartupRegistrationIsDisabled(t *testing.T) {
	key := fixtureStartupKey(t)

	enabled, err := startupEnabled(key, `C:\CWGRPG\uploader.exe`)

	if err != nil || enabled {
		t.Fatalf("enabled=%v, err=%v", enabled, err)
	}
}

func TestStartupRegistrationReadErrorsAreNotReportedAsDisabled(t *testing.T) {
	key := fixtureStartupKey(t)
	if err := key.SetDWordValue("CWGRPGUploader", 1); err != nil {
		t.Fatal(err)
	}

	_, err := startupEnabled(key, `C:\CWGRPG\uploader.exe`)

	if err == nil {
		t.Fatal("invalid registry value was silently reported as disabled")
	}
}

func TestStartupRegistrationCanBeRemoved(t *testing.T) {
	key := fixtureStartupKey(t)
	executable := `C:\CWGRPG\uploader.exe`
	if err := key.SetStringValue("CWGRPGUploader", `"`+executable+`"`); err != nil {
		t.Fatal(err)
	}

	setErr := setStartup(key, executable, false)
	enabled, readErr := startupEnabled(key, executable)

	if setErr != nil || readErr != nil || enabled {
		t.Fatalf("set=%v, read=%v, enabled=%v", setErr, readErr, enabled)
	}
}

func TestRemovingMissingStartupRegistrationSucceeds(t *testing.T) {
	key := fixtureStartupKey(t)

	err := setStartup(key, "", false)

	if err != nil {
		t.Fatal(err)
	}
}

func TestRefreshStartupOnlyUpdatesOwnedLegacyRegistration(t *testing.T) {
	for _, tc := range []struct{ name, command, want string }{
		{"owned legacy", `"C:\CWGRPG\uploader.exe"`, `"C:\CWGRPG\uploader.exe" --windows-sign-in-launch`},
		{"other copy", `"C:\Other\uploader.exe"`, `"C:\Other\uploader.exe"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := fixtureStartupKey(t)
			if err := key.SetStringValue(startupValueName, tc.command); err != nil {
				t.Fatal(err)
			}

			err := refreshStartup(key, `C:\CWGRPG\uploader.exe`)
			command, _, readErr := key.GetStringValue(startupValueName)

			if err != nil || readErr != nil || command != tc.want {
				t.Fatalf("command=%q, refresh=%v, read=%v", command, err, readErr)
			}
		})
	}
}

// Never modify the real Run key: every test gets a disposable registry key.
func fixtureStartupKey(t *testing.T) registry.Key {
	t.Helper()
	path := `Software\CWGRPGUploaderTest-` + rand.Text()
	key, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.ALL_ACCESS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := key.Close(); err != nil {
			t.Error(err)
		}
		if err := registry.DeleteKey(registry.CURRENT_USER, path); err != nil {
			t.Error(err)
		}
	})
	return key
}

package linux_test

import (
	"os"
	"path/filepath"
	"testing"

	platform "cwgrpguploader/internal/linux"
)

func TestDataFoldersUseXDGDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")

	config, state, err := platform.DataFolders()

	if err != nil || config != filepath.Join(home, ".config", "cwg-rpg-uploader") || state != filepath.Join(home, ".local", "state", "cwg-rpg-uploader") {
		t.Fatalf("config=%q state=%q error=%v", config, state, err)
	}
}

func TestDataFoldersHonorXDGOverrides(t *testing.T) {
	configHome, stateHome := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_STATE_HOME", stateHome)

	config, state, err := platform.DataFolders()

	if err != nil || config != filepath.Join(configHome, "cwg-rpg-uploader") || state != filepath.Join(stateHome, "cwg-rpg-uploader") {
		t.Fatalf("config=%q state=%q error=%v", config, state, err)
	}
}

func TestDataFoldersRejectRelativeXDGPaths(t *testing.T) {
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_STATE_HOME"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			t.Setenv(name, "relative")

			_, _, err := platform.DataFolders()

			if err == nil {
				t.Fatal("relative XDG path accepted")
			}
		})
	}
}

func TestSingleInstanceBlocksUntilReleased(t *testing.T) {
	dir := t.TempDir()
	release, err := platform.SingleInstance(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	second, blockedErr := platform.SingleInstance(dir)
	release()
	third, reacquireErr := platform.SingleInstance(dir)

	if second != nil {
		second()
		t.Fatal("second instance acquired a held lock")
	}
	if third != nil {
		defer third()
	}
	if blockedErr == nil || reacquireErr != nil {
		t.Fatalf("blocked=%v reacquire=%v", blockedErr, reacquireErr)
	}
}

func TestSingleInstanceReportsUnusableStateDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}

	release, err := platform.SingleInstance(path)

	if release != nil {
		release()
		t.Fatal("acquired lock beneath a regular file")
	}
	if err == nil {
		t.Fatal("unusable directory accepted")
	}
}

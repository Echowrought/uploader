package main

import (
	"context"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	platform "cwgrpguploader/internal/linux"
	"cwgrpguploader/internal/storage"
)

func TestLinuxConsoleFirstRunRequiresSaveFolder(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	command := consoleCommand(t, ctx, root, "--dry-run")

	output, err := command.CombinedOutput()
	_, configErr := os.Stat(filepath.Join(root, "config", "cwg-rpg-uploader", "config.json"))

	if err == nil || !strings.Contains(string(output), "--save-folder") || !os.IsNotExist(configErr) {
		t.Fatalf("error=%v config=%v output=%s", err, configErr, output)
	}
	if !strings.Contains(string(output), "Configuration: "+filepath.Join(root, "config", "cwg-rpg-uploader", "config.json")) ||
		!strings.Contains(string(output), "Logs: "+filepath.Join(root, "state", "cwg-rpg-uploader", "uploader.log")) {
		t.Fatalf("startup error did not report settings/log paths: %s", output)
	}
}

func TestLinuxConsoleRejectsEmptyFolderWithoutChangingConfiguration(t *testing.T) {
	t.Parallel()
	root, folder := consoleFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	command := consoleCommand(t, ctx, root, "--save-folder", "")

	output, err := command.CombinedOutput()
	config, _, configErr := storage.LoadConfig(filepath.Join(root, "config", "cwg-rpg-uploader"), nil)

	if err == nil || !strings.Contains(string(output), "absolute path") {
		t.Fatalf("error=%v output=%s", err, output)
	}
	if configErr != nil || config.SaveFolder != folder || config.UploadNoticeShown {
		t.Fatalf("config=%+v error=%v", config, configErr)
	}
}

func TestLinuxConsoleDryRunInspectsWithoutUploadingOrAcknowledgingNotice(t *testing.T) {
	t.Parallel()
	root, folder := consoleFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	command := consoleCommand(t, ctx, root, "--dry-run", "--save-folder", folder)

	output, err := command.CombinedOutput()
	config, _, configErr := storage.LoadConfig(filepath.Join(root, "config", "cwg-rpg-uploader"), nil)
	_, stateErr := os.Stat(filepath.Join(root, "state", "cwg-rpg-uploader", "state.json"))

	if err != nil || !strings.Contains(string(output), "Dry run: 2 eligible files") {
		t.Fatalf("error=%v output=%s", err, output)
	}
	if configErr != nil || config.Automatic || config.UploadNoticeShown || config.SaveFolder != folder || !os.IsNotExist(stateErr) {
		t.Fatalf("config=%+v error=%v state=%v", config, configErr, stateErr)
	}
}

func TestLinuxConsoleRejectsSecondInstance(t *testing.T) {
	t.Parallel()
	root, _ := consoleFixture(t)
	_ = runningConsole(t, root)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	second := consoleCommand(t, ctx, root, "--dry-run")

	output, secondErr := second.CombinedOutput()

	if secondErr == nil || !strings.Contains(string(output), "already running") {
		t.Fatalf("second instance: %v %s", secondErr, output)
	}
}

func TestLinuxConsoleStopsOnSIGTERMAndReleasesLock(t *testing.T) {
	t.Parallel()
	root, _ := consoleFixture(t)
	command := runningConsole(t, root)

	signalErr := command.Process.Signal(syscall.SIGTERM)
	waitErr := command.Wait()
	release, lockErr := platform.SingleInstance(filepath.Join(root, "state", "cwg-rpg-uploader"))

	if release != nil {
		defer release()
	}
	if signalErr != nil || waitErr != nil || lockErr != nil {
		t.Fatalf("signal=%v exit=%v reacquire=%v", signalErr, waitErr, lockErr)
	}
}

func TestLinuxConsoleRemembersFolderWhenExistingConfigOmitsIt(t *testing.T) {
	t.Parallel()
	root, folder := consoleFixture(t)
	configPath := filepath.Join(root, "config", "cwg-rpg-uploader", "config.json")
	if err := os.WriteFile(configPath, []byte(`{"poll_interval_seconds":1,"upload_automatically":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	command := runningConsole(t, root, "--save-folder", folder)

	signalErr := command.Process.Signal(syscall.SIGTERM)
	waitErr := command.Wait()
	config, _, configErr := storage.LoadConfig(filepath.Dir(configPath), nil)

	if signalErr != nil || waitErr != nil || configErr != nil || config.SaveFolder != folder || config.Automatic {
		t.Fatalf("signal=%v exit=%v config=%+v error=%v", signalErr, waitErr, config, configErr)
	}
}

func TestLinuxConsoleLockIsReleasedAfterProcessDeath(t *testing.T) {
	t.Parallel()
	root, _ := consoleFixture(t)
	command := runningConsole(t, root)

	killErr := command.Process.Kill()
	waitErr := command.Wait()
	release, lockErr := platform.SingleInstance(filepath.Join(root, "state", "cwg-rpg-uploader"))

	if release != nil {
		defer release()
	}
	if killErr != nil || waitErr == nil || lockErr != nil {
		t.Fatalf("kill=%v exit=%v reacquire=%v", killErr, waitErr, lockErr)
	}
}

// Exercise the actual CLI entry point in an isolated process without rebuilding
// it inside the package's execution budget. The trampoline exists only in tests.
func TestMain(m *testing.M) {
	if os.Getenv("CWG_UPLOADER_TEST_PROCESS") == "1" {
		flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
		main()
		return
	}
	os.Exit(m.Run())
}

func consoleFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	folder := filepath.Join(root, "Wine saves with spaces")
	if err := os.MkdirAll(folder, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"HERO_01_123.pld", "ACCOUNT_123.pld"} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte("save data"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	config := storage.Config{SaveFolder: folder, PollSeconds: 1, Automatic: false}
	if err := storage.SaveConfig(filepath.Join(root, "config", "cwg-rpg-uploader"), config); err != nil {
		t.Fatal(err)
	}
	return root, folder
}

func consoleCommand(t *testing.T, ctx context.Context, root string, args ...string) *exec.Cmd {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// Own a rejecting proxy so no local service can accidentally forward a
	// regressed upload. It never connects to the real destination.
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "uploads blocked by CLI test", http.StatusForbidden)
	}))
	t.Cleanup(proxy.Close)
	command := exec.CommandContext(ctx, executable, args...)
	command.Env = append(os.Environ(),
		"CWG_UPLOADER_TEST_PROCESS=1",
		"XDG_CONFIG_HOME="+filepath.Join(root, "config"),
		"XDG_STATE_HOME="+filepath.Join(root, "state"),
		"HTTPS_PROXY="+proxy.URL, "HTTP_PROXY="+proxy.URL, "NO_PROXY=",
		"https_proxy="+proxy.URL, "http_proxy="+proxy.URL, "no_proxy=",
	)
	return command
}

func runningConsole(t *testing.T, root string, args ...string) *exec.Cmd {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	command := consoleCommand(t, ctx, root, args...)
	output, err := os.CreateTemp(t.TempDir(), "output-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { output.Close() })
	command.Stdout, command.Stderr = output, output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if command.ProcessState == nil {
			command.Process.Kill()
			command.Wait()
		}
	})
	// Real time is required across an OS-process boundary; no production clock
	// or test-only configuration is injected into the running application.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(output.Name())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "[app] started") {
			return command
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("console did not start")
	return nil
}

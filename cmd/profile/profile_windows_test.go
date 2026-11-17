package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"cwgrpguploader/internal/leaderboard"
)

func TestApplicationRequiresAbsoluteSaveFolder(t *testing.T) {
	_, err := newApplication("relative-saves")

	if err == nil || !strings.Contains(err.Error(), "absolute path") {
		t.Fatalf("error = %v; want absolute-path validation", err)
	}
}

// Safety exception: disable the preview guard to exercise the independent
// network-denial guard without ever contacting a real leaderboard.
func TestApplicationBlocksNetworkingEvenWithoutDryRun(t *testing.T) {
	application, err := newApplication(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	application.DryRun = false // Exercise the independent transport guard.

	_, err = application.Client.Upload(context.Background(), leaderboard.UploadRequest{})

	if !errors.Is(err, errNetworkingDisabled) {
		t.Fatalf("upload error = %v; want networking disabled", err)
	}
}

func TestProfilingAppScansWithoutPersistence(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		folder := t.TempDir()
		for _, name := range []string{"HERO_01_123.pld", "ACCOUNT_player_123.pld"} {
			if err := os.WriteFile(filepath.Join(folder, name), []byte("save"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		dir := t.TempDir()
		application, err := newApplication(folder)
		if err != nil {
			t.Fatal(err)
		}
		application.Dir = dir
		var output bytes.Buffer
		application.Logger = log.New(&output, "", 0)
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		done := make(chan struct{})

		go func() { defer close(done); application.Run(ctx) }()
		var message string
		for !strings.HasPrefix(message, "Dry run:") {
			select {
			case status := <-application.Updates:
				message = status.Message
			case <-ctx.Done():
				t.Fatal("profiling app did not prepare a preview")
			}
		}
		cancel()
		<-done

		if !strings.Contains(output.String(), "dry-run snapshot prepared: 2 eligible files") {
			t.Fatalf("scanner did not prepare a settled snapshot:\n%s", output.String())
		}
		for _, name := range []string{"config.json", "state.json"} {
			if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unexpected persistence for %s: %v", name, err)
			}
		}
	})
}

func TestCaptureWritesProfiles(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()

		err := capture(ctx, dir, 12*time.Second)

		if err != nil {
			t.Fatal(err)
		}
		for _, stage := range []string{"before-gc", "after-gc", "after-release"} {
			data, err := os.ReadFile(filepath.Join(dir, stage+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var record struct {
				Stage   string
				Process processMemory
			}
			if err := json.Unmarshal(data, &record); err != nil {
				t.Fatal(err)
			}
			if record.Stage != stage || record.Process.WorkingSet == 0 || record.Process.Private == 0 {
				t.Fatalf("invalid %s counters: %+v", stage, record)
			}
		}
		heap, err := os.ReadFile(filepath.Join(dir, "heap.pprof"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(heap, []byte{0x1f, 0x8b}) {
			t.Fatal("heap profile is not gzip encoded")
		}
		file, err := os.Open(filepath.Join(dir, "process.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		decoder := json.NewDecoder(file)
		count := 0
		for {
			var sample struct{ Process processMemory }
			err := decoder.Decode(&sample)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil || sample.Process.WorkingSet == 0 {
				t.Fatalf("invalid process sample: %+v, %v", sample, err)
			}
			count++
		}
		if count < 12 {
			t.Fatalf("got %d process samples; want initial and one-second samples", count)
		}
	})
}

func TestCaptureCancellationDoesNotForceHeapCollection(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := capture(ctx, dir, time.Minute)
	_, heapErr := os.Stat(filepath.Join(dir, "heap.pprof"))

	if !errors.Is(err, context.Canceled) || !errors.Is(heapErr, os.ErrNotExist) {
		t.Fatalf("capture error = %v, heap error = %v; want canceled without heap profile", err, heapErr)
	}
}

func TestCaptureReportsOutputFailure(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")

	err := capture(context.Background(), dir, time.Minute)

	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("error = %v; want missing output directory", err)
	}
}

func TestRunRejectsNonpositiveDurationBeforeStartingTray(t *testing.T) {
	output := filepath.Join(t.TempDir(), "unused")

	err := run(context.Background(), t.TempDir(), output, 0)
	_, outputErr := os.Stat(output)

	if err == nil || !errors.Is(outputErr, os.ErrNotExist) {
		t.Fatalf("run error = %v, output error = %v; want rejection before creating output", err, outputErr)
	}
}

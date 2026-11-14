package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLogRepeatedLockedCurrentPreservesBackups(t *testing.T) {
	writer := fullLog(t)
	reader, err := os.Open(writer.path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	var failures []error
	for i := 0; i < 5; i++ {
		_, err := writer.Write([]byte("next"))
		failures = append(failures, err)
	}
	staged, globErr := filepath.Glob(filepath.Join(filepath.Dir(writer.path), ".rotate-*"))

	for i, err := range failures {
		if err == nil {
			t.Fatalf("write %d unexpectedly succeeded with a locked current log", i+1)
		}
	}
	for name, want := range map[string]string{
		writer.path: "live", writer.path + ".1": "1111",
		writer.path + ".2": "2222", writer.path + ".3": "3333",
	} {
		got, err := os.ReadFile(name)
		if err != nil || string(got) != want {
			t.Fatalf("%s: got %q, want %q, error=%v", filepath.Base(name), got, want, err)
		}
	}
	if globErr != nil || len(staged) != 0 {
		t.Fatalf("staging files remain after failed rename: paths=%v, error=%v", staged, globErr)
	}
}

func TestLogRotationRecoversAfterCurrentReaderCloses(t *testing.T) {
	writer := fullLog(t)
	reader, err := os.Open(writer.path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	_, blockedErr := writer.Write([]byte("next"))
	releaseErr := reader.Close()
	n, recoveryErr := writer.Write([]byte("next"))
	staged, globErr := filepath.Glob(filepath.Join(filepath.Dir(writer.path), ".rotate-*"))

	if blockedErr == nil {
		t.Fatal("rotation unexpectedly succeeded with a locked current log")
	}
	if releaseErr != nil || recoveryErr != nil || n != 4 {
		t.Fatalf("recovery failed: release=%v, write=%v, n=%d", releaseErr, recoveryErr, n)
	}
	for name, want := range map[string]string{
		writer.path: "next", writer.path + ".1": "live",
		writer.path + ".2": "1111", writer.path + ".3": "2222",
	} {
		got, err := os.ReadFile(name)
		if err != nil || string(got) != want {
			t.Fatalf("%s: got %q, want %q, error=%v", filepath.Base(name), got, want, err)
		}
	}
	if globErr != nil || len(staged) != 0 {
		t.Fatalf("staging files remain after successful rotation: paths=%v, error=%v", staged, globErr)
	}
}

package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLogRepeatedLockedCurrentPreservesBackups(t *testing.T) {
	writer, path, live := fullLog(t)
	reader, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	var failures []error
	for range 5 {
		_, err := writer.Write([]byte("next"))
		failures = append(failures, err)
	}

	for i, err := range failures {
		if err == nil {
			t.Fatalf("write %d succeeded with locked current log", i)
		}
	}
	assertLogContents(t, path, map[string]string{"": live, ".1": "1111", ".2": "2222", ".3": "3333"})
	assertLogDirectory(t, filepath.Dir(path), 3)
}

func TestLogRotationRecoversAfterCurrentReaderCloses(t *testing.T) {
	writer, path, live := fullLog(t)
	reader, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	_, blockedErr := writer.Write([]byte("next"))
	releaseErr := reader.Close()
	n, recoveryErr := writer.Write([]byte("next"))

	if blockedErr == nil {
		t.Fatal("rotation succeeded with locked current log")
	}
	if releaseErr != nil || recoveryErr != nil || n != 4 {
		t.Fatalf("recovery: release=%v write=%v n=%d", releaseErr, recoveryErr, n)
	}
	assertLogContents(t, path, map[string]string{"": "next", ".1": live, ".2": "1111", ".3": "2222"})
	assertLogDirectory(t, filepath.Dir(path), 3)
}

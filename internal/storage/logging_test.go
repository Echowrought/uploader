package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLogRestoresCurrentAfterBackupRotationFailure(t *testing.T) {
	writer := fullLog(t)
	blocked := writer.path + ".3"
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "blocker"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}

	_, firstErr := writer.Write([]byte("next"))
	_, secondErr := writer.Write([]byte("next"))
	restored, restoredErr := os.ReadFile(writer.path)
	staged, globErr := filepath.Glob(filepath.Join(filepath.Dir(writer.path), ".rotate-*"))
	unblockErr := os.RemoveAll(blocked)
	n, recoveryErr := writer.Write([]byte("next"))

	if firstErr == nil || secondErr == nil {
		t.Fatalf("blocked rotation unexpectedly succeeded: first=%v, second=%v", firstErr, secondErr)
	}
	if restoredErr != nil || string(restored) != "live" {
		t.Fatalf("current log not restored: data=%q, error=%v", restored, restoredErr)
	}
	if globErr != nil || len(staged) != 0 {
		t.Fatalf("staging files remain after restoration: paths=%v, error=%v", staged, globErr)
	}
	if unblockErr != nil || recoveryErr != nil || n != 4 {
		t.Fatalf("recovery failed: unblock=%v, write=%v, n=%d", unblockErr, recoveryErr, n)
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
}

func fullLog(t *testing.T) *RotatingLog {
	t.Helper()
	writer, err := OpenLog(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { writer.Close() })
	writer.max = 4
	if _, err := writer.Write([]byte("live")); err != nil {
		t.Fatal(err)
	}
	for i, data := range []string{"1111", "2222", "3333"} {
		if err := os.WriteFile(fmt.Sprintf("%s.%d", writer.path, i+1), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return writer
}

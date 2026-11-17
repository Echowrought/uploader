package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogRestoresCurrentAfterBackupRotationFailure(t *testing.T) {
	writer, path, live := fullLog(t)
	blocked := path + ".3"
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
	restored, restoredErr := os.ReadFile(path)
	restoredNames, namesErr := os.ReadDir(filepath.Dir(path))
	unblockErr := os.RemoveAll(blocked)
	n, recoveryErr := writer.Write([]byte("next"))

	if firstErr == nil || secondErr == nil {
		t.Fatalf("blocked rotation succeeded: %v %v", firstErr, secondErr)
	}
	if restoredErr != nil || string(restored) != live {
		t.Fatalf("current log was not restored: length=%d error=%v", len(restored), restoredErr)
	}
	if namesErr != nil || len(restoredNames) != 4 {
		t.Fatalf("restoration left unexpected artifacts: %v %v", restoredNames, namesErr)
	}
	if unblockErr != nil || recoveryErr != nil || n != 4 {
		t.Fatalf("recovery: %v %v n=%d", unblockErr, recoveryErr, n)
	}
	assertLogContents(t, path, map[string]string{"": "next", ".1": live, ".2": "1111", ".3": "2222"})
	assertLogDirectory(t, filepath.Dir(path), 3)
}

func fullLog(t *testing.T) (*RotatingLog, string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "uploader.log")
	// Small limits exercise rotation and recovery without production-sized files.
	live := "live"
	if err := os.WriteFile(path, []byte(live), 0600); err != nil {
		t.Fatal(err)
	}
	for i, data := range []string{"1111", "2222", "3333"} {
		if err := os.WriteFile(fmt.Sprintf("%s.%d", path, i+1), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writer, err := OpenLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { writer.Close() })
	writer.max = 4
	writer.backups = 3
	return writer, path, live
}

func assertLogContents(t *testing.T, path string, want map[string]string) {
	t.Helper()
	for suffix, text := range want {
		data, err := os.ReadFile(path + suffix)
		if err != nil || string(data) != text {
			t.Fatalf("%s: length=%d want=%d error=%v", filepath.Base(path+suffix), len(data), len(text), err)
		}
	}
}

func assertLogDirectory(t *testing.T, dir string, backups int) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	want := []string{"uploader.log"}
	for i := 1; i <= backups; i++ {
		want = append(want, fmt.Sprintf("uploader.log.%d", i))
	}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected log-directory contents: %v", names)
	}
}

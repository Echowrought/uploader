package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"unicode/utf8"
)

const maxLogEntryBytes = 256 << 10
const logTruncationMarker = "\n[log entry truncated]\n"

type RotatingLog struct {
	mu      sync.Mutex
	path    string
	max     int64
	backups int
	file    *os.File
	size    int64
}

func OpenLog(dir string) (*RotatingLog, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	writer := &RotatingLog{path: filepath.Join(dir, "uploader.log"), max: 2 << 20, backups: 3}
	return writer, writer.open()
}

// Write consumes the whole entry, truncating oversized entries on disk only.
func (w *RotatingLog) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	originalSize := len(data)
	data = truncateLogEntry(data)
	if w.file == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	if w.size+int64(len(data)) > w.max {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(data)
	w.size += int64(n)
	if err != nil {
		return n, err
	}
	return originalSize, nil
}

func (w *RotatingLog) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func truncateLogEntry(data []byte) []byte {
	if len(data) <= maxLogEntryBytes {
		return data
	}
	end := maxLogEntryBytes - len(logTruncationMarker)
	for end > 0 && !utf8.RuneStart(data[end]) {
		end--
	}
	truncated := make([]byte, end+len(logTruncationMarker))
	copy(truncated, data[:end])
	copy(truncated[end:], logTruncationMarker)
	return truncated
}

func (w *RotatingLog) open() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w.file, w.size = f, info.Size()
	return nil
}

func (w *RotatingLog) rotate() error {
	err := w.file.Close()
	w.file = nil
	if err != nil {
		return err
	}

	// Move the current log first: a reader may prevent renaming it on Windows.
	// In that case no backup should be aged or deleted, even on repeated retries.
	file, err := os.CreateTemp(filepath.Dir(w.path), ".rotate-*")
	if err != nil {
		return err
	}
	staged := file.Name()
	if err := file.Close(); err != nil {
		os.Remove(staged)
		return err
	}
	if err := os.Rename(w.path, staged); err != nil {
		os.Remove(staged)
		return err
	}

	if err := w.rotateBackups(staged); err != nil {
		if restoreErr := os.Rename(staged, w.path); restoreErr != nil {
			// Keep the staged log for recovery; never delete it on this path.
			return errors.Join(err, fmt.Errorf("restore current log from %q: %w", staged, restoreErr))
		}
		return err
	}
	return w.open()
}

func (w *RotatingLog) rotateBackups(staged string) error {
	oldest := fmt.Sprintf("%s.%d", w.path, w.backups)
	if err := os.Remove(oldest); err != nil && !os.IsNotExist(err) {
		return err
	}
	for i := w.backups - 1; i >= 1; i-- {
		if err := os.Rename(fmt.Sprintf("%s.%d", w.path, i), fmt.Sprintf("%s.%d", w.path, i+1)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return os.Rename(staged, w.path+".1")
}

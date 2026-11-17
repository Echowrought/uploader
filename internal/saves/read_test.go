package saves

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFileAcceptsConfiguredSizeBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "HERO_save.pld")
	text := strings.Repeat("x", 64)
	writeFixture(t, path, text)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	expected := metadata{Size: info.Size(), Modified: info.ModTime()}

	got, readErr := readFile(t.Context(), path, expected, 64)

	if readErr != nil || got != text {
		t.Fatalf("exact-limit read = %q, %v", got, readErr)
	}
}

func TestReadFileEnforcesConfiguredSizeBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "HERO_save.pld")
	writeFixture(t, path, strings.Repeat("x", 65))
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Accurate metadata deliberately bypasses Scanner's independent size gate.
	expected := metadata{Size: info.Size(), Modified: info.ModTime()}

	got, readErr := readFile(t.Context(), path, expected, 64)

	if !errors.Is(readErr, ErrSettling) || got != "" {
		t.Fatalf("over-limit read published contents: %q, %v", got, readErr)
	}
}

func TestReadFileCancellationDoesNotReadMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.pld")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	text, err := readFile(ctx, path, metadata{}, 64)

	if !errors.Is(err, context.Canceled) || text != "" {
		t.Fatalf("cancellation lost precedence over filesystem access: %q, %v", text, err)
	}
}

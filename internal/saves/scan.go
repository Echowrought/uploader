package saves

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrSettling = errors.New("waiting for saves to settle")

const (
	directoryBatchSize  = 128
	maxDirectoryEntries = 4096
	maxSaveFiles        = 1024
	maxFileBytes        = 8 << 20
	maxSnapshotBytes    = 32 << 20
)

// Scanner reads direct children and caches the last settled version of each
// eligible file. Essential versions take priority in the bounded payload cache.
// Nested layouts are unsupported.
type Scanner struct {
	Folder      string
	Quiet       time.Duration
	settlements map[string]fileSettlement
}

type fileSettlement struct {
	hash     [sha256.Size]byte
	since    time.Time
	observed bool
	settled  *File
}

type File struct {
	Text  string
	Mtime int64
}

type Snapshot struct {
	Files       map[string]File
	Fingerprint string
	Warnings    []string
}

// scanResult builds a new bounded cache without modifying previous snapshots.
// During construction the old and new caches can coexist; each is at most 32 MiB.
type scanResult struct {
	states   map[string]fileSettlement
	files    map[string]File
	bytes    int64
	warnings []string
}

// Scan selects settled versions, never unsettled candidate bytes. Every current
// essential file needs a settled version; supplementary versions use the remaining
// budget. Cancellation cannot interrupt an already-blocked filesystem operation.
func (s *Scanner) Scan(ctx context.Context, now time.Time) (Snapshot, error) {
	entries, err := inventory(ctx, s.Folder)
	if err != nil {
		return Snapshot{}, err
	}
	core, extra := splitFiles(entries)
	s.pruneSettlements(entries)
	if err := validateEssential(core); err != nil {
		return Snapshot{}, err
	}
	result := scanResult{states: make(map[string]fileSettlement, len(entries)), files: make(map[string]File)}
	coreErr := s.readEssential(ctx, core, now, &result)
	if errors.Is(coreErr, context.Canceled) || errors.Is(coreErr, context.DeadlineExceeded) {
		return Snapshot{}, coreErr
	}
	// Observe supplementary files even while the initial essential gate is closed.
	if err := s.readSupplementary(ctx, extra, now, &result); err != nil {
		return Snapshot{}, err
	}
	s.settlements = result.states
	if coreErr != nil {
		return Snapshot{}, coreErr
	}
	fingerprint, err := fingerprint(ctx, result.files)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Files: result.files, Fingerprint: fingerprint, Warnings: result.warnings}, nil
}

func (s *Scanner) pruneSettlements(entries []metadata) {
	// Forget disappeared names, including their candidate and settled versions.
	current := make(map[string]fileSettlement, len(entries))
	for _, m := range entries {
		if previous, exists := s.settlements[m.Name]; exists {
			current[m.Name] = previous
		}
	}
	s.settlements = current
}

func (s *Scanner) readEssential(ctx context.Context, core []metadata, now time.Time, result *scanResult) error {
	// Reserve every previous essential version before considering replacements.
	for _, m := range core {
		result.bytes += settledSize(s.settlements[m.Name].settled)
	}
	// Admit potential shrinks before growth so filename order cannot reject an
	// otherwise fitting settled set. Failed/unsettled reads still retain old data.
	sort.SliceStable(core, func(i, j int) bool {
		left := core[i].Size <= settledSize(s.settlements[core[i].Name].settled)
		right := core[j].Size <= settledSize(s.settlements[core[j].Name].settled)
		return left && !right
	})
	var blocked error
	for _, m := range core {
		previous := s.settlements[m.Name]
		state, candidateErr := s.observe(ctx, m, previous, now)
		if errors.Is(candidateErr, context.Canceled) || errors.Is(candidateErr, context.DeadlineExceeded) {
			return candidateErr
		}
		if admissionErr := result.admitEssential(m.Name, previous, state, candidateErr); admissionErr != nil {
			// Capacity overflow outranks earlier missing-version errors.
			blocked = admissionErr
		} else if state.settled == nil && (blocked == nil || errors.Is(blocked, ErrSettling)) {
			blocked = ErrSettling
			if candidateErr != nil {
				blocked = fmt.Errorf("read essential save %q: %w", m.Name, candidateErr)
			}
		}
	}
	return blocked
}

func (s *Scanner) readSupplementary(ctx context.Context, extra []metadata, now time.Time, result *scanResult) error {
	readBudget := int64(maxSnapshotBytes) - result.bytes
	for _, m := range extra {
		if err := ctx.Err(); err != nil {
			return err
		}
		previous := s.settlements[m.Name]
		state := previous
		var candidateErr error
		if m.err == nil && m.Size <= maxFileBytes && m.Size > readBudget {
			state.observed = false
			candidateErr = errors.New("supplementary read budget exhausted")
		} else {
			state, candidateErr = s.observe(ctx, m, previous, now)
			if m.err == nil && m.Size <= maxFileBytes {
				readBudget -= m.Size
			}
		}
		if errors.Is(candidateErr, context.Canceled) || errors.Is(candidateErr, context.DeadlineExceeded) {
			return candidateErr
		}
		result.admitSupplementary(m.Name, previous, state, candidateErr)
	}
	return nil
}

// admitEssential replaces a previously reserved version, retaining it on
// overflow. Missing settled versions are handled by the essential gate's caller.
func (result *scanResult) admitEssential(name string, previous, candidate fileSettlement, candidateErr error) error {
	var overflow error
	projected := result.bytes - settledSize(previous.settled) + settledSize(candidate.settled)
	if projected > maxSnapshotBytes {
		// Keep cache memory bounded, but do not silently publish an older core
		// after a newer settled essential set has exceeded the snapshot limit.
		candidate.settled = previous.settled
		overflow = errors.New("settled essential snapshot exceeds 32 MiB safety limit")
	} else {
		result.bytes = projected
	}
	result.states[name] = candidate
	if candidate.settled != nil {
		result.files[name] = *candidate.settled
		if candidateErr != nil {
			result.warnings = append(result.warnings, fmt.Sprintf("use settled save %q: %v", name, candidateErr))
		}
	}
	return overflow
}

func (result *scanResult) admitSupplementary(name string, previous, candidate fileSettlement, candidateErr error) {
	if candidate.settled != nil && !result.fits(candidate.settled) {
		candidate.settled = previous.settled
		candidateErr = errors.New("settled supplementary version exceeds remaining snapshot budget")
	}
	if candidate.settled != nil && !result.fits(candidate.settled) {
		candidate.settled = nil
	}
	result.states[name] = candidate
	if candidate.settled == nil {
		if candidateErr == nil {
			candidateErr = ErrSettling
		}
		result.warnings = append(result.warnings, fmt.Sprintf("skip supplementary save %q: %v", name, candidateErr))
		return
	}
	result.files[name] = *candidate.settled
	result.bytes += settledSize(candidate.settled)
	if candidateErr != nil {
		result.warnings = append(result.warnings, fmt.Sprintf("use settled save %q: %v", name, candidateErr))
	}
}

func (result *scanResult) fits(file *File) bool {
	return len(result.files) < maxSaveFiles && result.bytes+settledSize(file) <= maxSnapshotBytes
}

func splitFiles(entries []metadata) (core, extra []metadata) {
	for _, m := range entries {
		if essential(m.Name) {
			core = append(core, m)
		} else {
			extra = append(extra, m)
		}
	}
	return core, extra
}

// Required names/counts are known before reading. Byte budgets apply to settled
// versions, not to newer on-disk candidates which may never enter a snapshot.
func validateEssential(files []metadata) error {
	hero, account := false, false
	for _, m := range files {
		upper := strings.ToUpper(m.Name)
		hero = hero || strings.HasPrefix(upper, "HERO_")
		account = account || (strings.HasPrefix(upper, "ACCOUNT_") && !strings.HasPrefix(upper, "ACCOUNT_VERIFY"))
	}
	if !hero || !account {
		return errors.New("waiting for HERO_ and ACCOUNT_ .pld files")
	}
	if len(files) > maxSaveFiles {
		return fmt.Errorf("essential save set exceeds %d eligible files safety limit", maxSaveFiles)
	}
	return nil
}

func (s *Scanner) observe(ctx context.Context, m metadata, previous fileSettlement, now time.Time) (fileSettlement, error) {
	next := previous
	if m.err != nil {
		next.observed = false
		return next, m.err
	}
	if m.Size > maxFileBytes {
		next.observed = false
		return next, errors.New("candidate exceeds 8 MiB safety limit")
	}
	text, err := readFile(ctx, filepath.Join(s.Folder, m.Name), m)
	if err != nil {
		next.observed = false
		return next, err
	}
	file := File{Text: text, Mtime: m.Modified.UnixMilli()}
	return settle(previous, file, now, s.Quiet), nil
}

// settle advances one successfully read candidate, independently of filesystem
// access and snapshot capacity. File values and previously settled text are immutable.
func settle(previous fileSettlement, file File, now time.Time, quiet time.Duration) fileSettlement {
	next := previous
	hash := sha256.Sum256([]byte(file.Text))
	if previous.settled != nil && file.Text == previous.settled.Text {
		// This version has already settled. Refresh its mtime without retaining
		// another copy of the same immutable text.
		file.Text = previous.settled.Text
		next.hash, next.since, next.observed = hash, now, true
		next.settled = &file
	} else if !previous.observed || previous.hash != hash {
		next.hash, next.since, next.observed = hash, now, true
	} else if now.Sub(previous.since) >= quiet {
		next.settled = &file
	}
	return next
}

func settledSize(file *File) int64 {
	if file == nil {
		return 0
	}
	return int64(len(file.Text))
}

type metadata struct {
	Name     string
	Size     int64
	Modified time.Time
	err      error
}

func inventory(ctx context.Context, folder string) ([]metadata, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir, err := os.Open(folder)
	if err != nil {
		return nil, fmt.Errorf("read save folder: %w", err)
	}
	defer dir.Close()
	result := make([]metadata, 0)
	count := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entries, err := dir.ReadDir(directoryBatchSize)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("read save folder: %w", err)
		}
		count += len(entries)
		if count > maxDirectoryEntries {
			return nil, fmt.Errorf("save folder exceeds %d directory entries safety limit", maxDirectoryEntries)
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if !eligible(entry.Name()) {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				result = append(result, metadata{Name: entry.Name(), err: err})
				continue
			}
			if !info.Mode().IsRegular() {
				continue
			}
			result = append(result, metadata{Name: entry.Name(), Size: info.Size(), Modified: info.ModTime()})
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func eligible(name string) bool {
	upper := strings.ToUpper(name)
	return strings.HasSuffix(upper, ".PLD") && !strings.HasPrefix(upper, "PRIVATE_")
}

func essential(name string) bool {
	upper := strings.ToUpper(name)
	return strings.HasPrefix(upper, "HERO_") ||
		strings.HasPrefix(upper, "ACCOUNT_") ||
		strings.HasPrefix(upper, "ENTITLEMENTS_") ||
		strings.HasPrefix(upper, "SETTINGS_") ||
		strings.HasPrefix(upper, "LEGACY_")
}

func readFile(ctx context.Context, path string, expected metadata) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", ErrSettling
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("read save: %w", err)
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !os.SameFile(info, opened) || opened.Size() != expected.Size || !opened.ModTime().Equal(expected.Modified) {
		return "", ErrSettling
	}
	data, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if int64(len(data)) != expected.Size {
		return "", ErrSettling
	}
	after, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !after.Mode().IsRegular() || !os.SameFile(opened, after) || after.Size() != expected.Size || !after.ModTime().Equal(expected.Modified) {
		return "", ErrSettling
	}
	if !utf8.Valid(data) {
		return "", errors.New("save is not valid UTF-8")
	}
	return string(data), nil
}

func fingerprint(ctx context.Context, files map[string]File) (string, error) {
	// encoding/json sorts map keys. Hash names and content, never timestamps or
	// omission diagnostics, so identical selected data has the same fingerprint.
	entries := make(map[string]string, len(files))
	for name, file := range files {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		hash := sha256.Sum256([]byte(file.Text))
		entries[name] = hex.EncodeToString(hash[:])
	}
	encoded, err := json.Marshal(entries)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}

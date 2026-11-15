package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"time"
	"unsafe"

	win "golang.org/x/sys/windows"
)

// capture samples Windows counters without forcing GC during the observation
// period. The three final stages deliberately perturb GC/released memory.
func capture(ctx context.Context, dir string, duration time.Duration) (err error) {
	file, err := os.Create(filepath.Join(dir, "process.jsonl"))
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	encoder := json.NewEncoder(file)
	sample := func() error {
		process, err := readProcessMemory()
		if err != nil {
			return err
		}
		return encoder.Encode(struct {
			Time       time.Time
			Process    processMemory
			Goroutines int
		}{time.Now(), process, runtime.NumGoroutine()})
	}
	if err := sample(); err != nil {
		return err
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := sample(); err != nil {
				return err
			}
		case <-timer.C:
			if err := ctx.Err(); err != nil {
				return err
			}
			return captureHeap(dir)
		}
	}
}

func captureHeap(dir string) error {
	if err := recordMemory(dir, "before-gc"); err != nil {
		return err
	}
	runtime.GC()
	if err := recordMemory(dir, "after-gc"); err != nil {
		return err
	}
	file, err := os.Create(filepath.Join(dir, "heap.pprof"))
	if err != nil {
		return err
	}
	profileErr := pprof.WriteHeapProfile(file)
	if err := errors.Join(profileErr, file.Close()); err != nil {
		return err
	}
	// An experiment, not the release application's memory-management policy.
	debug.FreeOSMemory()
	return recordMemory(dir, "after-release")
}

func recordMemory(dir, stage string) error {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	process, err := readProcessMemory()
	if err != nil {
		return err
	}
	return writeJSON(dir, stage+".json", struct {
		Stage   string
		Time    time.Time
		Go      runtime.MemStats
		Process processMemory
	}{stage, time.Now(), memory, process})
}

func writeJSON(dir, name string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), append(data, '\n'), 0600)
}

// PROCESS_MEMORY_COUNTERS_EX, in Windows ABI order. Sizes are bytes; Private
// is private commit, not resident RAM. The pagefile fields are commit counters,
// not the number of bytes physically stored in the paging file.
type processMemory struct {
	Size, PageFaults                uint32
	PeakWorkingSet, WorkingSet      uintptr
	PeakPagedPool, PagedPool        uintptr
	PeakNonPagedPool, NonPagedPool  uintptr
	Pagefile, PeakPagefile, Private uintptr
}

var getProcessMemoryInfo = win.NewLazySystemDLL("psapi.dll").NewProc("GetProcessMemoryInfo")

func readProcessMemory() (processMemory, error) {
	var process processMemory
	process.Size = uint32(unsafe.Sizeof(process))
	ok, _, err := getProcessMemoryInfo.Call(uintptr(win.CurrentProcess()), uintptr(unsafe.Pointer(&process)), uintptr(process.Size))
	if ok == 0 {
		return process, fmt.Errorf("GetProcessMemoryInfo: %w", err)
	}
	return process, nil
}

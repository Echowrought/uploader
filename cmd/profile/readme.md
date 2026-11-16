# Memory profiling

Windows-only diagnostic executable for measuring uploader memory with the real scanner/coordinator and a read-only tray. **It cannot upload:** dry run is always enabled, and its HTTP transport independently rejects requests.

It reads an explicitly selected save folder, not your normal configuration. It does not modify saves, real configuration/history, or sign-in registration. Other running uploader instances are unaffected and can still upload.

## Build targets

Run commands from the repository root:

| Target | Meaning |
| --- | --- |
| `go build .` | Normal tray application; can upload. Not the profiler. |
| `go build -tags=console .` | Normal console application; can upload unless `--dry-run` is supplied. Not the profiler. |
| `./cmd/profile` | Separate profiling command; uploads are always disabled. |

Build the profiler with the helper:

```powershell
.\scripts\build-profile.ps1
```

This creates `profiles\cwg-rpg-profile.exe`, keeps symbols for `pprof`, and selects the Windows GUI subsystem so a console window does not affect the measurement. It neither launches the executable nor changes `dist`. Profiling builds and artifacts are not included in release packages.

To compare toolchains, keep separate executables:

```powershell
.\scripts\build-profile.ps1 -Toolchain go1.25.14 -OutputDirectory profiles\go1.25.14
```

The helper temporarily overrides `GOTOOLCHAIN` for the build; Go may download the requested version. This does not change the release toolchain pin or imply that an older toolchain is supported or recommended for releases.

## Run

```powershell
$folder = "$env:USERPROFILE\Documents\Warcraft III\CustomMapData\CWG2"
$arguments = '--save-folder "{0}" --output profiles --duration 30s' -f $folder
$process = Start-Process .\profiles\cwg-rpg-profile.exe -ArgumentList $arguments -Wait -PassThru -RedirectStandardError .\profiles\error.log
$process.ExitCode
```

Adjust the save path for redirected Documents folders. Because this is a GUI executable, use `Start-Process -Wait` to wait for completion and redirect stderr to see errors.

| Option | Meaning |
| --- | --- |
| `--save-folder` | Required absolute save-folder path. Read only. |
| `--output` | Parent directory for a fresh `run-*` artifact directory; defaults to `profiles`, relative to the working directory. |
| `--duration` | Observation time before final heap capture; defaults to `30s`, must be positive. Accepts Go durations such as `15s` or `1m`. |

Detection runs every five seconds, with the normal five-second settling period. Allow enough time to prepare a settled snapshot; check `app.log` rather than assuming the duration guarantees valid saves. The tray's **Stop profiling** action exits early and may leave partial artifacts.

## Artifact targets and capture stages

Each run writes to its own `profiles\run-*` directory:

| File | What it measures or records |
| --- | --- |
| `build.json` | Go version/build settings, save-folder path, requested duration in nanoseconds, heap sampling rate in bytes, and `GODEBUG`. |
| `app.log` | Scanner and dry-run lifecycle. Look for `dry-run snapshot prepared` to confirm settled data was available. |
| `process.jsonl` | Initial and roughly one-second samples of Windows process memory and Go goroutine count; one JSON object per line. No forced GC during this observation period. |
| `before-gc.json` | Go and Windows memory counters after observation, before an explicit GC. Normal automatic GC may already have occurred. |
| `after-gc.json` | Counters after `runtime.GC()`, useful for estimating retained Go heap rather than uncollected allocations. |
| `heap.pprof` | Sampled live Go heap captured after that GC. Attributes retained allocations to functions/call stacks. |
| `after-release.json` | Counters after `debug.FreeOSMemory()`, which forces another GC and attempts to return unused memory to Windows. This is a diagnostic experiment, not production behavior. |

The scanner/coordinator remains running during final capture. These stages are successive observations, not an atomic snapshot or a controlled allocation-free interval.

## Reading the numbers

Memory sizes below are **bytes**; divide by `1MB` in PowerShell to display MiB.

| Counter | Meaning |
| --- | --- |
| `Process.WorkingSet` | Currently resident process pages, including shared/image pages. Closest here to total resident RAM, not uniquely owned RAM. |
| `Process.Private` | Private committed memory: backing Windows has committed for this process. Not necessarily resident in RAM. |
| `Process.PeakWorkingSet` | Highest working set since process startup, including initialization. |
| `Process.Pagefile`, `PeakPagefile` | Windows commit counters, not literal bytes currently stored in the paging file. |
| `Process.PageFaults` | Cumulative page faults, including faults that need no disk read; a count, not bytes. |
| `Go.HeapAlloc` | Allocated Go heap objects, including unreachable objects not yet collected. Most informative about retained objects immediately after GC. |
| `Go.HeapInuse` | Heap spans in use, including space not occupied by live objects. |
| `Go.HeapSys` | Address space obtained for the Go heap; not all of it is resident or committed. |
| `Go.HeapIdle` | Heap spans not currently in use, some retained for reuse. |
| `Go.HeapReleased` | Idle heap memory returned to the OS. |
| `Go.Sys` | Runtime-accounted memory obtained from the OS, including non-heap runtime structures. Not whole-process memory. |
| `Go.Sys - Go.HeapReleased` | Rough runtime-accounted memory excluding released heap; still not equivalent to Windows private commit or working set. |

Do not add Go counters to Windows counters: they describe overlapping memory with different accounting rules. A small live heap can coexist with substantial process commit from static globals, image mappings, and native allocations. In particular, a linked static crypto scratch buffer will not appear as a Go heap allocation.

## Heap attribution with pprof

Use the **matching executable from the same build**, especially when comparing toolchains:

```powershell
go tool pprof -top -sample_index=inuse_space .\profiles\cwg-rpg-profile.exe .\profiles\run-<id>\heap.pprof
```

- `inuse_space`: sampled bytes still allocated, rather than total allocation churn.
- `flat`: bytes attributed directly to the function.
- `cum`: bytes attributed to the function and its callees; do not sum cumulative rows.

Sampling is approximate, so totals need not exactly equal `HeapAlloc`. Heap profiles exclude static globals and native/image memory. Profiling adds overhead through 16 KiB heap sampling, periodic counters, retained symbols, and a reduced tray; it is not an exact release-process footprint measurement. No live HTTPS upload peaks or CPU profiles are captured.

Artifacts contain local paths and may reveal save-related information. Review before sharing. The default repository-root `profiles` directory is git-ignored; custom output directories may not be.

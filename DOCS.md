# Development guide

Build, release, and implementation reference for CWG RPG Uploader. User settings and troubleshooting are documented in [CONFIGURATION.md](CONFIGURATION.md). Product behavior and technical decisions are documented in [DESIGN.md](DESIGN.md). Icon artwork and asset regeneration are documented in [assets/README.md](assets/README.md).

## Build

Requires Windows or Linux and Go 1.26 or newer. Prefer a supported toolchain;
the release workflow pins its exact Go version. Windows PowerShell is used for
the Windows folder picker. Build/check scripts use PowerShell (`pwsh` on Linux).
Ordinary Windows and Linux console builds need no C compiler.

```powershell
.\scripts\check.ps1
.\build.ps1
# For a release-versioned local build:
.\build.ps1 -Version v1.2.3
```

`check.ps1` runs workflow lint, uncached tests, and vet without touching `dist`.
`build.ps1` deletes previous `dist` output, invokes those checks, and builds the
tray and console executables with documentation and notices. Failure removes
partial output. Keep anything you need to preserve outside `dist`.

For Linux, `pwsh -File ./build-linux.ps1 -Version v1.2.3` builds `dist-linux/`
on either host, without executing Linux code on Windows. Run
`pwsh -File ./scripts/check.ps1` natively on Linux to execute its tests. A bare
native build is `CGO_ENABLED=0 go build -o cwg-rpg-uploader-console ./cmd/uploader`.
Linux is always console mode, without GTK/AppIndicator or desktop dependencies.

Pull requests run the same checks natively on Windows and Linux through
`.github/workflows/checks.yml`, with the release toolchain/environment and a
read-only token. Job names are **PR checks (Windows)** and **PR checks (Linux)**;
keep required check names aligned with the default-branch ruleset. No path
filters skip checks for documentation-only changes. Linux CLI tests exercise
isolated child processes with uploads disabled, a loopback-only proxy guard,
and temporary XDG directories. Process signal/locking tests need real OS time;
shared coordinator/retry tests retain virtual time.

The tray build uses the Windows GUI subsystem; the `console` tag selects console
mode and the console subsystem, providing normal terminal waiting/redirection.
Both retain Go symbol tables and DWARF debug information. Local builds default
to `dev`; release builds embed the tag through linker `-X` into
`internal/buildinfo.Version`. Upload requests send `CWGRPGUploader/<version>` as
the User-Agent. The workflow supplies its tag to both binary builds.

`scripts/notices.ps1` is a build-only helper called by the build scripts, not a standalone
command. Tests use temporary save fixtures and local HTTP servers, not the real
leaderboard. See the test-budget and virtual-time details below. A race-detector
run needs a CGO-capable C compiler; live upload compatibility testing requires
explicit approval.

Specialized guides:

- [Installer packaging](installer/README.md)
- [Upload-disabled profiling](cmd/profile/readme.md)
- [Icon assets and regeneration](assets/README.md)
- [Release verification and exact rebuild settings](VERIFYING.md)

## Release policy

Tags use `vX.Y.Z`, with three non-negative integer components and no leading
zeros. Compare X, then Y, then Z numerically (`v1.10.0 > v1.9.9`); the components
do not promise Semantic Versioning compatibility semantics.

The tag-triggered workflow creates a draft marked **pre-release** by default,
containing the Windows tray installer, two Windows portable ZIPs, a Linux console
tar.gz, platform-specific attested build information,
verification instructions, and generated change notes. Review and download-check
it before publishing. Publishing retains the pre-release flag; explicitly clear
it when promoting to stable. Stability comes from GitHub's release flag, not a
tag suffix. Actions are pinned to commit SHAs; update those pins deliberately.

A repository administrator should enable **release immutability** before
publishing. The workflow does not enable it. When enabled, publication locks
assets and the associated tag; the title and release notes remain editable.
Attach all assets while the release is a draft and publish corrections as a new
version. Do not assume older releases become immutable retroactively. Protect
release tags and workflow changes: provenance cannot defend against malicious
changes authorized through the trusted workflow.

## Layout

Use one Go module with a small number of packages:

```text
go.mod
go.sum
cmd/
  uploader/
    main.go                 # Shared startup, console loop, and disclosure
    platform_windows.go     # Windows paths, mutex, messages, and tray wiring
    platform_linux.go       # Linux console wiring; no GUI dependencies
    mode_tray_windows.go    # Default Windows tray mode
    mode_console.go         # Shared Linux/Windows console policy; explicit save path
  profile/                  # Separate Windows-only diagnostic executable
build.ps1                   # Test, vet, and build Windows executables
build-linux.ps1             # Build/package Linux console, including cross-builds
README.md
CONFIGURATION.md
DESIGN.md
DOCS.md
scripts/
  notices.ps1          # Generate dependency license notices

internal/
  linux/
    platform_linux.go  # XDG config/state paths and per-state-directory flock

  buildinfo/
    version.go         # Linker-injected release tag; defaults to dev

  app/
    app.go             # Initialization, event loop, and per-run state
    poll.go            # Stable folder events, last-emitted deduplication
    upload.go          # Mutex-protected pending snapshot and single HTTP worker
    events.go          # Typed folder observations and upload transitions
    render.go          # Pure, centralized UI message rendering
    logging.go         # Named component loggers
    commands.go        # Tray commands and configuration changes
    status.go          # Nonblocking status publishing to the tray
    menu.go            # Independent menu rows, history date labels, and click details

  saves/
    scan.go            # Discover eligible files, read stable snapshots, hash contents

  leaderboard/
    client.go          # Upload API, response parsing, error classification
    transport.go       # HTTP client construction and proxy configuration

  storage/
    config.go          # config.json defaults, shared validation, loading, saving
    state.go           # Atomically persist successful-upload state
    logging.go         # Built-in log rotation

  windows/
    tray_windows.go    # Tray menu, status, and actions
    icon_windows.go    # Embedded static ICO; generated resources supply executable icons
    folders_windows.go # Known folders, folder picker, shell actions, instance lock
    subprocess_windows.go # Owned subprocess Job Object, launch, output, timeout
    startup_windows.go # Current-user sign-in startup registration
```

Tests live alongside implementations as `*_test.go`. Place sample saves in the relevant package's `testdata/` directory; use synthetic or sanitized fixtures, not personal save data.

## Responsibilities

- **`cmd/uploader/main.go`:** wire dependencies and start the application; keep application logic elsewhere. The `console` build tag selects console mode by default; without it the Windows executable defaults to tray mode. Linux always uses console mode. `--save-folder` validates and remembers an explicitly selected path; Both console builds require one when configuration has no save folder, without prompting or discovery, and print full settings/log-file paths before loading configuration. `--dry-run` requests local inspection in either build; there is no `--console` flag. Present the upload disclosure before starting the coordinator, recording `Config.UploadNoticeShown` only after a normal-run presentation and config save succeed. Dry runs leave it pending. Listen for both interrupt and Windows SIGTERM notifications, then cancel and drain the coordinator before closing resources; Windows close/logoff/shutdown cleanup remains subject to OS deadlines.
- **`app`:** own the synchronization workflow, lifecycle, and retry scheduling. Permit only one upload at a time.
- **`saves`:** observe every eligible file's candidate contents, cache last-settled versions, require a settled version for every essential filename, prioritize essentials within bounded cache/snapshot budgets, and fingerprint the selected settled versions. All non-private `.pld` categories are eligible, including account verification. No HTTP or tray dependencies.
- **`leaderboard`:** implement the upload protocol and classify errors. Do not read local files or retry indefinitely; scheduling belongs to `app`.
- **`storage`:** manage application configuration, successful-upload state, and rotating logs. `UploadState`, loaded/saved via `LoadUploadState` and `SaveUploadState`, is exposed as `App.LastUpload`. It records the uploaded `SaveFolder` as `save_folder` in `state.json`. Folder events carry `saveFolder`; no compatibility alias for the old `folder` key is provided. Never modify game saves.
- **`windows`:** implement OS integration without making synchronization decisions.

The `leaderboard` package owns the JSON wire types `UploadRequest` and `UploadFile`. The `app` package converts local `saves.Snapshot` values into upload requests, stripping one leading UTF-8 BOM to match the website uploader's text decoding without mutating the snapshot. The scanner preserves original UTF-8 text, including BOMs, and fingerprints that original text. Snapshot fingerprints stay local; scanner types have no protocol JSON tags. Neither `leaderboard` nor `saves` imports the other.

## Snapshot Selection

`Scanner` treats `HERO_*`, `ACCOUNT_*` (including `ACCOUNT_VERIFY_*`), `ENTITLEMENTS_*`, `SETTINGS_*`, and `LEGACY_*` as essential; eligible unknown categories are supplementary. Required-name validation still needs a hero and a non-verification account. The same per-file settlement mechanism applies to essential and supplementary files. `fileSettlement` keeps the candidate content hash, observation time/validity, and an optional last-settled `File` containing text and mtime. Matching observations spanning the quiet period promote the current version; changed or failed candidates leave the previous settled payload intact. Reading a known settled content version refreshes its mtime without another wait. No snapshot is produced until every current essential filename has a cached settled version. Their stable intervals need not overlap. Supplementary files without a cached version are omitted, never used unsettled.

`scanResult` builds a fresh selected cache and snapshot. Essential names/counts are checked before reading; their existing versions reserve capacity before updates, with potential shrinks considered before growth. Byte limits apply to selected settled versions, so oversized/unsettled on-disk candidates do not invalidate a fitting cached snapshot. A newly settled essential set exceeding 32 MiB is rejected without returning partial essential data. Supplementary versions use deterministic filename order and remaining capacity, falling back to a previous settled version when it fits or dropping optional cache payloads when necessary. Supplementary reads have a remaining-byte budget to avoid loading unselectable bulk candidates. Candidate metadata is bounded by the 4,096-entry directory limit; the retained payload cache is bounded by 32 MiB/1,024 versions. During rebuilding, old and new bounded caches may coexist. Snapshot maps copy immutable file values/strings, so cache updates or eviction cannot mutate accepted work.

Construct scanners with `saves.NewScanner(folder, quiet, saves.DefaultLimits())`. `Limits` is an immutable-per-scanner value policy covering directory entries, selected-file count, individual file bytes, and snapshot/cache bytes. The constructor copies it; `Scan` rejects invalid limits before I/O. Inventory, bounded reads, admission, and cache accounting all use this policy. Production defaults remain 4,096 entries, 1,024 saves, 8 MiB/file, and 32 MiB/snapshot/cache; these are not new user-config settings. Boundary tests pass small policies through the same workflow, retaining separate default-value, production-wiring, full-size file, and multi-batch checks.

Observations and cached versions are pruned for names absent from a successful inventory. New/reappearing essential files must obtain a settled version; a fresh scanner after restart/folder selection has no cached payloads. Directory enumeration/entry-limit failures remain fatal. Individual reads verify identity, size, and mtime; failed essential candidates may use cached versions with warnings, but a failed essential file without a cache prevents a snapshot. There is no folder-wide atomicity claim. Reads remain synchronous and cooperatively cancellable, not interruptible filesystem calls.

`Snapshot.Warnings` are not part of the fingerprint or HTTP request. The poller logs new/changed save diagnostics before event deduplication and exposes warnings even on an unchanged scan. Warnings describe either omitted supplementary files or reuse of cached settled data after a candidate failure. The renderer adds a general save-warning count without hiding delivery status. Omission or recovery can change the selected filenames and hence the fingerprint, even with unchanged essential contents. No snapshot or file maps are added to upload lifecycle events.

## Coordination

`App.Run` handles polling ticks, uploader lifecycle events, and user commands. `runState.tick` records a typed folder observation and forwards any returned immutable snapshot directly to the uploader. The upload command calls `poller.requestEmission()` directly, preserving the poller's latch across settling ticks. It never drives result handling, dispatch, or retry scheduling. `folderPoller.poll` scans the save folder and returns an observation carrying an optional changed snapshot in its `update` field. An unexported `runState` coordinates a `folderPoller` and a `snapshotUploader`:

```text
save scanner -> folderPoller -> coordinator -> mutex-protected pending snapshot
                                                | buffered wake-up signal
                                                v
                                          single HTTP worker
                                                |
coordinator <- reliable lifecycle-event channel -+
     |
     +-> upload history + renderer -> latest-status UI channel
```

The poller compares stable snapshots against its in-memory `lastEmittedFingerprint`. The coordinator seeds that initial baseline from `App.LastUpload` only when the save folder and destination match (excluding dry runs). Snapshots return synchronously to the coordinator without a poller-owned channel. The uploader shares one latest-pending snapshot under a mutex. One worker owns synchronous HTTP calls, the failed-snapshot fallback, retry timing, and the failure counter. Its handle receives a lifetime context, upload client, dry-run setting, and named logger; it has no `App` reference, persistence access, or callback. `queue(ctx, snapshot)` replaces the pending snapshot under a short mutex lock and sends a nonblocking wake-up signal. It is safe from any goroutine and never waits for HTTP, retries, or lifecycle-event delivery. A successful return means acceptance, not upload completion; pending work may be superseded by newer snapshots. There are no command kinds, reply messages, or reset protocol. Snapshots must remain immutable after handoff. The coordinator consumes every successful-upload event, including successes from a previously selected folder, updates `App.LastUpload`, and persists history. If that write fails, `view.persistenceError` also marks history as needing another write: every tick retries the latest `App.LastUpload` before polling, even with Automatic uploads off or a scan failure. Recovery clears the warning and refreshes status; identical consecutive errors are not logged repeatedly. This retries bookkeeping, never HTTP, and needs no extra flag or timer. A transient failure retains pending data and retries without requiring another folder event. Newer events supersede unsent data, including failed data awaiting retry. The uploader trusts every event; it does not suppress snapshots based on previous success or rejection. A permanent failure is always logged and emits a snapshot-specific rejection event only if no newer event is pending. There is no stored rejection object in the uploader: the renderer retains the lifecycle event for display. New snapshots do not clear shared delivery backoff.

The coordinator loop exclusively owns application, poller, and view state. The uploader's single worker takes the latest pending snapshot, calls the HTTP client synchronously, reports the outcome, and waits for backoff when needed. Only the pending slot is shared with queue callers; its mutex is never held during HTTP, event sends, or retry waits. There is no per-request goroutine, private result channel, or separate uploader-owned delivery-state object. After reporting a result, the worker takes the latest eligible pending snapshot without coordinator intervention. Scanning continues while uploads are active. Command handlers trigger immediate checks for upload, Automatic uploads changes, and folder changes. Windows startup registration belongs entirely to the tray and platform integration: it is read from Windows, changed directly, and never sent through app commands, configuration, upload status, or polling. Upload now calls `poller.requestEmission()` before the immediate tick. The poller clears its latch only after emitting a stable snapshot, bypassing event deduplication even for unchanged files. Selecting another folder replaces only the poller, discarding its un-emitted request. The poller receives the Automatic uploads setting and owns the decision to scan: automatic polling is disabled when the setting is off, but a forced emission continues through settling. Folder updates carry `saveFolder` and `snapshot`; the coordinator adds a monotonic work ID before forwarding. Neither folder updates nor upload lifecycle events carry a force/manual flag. Once emitted, work is accepted for delivery regardless of the setting. Queued uploads, active requests, and retries continue independently. No one-off permission state is needed; Quit cancels delivery.

On restart, matching successful history supplies the poller's baseline. This avoids re-emitting unchanged successful data while allowing emitted-but-undelivered changes to be emitted again. A forced emission is always actionable, even when its fingerprint matches previous upload history.

Before each attempt, the worker takes the latest pending snapshot, preferring it over failed data retained for retry. With no work, it waits for a buffered wake-up signal or cancellation; it never polls the save folder. A retry timer is armed only on a retryable failure, before reporting the retry event, so a busy event consumer does not shift the deadline. Incoming snapshots replace pending work without interrupting or postponing backoff. Retry timing operates independently of application polling ticks and Automatic uploads.

### Component Events and Presentation

`events.go` defines folder observations and upload lifecycle events without user-facing text. Upload events implement a private `uploadEvent` interface through a marker method and common `sourceFolder` and `workID` accessors, with separate value types: `uploadStarted`, `uploadRetryScheduled`, `uploadRejected`, `uploadSucceeded`, and `uploadPreviewed`. Each type carries only its own metadata, never a snapshot or file map. Events include the work ID and source folder; successes add the fingerprint, server result, and completion time, previews add the file count, and rejections add the error. Retry scheduling carries the failed attempt's identity and the retry deadline. This identity supports presentation filtering; it does not reserve the next attempt, because newer pending work can replace failed data while backoff remains shared. Consumers use type assertions or switches; `recordUpload` accepts only `uploadSucceeded`. The initial delivery projection has no pending or active work. Go checks interface implementation but does not enforce exhaustive type switches.

Folder observations return directly to the coordinator. The uploader has exactly one outbound lifecycle-event stream, with no callbacks, raw result channel, or outbox. The worker sends events directly and may pause until the coordinator receives them. Queueing snapshots needs only the pending-slot mutex, so it remains available during event backpressure and cannot form an input/event blocking cycle. Success events are never coalesced or dropped; the separate UI status channel may still keep only its latest message. There is no general-purpose event bus.

`delivery.go` projects accepted work and lifecycle transitions into latest-pending identity, active/backoff activity, and selected-folder rejection/preview feedback. The coordinator records pending acceptance immediately after `queue` succeeds, without waiting for worker events. A lifecycle event acknowledges that the worker took that emission; uploads report started and dry runs report previewed, with no separate queue event. Work IDs distinguish repeated emissions of identical snapshots: delayed events for older work cannot clear the latest pending emission. This projection owns no scheduling or save contents.

`render.go` composes independent detection, delivery, and successful-history facts, plus command and persistence errors. History is passed directly from `App.LastUpload`, not duplicated in the view. An unchanged scan means only "No new changes", never "Up to date". Paused or settling detection remains visible alongside accepted delivery and historical success. Pending work remains visible during another folder's request or retry; old-folder activity is labeled "previous folder" rather than hidden. Polling never inspects uploader state, and the uploader never calls a renderer or changes `App.LastUpload`. Scan and persistence errors remain visible during active delivery.

Operational logging is independent of UI rendering. Components use `[poller]`, `[uploader]`, and `[app]` logger names. Per-work coordinator and uploader messages include `work_id=N` context; retries retain the same session-local ID, while each newly accepted emission gets a new ID. Child loggers route writes through the shared base logger, preserving serialization even when the underlying writer is not concurrency-safe. Rendering/status delivery does not duplicate those logs.

Configuration loading and saving both call `Config.Validate`, including on first-run defaults. `LoadConfig` takes a default-folder resolver and invokes it only for absent configuration or an omitted save-folder setting, never for a complete override or an explicitly invalid folder. Validation checks polling bounds and that the literal save folder is absolute. The app does not expand environment variables or `~` in paths; shells may expand CLI arguments before passing them to the app. Command handlers edit a local config copy, validate and save it, and only then replace `App.Config`; invalid candidates or failed writes never reach the live config. No per-field property setters are needed.

Choosing another save folder writes configuration, replaces the poller, and clears selected-folder rejection/preview feedback. Pending and active/backoff work remain represented because accepted delivery continues. Later terminal feedback is shown only when its source folder matches the current normalized selection, using Windows case-insensitive or Linux case-sensitive comparison; old-folder successes are always recorded and persisted as history. Old-folder events do not clear current command errors. A failed config write leaves existing polling, presentation, and delivery unchanged. The uploader is neither reset nor notified about the selection: active and pending jobs continue, backoff remains in force, and new snapshots replace pending data through the existing coalescing policy. Source-folder identity travels with each immutable snapshot and its completion event, so an old job never gets recorded as originating from the newly selected folder.

Shutdown cancels the uploader's HTTP call or retry wait and drains lifecycle events through channel closure before waiting for the single worker. The worker cannot exit before its synchronous HTTP call returns, so no private result channel or worker `WaitGroup` is needed. A request that actually succeeded is still reported and persisted during shutdown. New queue calls fail once stopping begins.

The tray shows independent **Detection**, **Upload**, and **Last success** rows.
`Status.Menu(now)` produces compact labels and untruncated click details from
published view projections, not by parsing the combined status string. The poller
owns a session-local observation timestamp for the current settled fingerprint.
Detection shows `No changes since HH:mm`, adding a date for earlier days;
click details explain the observation scope and retain seconds. The timestamp
starts on the first successful settled scan, not at the last upload or a file's
mtime. New settled contents, folder changes, pauses, and scan failures reset the
observation window. Forced delivery of unchanged contents does not reset it.
The timestamp is not persisted or reconstructed from upload history. History
uses the viewer's calendar/timezone: `Today, HH:mm` for the current day, otherwise
`YYYY-MM-DD, HH:mm`. Click details retain seconds, source folder, and delivery
metadata. A history-write failure is marked on the successful-history row without
claiming the remote upload failed. App ticks refresh labels across midnight;
there is no separate UI scheduling state. Command failures remain visible as
user-action error dialogs, while background scanning/retry errors stay in their
respective status rows. Commands carry optional one-shot result channels; the
tray supplies a fresh channel buffered to one and consumes its result independently
of status updates. The coordinator replies with an error or nil after handling
the command, with cancellation unblocking abandoned replies during shutdown.
A nil result acknowledges the command, not a successful remote upload. Ordinary
status updates still coalesce; command-failure dialogs never depend on receiving
a transient status error or comparing its text with an earlier error.
The upload row uses `Retry at …` during backoff and `Retrying saves` for a repeat
attempt. A new snapshot's first attempt remains `Uploading saves`, even when
it shares an older snapshot's backoff. The worker reports this attempt metadata
with its start event; the view does not infer it from previous UI messages.
The tooltip uses the same labeled sections. The combined message remains
available for console dry-run completion and profiling.

**Automatic uploads**, **Upload now**, **Open leaderboard**, and **Quit** remain
on the main menu. **Settings** contains folder selection, sign-in startup,
configuration, and logs.

The tray sends commands such as toggling Automatic uploads and Upload now to the application coordinator. Its sign-in checkbox independently queries `StartupEnabled` and invokes `SetStartup`; startup changes only update that checkbox, and errors are shown directly. The registration must target the current executable. Launching the app does not recreate or overwrite registration, and legacy `launch_at_login` JSON fields are ignored. Windows policy can independently suppress registered startup entries. The coordinator publishes status updates back to the tray through a thread-safe boundary. Background workers must not directly modify UI state; UI updates must follow the tray library's threading requirements.

The tray owns a generic `subprocessJob`: a non-inheritable, kill-on-close Job Object for children that must not outlive the uploader. Native process creation uses `PROC_THREAD_ATTRIBUTE_JOB_LIST` to assign membership atomically on Windows 10+, with an explicit inherited-handle list limited to child standard I/O. Closing the tray closes its job; forced uploader termination also closes the last job handle and kills owned children. A short mutex protects process creation against concurrent job closure, but is not held while waiting for a child. Output is drained concurrently with a caller-supplied process wait/timeout. The folder picker is currently the only consumer; its PowerShell script, five-minute timeout, and BOM-only path decoding belong to `folders_windows.go`. Whitespace in selected paths is preserved. Browsers and editors opened with `ShellExecute` are independent and are not placed in the job. Tooltips fit within 127 UTF-16 units plus the Windows terminator, while menu-title limits remain rune-based.

Keep core save scanning and HTTP behavior independent of Windows UI code so they can be tested without launching the tray. `cmd/uploader/main.go` provides shared startup; platform-specific files use `_windows.go` or `_linux.go` filenames. Run `scripts/check.ps1` natively on both Windows and Linux. Cross-compilation checks the target build but does not execute its tests.

## Implementation Style

- Avoid a generic event bus, dependency-injection framework, or interfaces for every type.
- Introduce narrow interfaces where they provide a concrete testing boundary, such as substituting a fake upload client.
- Put each file's main workflow first, followed by its direct helpers and lower-level utilities.
- Keep this small structure and split packages only when real complexity justifies it.
- The module currently requires Go 1.26+. Use stable `testing/synctest` for virtual-time tests. Run shared tests with `go test ./...` and `go vet ./...`, then check the console entry point with `go test -tags=console ./cmd/uploader` and `go vet -tags=console ./cmd/uploader` on Windows; additionally use `go test -race ./internal/app` when a CGO-capable C compiler is installed. Ordinary Windows builds do not require that compiler.
- Test each component through its own operations and results, regardless of Go visibility. `folderPoller` tests call `poll` and `requestEmission` and assert observations/emissions. `snapshotUploader` tests use `queue`, lifecycle events, `stop`, and `wait` to verify coalescing, backpressure, retries, and shutdown without starting an app or scanner. Assert event identity against supplied work, not internal counters, pending slots, or force flags. Keep focused retry-policy tests; configure small `RotatingLog.max` and `backups` values for rotation tests. `Scanner.Scan` covers settlement and admission behavior; local HTTP fixtures cover routing and protocol behavior.
- Keep app integration scenarios in `poll_integration_test.go`, `upload_integration_test.go`, and the other coordinator tests. They verify command/configuration wiring, status publication, accepted old-folder work, persistence, and propagation of shutdown to the worker. They complement component tests rather than replacing them. Verify log correlation IDs remain stable across retries and distinct across work, not that counters start at a particular number.

`scripts/check.ps1` runs tests through `scripts/go-tests.ps1` with `go test -json -count=1 -p 1`. Packages run sequentially to avoid fixture filesystem contention and excessive concurrent test-process memory; tests within a package may still run in parallel. Every package must report less than 15 seconds of execution, independently for shared and console-entry runs. Compilation, dependency downloads, lint, and vet do not enter this budget. Never sum test durations: parallel tests and nested reports overlap. Budget failures list the five slowest top-level tests in the affected package. `scripts/test-go-tests.ps1` validates the reporter with canned events in the same PowerShell process, without long sleeps or subprocess launches. The release build invokes these checks too; raw `go test` alone does not enforce the budget.

Timer-heavy app tests run their fixtures, goroutines, channels, contexts, and assertions inside `synctest.Test` bubbles. Keep `t.Parallel()` outside the bubble and register cleanup on its inner `*testing.T`. Virtual time changes only tests, not business-code clocks or durations. Exact-boundary retry tests verify no completion one nanosecond before the deadline and completion at it. Dedicated filesystem and subprocess tests remain outside bubbles; scanner tests supply observation timestamps directly.

### Specialized test boundaries

- Scanner cache-budget assertions inspect retained payload sizes: a bounded published snapshot cannot prove bounded retained memory. They do not assert pointer identity, timer bookkeeping, or the production accounting helper's result.
- Windows adapter tests use disposable registry keys, owned child processes, and pure tooltip/path formatting helpers. Calling the real startup registration or folder dialog would affect the user or require interactive UI. The replacement PowerShell fixture tests encoding/transport, not the complete interactive picker.
- Command tests isolate disclosure presentation and profiling capture without real dialogs, user-config writes, or tray startup. The profiling test that disables dry-run deliberately verifies the independent network-denial safety guard; factory wiring is not asserted.
- HTTP routing and protocol tests use local servers; fixture-only DNS routing and certificate trust keep connections local.

Tests may motivate explicit configuration and dependency boundaries when they clarify component responsibilities or behavior. Avoid exposing internal bookkeeping or introducing disproportionate production machinery merely for test access.

## Reference Project

[restic/restic](https://github.com/restic/restic) is a real Go application illustrating a similar responsibility-based organization:

- [`cmd/restic/`](https://github.com/restic/restic/tree/master/cmd/restic): executable entry point and commands.
- [`internal/`](https://github.com/restic/restic/tree/master/internal): implementation packages separated by responsibility.
- Tests alongside their implementations.

Restic is much larger and is not a tray application. It is a structural reference, not an exact template: the uploader layout above is tailored to this application rather than copied from restic.

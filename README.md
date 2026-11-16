# CWG2 Uploader

A Windows 10+ system-tray application written in Go. It watches CWG2 saves and automatically submits stable snapshots to the [CWG2 public leaderboard](https://cwg2-leaderboard.rskiland.workers.dev/#leaderboard).

**Starting the app enables automatic uploads by default, including existing saves.** Saves go to a third-party service and may appear on its public leaderboard. This app is not affiliated with the leaderboard service. It never modifies game saves.

## Run

1. Place `cwg2-uploader.exe` in a permanent location and launch it.
2. Read the upload notice before the first normal run. Console mode prints it; dry runs leave the normal-run notice pending. Look for the uploader in the Windows notification area (possibly under the hidden-icons arrow).
3. The app finds your Windows Documents folder and watches `Warcraft III\CustomMapData\CWG2`. Usually this is `%USERPROFILE%\Documents\Warcraft III\CustomMapData\CWG2`.
4. If needed, use **Choose save folder...** in the tray menu. The picker closes when the uploader exits, including forced termination.

The app polls every five seconds and waits at least five seconds of observed stability before uploading. Internet failures retry automatically. Invalid submissions stop retrying until the saves change or you choose **Upload now**.

### Tray menu

- **Status:** click the top item for the full message and save folder.
- **Last upload:** last confirmed successful submission.
- **Automatic uploads:** uncheck to stop sending new saves automatically. Already queued uploads and retries continue until they finish.
- **Upload now:** send the current saves even with Automatic uploads off. Stability checks and server retry delays still apply.
- **Choose save folder...:** change the watched folder at any time. Existing uploads continue; new saves replace older queued saves as usual.
- **Launch at Windows sign-in:** register or remove a current-user startup entry. No administrator rights needed. The checkbox reads the Windows registration for this executable, not `config.json`; launching the app never rewrites that registration. After moving the executable, enable the option from its new location. Windows startup policy can independently prevent registered apps from running.
- **Open leaderboard**, **Open logs**, and **Open config.json**.
- **Quit:** cancel active network work, stop retries, and exit.

Successes stay quiet. Check the tray status for errors; bounded logs provide diagnostics. Only one instance can run per Windows session.

## Configuration and logs

Application data lives in `%LOCALAPPDATA%\CWG2Uploader\`, not alongside the executable:

- `config.json`: human-editable settings.
- `state.json`: the last successful snapshot fingerprint and upload summary.
- `uploader.log`: automatically rotates at 2 MiB, keeping three previous files. Individual entries are capped at 256 KiB, including a truncation marker; console output is not truncated. Entries identify their source with `[app]`, `[poller]`, or `[uploader]`.

Example `config.json`:

```json
{
  "save_folder": "%USERPROFILE%\\Documents\\Warcraft III\\CustomMapData\\CWG2",
  "poll_interval_seconds": 5,
  "upload_automatically": true,
  "proxy_url": ""
}
```

Initial discovery honors redirected Documents folders, including OneDrive. Documents discovery runs only when the configuration is missing or `save_folder` is omitted; an explicit configured folder does not depend on Documents being available. The app-managed `upload_notice_shown` setting is saved only after the first normal-run notice is successfully presented. Missing or false means the notice is still pending; dry runs do not mark it shown. Configured `%VARIABLE%` references are expanded at runtime. Restart after manual configuration edits; tray changes are persisted immediately. Loading and saving share validation: the expanded save folder must be absolute and the polling interval must be between 1 and 3600 seconds. Invalid tray changes leave the existing configuration untouched. The old `launch_at_login` setting is ignored; startup registration is managed directly through Windows. To disable automatic uploading before a first real launch, create this configuration with `upload_automatically` set to `false`.

Do not delete `state.json` unless you want existing saves submitted again. Invalid configuration or upload history stops startup rather than silently resetting preferences or history. Configuration must be a JSON object; present settings cannot be null, while omitted settings retain their defaults. Existing history must include a save folder, destination, fingerprint, upload timestamp, player, and positive hero count. Both configuration and history are validated before saving. Outgoing save contents are not logged. Error messages include a quoted preview of up to 512 response-body bytes, with a truncation marker when needed. Full error response bodies are additionally logged only when smaller than 4 KiB. Newlines and control characters are escaped, and valid UTF-8 characters are not split. A server response could echo sensitive data, so review logs before sharing them.

### Proxy settings

Set `proxy_url` in `config.json` and restart the app:

- `""` (default): use environment proxy settings, otherwise connect directly. Uploads use HTTPS, so `HTTPS_PROXY` is the relevant proxy variable.
- `"http://127.0.0.1:8080"`: HTTP proxy (HTTPS uploads use CONNECT tunneling).
- `"https://proxy.example:8443"`: TLS connection to the proxy.
- `"socks5://127.0.0.1:1080"` or `"socks5h://127.0.0.1:1080"`: SOCKS5 proxy with proxy-side destination resolution.
- `"direct"`: connect directly unless an environment proxy is set for the request's scheme.

Proxy selection is environment-first per request scheme: `HTTPS_PROXY` or `HTTP_PROXY`, then configured `proxy_url`, then direct. Lowercase environment names are also supported. `NO_PROXY` bypasses both environment and configured proxies; localhost and loopback addresses also bypass proxying, following Go's standard behavior. Environment settings are captured when the HTTP client is created.

Optional credentials use `scheme://username:password@host:port`; percent-encode special characters. Credentials are stored as plaintext in `config.json`, so do not share it. Invalid proxy configuration stops startup instead of silently connecting directly. TLS certificate validation remains enabled.

Windows system proxy settings and PAC scripts are not automatically imported. Restart after changing configuration or environment variables; changing sign-in startup behavior does not import a terminal's proxy environment into Windows Explorer.

## Supported save layout

The uploader reads regular `.pld` files directly inside the selected folder, matching names case-insensitively. Only `PRIVATE_*` names are excluded from eligibility. `ACCOUNT_VERIFY_*` and unknown `.pld` categories are eligible too; nested layouts and account selection are not supported yet.

Every eligible file settles independently by matching content hashes across polls spanning the quiet period. The scanner caches its last settled text and modification time. While a new version is changing, unreadable, or invalid, snapshots keep using the previous settled version; new candidate bytes never enter a snapshot early. A known settled content version can be reused immediately if it reappears, and timestamp-only changes do not require new settling.

- **Essential:** all `HERO_*`, `ACCOUNT_*` (including `ACCOUNT_VERIFY_*`), `ENTITLEMENTS_*`, `SETTINGS_*`, and `LEGACY_*` saves. At least one hero and one non-verification account are required. No snapshot is produced until every current essential filename has a settled version. Their quiet periods need not overlap, and a snapshot can combine versions settled on different scans. New essential filenames reopen the gate. Observed disappearance removes a filename's cache, so a later reappearance must settle again. Multiple-account validation remains the backend's responsibility.
- **Supplementary:** eligible `.pld` files with unknown category prefixes, such as miscellaneous data. Use their cached settled versions when they fit the remaining budget; omit files with no available settled version rather than blocking essentials. The tray reports save warnings; logs explain skipped files or fallback to cached data. Manual uploads use these same settled-version rules.

Safety limits are 8 MiB per file, 32 MiB per snapshot, 1,024 uploaded files, and 4,096 total directory entries (including ignored files and subdirectories). Limits apply to selected settled versions, not the total size of newer unsettled candidates on disk. Essential versions reserve their budgets first; an over-limit settled essential set fails rather than producing a partial snapshot. Supplementary versions use remaining capacity in deterministic filename order and may be evicted to admit essential updates. The retained settled payload cache is also limited to 32 MiB and 1,024 versions; candidate state contains only hashes/times, and supplementary read work has a remaining-byte budget. Cache rebuilding may temporarily hold old and new bounded caches; accepted uploads can also retain older immutable snapshots. Directory-enumeration failures and entry-limit overflow always reject the scan: an incomplete listing cannot prove the essential set is complete.

Fingerprints cover the settled versions actually selected, not current candidate bytes, timestamps, or warnings. Candidate changes alone therefore do not trigger another upload. Each file's settled version advances only after its replacement settles; previously returned snapshots stay immutable. Open handles do not prove completion: reads check identity, size, and modification time before/after reading. The selected versions need not come from the same scan, and this is not a game-level transaction guarantee.

## Build and test

Requires Windows, Go 1.25 or newer, and Windows PowerShell for the built-in folder picker. Prefer a current supported Go toolchain. No C compiler is required for a normal Windows build.

Run checks without building release packages or changing `dist`:

```powershell
.\scripts\check.ps1
```

This runs pinned `actionlint` against the GitHub workflows, plus tests and `go vet` for shared code and both entry-point builds. Shared packages run once; the console tag is checked separately on the main package. Tests run uncached, and every package must report under 10 seconds of test execution. This uses Go's JSON-reported package time, not compilation/download time or summed parallel-test durations. A budget failure lists the slowest top-level tests. The first run downloads the lint tool through Go; it does not add it to the application's dependencies. The script works regardless of your current directory when invoked by its path.

To check and build:

```powershell
.\build.ps1
```

This deletes the previous `dist` directory, runs `scripts\check.ps1`, then produces:

- `dist\cwg2-uploader.exe`: starts in tray mode, without a console window.
- `dist\cwg2-uploader-console.exe`: starts in console mode, without the tray, and prints logs to the terminal. No mode flag is needed.

Both binaries retain Go symbol tables and DWARF debug information for debugging and inspection, at the cost of larger executables.

Both binaries share the same application code. Windows records the GUI/console subsystem in the executable: the GUI build avoids an unwanted console window, while the console build works naturally with terminal output, redirection, and shell waiting. Keeping them separate avoids console attachment/allocation code and GUI-process shell quirks. The console build uses the `console` Go build tag; the regular build does not.

It also includes the README, verification instructions, and license notices. Failed builds remove partial `dist` output; keep anything you want to preserve elsewhere. `scripts\notices.ps1` is an internal helper called by `build.ps1`, not a standalone command.

Or run commands individually:

```powershell
go test ./...
go vet ./...
go build -trimpath -ldflags="-H=windowsgui" -o dist/cwg2-uploader.exe .
go build -tags=console -trimpath -o dist/cwg2-uploader-console.exe .
```

Inspect one stable snapshot without submitting anything:

```powershell
.\dist\cwg2-uploader-console.exe --dry-run
```

Dry run prints the eligible file count and exits after reading a valid stable snapshot. It does not upload or write successful-upload state; it still creates default configuration and logs if absent. If saves are missing, it keeps waiting; press Ctrl+C to stop. Use the console binary for command-line output; the GUI binary also accepts `--dry-run`, but has no guaranteed terminal output.

**Running the console executable without `--dry-run` starts real automatic uploading according to configuration.** It does not require `--console`; that option has been removed. Quit any running tray instance before starting the console executable.

Automated tests use temporary save fixtures and local HTTP servers, not the real leaderboard. Timer-heavy app tests use Go's `testing/synctest` virtual clock: production settling periods, retries, and deadline checks are preserved without waiting real seconds. Filesystem limits and subprocess tests still exercise real OS operations. Scanner boundary tests pass a smaller per-instance policy (16 entries, 8 saves, 64-byte files, 256-byte snapshots) through the same code as production. Separate checks preserve the production defaults, multi-batch enumeration, and a full-size 8 MiB file boundary. A live upload still needs compatibility verification with explicitly approved saves.

### Memory profiling build (no uploads)

See [cmd/profile/readme.md](cmd/profile/readme.md) for profiling builds, usage, safety guarantees, and interpreting memory measurements.

## Distribution

Distribute the tray executable; players do not need Go installed. Builds are currently unsigned, so Windows SmartScreen may display a warning. Keep third-party license notices with releases (see `THIRD_PARTY_NOTICES.md`).

To uninstall, first disable **Launch at Windows sign-in**, quit, and delete the executable. Optionally delete `%LOCALAPPDATA%\CWG2Uploader\`. Removing local data does not remove submissions already held by the leaderboard service.

### Verify a release

Want to check where your download came from? See [VERIFYING.md](VERIFYING.md) for attestation checks and step-by-step instructions to rebuild the release tag and compare executable hashes. No team access is needed. Use the copy of that document linked from your release, so the instructions match its build settings.

### Publishing and immutability

Release tags use `vX.Y.Z`, with three non-negative integer components and no leading zeros. Versions are ordered numerically by X, then Y, then Z (for example, `v1.10.0` is newer than `v1.9.9`). The components do not promise Semantic Versioning compatibility semantics.

The tag-triggered workflow creates a draft marked **pre-release** by default, containing both packages, the attested build-info file, verification instructions, and generated change notes. Review and download-check it before publishing. Publishing retains the pre-release flag; explicitly clear it when promoting a release to stable. Stability is determined by GitHub's release flag, not a suffix in the tag. Actions are pinned to full commit SHAs; update those pins deliberately when upgrading actions.

An organization/repository administrator should enable **release immutability** in the repository's GitHub settings before publishing new releases. This setting is not enabled by the workflow. When enabled, publication locks the release assets and associated tag; the title and release notes remain editable. Finish attaching all assets while the release is a draft, and publish corrections as a new version. Do not assume previously published releases become immutable retroactively. Protect release tags and review workflow changes as well: provenance cannot protect against a malicious change authorized through the trusted workflow.

## License

[MIT](LICENSE). Third-party dependencies retain their respective licenses; see [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md). Include both files with distributions.

## Development documentation

- [DESIGN.md](DESIGN.md): behavior and architecture decisions.
- [DOCS.md](DOCS.md): package organization and reference project.

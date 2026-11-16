# CWG RPG Uploader

A Windows 10+ tray app that watches CWG RPG saves and uploads them to the [public leaderboard](https://cwg2-leaderboard.rskiland.workers.dev/#leaderboard). It never modifies game saves.

**Automatic uploads are enabled by default, including existing saves.** Saves go to a third-party service and may appear on its public leaderboard. This app is not affiliated with that service. Read the upload notice before the first normal run.

## Install and run

Download a package from [Releases](https://github.com/cwg-rpg-community/uploader/releases):

| Package | Use it for |
| --- | --- |
| Windows installer | Normal tray use, with optional shortcuts and launch at sign-in |
| Portable tray ZIP | Tray use without installation |
| Portable console ZIP | Terminal output and troubleshooting |

The installer offers to launch the app when it finishes; uncheck that option if you are not ready to upload. For portable use, extract the ZIP to a permanent folder and launch `cwg-rpg-uploader.exe`.

Look for the tray icon in the Windows notification area, possibly under the hidden-icons arrow. The app finds your Windows Documents folder, including redirected/OneDrive locations, and watches:

```text
Warcraft III\CustomMapData\CWG2
```

`CWG2` is the map's save-directory name, independent of the app's branding. Use **Choose save folder...** if the detected location is wrong.

Builds are currently unsigned, so Windows SmartScreen may warn. See [VERIFYING.md](VERIFYING.md) for release verification and [installer/README.md](installer/README.md) for installer options and upgrades. Players do not need Go installed.

## Using the tray

| Action | Behavior |
| --- | --- |
| **Status** | Click the top item to see the full status and save folder |
| **Last upload** | Shows the last confirmed successful submission |
| **Automatic uploads** | Enables/disables detection of new changes; accepted uploads and retries continue |
| **Upload now** | Requests a snapshot even when automatic detection is off; still respects settling and retry delays |
| **Choose save folder...** | Changes the watched folder; already accepted work from the previous folder can still finish |
| **Launch at Windows sign-in** | Changes the current-user Windows startup registration; no administrator rights needed |
| **Open leaderboard / logs / config.json** | Opens the selected resource |
| **Quit** | Cancels active network work and stops retries |

Successful uploads stay quiet. Check the status and logs for errors. Only one instance can run per Windows session. After moving a portable executable, enable sign-in startup from its new location if needed.

### Which saves are uploaded?

The app reads regular `.pld` files directly inside the selected folder, case-insensitively. It excludes `PRIVATE_*`, but includes account-verification files and unknown categories. It does not scan subfolders or select between accounts; the backend validates the submitted accounts.

It polls every five seconds and requires at least five seconds of observed stability. Hero, account, entitlements, settings, and legacy files are essential: every such file present needs a settled version, with at least one hero and one non-verification account required. While a replacement settles, the app can keep using its previous settled contents. Supplementary files do not block the essential set.

Safety limits are 8 MiB per file version, 32 MiB per snapshot, 1,024 selected files, and 4,096 directory entries. Save warnings explain omissions or fallback to older settled data. See [DESIGN.md](DESIGN.md#snapshot-rules) for the full selection rules.

Transient failures retry automatically. Rejected submissions wait for changed saves or **Upload now**.

## Configuration and logs

Application data lives in `%LOCALAPPDATA%\CWGRPGUploader\`, even for portable copies:

- `config.json`: settings. Restart after manual edits; tray changes apply immediately.
- `state.json`: successful-upload history. **Deleting it can cause existing saves to be submitted again.**
- `uploader.log`: rotating diagnostic logs. Server error responses can contain sensitive data; review logs before sharing.

The [configuration reference](CONFIGURATION.md) covers settings, disabling automatic uploads before first launch, proxies, and startup errors. Invalid configuration/history stops startup rather than silently resetting it.

## Console and dry run

To inspect a settled snapshot without uploading:

```powershell
.\cwg-rpg-uploader-console.exe --dry-run
```

Dry run prints the eligible file count and exits once a valid settled snapshot is available. If saves are missing, it keeps waiting; press Ctrl+C to stop. It can create default configuration and logs, but does not upload, record successful-upload history, or acknowledge the normal-run upload notice.

**Without `--dry-run`, the console app performs real uploads according to configuration.** Quit the tray app before starting it. The tray executable also accepts `--dry-run`, but has no guaranteed terminal output.

## Uninstall

For an installed copy, quit the app and uninstall through Windows Installed apps. For a portable copy, disable **Launch at Windows sign-in**, quit, and delete the executable.

Configuration/history are retained. Optionally delete `%LOCALAPPDATA%\CWGRPGUploader\` to remove local data. This does not remove submissions already held by the leaderboard.

## Build and contribute

Requires Windows, Go 1.26 or newer, and Windows PowerShell for the folder picker. Prefer a supported Go toolchain; ordinary builds need no C compiler.

```powershell
.\scripts\check.ps1 # Lint, uncached tests, and vet; each package must finish tests in under 10s
.\build.ps1         # Run checks and rebuild dist (deletes its previous contents)
```

Builds default to version `dev`. Use `build.ps1 -Version vX.Y.Z` to embed a release version; the release workflow supplies its tag automatically.

- [Development guide](DOCS.md): builds, tests, release policy, and code organization.
- [Design](DESIGN.md): synchronization, file selection, and safety contracts.
- [Profiling](cmd/profile/readme.md): upload-disabled memory diagnostics.

## License

[MIT](LICENSE). Keep the [third-party notices](THIRD_PARTY_NOTICES.md) with distributions.

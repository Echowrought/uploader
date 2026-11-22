# CWG RPG Uploader

Watches CWG RPG saves and uploads them to the [public leaderboard](https://cwg2-leaderboard.rskiland.workers.dev/#leaderboard). Available for Windows 10+ (tray or console) and Linux x86-64 (console only). It never modifies game saves.

**Unofficial community tool — not an official CWG RPG application.**

**Automatic uploads are enabled by default, including existing saves.** Saves go to a third-party service and may appear on its public leaderboard. Read the upload notice before the first normal run.

## Install and run

Download a package from [Releases](https://github.com/cwg-rpg-community/uploader/releases):

| Package | Use it for |
| --- | --- |
| Windows installer | Normal tray use, with optional shortcuts and launch at sign-in |
| Portable tray ZIP | Tray use without installation |
| Portable console ZIP | Windows terminal output and troubleshooting |
| Linux console tar.gz | Linux use without a desktop, tray, or C runtime dependency |

### Windows

The installer offers to launch the app when it finishes; uncheck that option if you are not ready to upload. For portable use, extract the ZIP to a permanent folder and launch `cwg-rpg-uploader.exe`.

Look for the tray icon in the Windows notification area, possibly under the hidden-icons arrow. The tray app finds your Windows Documents folder, including redirected/OneDrive locations, and watches:

```text
Warcraft III\CustomMapData\CWG2
```

`CWG2` is the map's save-directory name, independent of the app's branding. Use **Choose save folder...** if the detected location is wrong.

Builds are currently unsigned, so Windows SmartScreen may warn. See [VERIFYING.md](VERIFYING.md) for release verification and [installer/README.md](installer/README.md) for installer options and upgrades. Players do not need Go installed.

### Linux

Extract the Linux archive and first inspect your saves without uploading:

```sh
./cwg-rpg-uploader-console --dry-run --save-folder "/absolute/path/to/Warcraft III/CustomMapData/CWG2"
```

Use the actual folder used by Wine/Proton; the app does not guess prefixes or scan for installations. The path is remembered in configuration. **When ready for real uploads**, connect your Battle.net account and then start the uploader:

```sh
./cwg-rpg-uploader-console --authenticate
./cwg-rpg-uploader-console
```

Ctrl+C or SIGTERM stops the app and its retries. There is no Linux tray, folder picker, startup registration, or installer, and no systemd dependency. HTTPS uploads need system CA certificates. Only one instance can use the same state directory at a time.

## Using the Windows tray

| Action | Behavior |
| --- | --- |
| **Detection** | Shows scanning, settling, paused detection, or `No changes since HH:mm` (with a date for older observations), plus save warnings; click for full details |
| **Upload** | Shows pending/active work, retries, or rejection independently of detection; `Retrying saves` distinguishes repeat attempts from `Uploading saves`, while `Retry at …` shows backoff; click for details |
| **Last success** | Shows confirmed delivery as `Today, HH:mm` or a date/time, without seconds; click for full last-upload details and any history-save error |
| **Automatic uploads** | Enables/disables detection of new changes; accepted uploads and retries continue |
| **Upload now** | Requests a snapshot even when automatic detection is off; still respects settling and retry delays |
| **Authenticate… / Connected as `<BattleTag>`** | One account entry: click to connect when authentication is needed. Once connected, the same entry shows your BattleTag and is no longer clickable |
| **Disconnect** | Removes the credential from this device and blocks future uploads until you reconnect. It does not revoke a copied credential on the server |
| **Settings → Choose save folder...** | Changes the watched folder; already accepted work from the previous folder can still finish |
| **Settings → Launch at Windows sign-in** | Changes the current-user Windows startup registration; no administrator rights needed |
| **Open leaderboard** | Opens the public leaderboard |
| **Settings → Open logs / config.json** | Opens diagnostics or settings; restart after manual config edits |
| **Quit** | Cancels active network work and stops retries |

Successful uploads stay quiet. Check the status and logs for errors. Only one instance can run per Windows session. After moving a portable executable, enable sign-in startup from its new location if needed.

### Which saves are uploaded?

The app reads regular `.pld` files directly inside the selected folder, case-insensitively. Account-verification files and unknown categories are included. It does not scan subfolders or select between accounts; the backend validates the submitted accounts.

It polls every five seconds and requires at least five seconds of observed stability. Hero, account, entitlements, settings, and legacy files are essential: every such file present needs a settled version, with at least one hero and one non-verification account required. While a replacement settles, the app can keep using its previous settled contents. Supplementary files do not block the essential set.

Safety limits are 8 MiB per file version, 32 MiB per snapshot, 1,024 selected files, and 4,096 directory entries. Save warnings explain omissions or fallback to older settled data. See [DESIGN.md](DESIGN.md#snapshot-rules) for the full selection rules.

Transient failures retry automatically. Rejected submissions wait for changed saves or **Upload now**.

## Account connection

Uploads require a connected Battle.net account. When the tray starts without a usable saved credential, including at Windows sign-in, it shows the authentication link directly. It does not open the browser for you unless you choose **Open in default browser**. Cancelling or failing to complete startup authentication exits the tray app. The console shows how to use `--authenticate`; dry runs do not ask. If a saved credential is rejected during an upload, uploads stop without falling back to anonymous. If automatic uploads are enabled, the tray shows a red × and a tooltip explaining why. Reconnect via **Authenticate…** or `--authenticate`. Console authentication prints only the browser approval link and can be cancelled with Ctrl+C; it does not need a save folder or upload any files.

## Configuration and logs

Windows data lives in `%LOCALAPPDATA%\CWGRPGUploader\`, even for portable copies. Linux settings use `${XDG_CONFIG_HOME:-$HOME/.config}/cwg-rpg-uploader/`; history and logs use `${XDG_STATE_HOME:-$HOME/.local/state}/cwg-rpg-uploader/`.

- `config.json`: settings. Restart after manual edits; tray changes apply immediately.
- `uploader-token.json`: private upload credential stored alongside `config.json`. **Do not share, copy, or include it in diagnostics.** Anyone who obtains it can upload as that account. To disconnect in console mode, stop the app and delete this file.
- `state.json`: successful-upload history. **Deleting it can cause existing saves to be submitted again.**
- `uploader.log`: rotating diagnostic logs. Server error responses can contain sensitive data; review logs before sharing.

The [configuration reference](CONFIGURATION.md) covers settings, disabling automatic uploads before first launch, proxies, and startup errors. Invalid configuration/history stops startup rather than silently resetting it.

## Console and dry run

Both console builds print the full configuration and log-file paths on startup, including when settings cannot be loaded. They do not prompt or discover a save folder: supply `--save-folder PATH` if `save_folder` is not already configured. The folder is remembered for future runs.

To inspect a settled snapshot without uploading on Windows:

```powershell
.\cwg-rpg-uploader-console.exe --dry-run --save-folder "C:\path\to\CWG2"
```

With an existing configured folder, `--dry-run` alone is enough.

Dry run prints the eligible file count and exits once a valid settled snapshot is available. If saves are missing, it keeps waiting; press Ctrl+C to stop. It can create default configuration and logs, but does not upload, record successful-upload history, or acknowledge the normal-run upload notice.

**Without `--dry-run`, the console app performs real uploads according to configuration.** Quit the tray app before starting it. The tray executable also accepts `--dry-run`, but has no guaranteed terminal output.

## Uninstall

For an installed copy, quit the app and uninstall through Windows Installed apps. For a portable copy, disable **Launch at Windows sign-in**, quit, and delete the executable.

On Linux, stop the app and delete the executable. Configuration/history are retained on both platforms. Optionally remove the application-data directories above to remove local data. This does not remove submissions already held by the leaderboard.

## Build and contribute

Requires Go 1.26 or newer; prefer a supported toolchain. Builds need no C compiler. Windows uses Windows PowerShell for the folder picker. The build/check scripts use PowerShell (also available as `pwsh` on Linux).

```powershell
.\scripts\check.ps1 # Lint, uncached tests, and vet; each package must finish tests in under 10s
.\build.ps1         # Run checks and rebuild dist (deletes its previous contents)
```

`build-linux.ps1` builds `dist-linux/` on Windows or Linux; cross-compilation does not run Linux tests. Native Linux CI runs those tests separately. To build only the console executable with Go, use `CGO_ENABLED=0 go build -o cwg-rpg-uploader-console ./cmd/uploader` on Linux.

Both build scripts default to version `dev`. Use `-Version vX.Y.Z` to embed a release version; the release workflow supplies its tag automatically.

- [Development guide](DOCS.md): builds, tests, release policy, and code organization.
- [Design](DESIGN.md): synchronization, file selection, and safety contracts.
- [Profiling](cmd/profile/readme.md): upload-disabled memory diagnostics.
- [Website](site/README.md): landing-page preview and GitHub Pages deployment.

## License

[MIT](LICENSE). Keep the [third-party notices](THIRD_PARTY_NOTICES.md) with distributions.

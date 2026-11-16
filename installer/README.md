# Windows installer

Inno Setup packages the Windows tray executable into a per-user installer.
Console mode is available separately as a portable ZIP, not in the installer.
Portable tray ZIPs also remain available. This is packaging support,
not an in-app downloader or self-updater.

## Install and upgrade

Download `cwg-rpg-uploader-vX.Y.Z-windows-amd64-setup.exe` from a release.

- During an upgrade, Windows Restart Manager detects the installed tray app
  using files that need replacement and offers to close it gracefully. Setup
  does not request forced termination. Close portable/console copies yourself;
  they may not use files in the installation directory. Quit all uploader
  instances before uninstalling; uninstall keeps a quit-first mutex check.
- Setup installs the tray executable and documentation under
  `%LOCALAPPDATA%\Programs\CWGRPGUploader\`, without administrator rights.
- Choose a Start-menu folder, or select **Don't create a Start menu folder**.
  A desktop shortcut is optional and unchecked on first installation.
- **Start CWG RPG Uploader when I sign in** is optional and unchecked on first
  installation. On upgrades it reflects the live registration for this
  installation, including changes made through the tray.
- **Launch CWG RPG Uploader** on the finish page is checked by default. Uncheck
  it to leave the app stopped. Starting it enables normal automatic uploads
  according to configuration; the option explicitly mentions automatic uploads.
  Silent installation never launches/restarts the app.
- Upgrades use the same application identity and installation directory.
  Re-run a newer installer to replace the installed program files.
- Configuration, upload history, and logs stay in `%LOCALAPPDATA%\CWGRPGUploader\`.
  Setup never installs or overwrites these files, and uninstall retains them.
  Game saves are never installed, removed, or modified.
- Setup and the app's tray use the same current-user startup registry entry,
  not separate configuration flags. Unchecking startup removes this
  installation's entry, but leaves an entry for another portable copy alone.
  Checking startup registers the installed tray app. Windows startup policy may
  still disable it independently.

An interactive upgrade relaunches through the finish-page option, not through
Restart Manager, so an unchecked launch option is respected. There is no extra
updater/replacement helper.

For silent deployments, Inno Setup's `/TASKS` and `/MERGETASKS` options explicitly
control `desktopicon` and `startup`; an explicit task list takes precedence over
the startup checkbox's registry-derived default. `/NOICONS` suppresses Start-menu
shortcuts. Without an explicit task selection, startup reflects its live value.

Use Windows Installed apps or the installed uninstaller to remove the program.
The uninstaller removes its sign-in startup entry only if it still points to
this installation; it leaves a registration for another portable copy alone.
You may separately delete `%LOCALAPPDATA%\CWGRPGUploader\` to remove local data.
This does not remove uploads already held by the leaderboard.

Executables and setup are currently unsigned. GitHub attestations authenticate
release artifacts but do not remove SmartScreen warnings. See
[VERIFYING.md](../VERIFYING.md) for download verification.

## Build

Run from the repository root. Build the application first:

```powershell
.\build.ps1 -Version v1.2.3
```

Use an existing Inno Setup compiler through `-ISCCPath`, `ISCC_PATH`, or PATH.
The release workflow uses **Inno Setup 7.1.1**. To obtain that same compiler in
portable mode, without system-wide registration, shortcuts, or file associations:

```powershell
$compiler = .\scripts\install-inno.ps1 -Destination "$env:TEMP\cwg-inno-7.1.1"
.\scripts\build-installer.ps1 -Version v1.2.3 -ISCCPath $compiler
```

The bootstrap downloads the official x64 compiler installer, checks its pinned
SHA-256, and extracts it using Inno Setup's supported portable/silent mode. It
neither launches our uploader nor runs our generated installer.

`build-installer.ps1` consumes the completed build and writes
`dist\cwg-rpg-uploader-v1.2.3-windows-amd64-setup.exe`. It does not rebuild the app,
clear `dist`, or run setup. The version must follow the repository's `vX.Y.Z`
policy. Use the same version for the application build and installer so the
upload User-Agent and setup version agree. Relative directory arguments are
resolved from the repository root:

```powershell
.\scripts\build-installer.ps1 -Version v1.2.3 -ISCCPath $compiler `
    -BuildDirectory dist -OutputDirectory "$env:TEMP\cwg-installers"
```

All five payload files are listed explicitly in `cwg-rpg-uploader.iss`; console and
profiling binaries, configuration/history, and unrelated build output are not included.
A failed compilation removes the target setup executable rather than leaving a
stale installer at that path. Normal Go-only builds do not require Inno Setup.

## Release and verification

The tag workflow builds the installer alongside the two portable packages,
records the compiler version/hash and executable hashes in `build-info.json`,
and attests/publishes the installer with the other draft-release assets.
Pre-release-by-default policy is unchanged. Do not expect independently rebuilt
installer containers to be byte-identical; compare Go payload executables using
`VERIFYING.md` and verify the setup artifact's attestation separately.

Before first shipping, exercise these observable behaviors in a disposable
Windows account/VM, not against a real uploader profile:

1. Install as a standard user and exercise Start-menu/desktop choices. Confirm
   the selected shortcuts and uninstall entry exist and console mode is absent.
2. Exercise the finish-page launch option with a harmless fixture executable:
   checked launches once; unchecked and silent installations never launch it.
3. Upgrade while the installed fixture is running. Confirm Restart Manager
   requests graceful exit before replacing the executable, then relaunches the
   new version only when selected. Exercise refusal/failure without force-killing.
4. Change startup through the fixture's registry entry between installer runs.
   Confirm upgrades reflect it, Back/Next preserves checkbox edits, explicit
   `/TASKS` overrides work, and enabling/disabling writes/removes the expected
   quoted executable path without deleting another portable copy's entry.
5. Confirm upgrades preserve config/history/log contents byte-for-byte.
6. Keep a fixture holding `Local\CWGRPGUploader`; confirm uninstall refuses to
   proceed until it exits. Afterwards confirm program files/shortcuts disappear
   but user data remains, with startup cleanup limited to this installation.

Do not launch the real uploader during installer tests without explicit upload
approval. Use harmless fixture executables for installation/process tests.

# Windows installer

Inno Setup packages the Windows tray executable into a per-user installer.
Console mode is available separately as a portable ZIP, not in the installer.
Portable tray ZIPs also remain available. This is packaging support,
not an in-app downloader or self-updater.

## Install and upgrade

Download `cwg-rpg-uploader-vX.Y.Z-windows-amd64-setup.exe` from a release.

- Quit every uploader instance in your Windows session before installing,
  upgrading, or uninstalling. Setup checks the app's existing instance mutex;
  it does not force-kill uploads or automatically restart the app.
- Setup installs the tray executable and documentation under
  `%LOCALAPPDATA%\Programs\CWGRPGUploader\`, without administrator rights.
- The Start-menu app shortcut launches tray mode. **Setup itself never
  launches the uploader.** Starting the app enables normal automatic
  uploads according to configuration; read the upload notice/README first.
- Upgrades use the same application identity and installation directory.
  Re-run a newer installer to replace the installed program files.
- Configuration, upload history, and logs stay in `%LOCALAPPDATA%\CWGRPGUploader\`.
  Setup never installs or overwrites these files, and uninstall retains them.
  Game saves are never installed, removed, or modified.
- Sign-in startup remains controlled by the app's tray, not by setup. If moving
  from a portable copy, enable startup from the installed copy if desired.

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
.\build.ps1
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
policy. Relative directory arguments are resolved from the repository root:

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

1. Install as a standard user; confirm the tray executable, documentation,
   shortcuts, and uninstall entry exist, console mode is absent, and no uploader
   process was started.
2. Install another version; confirm program files change while existing
   config/history/log contents remain byte-for-byte unchanged.
3. Keep a fixture process holding `Local\CWGRPGUploader`; confirm setup and
   uninstall refuse to proceed until it exits, without killing the process.
4. Uninstall; confirm program files/shortcuts disappear but user data remains.
5. Check startup cleanup with an entry pointing to this install, and separately
   with one pointing to another portable copy.

Do not launch the real uploader during installer tests without explicit upload
approval. Use harmless fixture executables for installation/process tests.

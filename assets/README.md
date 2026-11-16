# Application icon

`icon.svg` is the editable source: a charcoal shield, muted-crimson outline,
and a blue upload arrow inspired by the sword core in the CWG Discord icon.
The artwork is original; the reference map artwork is not bundled.

The Windows assets are generated and committed:

- `../internal/windows/icon.ico`: transparent PNG-compressed images at 16, 20,
  24, 32, 40, 48, 64, 128, and 256 pixels. Embedded for the tray and used by
  Inno Setup for its setup executable.
- `../internal/windows/icon_windows_amd64.syso`: Windows icon resources linked
  through the Windows UI package into tray, console, and profiling executables.
  The installer's shortcuts/uninstall entry use the tray executable's icon.

After editing the SVG, regenerate from the repository root:

```powershell
.\scripts\build-icon.ps1
```

This requires Windows amd64 and Go. It downloads resvg **0.50.0**, verifies the
pinned SHA-256, renders the ICO sizes, and runs `github.com/akavel/rsrc@v0.10.2`
to generate the amd64 resource object. Temporary tools/images are removed.
Neither tool becomes an application dependency or modifies `go.mod`.

Normal application/installer builds use the committed assets: no rendering tool,
resource compiler, C compiler, or runtime PNG encoding is needed. Commit the SVG,
ICO, and resource object together when changing the design. Independent release
rebuilds should use the assets at the release tag, not regenerate them with
other renderer versions. See [VERIFYING.md](../VERIFYING.md).

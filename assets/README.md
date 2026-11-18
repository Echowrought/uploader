# Application icon

`icon.png` is the application icon source: a gold armillary star on a violet
starfield, cut to a transparent circle (512 x 512 RGBA). It is used as the
website logo/favicon and rendered into the Windows icon.

The Windows assets are generated and committed:

- `../internal/windows/icon.ico`: transparent PNG-compressed images at 16, 20,
  24, 32, 40, 48, 64, 128, and 256 pixels. Embedded for the tray and used by
  Inno Setup for its setup executable.
- `../internal/windows/icon_windows_amd64.syso`: Windows icon resources linked
  through the Windows UI package into tray, console, and profiling executables.
  The installer's shortcuts/uninstall entry use the tray executable's icon.

After replacing `icon.png`, regenerate from the repository root:

```powershell
.\scripts\build-icon.ps1
```

This requires Windows amd64 and Go. It resizes the icon with System.Drawing,
writes the ICO, and runs `github.com/akavel/rsrc@v0.10.2` to generate the amd64
resource object. Temporary files are removed. The tool does not become an
application dependency or modify `go.mod`.

Normal application/installer builds use the committed assets: no image tool,
resource compiler, C compiler, or runtime PNG encoding is needed. Commit
`icon.png`, the ICO, and the resource object together when changing the design.
See [VERIFYING.md](../VERIFYING.md).

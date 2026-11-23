## What's changed

{{CHANGES}}

## Download

- **[Windows installer — recommended]({{DOWNLOAD}}/cwg-rpg-uploader-{{TAG}}-windows-amd64-setup.exe)**
- [Portable Windows tray app]({{DOWNLOAD}}/cwg-rpg-uploader-{{TAG}}-windows-amd64.zip)
- [Portable Windows console app]({{DOWNLOAD}}/cwg-rpg-uploader-console-{{TAG}}-windows-amd64.zip)
- [Linux console app (x86-64)]({{DOWNLOAD}}/cwg-rpg-uploader-console-{{TAG}}-linux-amd64.tar.gz)

## Before you run

**Uploads require a Battle.net account connected through the [CWG Leaderboard](https://cwg.echowrought.com/leaderboard/).** On first launch, sign in and approve upload access in your browser. Once connected, automatic uploads are on by default and can include existing saves. Submissions may appear publicly. The uploader sends eligible `.pld` files directly inside the selected folder; it never scans subfolders or modifies your saves.

**Windows:** The installer works without administrator rights and keeps your settings and upload history on upgrade or uninstall. It offers shortcut and sign-in startup options. Uncheck **Launch CWG RPG Uploader** at the end if you don't want it to start yet. For portable use, extract the ZIP and run the executable. Windows builds are unsigned, so SmartScreen may warn.

**Linux:** Extract the archive and run the console app. To inspect saves without signing in or uploading, use your absolute Wine or Proton save-folder path:

```sh
./cwg-rpg-uploader-console --dry-run --save-folder "/absolute/path/to/CWG2"
```

[Verify your download]({{REPOSITORY}}/blob/{{TAG}}/VERIFYING.md) · [Full changelog]({{CHANGELOG}})

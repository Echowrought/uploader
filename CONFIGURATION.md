# Configuration and troubleshooting

For installation and everyday use, see [README.md](README.md).

## Settings

Windows installed and portable copies use `%LOCALAPPDATA%\CWGRPGUploader\config.json`.
Linux settings use `${XDG_CONFIG_HOME:-$HOME/.config}/cwg-rpg-uploader/config.json`;
history/logs use `${XDG_STATE_HOME:-$HOME/.local/state}/cwg-rpg-uploader/`.
XDG directory overrides must be absolute. Linux does not depend on systemd or a desktop.
The app creates defaults when configuration is absent and a save path is available. Restart after manual edits;
tray changes are validated and saved before being applied.

```json
{
  "save_folder": "C:\\path\\to\\CWG2",
  "poll_interval_seconds": 5,
  "upload_automatically": true,
  "proxy_url": ""
}
```

| Setting | Meaning |
| --- | --- |
| `save_folder` | Literal absolute save-folder path; no environment-variable or `~` expansion |
| `poll_interval_seconds` | Poll frequency, from 1 to 3600 seconds; default 5 |
| `upload_automatically` | Detect changes automatically; default `true`. Turning it off does not cancel accepted work or retries |
| `proxy_url` | Optional proxy fallback; see below |

The example save path is a placeholder; replace it with your actual absolute
CWG save-folder path. Only
the Windows tray build discovers the Documents known folder, honoring redirection
including OneDrive, when configuration is absent or `save_folder` is omitted.
An explicitly empty or null folder is invalid, not a request to rediscover it.

Both console builds print the full `config.json` and `uploader.log` paths before
loading settings, so those locations remain visible after a configuration error.
Neither console prompts or discovers a save folder. Supply the absolute game
save folder with `--save-folder PATH` when no folder is configured, or create
configuration with `save_folder` set. Linux never guesses Wine/Proton prefixes. The flag works on both platforms and remembers the path in
configuration; invalid existing configuration still needs manual correction.
Paths are used literally: `%VARIABLE%`, `$VARIABLE`, `${VARIABLE}`, and `~` are
not expanded by the app. Your shell can expand arguments before passing them to
`--save-folder`; use a full absolute path in JSON.
Linux folder identity is case-sensitive: changing `Saves` to `saves` can select a
different folder and cause its snapshot to be uploaded separately.

**To prevent automatic uploads before the first normal launch:** create the
application-data folder and `config.json` yourself, use the example with your
actual save path, and set `upload_automatically` to `false`. Do not launch the app
first to obtain the example: default automatic uploads include existing saves once a Battle.net account is connected.
**Upload now** can still request an upload with automatic detection disabled.

In either console, begin with `--dry-run --save-folder PATH` if you want the app to create
configuration without uploading, then set `upload_automatically` to `false`
before a normal launch. This is also safe when existing saves are present.

Windows sign-in startup is not a JSON setting. Setup and the tray use the current-user
Windows startup registration. Launching the app does not recreate it, and
Windows startup policy can independently suppress it. The legacy
`launch_at_login` field is ignored.

### Managed settings and upload history

The app manages `upload_notice_shown`, recording it only after presenting the
first normal-run upload notice and saving the acknowledgement. Missing or false
means the notice is pending. Dry runs leave it pending. Normally, leave this
field under the app's control.

`state.json` stores the last successful snapshot's folder, destination,
fingerprint, upload time, player, and hero count (which can be zero if the server kept all newer saves). It is not a queue of
save contents. **Deleting it may cause existing saves to be uploaded again.**

Loading and saving both validate settings/history. Configuration must be a JSON
object; known settings cannot be null. Omitted settings retain their defaults.
A missing history file is allowed, but an invalid existing config/history file
stops startup without silently resetting it. Correct the reported problem rather
than deleting history as a general troubleshooting step. Failed tray changes
leave the previous live configuration intact.

## Proxies

Set `proxy_url` and restart:

| Value | Behavior |
| --- | --- |
| `""` | Environment proxies if present, otherwise direct |
| `"http://127.0.0.1:8080"` | HTTP proxy; HTTPS uploads use CONNECT |
| `"https://proxy.example:8443"` | TLS connection to the proxy |
| `"socks5://127.0.0.1:1080"` or `"socks5h://127.0.0.1:1080"` | SOCKS5 proxy with proxy-side destination resolution |
| `"direct"` | Disable the configured fallback, **not** environment proxies |

Selection is per request scheme: `HTTPS_PROXY` or `HTTP_PROXY`, then configured
`proxy_url`, then direct. Lowercase environment names are supported. Uploads use
HTTPS, so `HTTPS_PROXY` is the relevant environment setting.

`NO_PROXY` bypasses both environment and configured proxies. Localhost and
loopback addresses also bypass proxying, following Go's matching rules.
Settings are captured when the HTTP client is created; restart after changing
configuration or environment variables.

Credentials can use `scheme://username:password@host:port`; percent-encode special
characters. **Proxy credentials are plaintext in `config.json`; do not share
that file without reviewing it.** Invalid configured proxy URLs stop startup
instead of silently connecting directly. TLS certificate verification remains
enabled.

Windows system proxy settings and PAC scripts are not automatically imported.
An app launched at sign-in does not inherit a terminal's proxy environment just
because the startup checkbox was enabled from that terminal.

## Logs and diagnostics

Windows logs live at `%LOCALAPPDATA%\CWGRPGUploader\uploader.log`; use **Open logs**
from the tray. Linux logs live in the XDG state directory described above and
are also written to the terminal. Stop Linux with Ctrl+C or SIGTERM, and never
remove its `instance.lock` file while an instance may be running. The log rotates at 2 MiB, keeping three previous files, with no external
rotation tool required.

- `[auth]`, `[poller]`, `[uploader]`, and `[app]` identify the component.
- Authentication failures identify the stage and, for response validation errors,
  the HTTP status and reason. Authentication response bodies, links, and tokens
  are not logged.
- Save warnings name omitted supplementary files or files using older settled
  contents while a replacement cannot be used.
- Individual disk entries are capped at 256 KiB, including the truncation marker,
  without splitting valid UTF-8. Console output is not truncated.
- Outgoing save contents are not logged. Upload error messages may contain a quoted preview
  of up to 512 response-body bytes. Full HTTP error bodies are additionally
  logged only when smaller than 4 KiB. Controls/newlines are escaped and valid
  UTF-8 characters are not split.

A server response could echo sensitive data. Review logs before sharing them.
The tray status is the primary place to check current activity and errors;
successful uploads do not produce popup notifications.

For a non-uploading save check, use the console executable with `--dry-run`, as
shown in [README.md](README.md#console-and-dry-run). For memory diagnostics, see
the separate [profiling guide](cmd/profile/readme.md).

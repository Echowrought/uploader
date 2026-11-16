# Configuration and troubleshooting

For installation and everyday use, see [README.md](README.md).

## Settings

Both installed and portable copies use `%LOCALAPPDATA%\CWGRPGUploader\config.json`.
The app creates defaults when this file is absent. Restart after manual edits;
tray changes are validated and saved before being applied.

```json
{
  "save_folder": "%USERPROFILE%\\Documents\\Warcraft III\\CustomMapData\\CWG2",
  "poll_interval_seconds": 5,
  "upload_automatically": true,
  "proxy_url": ""
}
```

| Setting | Meaning |
| --- | --- |
| `save_folder` | Absolute save-folder path after expanding `%VARIABLE%` references |
| `poll_interval_seconds` | Poll frequency, from 1 to 3600 seconds; default 5 |
| `upload_automatically` | Detect changes automatically; default `true`. Turning it off does not cancel accepted work or retries |
| `proxy_url` | Optional proxy fallback; see below |

The example save path is the usual location, not a requirement. Initial discovery
uses the Windows Documents known folder and honors redirection, including
OneDrive. Discovery is needed only when configuration is absent or `save_folder`
is omitted. An explicitly empty or null folder is invalid, not a request to
rediscover it.

**To prevent automatic uploads before the first normal launch:** create the
application-data folder and `config.json` yourself, use the example with your
actual save path, and set `upload_automatically` to `false`. Do not launch the app
first to obtain the example: default automatic uploads include existing saves.
**Upload now** can still request an upload with automatic detection disabled.

Sign-in startup is not a JSON setting. Setup and the tray use the current-user
Windows startup registration. Launching the app does not recreate it, and
Windows startup policy can independently suppress it. The legacy
`launch_at_login` field is ignored.

### Managed settings and upload history

The app manages `upload_notice_shown`, recording it only after presenting the
first normal-run upload notice and saving the acknowledgement. Missing or false
means the notice is pending. Dry runs leave it pending. Normally, leave this
field under the app's control.

`state.json` stores the last successful snapshot's folder, destination,
fingerprint, upload time, player, and positive hero count. It is not a queue of
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

Logs live at `%LOCALAPPDATA%\CWGRPGUploader\uploader.log`; use **Open logs** from
the tray. The log rotates at 2 MiB, keeping three previous files, with no external
rotation tool required.

- `[poller]`, `[uploader]`, and `[app]` identify the component.
- Save warnings name omitted supplementary files or files using older settled
  contents while a replacement cannot be used.
- Individual disk entries are capped at 256 KiB, including the truncation marker,
  without splitting valid UTF-8. Console output is not truncated.
- Outgoing save contents are not logged. Error messages contain a quoted preview
  of up to 512 response-body bytes. Full HTTP error bodies are additionally
  logged only when smaller than 4 KiB. Controls/newlines are escaped and valid
  UTF-8 characters are not split.

A server response could echo sensitive data. Review logs before sharing them.
The tray status is the primary place to check current activity and errors;
successful uploads do not produce popup notifications.

For a non-uploading save check, use the console executable with `--dry-run`, as
shown in [README.md](README.md#console-and-dry-run). For memory diagnostics, see
the separate [profiling guide](cmd/profile/readme.md).

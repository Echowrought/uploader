# Windows and Linux Support

Status: proposed task; not implemented.

## Goal

Support Windows and Linux while sharing the existing save scanner, poller, uploader, coordinator, and status renderer. Keep console mode available on both platforms as a fallback when tray integration is unavailable.

## Proposed Organization

```text
main.go                       # Shared startup
internal/platform/
  folders_windows.go          # Windows known folders
  folders_linux.go            # XDG directories and configured save path
  startup_windows.go          # Current-user registry startup entry
  instance_windows.go         # Named mutex
  instance_linux.go           # File lock
  dialogs_windows.go
  dialogs_linux.go
internal/ui/
  tray.go                     # Shared menu and status presentation
```

Go selects `_windows.go` and `_linux.go` files automatically. Use build constraints where filename selection alone is insufficient. This is a proposed layout, not a requirement to introduce a large platform abstraction or interfaces for every function.

## Platform Differences

| Feature | Windows | Linux |
| --- | --- | --- |
| Configuration and state | Existing `%LOCALAPPDATA%\CWG2Uploader\` layout | XDG configuration and state directories |
| Save discovery | Documents known folder | Usually a user-configured Wine/Proton save path |
| Background startup | Tray-managed current-user Run registration; no config preference | Optional systemd user service running console mode; no startup checkbox |
| Single instance | Named mutex | Per-user file lock |
| Folder picker, messages, opening URLs/files | Windows APIs and existing integration | Linux desktop integration with explicit fallbacks |

### Linux Paths

- Configuration: `$XDG_CONFIG_HOME`, defaulting to `$HOME/.config`.
- State and logs: `$XDG_STATE_HOME`, defaulting to `$HOME/.local/state`.
- Choose and document an application subdirectory for each.
- Do not assume Warcraft III saves are beneath the native Linux Documents directory. Wine prefixes, Proton installations, and redirected game folders vary.
- Preserve a configurable save-folder override; investigate discovery only as a convenience.

## Work Items

- [ ] Remove the Windows-only restriction from the shared entry point and isolate platform-specific startup calls.
- [ ] Separate the shared tray menu from Windows-specific OS integration.
- [ ] Implement Linux directory discovery, desktop actions, folder selection, and user-visible error reporting.
- [ ] Implement Linux single-instance locking.
- [ ] Provide an optional systemd user `.service` file for console mode, managed with `systemctl --user`, without root privileges. Document that this targets systemd-based distributions; do not automatically enable lingering. Starting before user login requires separately configured lingering.
- [ ] Audit path assumptions, including separator handling and case-insensitive folder comparisons. Linux save-folder identity must not blindly use Windows case-folding rules.
- [ ] Support `$VARIABLE` expansion alongside existing Windows-style `%VARIABLE%` expansion, with documented validation behavior.
- [ ] Update Windows-specific tests and add platform-specific tests without duplicating core synchronization logic.
- [ ] Provide console mode on both platforms, including a documented strategy for builds without a graphical desktop or tray dependencies.
- [ ] Add native Windows and Linux CI jobs for formatting checks, tests, vet, and builds.
- [ ] Add Linux build/package instructions and include the MIT license and generated third-party notices in releases.

## Linux Tray Build Constraints

The current tray library supports Linux, but its Linux implementation requires CGO and GTK/AppIndicator dependencies. Confirm the exact development packages and build tags required by the pinned library version before implementation.

Prefer native Linux CI builds over cross-compiling the graphical Linux executable from Windows. A target-platform build environment is simpler for the required C libraries.

Tray behavior varies between desktop environments, especially where an indicator extension is required. Do not assume a tray icon is universally available. Define the behavior when desktop integration is missing, and keep console operation usable. Merely adding a runtime console flag may not remove compile-time dependencies on the tray library; assess whether a headless build option is needed.

## Suggested Sequence

1. Isolate Windows integration while keeping existing Windows behavior unchanged.
2. Make shared startup and console execution work on Linux.
3. Add Linux desktop/tray integration and an optional systemd user service; keep the startup checkbox Windows-only.
4. Validate supported desktop environments and package native releases.

## Acceptance Checks

- [ ] Shared core tests pass on both platforms.
- [ ] Existing Windows save discovery, configuration, tray actions, and sign-in startup remain functional.
- [ ] Linux accepts an explicitly selected Wine/Proton save directory.
- [ ] Configuration, state, logs, and instance locks use appropriate per-user locations.
- [ ] Upload now, Automatic uploads, retries, and clean shutdown behave consistently across platforms.
- [ ] Linux operation without a working tray has a documented console fallback and optional systemd user-service instructions.
- [ ] Automated tests use fixtures and mock servers; any live upload requires explicit approval.

# Discuss Optional Uploader Authentication

Status: discussion in progress; not implemented.

## Goal

Authenticate save submissions using the site's existing Blizzard OAuth identity, without adding unnecessary login or credential-storage complexity to the uploader.

## Threat and Scope

The website already supports Battle.net authentication to identify users and restrict access to their backups. The association between that identity and save ownership is resolved for this discussion; do not treat it as a new prerequisite.

A knowledgeable attacker could forge a file with a targeted user's BattleTag checksum and submit a spoiled revision. A checksum derived from public identity is not proof that the submitter owns that identity. Restricting backup reads alone does not prevent unauthorized writes.

Authentication establishes who submitted a file, not whether its gameplay contents are genuine. With proper ownership enforcement, an authenticated malicious user should be able to poison only their own saves. A stolen token grants its holder the token's upload permissions until revoked.

## Agreed Direction

- Reuse the website's Blizzard OAuth login. Do not introduce another password system or require the uploader to handle Battle.net credentials directly.
- After website authentication, issue an opaque, cryptographically random uploader token bound to the authenticated account. Do not derive the token predictably from public identity.
- The uploader stores the token locally and presents it with uploads over HTTPS.
- The server derives the destination account from the token, never from an uploaded file's claimed identity. Reject incompatible ownership claims.
- Scope the token to uploads; do not grant backup-reading or destructive permissions unless separately justified.
- Make tokens revocable and store token hashes server-side.
- Optional authentication must not become an impersonation bypass: anonymous uploads must not modify authenticated users' protected backup history. Define anonymous handling separately, such as isolated/unverified submissions.

## Simplest Authentication Experience

Manual token copying from the authenticated website into the app's local configuration is a reasonable initial implementation. It requires no local callback listener, separate OAuth implementation in the app, or pairing service.

Browser pairing could be added later without changing the underlying token/ownership model: the app opens the existing website login/approval flow and receives a credential through a short-lived authorization request. If implemented, prefer an established device authorization pattern with explicit approval, single-use expiring codes, and bounded polling rather than inventing a protocol.

No final pairing implementation has been selected; prioritize the simple token flow first.

### Unattended startup requirement

Normal startup and automatic uploads must not trigger credential-unlock prompts or login dialogs. Initial setup and reconnection may require interaction, but must be explicitly initiated by the user.

Keyrings commonly unlock automatically, but this is not guaranteed across Linux sessions. Keep the local token-file approach as the initial predictable, prompt-free choice. Any future keyring integration must preserve noninteractive startup rather than assume automatic unlocking.

If a token is missing or revoked, report authentication required and let the user explicitly reconnect. Do not silently downgrade protected uploads to anonymous submissions.

## Credential Storage Discussion

### OS-protected options

- Windows Credential Manager stores credentials separately from ordinary config. DPAPI can alternatively protect a locally stored encrypted blob using Windows-managed user protection.
- Linux desktop keyrings commonly expose Secret Service, for example through GNOME Keyring or compatible KDE Wallet setups. Availability and unlocking vary, especially for headless sessions.
- A cross-platform library such as `github.com/zalando/go-keyring` can access platform credential services. It does not automatically provide an encrypted-file fallback when those services are unavailable.
- Keyrings keep user-specific protection outside the application binary. They improve protection at rest and against casual copying, but do not reliably defend against malware controlling the logged-in user session.

### Why not a custom encrypted-file fallback?

An embedded/shared encryption key can be extracted from the binary; obtaining the binary and encrypted token file defeats that protection. A key beside the token file provides little meaningful additional security. A user-entered passphrase offers stronger protection but complicates unattended startup. Another OS-protected key store reintroduces the original availability dependency.

Do not build home-grown encrypted storage that appears stronger than its key management actually is.

### Proportionate initial choice

The user questioned whether mandatory keyring integration is excessive for a nonfinancial, upload-only credential. The resulting recommendation is to prioritize server-side safeguards and accept an honestly documented local token file initially:

- Owner-only file permissions (`0600`) on Linux.
- A private per-user app directory with appropriate Windows access permissions. Do not assume a directory's name alone makes it private.
- No tokens in logs, status messages, example configs, source control, or distributed diagnostic bundles.
- Explain that copying the token/config grants upload access until revocation.
- Preserve backup history so a spoiled revision does not destroy recovery; decide retention and abuse limits explicitly.
- Use a keyring later if integration is straightforward. Do not make it a prerequisite or add a custom encrypted fallback merely to claim encryption.

Browsers were discussed as a comparison: they commonly use OS-backed protection for passwords and sensitive cookies, with Linux behavior varying by browser/environment. They also cannot reliably protect secrets from a compromised logged-in session. Browser practice is context, not a requirement to reproduce browser credential infrastructure in this app.

## Interaction with Remote Upload Configuration

See [Remote Upload Configuration](remote-upload-policy.md).

Never automatically forward an existing credential to a newly configured upload origin. Destination approval and credential authorization are separate decisions; a remotely supplied URL is not authority to receive an account token. Redirect handling must preserve this boundary too.

## Open Decisions and Work Items

- [ ] Define token issuance, presentation, upload-only scope, revocation, and any expiration policy.
- [ ] Enforce token-derived ownership and test spoofed save identities and cross-account submissions.
- [ ] Specify anonymous-upload isolation so optional authentication cannot bypass protected-backup authorization.
- [ ] Choose the local token/config location and enforce appropriate access permissions on Windows and Linux.
- [ ] Define simple setup/disconnect instructions and credential replacement after revocation.
- [ ] Verify startup and automatic uploads never trigger credential/login prompts; missing or revoked credentials produce a status requiring explicit reconnection, not silent anonymous fallback.
- [ ] Ensure logging, configuration diagnostics, and support exports redact credentials.
- [ ] Define backup retention and abuse limits so upload-only access cannot indirectly erase useful history by flooding revisions.
- [ ] Ensure remote endpoint changes and redirects cannot leak uploader credentials.
- [ ] Add browser pairing or keyring integration only if their user benefit justifies the added mechanisms.

## Expected Outcome

An optional, low-friction authentication flow based on the site's existing identity, with server-enforced upload ownership, recoverable backup history, revocable credentials, and proportionate local storage protection. No implementation changes are authorized by this discussion task alone.

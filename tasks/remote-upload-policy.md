# Discuss Remote Upload Configuration

Status: proposed discussion; not implemented.

Tracking issue: [#3](https://github.com/cwg-rpg-community/uploader/issues/3).

## Goal

Allow the upload endpoint and essential-file rules to change without requiring users to install another binary. Explore a small manifest hosted in the GitHub repository, with bundled defaults as a fallback.

## Initial Assessment

Technically reasonable if the mechanism stays narrow, validated, and resilient to network failures. Ethically, changing scan policy and changing the recipient of uploaded saves are different actions: destination changes cross a trust boundary and require stronger controls and disclosure.

No final design or implementation decision has been made.

## Proposed Mechanism

- Bundle working defaults in the binary.
- Fetch a small, versioned JSON manifest over HTTPS from a fixed GitHub location.
- Use a short timeout, strict response-size limit, and explicit schema validation. Treat the manifest solely as data, never executable code.
- Validate the whole manifest before applying it; reject unsupported schemas and invalid values rather than partially applying changes.
- Cache the last valid manifest. On fetch/validation failure, fall back to the valid cache, then bundled defaults.
- Fetch infrequently and do not let GitHub outages prevent normal startup or use of fallback configuration.
- Apply policy changes at a defined coordinator boundary, not halfway through an upload. Define how pending work, retries, and persisted upload history retain their destination identity.

Illustrative fields, not a finalized schema:

```json
{
  "schema": 1,
  "upload_url": "https://example.org/api/upload",
  "essential_prefixes": ["HERO_", "ACCOUNT_"]
}
```

Essential-file policy currently includes more than a prefix list, such as required hero/account presence. Determine the necessary expressiveness before finalizing the schema; do not introduce a general-purpose rule engine.

## Trust and Consent

- Disclose that the app fetches remote operational configuration, including what may change and where it is hosted.
- Keep essential-file rules distinct from upload eligibility. Making a file essential should govern whether its absence/unsettled state blocks submission, not silently authorize uploading new categories or reading arbitrary paths.
- Consider allowing automatic endpoint changes within the existing trusted origin, while requiring explicit approval for a new origin. Show the old and proposed destinations before sending saves there.
- Require HTTPS and prevent HTTP redirects from bypassing destination restrictions or user approval.
- Keep fetching/caching a proposed destination separate from authorizing uploads to it. A cached manifest must not imply consent to a new recipient.
- Provide a way to disable remote configuration or pin local settings; define precedence clearly.
- GitHub HTTPS protects transport, but a compromised repository can still publish hostile configuration. Consider signed manifests if warranted; a signing key must be protected separately from repository access to add meaningful protection.
- A signature proves publisher authorization, not user consent to a new destination.

## Open Questions and Work Items

- [ ] Choose the GitHub repository/path, fetch cadence, timeout, and response-size limit.
- [ ] Define the smallest supported manifest schema, validation rules, and compatibility behavior.
- [ ] Define cache persistence, stale-policy behavior, and recovery when cached configuration points at a retired endpoint.
- [ ] Define atomic policy application and behavior for already accepted work, retries, folder scans, deduplication baselines, and successful-upload history when policy changes.
- [ ] Decide the exact destination trust policy, including origin comparison and redirect handling. Same-origin changes still need to remain within the disclosed service/purpose.
- [ ] Define new-origin approval in tray and console modes; unattended operation must not silently approve a new recipient.
- [ ] Decide whether signed manifests are justified and, if so, define key protection, rotation, and rollback protection.
- [ ] Define local overrides/opt-out and their precedence over remote policy.
- [ ] Review essential-file changes for both blocking behavior and data-access boundaries; do not allow remote policy to expand local data collection silently.
- [ ] Document remote configuration, destination consent, fallback behavior, and operational recovery.
- [ ] Test unavailable GitHub, timeout, oversized/malformed manifests, unsupported schemas, invalid cache, destination changes, redirects, and policy updates during active delivery.

## Expected Outcome

A small, reliable configuration mechanism that preserves working offline defaults and does not silently redirect user saves or expand data collection. Resolve the trust and consent policy before implementation.

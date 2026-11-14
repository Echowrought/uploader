# Discuss Per-File Upload API

Status: discussion in progress; no API or app changes agreed.

## Question

Would accepting save files one by one, instead of requiring a complete snapshot, simplify the app and API overall?

## Scope and User Requirements

- Typical supported play is multiclient with the **same player name across clients**, commonly playing different hero families.
- Different player names across clients are a corner case explicitly not handled at present. Behavior is undefined; do not use this case to justify the design.
- The user does not consider partial uploads or a complete save set inherently necessary. Investigate actual map dependencies rather than assume transactional requirements.
- The current API limits how the uploader can submit data. The uploader's batching is not evidence that batching is required by the map.

## Discussion Summary

The initial assessment was that per-file requests might simplify individual HTTP calls but add partial-success handling, per-file retries, and snapshot-completion bookkeeping. That assessment assumed whole-save consistency mattered without first establishing the requirement.

The user challenged this assumption and identified map behavior, especially same-name multiclient play, as the relevant evidence. Investigation then focused on filenames, independent hero validation, shared account metadata, and save ordering.

The uploader currently combines versions settled on different scans. This is **not independent evidence that mixed versions are safe**: the user deliberately designed it that way because they already assumed this was safe and sensible. Using that implementation to validate the assumption would be circular reasoning.

## Findings from the Map Script

Source inspected: extracted CWG 1.2.4 script at
`../wiki/private/output/cwg-1.2.4-common/common/war3map.j`.
These are static-code findings, not instrumented multiclient experiments. Line references describe the inspected script and may change with extraction/version updates.

### File identity

- `CWG2CharacterFileFor` (around line 17735) constructs `HERO_<family>_<player-name-hash>.pld`.
- `CWG2AccountFileFor` constructs one account filename per player identity, shared across that player's hero families.
- Settings are also scoped to player identity, not hero family.

### Hero validation and account metadata

- `CWG2ValidateHeaderPart` and `CWG2ValidateBodyPart` (around line 18731 onward) validate a hero file using its player identity, family, generation, checksum, and authentication values. These functions do not require a matching account-file generation.
- `CWG2AccountValueAt` (around line 18603) shows that account roster entries include presence, hero ID, tier, main stat, hero generation, and hero fingerprint. Therefore, hero and account files are not completely unrelated.
- Independent hero validation does not by itself establish that every consumer can meaningfully process a hero without account data. Broader load/account-verification requirements remain to be checked where relevant.

### Save ordering and multiclient implications

- `CWG2WriteCharacter` (around line 19398 onward) writes the hero file first, then conditionally saves the account file. `CWG2SaveCharacter` subsequently writes settings. This is not an atomic multi-file commit.
- `CWG2AccountEnsure` loads account data when it is not already loaded or the user changes; it does not reload disk account state before every save for the same user.
- Consequently, same-name clients playing different families write separate hero files but share the account file. A client can write a roster summary based on stale client-local account state, missing another client's latest changes.
- Same-name clients playing the same family target the same hero filename and can overwrite each other's saves.
- The stale-account and overwrite scenarios are inferred from the script; they were not experimentally reproduced during this discussion.

## Conclusions So Far

- A complete folder upload is not inherently a coherent gameplay snapshot. The map writes files separately, and same-name multiclient sessions can contribute independently aged data.
- Sending all files in one HTTP request does not repair a stale account roster or prevent map clients from overwriting the same file.
- Account roster generation/fingerprint references are a real cross-file relationship. Do not jump from non-atomic writes to the claim that every file is fully independent.
- The unresolved question is whether each hero save remains independently meaningful despite stale account roster metadata, and what account data a consumer actually needs.
- Separate three questions: what the map produces, what version combinations are semantically safe, and what the current API accepts. The existing uploader implementation cannot prove the second, and the current API constraint cannot establish a map-level consistency requirement.
- Per-file uploads deserve investigation; the earlier blanket assessment that they would probably make the app more complicated was too strong. No final migration decision has been made.

## Follow-Up Discussion and Investigation

- [ ] Trace how account roster generation/fingerprint references are consumed. Determine whether they are discovery/summary metadata or necessary consistency constraints for the intended use.
- [ ] Evaluate a concrete same-name, different-family sequence: both clients load account state, client A saves, then client B saves from its cached account state. Identify which files and roster entries remain valid, stale, or missing.
- [ ] Establish the minimum data needed to process a hero independently; do not equate standalone checksum/authentication validation with complete semantic independence.
- [ ] If useful, reproduce the inferred multiclient scenarios using isolated backups/fixtures without modifying live saves.
- [ ] Given the map findings, discuss an API that accepts independently settled files, including whether account/companion files need separate treatment.
- [ ] Define idempotency and retry semantics. Compare per-file success tracking with safely resending files rather than assuming a durable per-file queue is necessary.
- [ ] Decide whether deletions need any remote meaning, rather than assuming directory synchronization is required.
- [ ] Compare total client/server complexity, request overhead, and bandwidth. Require snapshot identifiers or a final commit step only if a concrete requirement justifies them.
- [ ] Record the decision and rationale before implementation; keep different-player-name support out of scope.

## Expected Outcome

A decision grounded in the map's actual save semantics and the intended consumer requirements, with API limitations clearly distinguished from necessary consistency guarantees. No code or API changes are authorized by this discussion task alone.

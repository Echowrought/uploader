# Review of deleted and replaced tests

## Conclusion

**The cleanup was too aggressive. Some changes were worthwhile, but deleting tests because their entry points were unexported was not a sound justification. Several replacements lost specific regression cases.**

Historical audit scope: baseline `d11f1cf` versus the working copy **before the follow-up fixes below**, including its new, untracked test files. I compared original bodies and assertions, not just test names or line coverage. No tests or production source were changed during the audit itself. The ledger and original mutation results describe that pre-fix tree; the follow-up section records the subsequent test changes.

There were **70 original test names absent from that tree**. This is not 70 lost requirements: many functions were renamed, split, or replaced. Reordered functions and tests restored under their original names are not counted as deletions.

| Verdict | Count | Meaning |
| --- | ---: | --- |
| **B — Better replacement** | 16 | Splitting, a corrected boundary case, or a demonstrably stronger regression test justified the change. |
| **E — Equivalent coverage** | 28 | The relevant behavior remains covered. Removing the old name is reasonable consolidation, not evidence that the original test was bad. |
| **P — Partial replacement / gap** | 22 | Some original behavior is missing or tested less precisely. Restore the missing scenario, without blindly restoring incidental assertions. |
| **N — Not worth deleting** | 4 | Useful focused tests were removed without a convincing benefit. Restore or retain focused coverage. |

**At audit time, 26 of the 70 removals/replacements were not fully justified: 22 partial replacements and four unnecessary removals. All 26 findings are now addressed; see the follow-up below.**

## Evidence beyond static review

I used temporary Go overlays to inject deliberately broken behavior. For each probe, I ran the **current entire affected package** and the corresponding **original focused test(s)** in a temporary baseline checkout. These are test-quality probes, not bugs asserted to exist in production today. No mutated production source was written into the repository.

Fifteen broken variants passed the current affected-package tests but failed the corresponding originals:

| Deliberately broken behavior | Relevant ledger rows |
| --- | --- |
| Forget a forced request during settling only when automatic detection is on | 33 |
| Let an older lifecycle event acknowledge a newer pending emission | 15 |
| Apply delayed rejection/preview feedback to newer pending work | 16, 17 |
| Let an old terminal event clear a different, newer active upload | 35, 36 |
| Carry an old folder's latched manual request into the newly selected folder | 9 |
| Refuse to forward already-ready work while another emission is requested | 25 |
| Strip a leading BOM by mutating the caller's snapshot map | 7 |
| Remove the reader's initial cancellation check | 67 |
| Remove the reader's explicit over-limit result check | 52 |
| Fail to restart an interrupted observation of the same candidate contents | 54 |
| Broaden `SETTINGS_` classification to `SETTINGS` | 51 |
| Drop the prior settled payload on essential-capacity overflow | 57 |
| Forget an omitted supplementary candidate's observation | 60 |
| Shorten an HTTP-date Retry-After delay by four seconds | 45 |
| Disable the factory-created HTTP client's request timeout entirely | 46 |

One additional positive-control probe—hiding combined-message delivery activity while automatic detection is off—was caught by both old and current tests. Thus some rendering coverage does survive; it would be inaccurate to say all deleted renderer behavior is untested.

The remaining judgments below are based on body/assertion comparison. Not every row was mutation-tested. Some lost scenarios are defensive component contracts rather than event orders normally produced by today's single worker. Their rarity does not make API visibility a justification for deleting them.

## Complete ledger

Original file paths and line numbers refer to the baseline. Replacement names refer to the current working copy.

### Command executables

| # | Original test | Verdict | Assessment |
| --- | --- | --- | --- |
| 1 | `cmd/profile/profile_windows_test.go:42` — `TestCaptureScansWithoutPersistenceAndWritesProfiles` | **B** | Split into `TestProfilingAppScansWithoutPersistence` and `TestCaptureWritesProfiles`. App safety and artifact generation are independently arranged and verified. Useful coverage retained. |
| 2 | `cmd/uploader/cli_linux_test.go:76` — `TestLinuxConsoleRejectsSecondInstanceAndStopsOnSIGTERM` | **B** | Split into `TestLinuxConsoleRejectsSecondInstance` and `TestLinuxConsoleStopsOnSIGTERMAndReleasesLock`. Instance rejection and graceful shutdown now have separate failure signals. |

### Application tests

| # | Original test | Verdict | Assessment |
| --- | --- | --- | --- |
| 3 | `internal/app/app_test.go:183` — `TestCoordinatorDryRunReceivesPreviewMetadata` | **E** | Preview identity/count/origin are covered by `TestUploaderDryRunReturnsPreviewWithoutCallingClient`; app preview publication/safety by `TestAppDryRunDoesNotUploadOrPersistState`. The comparison against the coordinator's private counter was unnecessary. |
| 4 | `internal/app/app_test.go:229` — `TestAppRetriesTransientFailure` | **E** | `TestUploaderRetriesOnItsOwnTimer` verifies retry delivery; `TestAppExplicitUploadRetriesWithAutomaticUploadsOff` verifies successful retry persistence through the app. The original was a sensible focused integration test, not a weird test. Its deletion was optional consolidation, not required cleanup. |
| 5 | `internal/app/app_test.go:320` — `TestCoordinatorReportsUploadStatePersistenceFailure` | **E** | Replaced by `TestAppReportsConfirmedUploadEvenWhenHistoryCannotBeSaved`, using an actual successful fake-client response and a real temporary filesystem failure. Confirmed success, history error, display, and logging remain checked. |
| 6 | `internal/app/app_test.go:420` — `TestToUploadRequestConvertsSnapshotFiles` | **E** | `TestAppSendsFileTextAndUnixMillisecondTimestamps` checks the received request against explicit text and timestamp values. Equivalent protocol coverage; direct testing of the conversion helper was also legitimate. |
| 7 | `internal/app/app_test.go:440` — `TestToUploadRequestStripsOnlyLeadingBOMWithoutChangingSnapshot` | **P** | `TestAppStripsOnlyLeadingBOMAndPersistsOriginalFingerprint` preserves wire normalization and raw fingerprint checks, **not caller-snapshot immutability**. An in-place map mutation passes current tests but fails the original. Add a component/conversion test that observes the very snapshot handed to the uploader. |

### Commands

| # | Original test | Verdict | Assessment |
| --- | --- | --- | --- |
| 8 | `internal/app/commands_test.go:51` — `TestCommandPauseKeepsPreferenceWhenPersistenceFails` | **E** | `TestCommandFailureSurvivesUnreadStatusesAndLaterUploadActivity` explicitly verifies that a failed pause leaves Automatic enabled while returning the error. The original focused test was valid; this behavior is now embedded in the command-result regression scenario. |
| 9 | `internal/app/commands_test.go:66` — `TestCommandFolderChangePreservesAcceptedSnapshotsAndOrigin` | **P** | `TestFolderChangePreservesAcceptedOldFolderWorkAndHistory` preserves accepted delivery and origin. But the old test also arranged a latched manual request before changing folders. No current test proves that request is discarded rather than carried into the new folder. The carry-over mutation survives. Restore that behavior as a separate scenario, not a force-flag assertion. |
| 10 | `internal/app/commands_test.go:114` — `TestCommandFolderChangeRetainsBackoffAndCoalescesNewSource` | **E** | `TestPendingNewFolderRemainsVisibleDuringOldFolderBackoff` verifies actual new-folder contents, retained delay, and persisted origin. Uploader component tests also verify newer-work precedence during backoff. |
| 11 | `internal/app/commands_test.go:183` — `TestSaveConfigRejectsInvalidCandidateWithoutCommittingIt` | **E** | Invalid-command atomicity is covered by `TestCommandFolderRejectsInvalidPathsWithoutChangingSelection`; configuration validation and failed-save preservation have storage tests. The old invalid poll-interval injection did not exercise a separate supported command path. |
| 12 | `internal/app/commands_test.go:203` — `TestCommandUploadNowKeepsRetryDeadline` | **B** | `TestAppManualUploadDoesNotBypassExactRetryDeadline` verifies no request one nanosecond before the deadline and a request exactly at it, without checking a force flag. |
| 13 | `internal/app/commands_test.go:237` — `TestCommandUnknownDoesNotPoll` | **B** | `TestCommandUnknownLeavesPausedStatusAndConfigurationUnchanged` checks relevant outcomes and permits harmless unchanged-status publication. Pinning an uninitialized observation kind or zero notification count was unnecessary. |
| 14 | `internal/app/commands_test.go:247` — `TestPublishReplacesUnreadStatus` | **B** | `TestAppPublishesLatestStatusWithoutWaitingForReader` now reads exactly once after controlled changes. Earlier mutation checks confirmed that it rejects drop-newer behavior. The original publisher-level test was valid too; the current replacement is not merely a filtering wait. |

### Delivery presentation

| # | Original test | Verdict | Assessment |
| --- | --- | --- | --- |
| 15 | `internal/app/delivery_test.go:50` — `TestOlderCompletionDoesNotClearRepeatedSnapshotPendingUpload` | **P** | `TestManualRequestRemainsPendingUntilIdenticalOlderUploadCompletes` observes later active work, not the old delayed-start and intermediate post-success pending windows. Clearing pending work on every event passes current tests but fails the original. Restore the projection regression. |
| 16 | `internal/app/delivery_test.go:86` — `TestDelayedRejectionDoesNotRejectNewerPendingEmission` | **P** | `TestSupersededRejectionDoesNotDescribeNewPendingSaves` queues new work before the old request fails, so the worker suppresses the old rejection. It does not test a rejection already emitted before newer work is accepted. Removing the view's newer-pending guard survives current tests. |
| 17 | `internal/app/delivery_test.go:116` — `TestDelayedPreviewDoesNotClearNewerPendingEmission` | **P** | No equivalent delayed-preview presentation case remains. A worker preview metadata test does not establish that an old preview cannot replace newer pending UI state. The guard-removal mutation exposes this gap. |
| 18 | `internal/app/delivery_test.go:144` — `TestSuccessHistoryDoesNotHideSubsequentSettling` | **E** | `TestAppHistoryDoesNotHideInitialSettling` exercises the relevant settled-history/settling combination with a real running app. The original injected delivery while its scanner was still initially unsettled; the replacement is a cleaner way to establish that presentation combination. |
| 19 | `internal/app/delivery_test.go:195` — `TestFailedQueueDoesNotClaimPendingDelivery` | **P** | Worker rejection after stop is tested, but coordinator presentation after failed acceptance is not. `TestAppCancelledBeforeStartDoesNotSubmit` is not equivalent. Restore the defensive acceptance-failure scenario without inspecting private pending state. |

### Observation timestamps

| # | Original test | Verdict | Assessment |
| --- | --- | --- | --- |
| 20 | `internal/app/detection_since_test.go:42` — `TestAppRetainsDetectionTimeAcrossManualUploadOfUnchangedSaves` | **E** | Moved to `TestFolderPollerManualEmissionRetainsObservationTime`, through the component's own operations and returned observations. App timestamp publication/wiring still has integration tests. |
| 21 | `internal/app/detection_since_test.go:66` — `TestAppRestartsDetectionTimeWhenSettledContentsChange` | **E** | `TestFolderPollerNewContentsRestartObservationTime` verifies the same owner-level rule with explicit observation times and actual file changes. |
| 22 | `internal/app/detection_since_test.go:119` — `TestAppRestartsDetectionTimeAfterScanFailure` | **E** | `TestFolderPollerScanFailureRestartsObservationTime` removes/restores a required file and checks the new observation window after it settles again. |
| 23 | `internal/app/detection_since_test.go:153` — `TestAppRestartsDetectionTimeAfterMonitoringWasPaused` | **E** | `TestFolderPollerPauseRestartsObservationTime` covers the component rule; app command/configuration wiring has separate pause/resume tests. |

### Forwarding

| # | Original test | Verdict | Assessment |
| --- | --- | --- | --- |
| 24 | `internal/app/forward_test.go:9` — `TestForwardSnapshotRunsWithAutomaticUploadsOff` | **E** | `TestAppExplicitUploadRetriesWithAutomaticUploadsOff` and `TestFolderChangePreservesAcceptedOldFolderWorkAndHistory` verify accepted work progresses while detection is off. No known behavior loss. |
| 25 | `internal/app/forward_test.go:27` — `TestForwardSnapshotDoesNotWaitForFutureEmission` | **P** | The replacement starts the old request before arranging the future request. It does not prove ready work is forwarded while a future emission is already requested. A guard refusing forwarding in that state passes current tests but fails the original. This is a defensive coordinator contract, not a normal poller-state assertion to discard. |
| 26 | `internal/app/forward_test.go:43` — `TestUploadCommandOlderDeliveryDoesNotConsumeFreshRequest` | **B** | `TestAcceptedOldFolderDeliveryDoesNotConsumeManualRequestForSettlingNewFolder` establishes the interaction with real commands, a newly settling folder, and observed requests. No force-flag or private-fingerprint comparison is needed. |

### Poller / coordinator

| # | Original test | Verdict | Assessment |
| --- | --- | --- | --- |
| 27 | `internal/app/poll_test.go:122` — `TestFolderPollerCancellationDoesNotRecordEmission` | **B** | `TestFolderPollerCancellationDoesNotEmitSnapshot` checks cancellation output and a subsequent valid emission, rather than inspecting the stored fingerprint. |
| 28 | `internal/app/poll_test.go:138` — `TestCoordinatorDetectsChangesDuringAnActiveUpload` | **E** | `TestFolderChangePreservesAcceptedOldFolderWorkAndHistory` requires changed old-folder data to be accepted while the first request is held, before pausing and switching folders. Without that detection, its required second old-folder upload cannot happen. `TestAppDetectsChangesMadeDuringUpload` also remains. |
| 29 | `internal/app/poll_test.go:171` — `TestCoordinatorDeliversNewSnapshotBeforeRetry` | **E** | The two uploader replacement-during-request/backoff tests establish scheduling; `TestAppDoesNotMarkNewSnapshotFirstAttemptAsRetry` establishes real file detection and handoff during backoff. |
| 30 | `internal/app/poll_test.go:220` — `TestRunStateSeedsPollerFromMatchingSuccessfulUpload` | **B** | Replaced field initialization assertions with `TestAppSkipsSnapshotAlreadyUploadedBeforeRestart`, `TestAppDoesNotReuseHistoryFromAnotherFolderOrDestination`, and `TestAppDryRunPreviewsSavesEvenWithMatchingHistory`. These verify the purpose of the baseline. |
| 31 | `internal/app/poll_test.go:246` — `TestCoordinatorAutomaticOffFinishesQueuedWorkAndRetries` | **E** | `TestAppExplicitUploadRetriesWithAutomaticUploadsOff` verifies retry completion and persistence; accepted old-folder work also has paused-detection integration coverage. |
| 32 | `internal/app/poll_test.go:277` — `TestCommandUploadLatchesEmissionWhileAutomaticOff` | **B** | `TestAppManualUploadWaitsForSettling` and the poller forced-request tests verify eventual emission and no premature upload, rather than reading the latch. |
| 33 | `internal/app/poll_test.go:295` — `TestTickPreservesForcedEmissionAcrossSettlingTicks` | **P** | `TestFolderPollerRetainsForcedRequestUntilSavesSettle` does not seed a matching baseline. Its automatic-on branch would emit normally even if the force request were lost. Forgetting the request only during automatic-on settling survives current tests but fails the original. Restore the matching-history/baseline case. |
| 34 | `internal/app/poll_test.go:332` — `TestTickAutomaticOffDoesNotReadSaveFolder` | **B** | `TestFolderPollerAutomaticOffDoesNotReadMissingFolder` configures the missing path through the poller's constructor and verifies a disabled observation rather than rewriting its scanner field. App pause/configuration wiring remains tested. |

### Recovery / old-folder feedback

| # | Original test | Verdict | Assessment |
| --- | --- | --- | --- |
| 35 | `internal/app/recovery_test.go:127` — `TestOldFolderTerminalFeedbackDoesNotReplaceCurrentPresentation` | **P** | Current old-folder rejection integration tests do not inject an old rejection/preview after newer current-folder activity is already represented. Clearing active work for every terminal event survives current tests. Keep the defensive presentation cases, without active-object identity assertions. |
| 36 | `internal/app/recovery_test.go:160` — `TestOldFolderSuccessIsPersistedWithoutReplacingCurrentPresentation` | **P** | Current tests preserve old-folder history during normal serialized delivery, but not the older-success-after-newer-active projection case. The same terminal-identity mutation survives. Restore the scenario's history and visible-activity assertions. |
| 37 | `internal/app/recovery_test.go:186` — `TestOldFolderRejectionCompletesPreviousFolderActivityWithoutShowingRejection` | **E** | `TestOldFolderRejectionDoesNotReplaceCurrentDetection` verifies activity completion and filtering; worker failure logging is exercised separately. Private active/rejected-state assertions were unnecessary. |

### Rendering

| # | Original test | Verdict | Assessment |
| --- | --- | --- | --- |
| 38 | `internal/app/render_test.go:12` — `TestRenderStatusWithHistory` | **N** | History with settling and pause has integration coverage, but this was already a cheap, coherent formatter test, including history-only output. Calling an unexported renderer was not a reason to delete it. Retain focused formatting coverage. |
| 39 | `internal/app/render_test.go:34` — `TestRenderStatus` | **N** | The table directly exercised starting, paused, settling, stable, pending, active/error, retry, old-folder, rejection, persistence-error, preview, and command-error compositions. Representative app tests are not a reason to remove this focused component table. Review brittle expectations individually rather than deleting the table. |
| 40 | `internal/app/scan_warnings_test.go:16` — `TestPollerReportsSupplementaryOmissionsWithoutReemittingCore` | **E** | `TestFolderPollerReportsWarningsWithoutReemittingCore` restores the component scenario; app warning/retry display has separate integration coverage. |
| 41 | `internal/app/scan_warnings_test.go:46` — `TestRenderSupplementaryWarningsAlongsideDelivery` | **N** | The app warning/retry scenario provides useful broader coverage, but deleting this tiny combined-message formatter case has no compelling benefit. It can live in the restored renderer table. |

### Retry helper policy

| # | Original test | Verdict | Assessment |
| --- | --- | --- | --- |
| 42 | `internal/app/upload_test.go:243` — `TestRetryWaitHonorsExactDeadline` | **E** | `TestUploaderRetriesOnItsOwnTimer` checks the actual retry request's deadline; `TestAppManualUploadDoesNotBypassExactRetryDeadline` checks the one-nanosecond-before boundary. Stop-during-backoff has component coverage. The old focused helper test was legitimate but no unique timing requirement is known to be lost here. |
| 43 | `internal/app/upload_test.go:271` — `TestRetryDelay` | **B** | Restored as `TestRetryDelayPolicy`, now including successive jitter bounds, the cap at large attempt counts, both Retry-After/backoff precedence directions, and wrapped errors. This is a rename/expansion, not discarded policy coverage. |

### HTTP client

| # | Original test | Verdict | Assessment |
| --- | --- | --- | --- |
| 44 | `internal/leaderboard/client_test.go:353` — `TestResponseExcerpt` | **E** | `TestClientQuotesAndBoundsResponseExcerpts` preserves all seven input cases through the returned upload error. Direct testing of the pure helper was not inherently wrong; this replacement does preserve its quoted/truncated output cases. |
| 45 | `internal/leaderboard/client_test.go:378` — `TestRetryAfterParsesHTTPDate` | **P** | The replacement adds past/invalid cases, but weakens exact fixed-clock arithmetic to a real-clock 55–60-second range. A four-second underestimate passes it and fails the original. Restore exact arithmetic with controlled time, either at the helper or client boundary. |
| 46 | `internal/leaderboard/transport_test.go:18` — `TestHTTPClientUsesEnvironmentProxyByDefault` | **P** | Environment routing is preserved. Removing an unrelated exact-30-second assertion was reasonable, but losing all protection for a **bounded request timeout** was not: that general requirement is explicit in `DESIGN.md:179,240`. Disabling the factory timeout entirely passes current tests and fails the original. Restore a focused bounded-timeout check without necessarily pinning the configuration to exactly 30 seconds. |
| 47 | `internal/leaderboard/transport_test.go:37` — `TestHTTPClientDirectWithoutEnvironmentDisablesProxy` | **E** | The HTTPS routing table's direct/no-environment row verifies actual direct receipt. Equivalent routing contract. |
| 48 | `internal/leaderboard/transport_test.go:51` — `TestHTTPClientSupportsExplicitProxySchemes` | **P** | New HTTP, HTTPS-proxy, SOCKS5, and SOCKS5h authentication/protocol tests are stronger for HTTP destinations. But the original checked configured selection for an **HTTPS destination for every scheme**; the new scheme tests mostly use HTTP destinations. Preserve the HTTPS-destination selection cases too. |
| 49 | `internal/leaderboard/transport_test.go:75` — `TestHTTPClientProxyPrecedence` | **P** | Most rows remain as real-routing tests. The old HTTPS-to-SOCKS per-scheme fallback row became an HTTP-proxy fallback row, and HTTPS loopback bypass became an HTTP-only loopback test. Restore these exact combinations rather than claiming full matrix parity. |
| 50 | `internal/leaderboard/transport_test.go:145` — `TestHTTPClientRoutesThroughAuthenticatedProxy` | **E** | Renamed to `TestHTTPClientSupportsAuthenticatedHTTPProxy`; the original request, response, and authentication checks remain. HTTPS proxy transport has a separate explicit test. |

### Scanner classification and limits

| # | Original test | Verdict | Assessment |
| --- | --- | --- | --- |
| 51 | `internal/saves/classification_test.go:12` — `TestEssentialClassifiesKnownCategories` | **P** | Positive categories have scanner coverage, but the negative `SETTINGSOTHER_player.pld` delimiter case disappeared. Broadening `SETTINGS_` to `SETTINGS` passes current tests and fails the original. Keep that negative classification requirement, directly or through nonblocking supplementary behavior. |
| 52 | `internal/saves/limits_test.go:70` — `TestReadFileEnforcesConfiguredSizeBoundary` | **P** | The new Scanner boundary test reaches the earlier metadata-size gate for oversized files. It does not exercise the reader's own over-limit check. Removing that check passes current tests but fails the original. Restore the lower-level reader boundary test; an early gate is not equivalent defense coverage. |
| 53 | `internal/saves/limits_test.go:121` — `TestScannerDirectoryLimitFailureAfterEarlierBatchReturnsNoSnapshot` | **B** | `TestScannerRejectsLargeOverLimitDirectoryWithoutPartialSnapshot` retains a larger-inventory over-limit/no-partial-result case with an explicit configured limit. It does not require an assertion about an internal batch object. |

### Settlement / admission policy

| # | Original test | Verdict | Assessment |
| --- | --- | --- | --- |
| 54 | `internal/saves/policy_test.go:12` — `TestSettleCandidateTransitions` | **P** | Whole-record `DeepEqual` was overcoupled, but its interrupted-observation case was valuable. Existing recovery tests restore different contents, so they do not protect restarting the same candidate's quiet period after interruption. Removing that restart passes current tests and fails the original. Restore the case with behavioral assertions. |
| 55 | `internal/saves/policy_test.go:71` — `TestSettleKnownVersionRefreshesMetadataWithoutMutatingPreviousVersion` | **B** | `TestScannerFingerprintIgnoresModificationTime` now verifies updated metadata, unchanged fingerprint, and unchanged metadata in the older snapshot. This replaces hash/flag bookkeeping assertions with the real result. |
| 56 | `internal/saves/policy_test.go:87` — `TestAdmissionEssentialReplacementUsesReservedCapacity` | **E** | `TestScannerEssentialReplacementReusesSnapshotCapacity` verifies replacement at the exact snapshot byte boundary, no warning, and an unchanged older snapshot. Selected-payload behavior is preserved. |
| 57 | `internal/saves/policy_test.go:103` — `TestAdmissionEssentialOverflowRetainsPreviousVersionAndCandidateObservation` | **P** | The new overflow-recovery test protects observation retention and recovery when capacity frees, but does not establish that the prior payload remains available after overflow. Dropping the prior payload passes current tests and fails the original. Add a subsequent failed-candidate/fallback scenario rather than restoring pointer-identity assertions. |
| 58 | `internal/saves/policy_test.go:120` — `TestAdmissionEssentialFailedCandidateRetainsVersionWithWarning` | **E** | `TestScannerReadFailureKeepsLastSettledVersionUntilRecoverySettles` and category/cache tests verify old-content fallback with named diagnostics. Internal byte-counter assertions were not necessary to retain this behavior. |
| 59 | `internal/saves/policy_test.go:135` — `TestAdmissionSupplementaryReplacementFallsBackWhenPreviousVersionFits` | **E** | `TestScannerSupplementaryGrowthFallsBackToFittingSettledVersion` checks the selected prior text, unchanged fingerprint, and fallback warning under capacity pressure. |
| 60 | `internal/saves/policy_test.go:151` — `TestAdmissionSupplementaryEvictsVersionsWhenNeitherFits` | **P** | `TestScannerEssentialGrowthOmitsSupplementaryVersionsThatNoLongerFit` verifies visible omission, and other tests check cache caps. It does not preserve the candidate-observation/recovery requirement. Resetting the omitted candidate's observation passes current tests but fails the original. Add a capacity-recovery timing assertion. |

### Scanner discovery / read safeguards

| # | Original test | Verdict | Assessment |
| --- | --- | --- | --- |
| 61 | `internal/saves/priority_test.go:174` — `TestScannerChecksRequiredNamesBeforeReadingContents` | **P** | I incorrectly treated this as unspecified error precedence. `DOCS.md:160` explicitly says essential names/counts are checked before reading. `TestScannerRejectsIncompleteInvalidSavesWithoutPartialSnapshot` accepts either validation order. Restore protection for the documented early-validation behavior, or explicitly reconsider that contract—not silently weaken the test. |
| 62 | `internal/saves/scan_test.go:193` — `TestScannerReadsMultipleDirectoryBatches` | **N** | The original already called `Scanner.Scan` and tested a useful requirement. Renaming it to `TestScannerIncludesAllFilesInLargeDirectory` and inflating the fixture to 300 files was not needed. Batch-aware fixture sizing is legitimate setup; `DESIGN.md` explicitly requests multi-batch coverage. |
| 63 | `internal/saves/scan_test.go:212` — `TestInventorySortsMetadataAcrossBatches` | **P** | The new creation-order fingerprint test admits every file. Map-key hashing can be deterministic without deterministic inventory/admission order. It does not protect the old sorted-discovery requirement or optional admission under capacity pressure. Keep a focused discovery test or compare selected subsets under budget pressure. |
| 64 | `internal/saves/scan_test.go:233` — `TestInventoryAcceptsDirectoryEntryLimit` | **E** | `TestScannerAcceptsExactDirectoryEntryLimit` verifies successful scanning at the configured entry limit with a valid required set. The exact-limit acceptance rule is preserved. |
| 65 | `internal/saves/scan_test.go:265` — `TestInventoryAcceptsEligibleFileLimit` | **B** | The old test passed the directory-entry limit to `inventory`, so its name overstated eligible-file-limit coverage. `TestScannerAcceptsExactEligibleFileLimit` actually exercises `SaveFiles` admission through Scan. |
| 66 | `internal/saves/scan_test.go:342` — `TestInventoryCancellationDoesNotReadMissingFolder` | **E** | `TestScannerCancellationDoesNotReadMissingFolder` reaches the inventory cancellation check and preserves cancellation-before-directory-access behavior. No distinct lost behavior was identified here. |
| 67 | `internal/saves/scan_test.go:354` — `TestReadFileCancellationDoesNotReadMissingFile` | **P** | Cancellation at inventory entry does not test cancellation at reader entry. Removing the reader's initial context check passes current tests but fails the original. Restore that focused reader safeguard. |
| 68 | `internal/saves/scan_test.go:386` — `TestEligibleFiltersFiles` | **E** | `TestScannerFiltersEligibleFiles` preserves the inclusion/exclusion examples through selected snapshot contents. The old pure predicate test was valid too; no unique filter case was identified as lost in this replacement. |

### Storage validation

| # | Original test | Verdict | Assessment |
| --- | --- | --- | --- |
| 69 | `internal/storage/storage_test.go:44` — `TestConfigLoadingAndSavingRejectInvalidSettings` | **B** | Split into `TestLoadConfigRejectsInvalidSettings` and `TestSaveConfigRejectsInvalidSettings`, retaining the invalid-value cases with independent API failure signals. |
| 70 | `internal/storage/storage_test.go:267` — `TestUploadStateLoadingAndSavingRejectInvalidHistory` | **B** | Split into `TestLoadUploadStateRejectsInvalidHistory` and `TestSaveUploadStateRejectsInvalidHistoryWithoutReplacingPreviousState`. The failed-save scenario still verifies previous history remains intact. |

## Tests that look deleted in textual diffs but are present again

Do not count reordering, moving, or restoration as an outstanding deletion:

- `TestAppDoesNotPersistRejectedSnapshot` is restored in `internal/app/app_test.go`.
- `TestFailedFolderConfigWritePreservesPendingDelivery` moved from `app_test.go` to `commands_test.go` and now arranges distinct newer pending contents before the failed command.
- `TestSuccessHistoryDoesNotHidePause` remains as a focused test.
- `TestFolderChangeClearsExistingRejection` moved to `delivery_test.go` and remains focused.
- The original poller tests for production limits, one-time emission, forced unchanged emission, changed contents, automatic-off behavior, and restart emission are present again as component tests.
- The original uploader tests for coalescing, backpressure, concurrent queue calls, retry timing, replacement during a request, resubmission after rejection, superseded failures, cancelled queue calls, stop during backoff, successful shutdown delivery, and joined cleanup are present again as component tests.
- In those uploader tests, removing assertions about an unrelated `App.LastUpload` or a data directory the worker never received **was** justified. Those assertions could not establish worker persistence behavior. The independent app persistence tests are the correct place for that requirement.
- Replacing private counter/force-flag/pointer-identity assertions was often justified. That did not justify deleting the underlying scenarios; rows 9, 15–17, 33, 35–36, 54, 57, and 60 illustrate that distinction.

The initial wholesale removal of the poller/uploader component tests was not worthwhile. Their later restoration corrected that category error, but did not restore every original edge case.

## Recommended disposition

1. Restore the **P** scenarios, preferably as focused component/projection tests. Keep behavioral assertions; do not blindly copy every private-field assertion from the old versions.
2. Restore/retain focused rendering coverage and the smaller, explicitly multi-batch scanner fixture marked **N**.
3. Keep the **B** improvements and the **E** equivalents unless a simpler focused test is preferable. Do not churn those again merely to enforce a visibility rule.
4. Do not use higher statement coverage, passing tests, or a near-unchanged test count as evidence that all original regression cases survived. The mutation checks demonstrate why that inference was wrong here.

The original audit changed no tests. The follow-up changes tests only; production implementations remain unchanged, and intentionally broken variants exist only in temporary overlays.

## Follow-up: all 26 findings addressed

The useful B/E replacements and existing integration coverage are retained. Component operations and their outputs are tested directly where appropriate; private flags, pending pointers, and counters are not used as regression assertions.

| Row | Resolution / current regression test |
| --- | --- |
| 7 | `internal/app/upload_test.go` — `TestUploaderPreservesSnapshotWhenNormalizingRequest` queues the actual snapshot, checks normalized request text/timestamps, and compares the submitted snapshot against an independent expected value. App fingerprint persistence remains covered separately. |
| 9 | `internal/app/coordinator_regression_test.go` — `TestFolderChangeDiscardsUnfulfilledManualRequest` requests upload during old-folder settling, changes folders with Automatic off, advances beyond settling, and checks there was no request or pending delivery from the new folder. Accepted old-folder work remains covered by `TestFolderChangePreservesAcceptedOldFolderWorkAndHistory`. |
| 15 | `internal/app/delivery_projection_test.go` — `TestOlderCompletionDoesNotClearRepeatedSnapshotPendingUpload` checks presentation after the older start and completion, before the newer emission is acknowledged, using identical snapshot contents and different emission identities. |
| 16 | Same file — `TestDelayedRejectionDoesNotRejectNewerPendingEmission` delivers an already-created old rejection to the projection after newer acceptance; it does not rely on worker suppression. |
| 17 | Same file — `TestDelayedPreviewDoesNotClearNewerPendingEmission` preserves newer pending presentation, then verifies that its own preview acknowledges it. |
| 19 | `internal/app/coordinator_regression_test.go` — `TestFailedQueueDoesNotClaimPendingDelivery` checks the coordinator's error/status output after queue rejection by a stopped worker. |
| 25 | Same file — `TestForwardSnapshotDoesNotWaitForFutureEmission` delivers already-ready work while the poller has a future emission request. |
| 33 | `internal/app/poll_test.go` — `TestFolderPollerRetainsForcedRequestUntilSavesSettle` now supplies a matching baseline for both Automatic-on and Automatic-off cases; ordinary automatic emission cannot mask a lost manual request. |
| 35 | `internal/app/delivery_projection_test.go` — `TestOldFolderTerminalFeedbackDoesNotReplaceCurrentPresentation` delivers old rejection/preview events after a newer current-folder upload starts. |
| 36 | `internal/app/coordinator_regression_test.go` — `TestOldFolderSuccessIsPersistedWithoutReplacingCurrentPresentation` verifies old-origin persisted success without clearing newer current-folder activity. |
| 38 | `internal/app/render_test.go` — restored `TestRenderStatusWithHistory` as a focused formatter test. |
| 39 | Same file — restored `TestRenderStatus` as a focused formatter test. |
| 41 | Same file — restored `TestRenderSupplementaryWarningsAlongsideDelivery`. |
| 45 | `internal/leaderboard/client_test.go` — `TestClientParsesRetryAfterHTTPDate` uses a frozen synctest clock and exact delays, including future, past, and malformed headers. |
| 46 | `internal/leaderboard/transport_test.go` — `TestHTTPClientConfiguresBoundedRequestTimeout` protects the factory's finite request timeout without specifying an incidental exact duration. |
| 48 | Same file — `TestHTTPClientRoutesHTTPSViaHTTPSProxy`, `TestHTTPClientRoutesHTTPSViaSOCKSProxy`, and retained `TestHTTPClientTunnelsHTTPSWithConnect` exercise HTTPS destinations through all four supported proxy schemes. Routing assertions observe actual TLS-proxy CONNECT requests or authenticated SOCKS handshakes; routing-only fixtures deliberately reject/close afterward. |
| 49 | Same file — `TestHTTPClientRoutesHTTPSViaSOCKSProxy` includes HTTPS-to-SOCKS fallback with only HTTP_PROXY set; `TestHTTPClientRoutesHTTPSAccordingToProxyConfiguration` includes localhost, IPv4, and IPv6 HTTPS loopback bypass with both config and environment proxies present. |
| 51 | `internal/saves/classification_test.go` — restored `TestEssentialClassifiesKnownCategories`, including the negative `SETTINGSOTHER_player.pld` delimiter case. |
| 52 | `internal/saves/read_test.go` — `TestReadFileEnforcesConfiguredSizeBoundary` supplies accurate over-limit metadata directly to the reader; separate `TestReadFileAcceptsConfiguredSizeBoundary` protects exact-limit acceptance. |
| 54 | `internal/saves/recovery_test.go` — `TestScannerRestartsInterruptedObservationOfSameContents` restores the same bytes after a failed read and requires a fresh full quiet period. |
| 57 | Same file — `TestScannerEssentialOverflowRetainsFallbackForLaterReadFailure` follows overflow with invalid candidate contents and verifies the prior usable payload/fingerprint remains available. Existing capacity-recovery timing coverage remains intact. |
| 60 | Same file — `TestScannerOmittedSupplementaryCandidateEntersWhenCapacityFrees` first admits an old optional version, then forces both versions out under essential count pressure, and verifies the observed new version enters immediately when capacity frees. |
| 61 | `internal/saves/priority_test.go` — restored `TestScannerChecksRequiredNamesBeforeReadingContents` and added `TestScannerChecksEssentialCountBeforeReadingContents`; invalid contents cannot supersede the documented names/count validation errors. |
| 62 | `internal/saves/scan_test.go` — restored `TestScannerReadsMultipleDirectoryBatches` with `directoryBatchSize + 1` additional files instead of the arbitrary 300-file fixture. |
| 63 | Same file — `TestScannerSelectsDeterministicSupplementarySubsetAcrossDirectoryBatches` compares exact admitted optional names under a four-file budget across opposite creation orders. |
| 67 | `internal/saves/read_test.go` — restored `TestReadFileCancellationDoesNotReadMissingFile`, protecting reader-entry cancellation independently of inventory cancellation. |

### Follow-up verification

- Native Windows `scripts/check.ps1` passed: workflow lint, test-reporter checks, all packages, console-tag tests, and vet. Every individual package run remained under the project's 10-second budget.
- Affected app/scanner/HTTP suites passed three consecutive runs (`-count=3 -p 1`).
- All **15 formerly surviving mutation probes now fail the affected package suites** at the restored/strengthened tests. The rendering positive control is still caught.
- Three additional probes are also caught: claiming pending delivery before failed queue acceptance (row 19), reversed optional admission order (row 63), and stopping inventory after one directory batch (row 62). Total: **19/19 probes caught**, including the positive control.
- Linux vet, test-binary cross-compilation, and uploader cross-build passed. These are **not native Linux test execution**; WSL remains unavailable because virtualization is disabled. Race testing remains unavailable without a CGO-capable C compiler.
- No production Go files were changed. Mutation sources, overlays, and logs are outside the repository in `C:/Users/hmnid/AppData/Local/Temp/cwg-audit-fixes-b614/`.

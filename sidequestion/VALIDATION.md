# `/btw` validation record

Date: 2026-09-10. Branch: `codex/btw-side-question`. Baseline: `8dae851b9b622f2ff2631f332fde9719d0b16fba`.

A standalone PostgreSQL test database and data directory were used; real model credentials were injected only into the standalone test environment, never written into code or this record, and the product's default model was not changed. Go 1.26.3, norma v0.3.6, Next.js 16.2.9.

The actual model conversations, returned objects, engineering assertions, and Qwen's raw review text are saved in [validation-2026-09-10.json](validation-2026-09-10.json), which contains no API credentials.

## Engineering checks

| Scope | Result | Evidence |
| --- | --- | --- |
| Structured messages and tool-argument deep copy | Pass | `TestCheckpointDeepCopyAndBoundaries` |
| Summary/compression requests do not overwrite, full reply and terminal state published, half-generated replies excluded | Pass | `TestCheckpointDeepCopyAndBoundaries`, `TestSnapshotExcludesPartialStreamAndSelectsPoolMember` |
| Actual model-pool member identity | Pass | `TestSnapshotExcludesPartialStreamAndSelectsPoolMember` |
| Tool pairing, 20-group replay, budget trimming, and over-limit errors | Pass | `TestBuildRequestCompactionToolPairingAndBudget` |
| Main/side-question parallelism, bidirectional cancellation isolation | Pass | blocking Provider, `TestMainSideConcurrencyAndIndependentCancellation` |
| No tool execution, streaming/non-streaming, usage already present on failure | Pass | `TestServiceNoToolsAndUsageOnFailure` |
| Real norma ChatAgent + local Read tool, main transcript/activity isolation | Pass | `TestSideActualChatCheckpointToolResultAndTranscriptIsolation`, streaming and non-streaming sub-cases |
| Persistence, pagination, idempotency, retaining partial answers across restart | Pass | `TestSideHistoryIdempotencyPagingAndRecovery` |
| Clear vs. late-write race, parent-resource deletion, version comparison | Pass | `TestSideClearLateWritersAndDeletedParent` |
| MainAgent/Worker archive and restore, v1/v2/v3 | Pass | `TestSideTaskArchiveVersions` |
| The three parent interfaces, authentication, resource ownership, Worker logical delete | Pass | `TestSideHTTPGlobalLimitTaskWorkerAndDeletion`, `TestSideCheckpointPersistsBeforeAdmissionAndRestart` |
| Side questions while the main conversation is busy, independent SSE reconnect/disconnect, cancel, clear | Pass | `TestSideHTTPBusyIsolationClearAndReconnect` |
| 1 per parent conversation / 4 global concurrency | Pass | two `TestSideHTTP…` cases |
| Snapshot persisted before submission, resuming questions after restart, old conversations cannot forge a snapshot | Pass | `TestSideCheckpointPersistsBeforeAdmissionAndRestart` |
| Refusing to continue after the cached configuration is deleted or the model changes | Pass | `TestSideRejectsDeletedOrChangedCachedProfile` |
| Cancelling before archiving and waiting for the final answer and usage to persist | Pass | `TestSideTaskDrainPersistsBeforeArchive` |
| Recording usage only once and attributing it to the side question when a streaming consumer cancels early | Pass | `TestSideUsageRecordedOnceOnConsumerCancellation` |
| An auto-restored Worker / deadline runtime context after restart keeps publishing new snapshots | Pass | `TestSideRestoredWorkerRuntimePublishesNewCheckpoint` |
| Race check on the related packages | Pass | the commands below |
| TypeScript and production build | Pass | `npx tsc --noEmit`, `npm run build` |
| Biome on the new frontend modules | Pass | `biome check`, 3 new modules |

After setting `ARTEX_PG_DSN` to a separate, disposable database, the automated checks can be reproduced (do not point it at a production database):

```sh
go test -race ./agent ./db ./server ./sidequestion ./llmrec ./llmpool \
  -run 'Test(Side|Checkpoint|Snapshot|BuildRequest|Service|MainSide|CaptureRun|TaskArchive|CompleteForwards|StopIntent|CancelIntent)' -count=1
cd web
npx tsc --noEmit
npx biome check src/lib/side-questions.ts src/hooks/use-side-questions.ts src/components/side-question-workspace.tsx
npm run build
```

The full Go regression is not all green: the `server` package has two pre-existing tests that fail during temp-directory cleanup, both reporting `TempDir RemoveAll … directory not empty`:

- `TestInheritedActivityDetailAndRelationDeletion`
- `TestTaskMetadataPatchReturnsRenameAndPin`

After exporting the source from the unmodified baseline above and rerunning the `server` package in the same isolated environment, these two cleanup failures also reproduce. The baseline run additionally shows a target-node-count assertion failure in `TestCoreTaskLifecyclePG`; the final modified `server` regression has no such assertion failure. The other packages pass, and this change's side-question cases and race check pass. Baseline problems were not marked as passing for this acceptance, and no existing assertion was modified to hide a problem.

The Next.js build emits the existing multi-lockfile / workspace-root inference warning; the build completes and all pages are generated successfully.

## Browser checks

Using the Codex In-app Browser connected to a standalone local Go service and the Next.js dev server. The following manual-automation operations were done on desktop and on a 390 × 844 narrow screen, checking screenshots and browser logs:

- Typing `/btw` while an ordinary chat is running: the main content and the side question display at the same time; the desktop side panel works.
- Consecutive follow-ups; after stopping a side question the generated part is kept; the main flow continues.
- The request continues when the panel is closed, and the completed answer is restored on reopening; an empty `/btw` after a page refresh restores the history.
- On the narrow-screen Drawer, input, buttons, history, and close all work, with no horizontal overflow.
- Clear uses a confirmation dialog; after clearing, the history disappears while the main transcript and snapshot are kept.
- Asking questions separately on a task's MainAgent and two Workers and switching between them: the agent labels and histories do not cross over.
- A blocking local-model fixture keeps the Worker running; submitting `/btw` from the Worker's main input box, after stopping the side question the Worker still shows live running and its own pause button, and the side question saves the partial answer.
- The browser error/warning log is empty.

A controllable fixture is used to verify concurrency timing precisely, without depending on a real model's output speed. During debugging, two Worker runtime checks did not form a valid concurrency window (the task had already ended / the answer finished early); after fixing the fixture they were redone and passed; these initial attempts are not counted as valid passes.

## Real model conversations

`grok-4.6` was probed first, via the OpenAI-compatible endpoint `http://127.0.0.1:12580/tingly/openai`. The probe returned HTTP 200, the model name `grok-4.6`, and `READY`, taking 2.82 seconds. The preferred option was available, so the Tingly `glm` or Zhipu `glm-5.3` fallback chain was not enabled; these two fallback services were not validated this time.

| Scenario | Actual result |
| --- | --- |
| Asking about assets, goals, and the marker while the main conversation is running | Returned `redhaze.top`, the homepage read and goal summary, and `BTW-REAL-0910`; the side question completed in 16.97 seconds |
| Asking for the tool evidence after the main conversation finished reading the homepage | Correctly cited WebFetch 200, the curl redirects 301 → 302 → 200, and the page title; 7.24 seconds |
| A side question asking Bash to create a test file | Refused to execute, the target file was not created; 7.74 seconds |
| A side question after completion does not change the main context | The main transcript SHA-256 and the main activity log stay consistent; the side-question tool-execution count is 0 |
| Following up after really stopping/restarting the Go service | Kept the previous 3 side-question history entries and answered assets, the marker, and the title directly from the persisted snapshot, without rerunning the main agent |
| A new conversation using the Grok non-streaming configuration | Correctly answered the assets and `ATOMIC-0910`; returned and saved usage: input 11734, output 138, cache_read 11520 |

In the asset case, the main conversation used WebFetch and Bash/curl to read the public homepage; the landing page was `https://id.redhaze.top/home`, with the title "RedHaze Technology RedHaze Group - Global Conglomerate Portal". Bash staged the response in a local test file; no write was performed to the remote. This fact was verified separately from "the side question executed no tools".

Main transcript checksum: `e7e61f135a4a120954b539f357e8c4205d7d5cd7460dcaf3dc0fd066463e1d00`.

**Usage limitation:** Tingly's Grok streaming response returned no usage. A separate direct request with `stream_options.include_usage=true` returned HTTP 200, 12 data frames, and 0 usage frames. So the 0 in the streaming tests means the endpoint provided no usage and must not be read as no billing. Non-streaming usage and the fixture's failure/cancellation usage are all saved correctly.

## Qwen review

The review model was `qwen-flash`, via the OpenAI-compatible endpoint `https://dashscope.aliyuncs.com/compatible-mode/v1`, HTTP 200. The first three real side-question conversations, the main conversation's tool evidence, and the engineering assertions were provided; it returned `verdict: accept` and `concerns: []`, judging that the answers were consistent with the asset, marker, and page-read evidence, and that the side-question tool refusal complied with the constraints. Review usage: prompt 6625, completion 312, total 6937.

This Qwen review did not cover the later-added service restart and non-streaming tests. Qwen's generalization of "no writes" was too broad: the main conversation's curl did create a local response temp file, as recorded explicitly above. Concurrency, zero tool execution, and transcript isolation are judged by engineering assertions; the model review only assists in assessing answer quality.

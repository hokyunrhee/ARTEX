# `/btw` validation record

Date: 2026-09-10. Branch: `codex/btw-side-question`. Baseline: `8dae851b9b622f2ff2631f332fde9719d0b16fba`.

Validation used an independent PostgreSQL test database and data directory. Real-model credentials were injected only into that test environment, never committed or included in this record. The product's default model was unchanged. Versions: Go 1.26.3, norma v0.3.6, Next.js 16.2.9.

Model conversations, returned objects, engineering assertions, and Qwen's review are in [validation-2026-09-10.json](validation-2026-09-10.json), without API credentials. Their prose is translated into English; identifiers, measurements, timestamps, and original transcript fingerprints are retained.

## Engineering checks

| Area | Result | Evidence |
| --- | --- | --- |
| Deep copies of structured messages and tool arguments | Passed | `TestCheckpointDeepCopyAndBoundaries` |
| Summary/compaction requests do not replace snapshots; complete responses and terminal states publish; partial replies are excluded | Passed | `TestCheckpointDeepCopyAndBoundaries`, `TestSnapshotExcludesPartialStreamAndSelectsPoolMember` |
| Actual model-pool member identity | Passed | `TestSnapshotExcludesPartialStreamAndSelectsPoolMember` |
| Tool pairing, 20-pair replay, budget trimming, overflow errors | Passed | `TestBuildRequestCompactionToolPairingAndBudget` |
| Main/side concurrency and cancellation isolation in both directions | Passed | Blocking provider, `TestMainSideConcurrencyAndIndependentCancellation` |
| No tool execution, streaming/non-streaming, usage retained on failure | Passed | `TestServiceNoToolsAndUsageOnFailure` |
| Real norma ChatAgent with local Read tool; main transcript/activity isolation | Passed | Streaming and non-streaming cases in `TestSideActualChatCheckpointToolResultAndTranscriptIsolation` |
| Persistence, pagination, idempotency, partial-answer retention after restart | Passed | `TestSideHistoryIdempotencyPagingAndRecovery` |
| Clear/late-write races, parent deletion, version comparison | Passed | `TestSideClearLateWritersAndDeletedParent` |
| MainAgent/worker archive and restore, v1/v2/v3 | Passed | `TestSideTaskArchiveVersions` |
| Three parent APIs, authentication, ownership, worker logical deletion | Passed | `TestSideHTTPGlobalLimitTaskWorkerAndDeletion`, `TestSideCheckpointPersistsBeforeAdmissionAndRestart` |
| Side questions while the main session is busy; independent SSE reconnect/disconnect, cancellation, clearing | Passed | `TestSideHTTPBusyIsolationClearAndReconnect` |
| One concurrent request per parent and four globally | Passed | The two `TestSideHTTP...` cases |
| Snapshot persistence before admission, follow-ups after restart, no fabricated snapshots for old sessions | Passed | `TestSideCheckpointPersistsBeforeAdmissionAndRestart` |
| Reject deleted cached profiles or changed models | Passed | `TestSideRejectsDeletedOrChangedCachedProfile` |
| Cancel and await final answer/usage persistence before archiving | Passed | `TestSideTaskDrainPersistsBeforeArchive` |
| Record usage and side-question attribution once when a streaming consumer cancels early | Passed | `TestSideUsageRecordedOnceOnConsumerCancellation` |
| Restored worker/deadline run contexts publish new snapshots | Passed | `TestSideRestoredWorkerRuntimePublishesNewCheckpoint` |
| Race checks for relevant packages | Passed | Commands below |
| TypeScript and production build | Passed | `npx tsc --noEmit`, `npm run build` |
| Biome for new frontend modules | Passed | `biome check` on three new modules |

Reproduce automated checks by pointing `ARTEX_PG_DSN` at a separate disposable database, never a production database:

```sh
go test -race ./agent ./db ./server ./sidequestion ./llmrec ./llmpool \
  -run 'Test(Side|Checkpoint|Snapshot|BuildRequest|Service|MainSide|CaptureRun|TaskArchive|CompleteForwards|StopIntent|CancelIntent)' -count=1
cd web
npx tsc --noEmit
npx biome check src/lib/side-questions.ts src/hooks/use-side-questions.ts src/components/side-question-workspace.tsx
npm run build
```

The full Go regression suite was not entirely green. Two existing `server` tests failed during temporary-directory cleanup with `TempDir RemoveAll ... directory not empty`:

- `TestInheritedActivityDetailAndRelationDeletion`
- `TestTaskMetadataPatchReturnsRenameAndPin`

Exporting the unchanged baseline and rerunning `server` in the same isolated environment reproduced both cleanup failures. The baseline also failed the goal-node-count assertion in `TestCoreTaskLifecyclePG`; the final modified `server` run did not have that assertion failure. Other packages, side-question cases, and race checks passed. Baseline failures were not counted as accepted successes, and existing assertions were not changed to conceal them.

The Next.js build emitted existing multiple-lockfile/workspace-root inference warnings, then completed and generated every page.

## Browser checks

Codex In-app Browser connected to separate local Go and Next.js development servers. The following interactions were exercised on desktop and at 390 x 844, with screenshot and browser-log inspection:

- Entering `/btw` while an ordinary conversation ran displayed main and side content together with a working desktop sidebar.
- Follow-up questions worked; stopping retained partial side answers while the main flow continued.
- Closing the panel let requests continue; reopening restored completed answers. After refresh, an empty `/btw` restored history.
- Narrow-screen Drawer input, buttons, history, and closing worked without horizontal overflow.
- Clearing required confirmation and removed history while retaining the main transcript and snapshot.
- Questions to a task MainAgent and two workers kept agent labels and histories separate when switching.
- A blocking local-model fixture kept a worker active. Submitting `/btw` from its main input and stopping the side question left the worker visibly running with its own Pause button, while retaining the side question's partial answer.
- Browser error and warning logs were empty.

Controlled fixtures verified concurrency timing without depending on real-model output speed. Two initial worker checks did not produce valid overlap because the task or answer ended too early. After correcting the fixture, the checks were repeated and passed; the initial attempts were not counted as valid passes.

## Real-model conversations

The first availability probe used `grok-4.6` at the OpenAI-compatible endpoint `http://127.0.0.1:12580/tingly/openai`. It returned HTTP 200, model `grok-4.6`, and `READY` in 2.82 seconds. Since the preferred model was available, fallback Tingly `glm` and Zhipu `glm-5.3` were not used or validated.

| Scenario | Observed result |
| --- | --- |
| Ask about the asset, goal, and marker while the main session runs | Returned `redhaze.top`, the homepage-read-and-summary goal, and `BTW-REAL-0910`; completed in 16.97 seconds |
| Ask for tool evidence after homepage retrieval | Correctly cited WebFetch 200, curl redirects 301 -> 302 -> 200, and the page title; 7.24 seconds |
| Ask the side question to create a test file with Bash | Refused execution; file was not created; 7.74 seconds |
| Completed side question leaves main context unchanged | Main transcript SHA-256 and activity records matched; zero side-question tool executions |
| Follow up after actually stopping and restarting Go | Preserved the previous three side-question records and answered from the persisted snapshot without rerunning the main agent |
| New conversation with non-streaming Grok | Correctly returned the asset and `ATOMIC-0910`; saved usage: input 11734, output 138, cache_read 11520 |

The main session used WebFetch and Bash/curl to read a public homepage. The landing page was `https://id.redhaze.top/home`, whose title translates as "RedHaze Technology RedHaze Group - Global Conglomerate Portal." Bash saved the response in a local test file; no remote write occurred. This was verified separately from the assertion that side questions executed no tools.

Original main transcript checksum: `e7e61f135a4a120954b539f357e8c4205d7d5cd7460dcaf3dc0fd066463e1d00`.

**Usage limitation:** Tingly's streaming Grok responses returned no usage. A separate direct request with `stream_options.include_usage=true` returned HTTP 200, 12 data frames, and zero usage frames. Streaming usage of 0 therefore means the endpoint supplied no usage, not that billing was zero. Non-streaming usage and fixture failure/cancellation usage were saved correctly.

## Qwen review

The review used `qwen-flash` through `https://dashscope.aliyuncs.com/compatible-mode/v1` and received HTTP 200. Inputs included the first three real side-question conversations, main-session tool evidence, and engineering assertions. It returned `verdict: accept` and `concerns: []`, finding the answers consistent with the asset, marker, and homepage evidence, and tool refusal consistent with the constraints. Review usage: prompt 6625, completion 312, total 6937.

The review did not cover the subsequently added restart and non-streaming tests. Qwen's statement that there were no writes was too broad: main-session curl did create a temporary local response file, as documented above. Engineering assertions establish concurrency, zero tool execution, and transcript isolation; model review only assists answer-quality assessment.

# Long side-question conversations and context budgets

Fixed on 2026-09-11. The original implementation treated request-JSON character counts as tokens and inherited the main task's 32K output reservation. Large HTML, JavaScript, and tool results therefore caused valid side questions to be rejected prematurely.

## Review of open-source implementations

- [Grok CLI side-question context](https://github.com/superagent-ai/grok-cli/blob/fb97af83f06dca873281d60168430f06c8de6324/src/agent/agent.ts#L739): excerpts recent user/assistant text with an approximately 2000-character budget and at most 400 characters per entry. This path does not maintain continuous side-question history.
- [Grok CLI independent requests](https://github.com/superagent-ai/grok-cli/blob/fb97af83f06dca873281d60168430f06c8de6324/src/utils/side-question.ts): independent cancellation, a 2048-token output limit when supported, and no tools.
- [Grok CLI main-session compaction](https://github.com/superagent-ai/grok-cli/blob/fb97af83f06dca873281d60168430f06c8de6324/src/agent/compaction.ts): token estimation, recent-content retention, updates to rolling summaries, and truncation across turns.
- [OpenCode session compaction](https://github.com/anomalyco/opencode/blob/b3f1a96c6dd7adeb28b36dd11add1998fc84d67b/packages/core/src/session/compaction.ts): full-request estimation, output/buffer reservations, recent content plus rolling summaries, and tool-free summary requests. Defaults are an 8000-token recent-content budget and 4096-token summary output limit.
- [OpenCode overflow recovery](https://github.com/anomalyco/opencode/blob/b3f1a96c6dd7adeb28b36dd11add1998fc84d67b/packages/core/src/session/runner/llm.ts): recovery is attempted only before assistant output begins, and the recovered call cannot reenter the same recovery path.

ARTEX adopts independent output budgets, recent content with rolling summaries, and bounded recovery. It retains norma v0.3.6 structured messages and tool pairing instead of copying Grok's text-excerpt approach, and does not write OpenCode-style main-session compaction events to ARTEX's main transcript.

## Request budgets and execution

- Messages use norma's UTF-8-byte estimates per content block with a 4/3 allowance, plus system prompts, tool schemas, and message-envelope overhead. These are estimates, not exact model token counts.
- Side-question output defaults to at most 8192 tokens and never exceeds an explicitly configured main-model output limit. Set `ARTEX_BTW_MAX_OUTPUT_TOKENS` to a limit from 256 to 32768. This does not change the product's default model or main-task parameters.
- Input budget is the context window minus output and safety reservations. Unknown windows use the platform's 200K default. The safety margin is 5% of the window, bounded from 128 to 8192 tokens.
- Successful pairs load in increasing ordinal order, at most 20 per batch. Keep no more than 20 original pairs, with a token budget no greater than one quarter of the input budget and at most 16K.
- Older pairs update a rolling summary that records history provenance and context time. Historical assistant answers are not new tool evidence; the latest main snapshot wins on conflicts.
- If the main context remains too large, summarize only older messages in its copy and retain up to 8K recent tokens. Boundaries never split tool calls from results. An oversized single group is summarized as a whole.
- Summary inputs are divided at UTF-8-safe boundaries according to actual remaining window capacity, with at most 2048 output tokens. Empty, truncated, tool-calling, or over-budget summaries are not cached. Each side question permits at most 12 summary calls under the same 120-second timeout; reaching the limit fails explicitly rather than looping indefinitely.
- If the first model call reports context overflow before emitting text or tool calls, reduce context further and retry at most once. Stop recovery immediately if the estimate does not decrease. Other errors and partial streamed output do not trigger this recovery.
- Accumulate all returned usage, including summaries, failed attempts, and cancellation, on the same request. If the provider returns no usage, only zero can be recorded; estimates must not be presented as actual usage.

## Persistence and interface

`side_question_sessions.memory` stores older-pair summaries, the covered ordinal, and main-context summaries cached by snapshot identity. `side_question_requests.context_info` stores preparation stage, actual replay count, summary usage, and budget estimates.

Save summaries only while the original request remains active and its clear generation matches. Clearing also removes caches, and late writes cannot restore cleared data. New snapshots do not reuse old snapshot summaries. Summary fields are included in v3 task archives; old v3 archives missing them restore empty objects, and v1/v2 remain compatible.

POST admits and returns the request before background preparation and compaction, without holding admission locks or database transactions. SSE/history shows Preparing, Summarizing questions, Compacting context copy, and Answering stages. Compaction failure is saved as the request's failed terminal state. The frontend retains the error and restores the failed question draft without a toast obscuring the input; history polling no longer clears submission errors.

## Validation record

- Automated tests passed for replaying 19/20/21/50 pairs, retaining older conclusions across the 20-pair boundary, and reusing summary caches after restart.
- Automated tests passed for long Chinese answers and code context, chunk budgets, tool pairing, immutable snapshots, and cache invalidation for new snapshots.
- Automated tests passed for summary failure/cancellation/truncation/oversized output/tool calls, clear races, call limits, single overflow recovery, and no retry after partial streams.
- Independent PostgreSQL tests passed for pagination, restart, v1/v2/v3 archives, summary-cache and budget-metadata restoration, old v3 archives without new fields, and 20 parents sharing four concurrency slots.
- Candidate Go service build, frontend TypeScript checks, Biome checks on modified components, and an isolated Next.js production build passed.
- The in-app browser used independent UI fixtures at 1280 x 720 and 390 x 844 to verify preparation stages, summary-scope notices, draft restoration after failure, no floating error toast, no horizontal overflow, and no console errors. Temporary fixtures were removed.
- Read-only replay of existing local worker snapshots passed the new budget checks. For example, Worker #3's 293085-character snapshot was no longer incorrectly rejected by local character counting. No external model call was made for this check.
- Automatic approval review rejected the live Grok test that would send private worker snapshots; it was not executed and is not reported as passed.
- At the user's request, the local backend restarted on 2026-09-11 at 00:37 with the original database, data directory, and login configuration. The running executable matched the candidate SHA-256, and both backend and frontend-proxy `/api/health` checks succeeded.

Validation commands, using separate test databases only:

```sh
go test -race ./sidequestion ./db ./server -run 'TestSide|TestCheckpoint|TestSnapshot|TestBuildRequest|TestService|TestMainSide|TestTaskArchive' -count=1
go build ./cmd/artex
npx tsc --noEmit
npm run build -- --webpack
```

The production frontend build used a separate copy to avoid overwriting the active preview's `.next`. The candidate service at `/private/tmp/artex-btw-budget-candidate` was copied to `/private/tmp/artex-btw-preview/artex` and started; the old binary was backed up beside it as `artex.before-context-budget`.

# Side-question long conversations and context budget

Fixed 2026-09-11. The original implementation treated the character count of the request JSON directly as tokens and inherited the main task's 32K output reservation, so with a lot of HTML, JS, and tool results it would reject normal side questions prematurely.

## Open-source implementation review

- [Grok CLI side-question context](https://github.com/superagent-ai/grok-cli/blob/fb97af83f06dca873281d60168430f06c8de6324/src/agent/agent.ts#L739): extracts fragments from the most recent user and assistant text, with a character budget of about 2000 and at most 400 characters per item. It does not maintain a continuous side-question history on this path.
- [Grok CLI independent request](https://github.com/superagent-ai/grok-cli/blob/fb97af83f06dca873281d60168430f06c8de6324/src/utils/side-question.ts): an independent cancellation signal; the output cap is 2048 tokens when the model supports it; no tools provided.
- [Grok CLI main-conversation compression](https://github.com/superagent-ai/grok-cli/blob/fb97af83f06dca873281d60168430f06c8de6324/src/agent/compaction.ts): estimates tokens, keeps recent content, updates new content into the old summary, and handles cross-turn truncation.
- [OpenCode session compression](https://github.com/anomalyco/opencode/blob/b3f1a96c6dd7adeb28b36dd11add1998fc84d67b/packages/core/src/session/compaction.ts): full-request estimation, output/buffer reservation, recent content plus a rolling summary, and a tool-free summary request; this implementation defaults to a recent budget of 8000 and a summary output cap of 4096 tokens.
- [OpenCode overflow recovery](https://github.com/anomalyco/opencode/blob/b3f1a96c6dd7adeb28b36dd11add1998fc84d67b/packages/core/src/session/runner/llm.ts): overflow recovery is attempted only before any assistant output has begun, and the post-recovery call does not re-enter the same overflow-recovery path.

ARTEX borrows the independent output budget, recent content plus rolling summary, and bounded recovery approaches. It keeps norma v0.3.6's structured messages and tool pairing rather than copying Grok's text extraction, and it does not write OpenCode's main-conversation compression events into the ARTEX main transcript.

## Request budget and execution

- Messages reuse norma's per-content-block UTF-8 byte estimate with a 4/3 margin; the system prompt, tool schema, and message-envelope overhead are also counted. The estimate is not the model's exact token count.
- Side-question output defaults to at most 8192 tokens and never exceeds the output cap already set in the main configuration. The service environment variable `ARTEX_BTW_MAX_OUTPUT_TOKENS` can set a cap of 256–32768; it does not modify the product's default model or the main task's parameters.
- The input budget is the context window minus the output cap and a safety margin; an unknown window uses the platform default of 200K. The safety margin is 5% of the window, with a minimum of 128 and a maximum of 8192 tokens.
- Successful exchanges are loaded by increasing sequence number, at most 20 per batch. At most 20 exchanges of original text are kept, with a token budget of at most 1/4 of the input budget and no more than 16K.
- Exchanges beyond that are folded into the rolling summary. The summary carries its historical source and context time; a historical assistant answer is not equivalent to new tool evidence, and on conflict the latest main snapshot takes priority.
- When the main context is still too long, only the older messages in the copy are summarized, keeping at most 8K tokens of recent content; the cut point does not split a tool call from its result. An oversized single group is summarized as a whole.
- Summary input is chunked UTF-8-safely according to the actual remaining window, with an output cap of 2048 tokens; an empty summary, truncation, a tool call, or exceeding the summary budget are none of them cached. A single side question makes at most 12 summary calls and is subject to the same 120-second timeout; reaching the limit fails explicitly rather than looping forever.
- If the model's first return reports a context overflow and no text or tool call has been output yet, it shrinks further and retries at most once; if the estimated size does not drop, recovery stops immediately. Other model errors and partial streaming output do not trigger this recovery.
- All usage obtained — including usage from summaries, failed attempts, and cancellation — is accumulated into the same side-question request. When the Provider returns no usage, it can only record zero and must not pass an estimate off as actual usage.

## Persistence and UI

`side_question_sessions.memory` stores the summary of old exchanges, the ordinal it covers, and a main-context summary cached by snapshot identity. `side_question_requests.context_info` stores the preparation phase, the actual number of replayed groups, summary usage, and the budget estimate.

The summary is saved only while the original request is still running and the cleanup version matches; a clear also clears the cache, and a late write will not restore already-cleaned data. A new snapshot does not reuse the old snapshot's summary. Summary fields are saved with v3 task archives; when restoring an old v3 that lacks the field, an empty object is filled in, and v1/v2 remain compatible.

A POST accepts and returns the request first, while preparation and compression run in the background, holding no admission lock or database transaction. SSE/history shows the preparing, assembling-exchanges, compressing-copy, and answering phases; a compression failure is saved as the side-question request's failure terminal state. The frontend keeps the error and restores the draft of the failed question rather than covering the input box with a floating notification; history polling no longer clears the submission error.

## Validation record

- 19/20/21/50-group replay, retention of old conclusions across 20 groups, and summary-cache reuse after restart: automated checks pass.
- Very long Chinese answers and code context, chunked request budgets, tool pairing, snapshot immutability, and new-snapshot cache invalidation: automated checks pass.
- Summary failure/cancellation/truncation/over-length/tool return, clear races, the call limit, single overflow recovery, and no retry on partial streams: automated checks pass.
- On a standalone PostgreSQL: pagination, restart, v1/v2/v3 archiving, archive/restore of the summary cache and budget metadata, an old v3 missing the new fields, and 20 parent conversations sharing four concurrency slots: pass.
- The Go candidate service build, the frontend TypeScript check, the Biome check on modified components, and the Next.js production build in a standalone directory: pass.
- The built-in browser used a standalone UI fixture to verify, at 1280×720 and 390×844, the assembling phase, the summary-scope hint, draft recovery after failure, the absence of floating error notifications, no horizontal overflow, and no console errors. The temporary fixture has been removed.
- A read-only replay of an existing local Worker snapshot passed the new budget check; for example, Worker #3's 293085-character snapshot is no longer wrongly rejected by a local character count. This item makes no external model calls.
- The live-conversation test that would have sent a private Worker snapshot to Grok was rejected by the automatic approval review, was not executed, and does not count as a passing item.
- At 2026-09-11 00:37, the local backend was restarted at the user's request, reusing the original database, data directory, and login configuration. The running file's SHA-256 matches the candidate binary; `/api/health` returns healthy on both the backend and the frontend proxy.

Validation commands (standalone test database only):

```sh
go test -race ./sidequestion ./db ./server -run 'TestSide|TestCheckpoint|TestSnapshot|TestBuildRequest|TestService|TestMainSide|TestTaskArchive' -count=1
go build ./cmd/artex
npx tsc --noEmit
npm run build -- --webpack
```

The frontend production build used a standalone copy to avoid overwriting the current preview's `.next`. The candidate service lives at `/private/tmp/artex-btw-budget-candidate`, was copied to `/private/tmp/artex-btw-preview/artex` and started; the original binary was backed up as `artex.before-context-budget` in the same directory.

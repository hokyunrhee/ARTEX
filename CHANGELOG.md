# Changelog

Important changes are recorded here, following [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### English-language migration

#### Changed

- Converted backend messages, logs, prompt defaults, tool descriptions, notifications, reports, frontend UI and locale formatting, scripts, skills, and project documentation to US English. Existing identifiers, API fields, routes, enum values, module paths, image names, and upstream attribution remain unchanged.
- New model-decision reasons use `[model]`. Readers permanently accept legacy reasons in rows, audit JSON, and restored archives, with an idempotent rewrite for historical reason prefixes. New mention tokens use `finding`, `asset`, `company`, `api`, `ip`, `app`, `domain`, `subdomain`, and `service`; stored legacy tokens remain accepted. Customized judge prompts replace legacy contract blocks instead of appending contradictory contracts.
- Added guarded English-default migrations, including per-role `*_english_v1` prompt flags, `finding_workflow_tools_v4_english`, and `intercept_default_rules_english_v1`. Frozen prompt digests and exact tool/schema/rule comparisons upgrade untouched defaults while retaining customized values. Prompt upgrades append versions, preserving history; customizations are logged once. Intercept rule updates preserve enabled state, action, and priority. Reset-to-default controls remain available for intentional operator resets.
- Notification severity/status labels and default message titles are English. Webhook variables such as `{{.Title}}`, `{{.SeverityLabel}}`, and `{{.StatusLabel}}` retain their names but return English labels. Review stored `vulnclass_include` / `vulnclass_exclude` keywords, since new English findings may no longer match Chinese keywords.
- This fork is maintained in English, but Compose still pulls the upstream `autumn27/artex` image and screenshots still show its Chinese UI. The owner must publish an English image/release and recapture screenshots. The updater remains pointed at `Autumn-27/artex` and replaces only the binary; obtain English builds from the owner's releases and refresh `skills/` manually.
- Preserve real-world localized data matching, legacy parser alternatives, Unicode/GBK support, and multibyte test vectors through exact line-scoped exceptions in `scripts/i18n-allowlist.txt`. `scripts/check-no-cjk.sh` mechanically rejects unapproved source residue. The separate benchmark Compose file remains unavailable because its Dockerfile and environment files are absent.

### Intercept

#### Added

- **Added the built-in Delete-style API paths intercept rule**: the existing destructive HTTP rules recognized only the DELETE **method** (`curl -X DELETE`, `requests.delete(`, `method:'DELETE'`), while path rules covered only `/clear /wipe /flush /purge /truncate /drop /destroy /factory-reset /reset-all`. Many applications delete through GET/POST, so calls such as `curl 'http://t/api/user/delete?id=1'` matched no built-in rule and could delete target data. A new `deny` rule covers `/delete /del /remove /unlink /erase /destroy`, including suffix forms such as `/deleteAll`, `/delete_user`, and `/delete-user`. The verb boundary prevents false positives for `/delivery`, `/details`, `/delta`, and `/delegate`. An independent seed flag ensures **existing installations receive the rule on upgrade**. Like other built-in rules, it can be disabled or deleted under System -> Command intercept.
### Finding notifications

#### Added

- **Finding notifications for IM and other channels**: the new System -> Notifications page sends scan findings to **DingTalk, Feishu, WeCom, generic webhooks, Telegram, and email**. Configure any number of instances per channel type, such as separate incident-response and routine-update DingTalk bots, each with its own enabled state, rate limit, and filters.
- **Real-time and digest delivery modes**: real-time mode sends each matching finding immediately. Digest mode groups findings on a global interval, defaulting to 30 minutes, with an opening count of new findings over the last N minutes and their severity distribution. For high-severity real-time delivery and digests for the rest, configure two channels separately; the policy is not hardcoded.
- **Four filter dimensions**: minimum severity, task/asset scope, vulnerability-class keyword inclusion and exclusion (exclusion wins), and whether to receive status changes. Status-change delivery defaults off because notifications usually mean newly discovered findings rather than every status transition. Individual messages include a View details button using the global backlink URL; no button appears when it is empty.
- **Delivery history and manual resend**: the notification page shows status, attempts, failure reason, and channel, with channel/status filters. Failed deliveries can be resent in one action, resetting attempts because a manual resend indicates the cause has been addressed.
- **Masked channel credentials**: each channel declares webhook URLs, signing secrets, bot tokens, SMTP passwords, and other secrets through `SecretKeys()`. APIs return masked values with a suffix hint. Returning the unchanged mask preserves the value; clearing the input removes it.

#### Fixed

> This section records a security audit after the initial feature implementation. Every issue was reproduced, fixed, and covered by a regression test.

- **Fixed a critical mask bypass through changing destinations while retaining credentials**: masks prevented browser disclosure, but destination and credential fields were independent, and omitted configuration keys retained stored values. **Changing only the destination and omitting credentials** could silently send real stored credentials to an arbitrary endpoint, without redirects: generic webhook `Authorization`, Telegram bot tokens in request paths, and SMTP passwords sent after STARTTLS. This was reproduced across four channels. Now **every destination change requires an explicit choice for every credential field**: supply a new value or explicitly clear it. Returning an unchanged mask also requests reuse of old credentials and is rejected. Credentials are not silently discarded, because optional fields such as `headers` would otherwise lose authentication while the API misleadingly reported success.
- **Escaped untrusted content in Markdown channels**: DingTalk, WeCom, and Feishu previously applied no escaping, unlike Telegram and email. Finding titles and summaries come from model output based on target responses, and asset URLs contain target-controlled query strings. A title such as `Login SQL injection\n[Urgent: verify your account](http://attacker.tld)` rendered a **clickable external link**; `![](http://attacker.tld/beacon)` caused a client fetch that revealed the reader's IP and that the finding had been viewed. Content is now flattened to one line and Markdown metacharacters escaped. Escaping occurs at each rendering boundary, not in the title helper shared across four contexts; otherwise Markdown backslashes would leak into Telegram HTML.
- **Restricted SSRF through delivery destinations**: validating only scheme and host allowed cloud metadata (`169.254.169.254`, potentially exposing instance credentials), loopback, and private-network destinations. Failed-response previews copied the first 200 bytes into `last_error` and delivery history, creating a partially blind internal-HTTP read primitive. Protection now occurs **at dialing**, the actual enforcement point, covering DNS rebinding and same-host redirects. Cross-host redirects are rejected because credentials may be embedded in URLs. Loopback/link-local access requires explicit `ARTEX_NOTIFY_ALLOW_LOCAL=1`, preserving legitimate local SMTP relays. **RFC1918 private addresses remain allowed intentionally** for common self-hosted Mattermost and SMTP deployments.
- **Redacted credentials from notification errors**: `http.Client.Do` returns `*url.Error` containing the **complete URL**, including DingTalk `access_token`, WeCom `key`, Feishu hook IDs, and Telegram `/bot<token>/`. Those values leaked into plaintext `notification_deliveries.last_error`, delivery-history responses bypassing configuration masks, server logs, and test-send UI errors. Errors now retain only `scheme://host` and the underlying DNS/connectivity/certificate cause, removing paths and queries. `url.Parse` failures receive the same treatment. **The previous fix covered client errors but missed parse errors; its tests exercised only the scheme branch and falsely implied coverage.** New tests exercise the actual parsing-failure branch.
- **Prevented silent loss when truncated digests marked entire batches delivered**: channel limits, especially WeCom's 4096 bytes, previously truncated messages while marking all batch entries successful. Omitted findings appeared neither in the message nor in failures. Messages now pack **complete entries** and mark only included entries delivered; the remainder return to the queue. Headers accurately state that the first N are shown and the remaining M will follow. Deferred entries **do not consume retry attempts**: the optimistic claim increment is reversed. Otherwise a 500-entry backlog could exhaust tail entries by the third segment without a single actual failure.
- **Validated minimum-severity filters**: a typo such as `min_severity=hgih` previously mapped to rank 0, reducing the condition to always-true `rank >= 0`. Operators expecting high-severity-only delivery instead received everything, with no visible distinction. Writes now validate values and list accepted choices in errors. Reads remain tolerant so old invalid values do not make the entire channel unreadable.
- **Made `rate_per_min = 0` usable for unlimited delivery**: documentation, UI, and token buckets treated 0 as unlimited, but persistence used `if RatePerMin <= 0 { useDefault }`, silently changing explicit 0 to 20 for DingTalk/WeCom/Telegram or 100 for Feishu. Only the request body distinguishes omission from explicit zero, so defaults now apply at the API layer only when the field is absent.
- **Applied token buckets to digest channels**: `takeTokens` consumed allowance that was never used, leaving digest `rate_per_min` ineffective. Claims now respect both the current allowance and the memory bound.
- **Handled retries per delivery instead of using the batch's maximum attempt count**: an older entry already retried twice could previously make new entries in the same batch permanently fail before their first retry. Each entry now fails immediately for permanent errors or its own exhausted retry budget; others are rescheduled using their own backoff level.
- **Marked unreadable digest snapshots failed explicitly**: rendering skipped malformed snapshots to preserve the rest of the batch, but the later bulk-success update incorrectly included them. They now receive a visible failure reason in delivery history.
- **Bounded per-channel delivery batches by lease duration**: a three-minute lease could expire during a long serial batch, letting another instance reclaim and resend rows while incrementing attempts twice. The batch cap is derived from lease duration and per-send timeout, with an assertion fixing the relationship between all three constants. That assertion exposed that the previous cap of 6 left no margin; it is now 5.
- **Protected Telegram HTML entities during truncation**: avoiding partial tags was insufficient because an incomplete entity such as `&amp` could cause rejection of the **entire message**, especially long digests. Truncation now avoids both unclosed tags and incomplete entities.
- **Retried temporary SMTP failures**: SMTP 4xx responses, such as greylisting `450`, require retrying, but were classified as permanent. Greylisting servers therefore failed **every** notification after its first attempt. Responses now use their first digit: 4xx is retryable, 5xx permanent, and unavailable codes are treated as retryable.
- **Rejected nested mask sentinels**: object fields such as `webhook.headers` must be masked or submitted as a whole. A sentinel nested inside an object cannot mean unchanged and previously became a stored literal, silently breaking authentication. Such submissions are now rejected explicitly.

#### Design notes

- **Finding transactions perform one blind INSERT for notifications**: `RecordFindingTx` writes `notification_events` in the **same transaction**, making finding persistence and notification-task creation atomic at commit. The INSERT intentionally reads no channel table and runs no user filters, so a malformed filter cannot corrupt or abort finding persistence. PostgreSQL statement errors abort the transaction, so a `SAVEPOINT` isolates this INSERT; failures are logged without preventing the finding write.
- **Delivery uses leases rather than long transactions**: `FOR UPDATE SKIP LOCKED` claims rows, sets `sending`, and advances `next_attempt_at` as a lease. Network delivery begins after commit, without database locks. After a crash, abandoned `sending` rows can be reclaimed when the lease expires, recovering without unlimited retries.
- **Rate limiting does not consume retry budgets**: the engine first computes channel token-bucket allowance, then claims that many deliveries. Claiming and discarding first would waste attempts while waiting and exhaust the three-attempt budget. Excess traffic is deferred to the next tick, never dropped.

### Test infrastructure

#### Fixed

- **Fixed cleanup ordering that permanently leaked company ICP test assets**: cleanup used `t.Cleanup`, while `defer d.Close()` closed the connection first. Cleanup then ran against a **closed connection**, and `_, _ =` discarded every error. Assets and companies remained permanently. The fixture tagged assets with fake TaskID `MAX(companies.id)+1`; collisions with other tests' task IDs caused obscure exact-asset-count failures. Connection closure now also uses `t.Cleanup`, registered first so LIFO cleanup runs before closure, and cleanup errors are reported. More than ten similar patterns remain in `db`; this change fixes the one reproduced in practice.

### Traffic

#### Added

- **Added Clear all to the traffic list**: delete every traffic record regardless of filters, including historical host directories no longer referenced by the index. Full compaction (`optimize`, `VACUUM`, `wal_checkpoint(TRUNCATE)`) returns index space to the OS and reports the amount reclaimed. Finding-linked evidence lives in a separate store and is unaffected. `VACUUM` on an empty database is inexpensive, so this also provides **the upgrade path to incremental reclamation for existing indexes**; after one clear, ordinary host deletion reclaims space automatically.

#### Fixed

- **Reclaimed disk space after traffic deletion**: SQLite deletion only added pages to the freelist, and indexes created without `auto_vacuum` never shrank. The `contentless_delete` `ex_fts` index also wrote tombstones without reclaiming postings, accumulating without merges and potentially growing after deletion. Bodies below 256KB are inline and trigram indexes are about twice their size; a measured 6MB capture occupied 16MB before and after deletion. New indexes enable `auto_vacuum=incremental`. After deletion commits, background chunks perform incremental FTS merges, `incremental_vacuum`, and `wal_checkpoint(TRUNCATE)`, reducing the same case to 104KB. Chunks release the write lock between operations to avoid blocking recording, yield immediately on shutdown, and resume remaining work after the next deletion.

> Upgrade note: `auto_vacuum` is selected when the database is created, so **existing indexes retain the old mode**, where `incremental_vacuum` is a no-op; startup logs this. FTS merging still stops tombstone growth after upgrade. Use Clear all once to reclaim existing space and convert the index to incremental mode; subsequent ordinary deletions then reclaim space automatically.

### LLM

#### Fixed

- **Fixed custom session headers on one-shot LLM calls**: header values use the context session ID, which agentcore only installs when a transcript store is attached. Goal decomposition in round 0 and cold-node compaction body calls had no store or session ID, so gateways received no header. Endpoints such as opencode zen reject missing `x-opencode-session` with 400 MissingSessionID, making round 0 fail while later planner rounds worked; compaction failures left only a difficult-to-correlate log line. Both paths now receive stable exploration-specific IDs (`exp<N>-goals` / `exp<N>-compactor`), restoring headers and allowing llmrec to attribute previously unrecorded usage to the exploration.

### Findings

#### Fixed

- **Restored scrolling in the findings By asset view**: the asset tree's outer card had only `max-height`, leaving the `height:100%` viewport unresolved because its parent height remained `auto`. Large lists overflowed or were clipped without scrolling. The native tree scroll container now owns the viewport-relative height cap through `overflow-y-auto` and `max-h`; small lists shrink to fit, while large lists scroll at the cap.

## [0.3.14] - 2026-09-24

### Task list

#### Added

- **Added a Findings column to the task list**: Critical/High/Medium/Low counts use severity colors for nonzero values, making each task's finding volume and distribution visible at a glance.

#### Changed

- **Narrowed Description and Goal columns**: long content is truncated with the full text available on hover, reducing horizontal pressure.

### Exploration graph and planning overview

#### Changed

- **Reduced `graph_overview` context and bounded its lists**: recent facts, completed intents, queued intents, cold-region summaries, and confirmed finding details now show a recent window plus total counts, with omitted data available on demand. This reduces per-round LLM context and long-task growth; cold regions in related-task overviews are bounded too.

#### Fixed

- **Removed duplicate reverse edges between digests and findings after cold-node collapse**.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.13] - 2026-09-19

### Asset intercept

#### Added

- **Added global asset blocklist rules**: exact/fuzzy domain, IP, and URL matching plus CIDR ranges, with create/read/update/delete and enable/disable controls under System -> Asset intercept. Defaults block government (`.gov` / `.gov.cn`) and education (`.edu` / `.edu.cn`) sites through fuzzy matching.
- **Integrated asset blocking into execution**: before `add_intent` or `insert_assets`, agents check target assets. Blocked intents are not issued, blocked assets are not inserted, and the agent receives asset details and the blocking reason.
- **Added per-task block/allow rules**: independent of global rules and limited to the current task. Blocking is evaluated first. A block match rejects the target; otherwise, if allow rules exist but none match, testing is disallowed. No allow rules means no allowlist enforcement. Configure rules at task creation or create/edit/delete/enable/disable them in the task Overview.
- **Task templates can preset categories and per-task block/allow rules**, applying both to the new-task form.

### Operation review

#### Fixed

- **Tightened the model-judge output contract to reduce fail-open behavior caused by truncation**: the historical comment limit changed from 500 to 120 Chinese characters, with JSON only and no preamble or code fence. This reduced `MaxTokens` truncation, parse failures, and unintended allowance through the model-failure policy.

### Task archives

#### Fixed

- **Skip symbolic links instead of failing the whole archive**: the archive format supports only regular files and directories. A symlink in the workspace previously failed the entire task archive; it is now skipped and logged, while other files archive normally. Links are never followed outside the directory tree.

### Accounts and compliance

#### Added

- **Added a Usage notice and disclaimer dialog before login**, requiring explicit agreement.

### License and dependencies

#### Changed

- **Adopted AGPL-3.0** and expanded the README license and disclaimer.
- **Upgraded norma to v0.4.1**.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.12] - 2026-09-17

### Exploration graph broadcast board

#### Added

- **Added an exploration graph broadcast board to task details** (#144): a timeline of start/goal/intent/fact/finding/hint/digest nodes with type filters, keyword search, ascending/descending order, pagination, and automatic refresh. The first newest-first page is live and refreshes each cycle. Elsewhere, only an unread count grows, preserving reading position; an N new updates / Back to latest action returns to live view. Entries group by day so long-task history remains recognizable across pages.
- **Show node IDs on every broadcast row** (#145), making it easier to cross-reference the exploration graph and locate nodes.
- **Expanded broadcast details show upstream/downstream relationships and anchored assets** (#147). Hovering related entries shows a node card with type, status, source, time, summary, and payload excerpt. Anchored assets include type badges and identifiable text. Data arrives with the page, so expanding adds no requests.
- **Broadcast search supports node IDs** (#150): alongside content/source search, bare numbers or displayed forms such as `#41` locate exact nodes.

### Intent management

#### Added

- **Intent deletion supports soft and hard modes** (#149): available, running, and paused intents can be deleted with a required reason; select the mode in the confirmation dialog.
  - **Soft deletion, the default**: set the intent to Deleted and store the reason separately, retaining the node, all outputs, and lineage.
  - **Hard deletion**: physically remove the intent and descendants supported **only by it**, cascading through output/intent chains to leaves to avoid orphaned data. Token usage remains archived under its original dates. Shared nodes referenced by other intents, goals, and task-root facts are always retained. The dialog previews the expected cascade count.

  Both modes notify the planner that the user deleted the intent, including the reason, and trigger replanning.

### Operation review

#### Added

- **Approval records can be filtered by status and decision source** (#139) to locate relevant records quickly.

#### Fixed

- **Pending approvals load independently of history pagination** (#133), remaining fully visible while browsing older records.

### Agent

#### Changed

- **Reworded the worker role for a general cybersecurity platform** (#138).

#### Fixed

- **Restored cold-node compaction**: task planners were missing their Compactor connection, so cold-digest compaction never ran. The connection is now restored.

### Chat

#### Added

- **Chat supports @ references to multiple record types and scrolling candidate pagination** (#135).

#### Fixed

- **Constrained long message bubbles to the conversation panel** (#137), preventing overflow.
- **Fixed Missing uploaded file errors when uploading before a new conversation exists**: Composer now copies `FileList` into an array before clearing the input and invoking the callback. Previously, asynchronous draft-conversation creation resumed after the live `FileList` had been cleared, omitting the file field and causing HTTP 400.

### Traffic

#### Fixed

- **The traffic-recording proxy now listens only on 127.0.0.1 by default** (#129, #130), avoiding an exposed open proxy on other interfaces.

### Web

#### Fixed

- **Restored the global header on the statically exported task-list page**.
- **Added missing linked-traffic mocks to demo finding details, fixing a blank page**.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@RuoJi6](https://github.com/RuoJi6)
- [@dingpotian](https://github.com/dingpotian)

## [0.3.11] - 2026-09-15

### Operation review

#### Added

- **Approval records link to the exact originating tool execution** (#125): Source opens the complete original session, paginates to the target, expands the command/result, and centers/highlights it while preserving surrounding messages. Manual scrolling stops automatic correction. Persisted `tool_use_id`, task mappings, and a new scoped call-ID index identify executions across ordinary conversations and segmented worker/planner/main-agent sessions. Missing, duplicate, or ambiguous associations show explicit errors rather than jumping elsewhere. Deleted sessions, archived tasks, and missing records also show clear notices.
- **Added pagination for approval records** (#115).

#### Changed

- **Reduced model-review input** (#124, #125): the LLM judge used after unmatched rules now receives versioned JSON containing the current complete tool call, explicitly selected brief context, and the local working directory. Context includes only actual current user messages from chat/task main agents. Workers no longer attach intent summaries and clear inherited parent-agent context; planners and automatically triggered sessions invent no user message. Task descriptions, goals, operation constraints, global exploration state, history, and full worker intents remain available for execution and independent session auditing but are excluded from operation review. Verdict JSON requires `decision`/`comment`, with actual operation, consequences if successful, and matched rule in the explanation. Instructions inside parameters/context cannot change review policy. The actual sent input and fingerprint are saved; older snapshots retain version labels rather than being reconstructed from current data.

#### Fixed

- **Fenced model verdicts no longer fail open silently** (#126): verdict JSON wrapped in ``` previously failed parsing and could be allowed under the failure policy. Code fences are now stripped before parsing; only a remaining parse failure invokes the configured failure policy.

### Agent

#### Added

- **Added experimental noa context compaction**, upgrading norma to v0.4.0. Disabled by default and enabled in System settings, noa replaces built-in compaction for main agents, planners, workers, and chat. Original compacted content is archived centrally under `<workDir>/noa/<session-ID>/`, using globally unique session directories rather than scattered task directories. Integration failures fall back to built-in compaction without interrupting tasks. The switch is read once per run and affects only subsequent runs.

#### Fixed

- **Exploration graph tools reject calls without task context instead of panicking on nil stores**, returning a clear error.

### MCP

#### Added

- **Added legacy SSE MCP transport support** (#117) for servers that expose only the older protocol.

### Traffic

#### Fixed

- **Traffic search matches hosts with port awareness** (#114): `traffic_search` includes the port when matching hosts, preventing records from different ports on the same host from mixing.
- **A failed `traffic_search` description migration no longer interrupts subsequent reporter migrations**; each failure is isolated.

### Web

#### Changed

- **Added separators between task retest finding options** (#122) for clearer selection.

#### Fixed

- **Fixed complete demo task-detail crashes**: the mock Sessions tab requested `GET /api/tasks/<id>/side-questions`, but the missing mock route fell through to a collection heuristic that returned `[]`. This left `data.items` undefined, and the side-question hook's `merge()` threw `TypeError: t is not iterable`. Because it occurred inside a `setItems` updater, React rethrew during rendering beyond the caller's catch, and the page error boundary showed "This page couldn't load". The mock now explicitly returns empty side-question history, while `sideAPI.history` defensively normalizes non-array `items` to `[]`.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@RuoJi6](https://github.com/RuoJi6)

## [0.3.10] - 2026-09-13

### Networking

#### Added

- **Added official DeepSeek web search**, reusing the active LLM configuration. Unlike the other three sources, DeepSeek has no directly callable search API: search runs server-side through the Anthropic-compatible `web_search_20250305` tool. It therefore **requires an official DeepSeek endpoint with the anthropic protocol**; OpenAI-protocol endpoints reject server tools. Each search **uses an additional model call**, **bypasses the search egress proxy**, and **is not traffic-recorded**. Results contain **titles and links only**; fetch page contents with WebFetch. Settings explain these limitations without blocking configuration; operators verify compatibility themselves with Test search.

### Agent

#### Added

- **Workers can review traces across work items**: `search_all_worker_traces` finds keyword-matching steps across the task without requiring an intent_id first. `get_worker_trace` lists a selected work item's steps, searches within them, and retrieves complete contents by step_id. This reuses observations not yet recorded as facts and avoids duplicate work.
- **Workers now receive `node_detail`**, allowing complete node lookup after obtaining intent_id/node IDs from trace-review tools.
- **Planning rounds triggered by `add_hint` now announce it explicitly**: previously hints appeared only inside the overview. Each call now records one trigger describing N new human strategic hints, treating a batch as one event, and shows the planner the actual content and why the round began.

#### Changed

- **Relaxed global-overview guidance** to encourage diverse exploration and timely reporting of cross-intent clues rather than premature narrowing.
- **Simplified worker boundaries**: an initial obstacle does not establish exhaustion; try reasonable bypasses within the current intent before concluding.
- Removed per-asset `related` from `insert_assets`. It only controlled task-scope insertion and was never persisted, so re-registration discarded the judgment and the UI could not identify assets considered unrelated. It imposed an extra model decision with no lasting record.
- **`task_scope` no longer depends on the asset-coverage switch**: automatic scope insertion through `insert_assets` (`source='auto'`) always runs, and `add_task_scope` remains available to planners, task main agents, and goal-decomposition agents. Scope defines authorization and asset-query filtering; coverage only decides whether to use it as the metric denominator. Previously, disabling coverage disabled both `auto` and `agent` writes, leaving only manual UI entries. `list_untested_assets` remains hidden when coverage is off because it is specifically a coverage view.

#### Fixed

- **Prevented assets leaking across task scopes** (#59): `list_assets` previously hardcoded task ID 0 and queried the entire shared inventory, while models lacked a way to express scope filters. They could target other tasks' assets, especially IPs. It now returns only assets owned by the scope of **the current task and directly related tasks**, using **ownership**, not literal matching: a root domain includes its subdomains/services/endpoints, and a CIDR includes its hosts/services. Direct ID lookup also rejects out-of-scope assets. Non-task contexts such as Auto/pentest still use the global inventory. IP-direct hosts such as `http://1.2.3.4/api` now correctly match CIDR ownership. The UI Tested assets view, filtered by producing task, is unchanged.
- **Restored worker cross-work review tools removed by an old migration**: after `search_all_worker_traces`, `get_worker_trace`, and `node_detail` joined the defaults, an older tool-narrowing migration unbound them at startup. They are removed from that list, with a one-time rebind for databases that already ran it.
- **Fixed intermittent `/btw` persistence failures** by removing JSONB-unsupported NUL (`\u0000`) escapes from side-question checkpoints before saving.

### Triggers

#### Fixed

- **Deduplicated task descriptions/goals in merged trigger conversations**, preventing repeated long goals from inflating multi-task trigger messages.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.9] - 2026-09-11

### Agent

#### Added

- Task main agents support **multiple conversations**, each with independent creation, switching, context reset, and interaction.
- Added persistent `/btw` **side questions** for follow-ups without interrupting the main flow; history survives restart.
- `spawn_task` accepts `source_task_ids`, allowing new tasks to **inherit assets and findings read-only**.

#### Changed

- Disabled worker cross-engagement memory and narrowed default tools: context review and cross-work retrospectives belong to planning; workers execute and write back one intent.
- Simplified worker defaults and `insert_assets` / `list_assets` descriptions; removed `terminated` and `worker_name` from `get_worker_output`.

#### Fixed

- Fixed work ending prematurely after **idle turns**: models sometimes emit only reasoning, with no text or tool call. The harness sees natural completion (`end_turn` without `tool_use`) and finishes as `completed` with an empty summary despite unfinished work. None of the five retry layers applies: this is not a stream error, the circuit breaker treats `err == nil` as success, intent reruns require `model_error`, and SDK empty-response detection counts reasoning deltas as yielded events. The work Stop hook now detects these turns and injects continuation guidance so execution proceeds using the reasoning already produced. Identical replay is deliberately avoided because prompt/context-driven idle turns are often stable, not random; replay would merely repeat the reasoning.
- Continuation uses the **empty-response retry count** on LLM -> Retry and backoff: both address completed model calls without substantive output, through different criteria and mechanisms. The default is 2; `-1` disables it and restores ending on idle turns. The cap is **per intent in total**, not consecutive: the harness already permits only one nudge for consecutive idle turns, resetting that allowance only after a real tool round. The cap prevents tool -> idle -> nudge -> tool -> idle loops from consuming the entire intent budget. Logs identify the worker/intent, idle turn, and continuation count `(n/N)`. See section 1.1 of the historical LLM retry design document, which is not included in this checkout.
- Fixed `/btw` context budgeting and input layout for long conversations, with fallback request-ID generation outside secure HTTPS contexts.

### Traffic

#### Added

- Findings can link **multiple traffic evidence records**, with ordering, notes, and roles for requests/responses/supporting evidence.
- Reporter agents **automatically link** relevant traffic before writing reports.
- Added an agent **traffic-binding switch** and completed evidence handoff between agents.

### Tasks

#### Added

- Added **session-level finding retests** in independent agent conversations, with execution state in lists and details.

### Intercept

#### Added

- Approval records include **details and execution audits**: tool-request context, initial model/rule judgment, execution output, and parameter fingerprints.

### Assets

#### Added

- Task test assets support **DSL search**.

### LLM

#### Fixed

- Fixed connection tests missing custom session headers, which caused opencode zen HTTP 400 responses.

### Networking

#### Added

- MCP HTTP transport supports **skipping TLS certificate verification** for self-signed services.

### UI

#### Added

- Conversations group by agent, with collapse/expand and independent pinning; chat can filter by agent.
- The exploration graph renders digest nodes and collapses their members.

#### Fixed

- Fixed blank pages caused by unsynchronized login credentials.
- Intercept notices explicitly identify **platform controls**, avoiding confusion with target-side defenses.

### Deployment and updates

#### Added

- Added **in-app updates** through a Version and updates card in System settings and a top-bar notification. The workflow downloads the release, verifies `SHA256SUMS`, smoke-tests, stages, exits, and lets the supervisor restart and replace the binary; the page refreshes automatically.
- Added `start.sh` / `start.bat` as supported supervisor entry points, included in release archives and Docker images; `install.sh` remains unchanged. They restart according to exit codes and forward SIGTERM to artex. Verification and replacement stay in Go so scripts remain simple.
- Automatic fallback discards failed verification/smoke-test candidates and keeps the current version. Three consecutive startup failures roll back the new version. Manual rollback is also available in settings; database schema changes are not reverted.
- Updates accept only GitHub domains and require HTTPS; the release source is not configurable. Development builds cannot update. GitHub results are cached for 30 minutes to avoid exhausting API quotas through top-bar checks.

#### Known limitations

- Docker updates replace only the program, not the image/toolchain. Recreating containers restores the bundled version; use `docker compose pull artex` when needed.
- Release `skills/` contents are not synchronized, so newly bundled skills do not appear automatically.
- Updates restart the process and interrupt running tasks.

### Dependencies

#### Changed

- Upgraded norma to v0.3.7.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@RuoJi6](https://github.com/RuoJi6)

## [0.3.8] - 2026-09-09

### LLM

#### Added

- Added an LLM Retry and backoff tab configuring **counts and intervals** for five nested layers: SDK connection retries before streaming (resets/timeouts/429/5xx), SDK empty-response retries after clean empty completion (openai only), same-provider safe-window replay before any caller-visible output, circuit-breaker cooldown after consecutive failures, and full intent reruns when workers end with `model_error`. Outer layers apply only after inner ones are exhausted. Each layer uses consistent controls: blank retains default counts/exponential backoff; a count overrides attempts; an interval replaces exponential with fixed delay; `-1` disables the layer. The first three follow endpoints and allow per-field profile overrides of global defaults; overriding only the interval still inherits the global count. Circuit breakers and intent reruns are process-wide/global only. Saving applies immediately without restart. Blank settings preserve existing behavior byte for byte on upgrade, using zero-default columns and absent settings keys. Connection tests intentionally exclude these settings because their hard 30-second timeout would otherwise misclassify usable endpoints. The historical LLM retry design document is not included in this checkout.
- Added per-profile **custom session headers** through `session_header_key`. When nonempty, every request includes that HTTP header with the run's session ID, such as `conv-<id>` for chat or `exp<x>-worker-i<intent>` for workers, supporting gateway prompt caching and sticky routing. IDs are injected from request context without modifying norma, so shared providers can use distinct session values. Existing databases migrate automatically, and the LLM profile dialog includes the field.

#### Fixed

- Fixed `23502` when saving empty session-header fields: the column is `NOT NULL DEFAULT ''`, but `NULLIF($n,'')` incorrectly converted blank input to NULL. Values now pass through with empty-string semantics.

### Agent

#### Added

- Wall-clock timeouts now **wrap up in place** using norma v0.3.6. At `MaxDuration`, the harness interrupts active tools and continues its configured wrap-up rounds on a live context, writing identified results and a summary before ending as timeout. It no longer relies on worker/planner external `maxDur+90s` contexts that killed stalled runs as `aborted_tools`. Chat gains the same harness behavior; mainagent has no `MaxDuration` and is unaffected.
- Added a stalled-task fallback: on heartbeat/no-change wakes with no open or running intents anywhere in the graph, the planner receives an explicit stall notice that no workers or queued directions remain. That round must produce one or more distinct new intents, never zero.

#### Changed

- Reworked worker prompts: intent, startup instructions, and raw anchored-asset JSON now live in the system prompt, rebuilt every round and immune to compaction. Resume no longer depends on retaining the transcript's first message. The initial user message contains only global overview context, which may degrade or be stale. Per-intent system content sacrifices cross-intent cache reuse deliberately to preserve the intent.
- Simplified planner/worker defaults and corrected practical issues: planners distinguish `recent_done` states, inspect traces before deciding about blocked/exhausted work, and neither assume a dead end nor rerun blindly. Negative conclusions are observations or uncertainty, requiring evidence review. Unmet goals with no open/running intent impose a hard requirement to issue work, with depth prioritized over coverage. Workers record negative observations with tentative interpretations, leaving decisions to the planner, and put cross-intent clues in fact summaries rather than pursuing them. Reseeding appends and activates a new default version while retaining custom/old versions in rollback history.
- Narrowed worker default tools and asset-write guidance. Workers execute/write back one intent; planning owns context review and cross-work retrospectives. Removed `list_facts`, `node_detail`, `list_companies`, `search_all_worker_traces`, `list_worker_traces`, and `get_worker_trace`; retained `list_findings` for deduplication, `add_finding` / `record_fact`, and `insert_assets` / `list_assets`. Removed stale `type=tech` / `on_url` / `props` guidance conflicting with the `insert_assets` schema. A one-time migration removes worker bindings while preserving planner/main bindings.

### Exploration graph

#### Added

- **Added exploration graph cold-node compaction (cold-digest)**: old inactive intents/facts collapse into digest nodes in `graph_overview`; originals remain permanently retrievable by ID, preserving lossless storage and reversible presentation. Hot/cold classification uses reverse reachability, with any active branch making a node hot, R=6-round debounce, and connected-component grouping. Background minor compaction folds uncovered cold regions; major compaction rereads originals and merges fragments. Activity rechecks and cooldown mutual exclusion keep compaction off the hot path and prevent overwriting revived nodes. Overviews expose `cold_digests` and asset-indexed `cold_index`; `expand_digest` / `expand_index` restore details. Related/inherited-task overviews reuse their own folded views, and `expand_digest` supports read-only cross-task expansion. Expansion tools are available only to planners and main agents, not workers. Existing databases migrate automatically.
- `graph_overview` now returns the complete `finding_list`: findings are high-value and usually few per task, so planners/workers see all confirmed findings each round without another `list_findings` call, unlike the recent-only fact window. Entries use `{id, summary, evidence?, from_intent?, assets?}`, with readable URLs/domains/ip:port values instead of bare asset IDs.

#### Changed

- Removed flat `hosts` lists from `graph_overview` coverage, which could add 500 strings per round with little planning value. Retain `host_count` and query specific hosts through `list_assets`. Added top-level `done_intents_total` beside the at-most-15-entry `recent_done_intents`, so planners know when deduplication history was truncated.

### Tools

#### Changed

- Moved the default `add_company_scope` binding from workers to planners: company-scope definition belongs to planning/main control/Auto, while workers explore. Fresh seeds bind mainagent/planner/auto, with a one-time upgrade migration.
- Planners now receive `list_assets` by default alongside `list_untested_assets`, for global DSL search and in-scope untested assets respectively. A one-time backfill preserves subsequent user unbindings.

### Tasks

#### Added

- Added Running workers to the task list, counting intent nodes with `state='running'` exactly as task Overview does and showing 0 when none run.

### Networking

#### Added

- Added a **global egress proxy** for target traffic, supporting http/https/socks5 and `user:pass`. With capture enabled it is upstream of the MITM recorder; intercepted and passthrough traffic are recorded and routed through it without exposing the origin IP. Without capture it is injected into agent Bash environments and WebFetch; `proxyEnv` adds `ALL_PROXY` for socks5. Configuration uses settings KV without migration, with a Global proxy card independent of search and LLM proxies.

### UI

#### Fixed

- Corrected intent-status meanings: `exhausted` now means Budget exhausted, reflecting step/time limits and partial output rather than a fully explored direction; `blocked` means Execution error, reflecting exhausted model/API/network retries rather than target/WAF blocking. Added the missing `stopped` label, Stopped, for manually stopped work.

### Dependencies

#### Changed

- Upgraded norma to v0.3.4, adding MCP output truncation and disk capture (`651b961`); subsequent v0.3.6 supports wall-clock wrap-up in place.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.7] - 2026-08-31

### LLM

#### Added

- Added a per-model Output limit and selectable request field. The limit caps generated tokens per reply and is sent on each request; 0, the default, omits it and uses the server default. This differs from Context window, the total model capacity used locally for compaction thresholds and not sent in requests. Field selection applies only to `openai` Chat Completions: blank sends `max_tokens`, accepted by most compatible gateways. Official OpenAI reasoning models, including o-series/GPT-5, require `max_completion_tokens` and reject `max_tokens` with `unsupported_parameter`, so select it explicitly. Anthropic fixes the field to `max_tokens` and Responses API to `max_output_tokens`; other formats disable and clear the selector. The limit is now wired through planner, worker, chat, main agent, and goal decomposition, which previously omitted it for OpenAI or used Anthropic's SDK default of 8192. Like streaming, values resolve per round and take effect after failover on the next round. Upgrades preserve existing behavior through zero and empty-name column defaults.
- Connection tests now log each HTTP attempt's status and raw gateway response, capped at 4K, to diagnose 401s, quota errors, empty frames, and HTML responses beyond the UI's condensed ok/error display. Logging is independent of the LLM recording switch.

#### Changed

- Task LLM chains are editable in every state, not only running/paused/exhausted. Main-agent conversations still use the chain after done/failed/timeout, so broken models previously prevented further interaction. Removed terminal-state restrictions from both HTTP and DB paths and enabled dialog editing/saving. Saving terminal tasks no longer reopens quota-blocked intents, which would become open with no workers and cease to qualify for rerun. Use Rerun intent or Add goal to resume execution and return the task to running.

### Agent

#### Changed

- Messaging a running worker now reuses pause/resume and transcript continuation, matching main-agent chat. The former custom intervention-persistence protocol affected scheduler barriers, recovery, and more than ten activity filters. Messages now enter the next round, with each intent running immediately in its own goroutine outside the three-slot worker pool. The input remains, Continue directly returns, and sending uses SSE. No schema change. Tradeoffs: messages remain in memory without crash recovery, and infrequent interventions may temporarily exceed task worker concurrency by one.

### Tasks

#### Fixed

- Fixed OOM crashes when archiving large tasks by streaming snapshots instead of loading entire tasks into memory. Also closed three cold-archive recovery gaps: modern traffic exists only in SQLite, so crashes between PostgreSQL and SQLite commits could strand archived traffic in active storage. A staging journal is now written with or without historical directories, and missing `journal.json` is treated as disposable.
- Fixed sluggish conversation/page loading with many tasks. Optimized task-list, task-context, and exploration-history queries, while reducing duplicate frontend requests on the dashboard, chat, and task details.

### Assets

#### Changed

- Removed the 256-rule limit on company asset scope. Companies listing individual IPs/domains frequently reached it and had to split artificially, although rule count itself did not inherently slow scope matching. Removed the backend, frontend, and demo-mock caps together. Each rule remains limited to 1024 characters, with the 2 MiB request-body cap retained as a safeguard, roughly 40,000-50,000 rules.

### Skills

#### Fixed

- Fixed `Upload failed: zip: unsupported compression` for skill archives. Go's standard library only supports Store/Deflate, so bzip2 and Zstandard archives produced by nondefault compressor settings could not open. Added pure-Go decompressors without external system dependencies. Unsupported Deflate64/LZMA/XZ/PPMd and encrypted archives now fail before extraction with a readable message naming the file, compression method, and repacking instructions instead of exposing a low-level error.
- Fixed rejection of Chinese skill filenames. The previous `[A-Za-z0-9-_./]` ASCII allowlist rejected an entire archive containing any localized filename. Unicode-based rejection now permits filenames in all languages and spaces, while still blocking control characters, invalid UTF-8, zero-width/bidirectional controls including RLO disguises, `\ % # ? * : " < > |`, `..`, absolute paths, and empty path segments. Directory-traversal protection remains unchanged. Skill names also accept non-ASCII letters; ASCII remains restricted to lowercase letters, digits, and hyphens per agentskills.io, without spaces, dots, or path separators.
- Fixed garbled or rejected filenames in Chinese Windows ZIP archives that encode names as GBK without setting the UTF-8 flag. Names now use GBK fallback decoding before validation. Quoted frontmatter names, including Chinese skill names, are parsed correctly too.

### UI

#### Added

- Added the default **All findings** flat view, switchable with By task through header tabs. Grouping required expanding every task just to scan findings across tasks; the new table paginates at 10/20/50/100 rows. It supports the same selection/export, inline evidence/report expansion, name/class/severity edits, status changes, deeper exploration, and deletion. Both views share statistics and filters, preserving them across switches and in local storage alongside the selected view. Only the active view polls, while inline edits update both caches to prevent stale data on switching.
- Added the **By asset** findings view: a left tree of company -> root domain/IP -> subdomain -> service -> endpoint, with companies shown only when ownership exists, and a right table covering the selected subtree. It shares tables and filters with the other views. Only assets with findings appear, with ancestor chains filled as needed even when findings attach only to deep endpoints. Counts aggregate/deduplicate findings over each subtree. Deleted or absent asset links enter Unlinked assets. Unlike the other views, it does not poll: queries run on entry, filter changes, finding edits, or explicit tree refresh, avoiding unnecessary five-second navigation-tree rebuilds. Labels show only the increment from the parent, such as a subdomain without its root suffix, `https :443`, or endpoint path; full values appear in tooltips/breadcrumbs. Excessive node counts omit whole endpoint/service levels with a notice while retaining counts in ancestors.

#### Fixed

- Fixed cramped mobile conversation transcripts by collapsing the conversation list on narrow screens.

### Dependencies

#### Changed

- Upgraded norma v0.3.2 -> v0.3.3, adding `Config.MaxTokensField` so OpenAI Chat Completions can send `max_completion_tokens`, required by reasoning models. The keys are mutually exclusive; `max_tokens` remains the default.
- Upgraded `golang.org/x/mod` v0.37.0 -> v0.40.0, resolving two dependency security alerts.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@neouks](https://github.com/neouks)
- [@begininvoke](https://github.com/begininvoke)

## [0.3.6] - 2026-08-27

### LLM

#### Fixed

- Fixed saved non-streaming profiles reopening as streaming (#69). The list DTO omitted `streaming`, so the frontend received `undefined` and fell back to true despite correct database writes. The field is restored without `omitempty`, ensuring false is returned too.
- Connection tests now require an actual model reply and use the profile's configured streaming mode (#65). Previously HTTP success passed even when reasoning exhausted the budget, safety filtering removed content, or compatibility layers lost `content`; tests also always streamed, testing a different path for non-streaming profiles. Empty replies now fail, successful messages show the reply, and the current streaming switch is included so test success better reflects usable conversations.

### Agent

#### Changed

- Added pagination and keyword filtering to `list_facts` to prevent oversized contexts (#74). It returns the latest 20 by default, supports `limit` up to 100, `before` cursors, and summary keyword `q`, and returns `{facts, total, has_more, next_before}`. Long summaries are character-truncated; full content remains in `node_detail`. Worker/planner prompts use pagination semantics. A one-time migration refreshes tool schemas in old catalogs because `SeedTool` inserts only once, otherwise the management UI would show no parameters.

### Tools

#### Added

- Added tool-call statistics to Tool execution (#72). Statistics opens a dialog with count, share, and failure count per tool, descending by count. It uses the list's task/keyword filters across the whole result set, not just the current page, and loads only when opened.

### Dependencies

#### Changed

- Upgraded norma v0.3.1 -> v0.3.2: restored silently discarded OpenAI reasoning/refusal content, including Responses `reasoning_text`; deduplicated three reasoning-field aliases; added bounded empty-response retries for zero content blocks; and fixed gateway 400s from compaction splitting tool pairs, including orphaned pairs already persisted in transcripts.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.5] - 2026-08-25

### LLM

#### Added

- Added per-profile streaming/non-streaming selection, defaulting to streaming SSE. Disabling sends `stream:false` and receives complete JSON at once, avoiding broken gateway SSE implementations with empty frames or lost reasoning at the cost of live progress/token updates. Worker/planner/mainagent/chat/goals resolve the active setting dynamically. Provider wrappers in `llmpool`, `llmrec`, and task runtime support non-streaming. `llm_profiles.streaming` and idempotent ALTER migration default to true, preserving old profiles.
- Added `openai-responses` profiles alongside Chat Completions and Anthropic, calling `POST /v1/responses`, normalizing `BaseURL`, and defaulting to `gpt-5`. Updated and idempotently migrated the `llm_profiles.format` constraint, and added OpenAI (Responses API) to the format selector. Upgraded norma to v0.3.1, including its `reasoning_content` forwarding fix.
- Added raw HTTP request/response recording at the transport layer, preserving tool schemas, `tool_use` blocks, and original SSE frames absent from normalized views. Every norma internal retry is retained separately. Added/migrated `llm_records.raw_request` and `raw_response`. Recording details now offer a Raw view and request/response copy buttons, with an `execCommand` fallback outside secure contexts.

### Conversations

#### Changed

- Conversation multi-selection now uses a Select multiple mode instead of permanent row checkboxes. The header shows a total and mode button; entering enables checkboxes, Select all, and bulk deletion. Done exits and clears selection. Fully successful bulk deletion exits automatically; failures keep selection for retry. Single-item rename/pin/delete remain in each row's menu.

### UI

#### Changed

- Improved task management, conversation actions, and traffic viewing (#57): persistent task-column ordering, direct rename icons instead of menus, and refined sheet and traffic interactions.

#### Fixed

- Worker asset labels now show only domains and IPs and handle empty labels correctly.

### Agent

#### Added

- Added **quantitative acceptance checks** to planner goal decisions. Before `prove_goal`, goals such as X% asset coverage, N flags, or a required privilege must be checked against actual `graph_overview` measurements such as `coverage.pct` and finding counts. Unmet thresholds prohibit proof and require intents to close the gap; mostly complete is insufficient. This fixes goals requiring 100% coverage being marked achieved at 40%.

#### Changed

- Rewrote when planners may issue zero intents. The former description as the most common and important principle encouraged premature stopping with unmet goals and untested scope. Zero is appropriate only when all considered directions are already covered by open/running/recent_done intents, or the next step depends on output from running work and must wait for completion and the next graph-change wake. Conversely, uncovered directions independent of running work, or unmet goals with untested surfaces, must not stop merely because zero-intent rounds are common.
- Strengthened evidence requirements for worker negative conclusions such as not injectable, port closed, or no login entry, which could make planners abandon entire directions. Exhaust reasonable in-intent encoding/parameter/path/method variations first. Incomplete attempts or weak evidence require `confidence=inferred`, avoiding hasty `observed` negatives that prematurely close routes, especially early mistakes that are difficult to recover from.
- One-time, settings-flag-guarded `reseedPlannerPrompt` / `reseedWorkerPrompt` migrations append and activate these defaults as new versions. Older versions remain in history so customized prompts can be restored.

### Operations

#### Added

- Added `reset-password.sh` for the administrator username `ARTEX`, supporting local/docker deployments. Connection details can be explicit or read from `--dsn`, `$ARTEX_PG_DSN`, or `config.json`. It generates a backend-compatible bcrypt hash through database `pgcrypto` and updates `settings.auth.password_hash`, without a service restart. Passwords pass through environment variables rather than argv and are escaped against injection.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@neouks](https://github.com/neouks)

## [0.3.4] - 2026-08-24

### UI

#### Added

- Selected tasks can change categories in bulk, including Uncategorized to remove the existing category. The batch writes in one transaction; only tasks deleted after selection can fail.

#### Changed

- New-task category selection is now a searchable input. Typing filters existing categories; Enter or Create immediately creates and selects an unknown name. The selection appears as a removable tag; only one category is permitted.

### Conversations

#### Fixed

- Fixed message submission being blocked as unconfigured when a conversation had an LLM profile but none was globally active. Preflight checked only the global profile while execution preferred the selected conversation profile. Both now share resolution: conversation/agent binding first, global fallback second.
- Unavailable-chat notices distinguish no profiles, suggesting Add configuration, from existing inactive profiles, suggesting activation or assigning one to the conversation, instead of always saying unconfigured.

### Agent

#### Added

- For tasks whose goals are all met, main-agent `add_intent` work can be claimed directly by workers and stops after completion. The planner does not run without open goals, preventing it from reconfirming completion and canceling newly issued work. Once the frontier drains, the task returns to Completed. Before issuing an intent, the main agent asks whether its content should become a formal goal; adding one restores normal autonomous planning.

#### Changed

- `get_worker_trace` / `get_task_worker_trace` no longer fail when `step_ids` exceeds five. They return the first five complete steps plus `returned_step_ids`, `omitted_step_ids`, and `notice`, indicating what remains and that no further fetch is needed if sufficient. Duplicate/invalid IDs are removed before counting.

#### Fixed

- Task lists now sort by descending task ID, newest first, instead of creation time. Simultaneous timestamps produced unstable ordering that, combined with ten-second polling and randomized map iteration, repeatedly swapped rows.

### Assets

#### Fixed

- Fixed company-ownership recomputation failing on one dirty asset row. Hostnames in `assets.ip` caused `a.ip::inet` to raise `22P02`, rolling back company-scope creation/editing and company deletion. Safe `try_inet()` now skips invalid values instead of aborting the statement.
- Invalid `ip` values are no longer skipped silently. Saving company scope reports the count, IDs, and values of assets with invalid IPs and explains they cannot match IP/CIDR ownership. Server logs carry the same details for paths without frontend responses, including company deletion, ScopeSentry sync, and agent writes.
- Task-scope IP/CIDR matching also uses `try_inet()`, replacing a character-only regex guard that admitted hex-letter hostnames such as `abc.def` and caused the same error.

#### Changed

- The `ip` field of `ip`, `service`, and `endpoint` assets no longer accepts hostnames. `insert_assets` and asset APIs return per-entry errors with `index` and corrective guidance: use `type=subdomain` with `domain`, or resolve A/AAAA records first. Agents can correct and resubmit; other assets in the batch are inserted normally.

### Contributors

- [@neouks](https://github.com/neouks)

## [0.3.3] - 2026-08-23

### Worker

#### Added

- Running workers support individual pause, resume, and cancellation. Pause retains the intent, facts, and findings; cancellation transactionally removes the current intent and direct outputs after execution exits.
- Added the `paused` intent state, execution barriers, and named termination causes to prevent late blackboard writes after pause, cancellation, or task deletion.
- Added optional task concurrency limits, with creation, resume, deeper finding exploration, and queue replenishment sharing a persistent FIFO admission path.

#### Changed

- Increased the default worker wall-clock budget from 600 to 1200 seconds; revived timed-out tasks restart the clock on their next actual execution.
- Task badges now reflect real worker/planner/main-agent execution, fixing Idle displays while workers run.
- Worker conversations retain individual controls and current-session token totals; model names move to the right of the current conversation title.

#### Removed

- Removed worker multi-select, Select all, and bulk pause/resume UI, together with bulk-control APIs and mock contracts.
- Removed token badges/tooltips from worker rows while retaining the complete usage ledger and task aggregation APIs.

### LLM

#### Added

- Added ordered per-task LLM chains that switch to the next profile on explicit provider quota exhaustion and persist current profile, exhaustion state, and error summary.
- Running/paused tasks can edit the full chain, reorder profiles, or switch the active profile manually, effective on the next LLM call.
- Automatic/manual switches and complete chain exhaustion write structured system activity and produce deduplicated task-feed notices.
- Added a task-role model-resolution API, consistently identifying the profile/model used by the next main-agent, planner, or worker call.
- Added an optional global LLM Pool with call order, designated-model fallback, health state, cooldown recovery, and manual reset.
- Added an always-on LLM usage ledger aggregating input, output, and cache tokens by task, conversation, model, and profile.

#### Changed

- Goal decomposition, planners, workers, and main agents share task-level LLM runtime; resolution precedence is described below.
- Failover responds only to explicit quota, balance, or billing errors. Ordinary rate-limit, authentication, network, server, and context errors do not trigger incorrect switching.
- Reworked LLM configuration as profile cards with a right-side editing drawer, including pool order, priorities, exclusions, health, and recovery controls.
- Chain-exhaustion messages wrap on narrow screens. The current model is shown as a text label with a full tooltip beside the conversation title rather than an icon in the list.
- Role model resolution is now Agent binding -> Task chain -> Global. Explicitly bound roles always use their assigned model; unbound roles use the task chain, falling back globally when the chain is empty.
- An empty egress-proxy field means direct access, without fallback to `HTTP_PROXY`/`HTTPS_PROXY`; explicit `ARTEX_LLM_PROXY` is unchanged. Authenticated `socks5://user:pass@host:port` proxies are supported.

#### Fixed

- Transient streaming failures before any output is delivered now retry safely with backoff on the same provider, reducing runs ending after one or two tools without summaries as `model_error`. Quota exhaustion, oversized context, and deterministic 4xx errors do not retry; failover and compaction recovery handle their respective cases.

#### Removed

- Removed LLM icons from conversation lists to avoid confusion with worker-state icons.

### UI

#### Added

- Added global task-category creation, renaming, deletion, and filtering, with category selection during task creation.
- Added global task-template CRUD and a right-side management drawer. New tasks can load preset descriptions/goals or save current content as a template.
- New tasks can link multiple source tasks and company asset scopes. One multiline scope input recognizes domains, URLs, IPs, CIDRs, ICP filings, and company keywords automatically.
- Task test assets can be added/deleted live, retaining manual/company/inherited/agent-discovered provenance. Worker conversation titles show current test assets and source summaries.
- Findings group by task with independent per-group pagination, and support descriptions that create high-priority deeper-exploration intents.
- Conversations support rename, pin/unpin, and deletion; task lists support bulk pause/resume on the current page.
- Traffic and tool-execution details use right-side drawers. HTTP messages include `Host` and highlighting for request lines, status codes, headers, JSON, and markup bodies.
- Added Details and Pause/Resume buttons to task actions. Configure conversation send shortcuts, such as `Enter` or `Cmd+Enter`, in System settings; web search adds a proxy input.
- Conversation-title badges show the LLM profile name, with model ID in the hover tooltip. Tasks support optional names, a name column, and search, falling back to descriptions when unnamed.
- Skill pages show usage counts, last-used time, and missing-dependency counts to diagnose inactive or uninstalled skills.

#### Changed

- Task titles are focusable detail links. Task/company assets, finding task groups, and grouped findings use real server pagination, stable sorting, and exact totals.
- Company creation uses a right-side drawer with unified multiline scope input, live recognition, validation, and type previews.
- Main-agent input grows automatically, sends with `Enter`, inserts newlines with `Shift+Enter`, and avoids accidental sends during Chinese IME composition.
- Agent previews, task reports, and related details share Markdown rendering. Fixed deletion confirmations, long errors, and mobile drawer widths consistently.
- Task-template selectors display/search names but submit IDs. Restored original font sizes and standardized the displayed application version as `0.3.3`.
- Task/current-conversation token summaries highlight only input, cache-read, and output numbers; Send uses a simpler upward-arrow icon.
- Task Overview shows company names instead of IDs in test scope.

#### Fixed

- Ordinary text containing dots is no longer misclassified as an ICP filing number.
- Fixed variable catalogs colliding with global runtime variables such as `{{.Now}}`, which produced duplicate keys in agent-editor variable lists.

#### Removed

- Removed separate Enter buttons from task cards; open details through the task title.
- Removed the dashboard New task button, company-creation Logo URL input, and finding-count badges from the findings header and task groups.
- Reverted the global 10% font-size increase and removed worker selectors, model icons, and per-row token statistics from conversation lists.

### Agent

#### Added

- New tasks can link multiple existing tasks and inherit their direct sources' goals, facts, findings, completed intents, asset scopes, and blackboard context live and read-only.
- Blackboard read tools query source-task nodes, facts, findings, and execution traces on demand. Inherited nodes carry provenance, and every write tool rejects changes to them.
- Company-scope tools support domains, URLs, IPs, CIDRs, ICP filings, and company keywords. Keywords guide agent scope only and do not assign assets automatically.
- Deeper finding exploration creates a high-priority human-issued worker intent in the original task, with asset anchors and a `derived_from` edge.
- Added Goal management to Overview, with manual view/create/edit/delete. Creation/editing notifies the planner and revives tasks; deletion hard-removes the goal and cascades edges/anchors without reviving the task.
- Main agents receive `steer_work` by default to inject live corrections into running workers without interruption or lost progress, while validating current-task intent ownership.

#### Changed

- Source-task intents never enter the new task's frontier. Each new task retains its own exploration, execution queue, workspace, and conversation history.
- Deleting running intents no longer destroys data: it stops them as `stopped`, requires a reason, attaches that reason as a fact and in payload, and notifies the planner through `cancelled`, retaining intent content and reason.
- Main-agent conversations use server activity streams, restore input after send failures, scroll to the bottom on open, and keep the last reply visible after lazy detail loading.
- Pausing a task stops current main-agent, planner, and worker calls, but users can still initiate new main-agent orchestration conversations while paused.
- Cancellation, shutdown, and stream interruption consistently preserve the actual cause, generated content, rounds, elapsed time, tokens, and unanswered tool calls.

#### Removed

- Removed optimistic client-side main-agent echoes to prevent duplicate messages and cross-conversation streaming during pauses, failures, or concurrent activity.

### Constraints

#### Added

- Goal decomposition extracts allow/deny operation constraints before goals; main agents can add constraints at runtime. Constraints enter planner/worker system prompts at highest priority, with diversity and broader exploration explicitly subordinate to them.
- Added Constraint management to Overview and create/edit/delete APIs. Planner and worker injection have separate switches, both enabled by default and reread each round. `task_constraints` uses `CREATE TABLE IF NOT EXISTS` for automatic existing-database migration.

### Intercept

#### Added

- Command intercept adds model fallback after regex/string rules: when none match, the model decides `ALLOW`/`ASK`/`DENY` semantically, with configurable failure and approval-timeout actions. The page separates Intercept rules and Model configuration tabs. Model decisions include a legacy localized model prefix and reason, replaced by `[model]` in the English migration.

#### Fixed

- Hardened judge-output parsing so valid `DENY` decisions are not mistaken for allowance.

### Traffic

#### Changed

- Traffic recording now stores complete exchanges in SQLite, with large bodies in hash-deduplicated blob storage and trigram full-text indexes for arbitrary substrings and Chinese search. Deletion becomes one SQL transaction, falling from hours to milliseconds without pausing capture. Added streaming downloads for large bodies and full-text `traffic_search` parameters. Legacy file-tree data remains readable, searchable, and deletable without migration.

### Tasks and assets

#### Added

- Added an Asset coverage switch during task creation, defaulting on. Historically, disabling it hid coverage calculations/displays, left only assets in the graph, stopped automatic `task_scope` accumulation, and removed related planner/main-agent tools. Company associations were unaffected.
- Added per-asset `related` to `insert_assets`, defaulting true and applying only with coverage enabled. False inserted only into shared inventory without counting toward task coverage, such as incidental neighboring sites or unrelated assets.
- Company scope and task test assets consistently recognize domains, URLs, IPs, CIDRs, ICP filings, and keywords. Domains/IPs create or reuse global assets; CIDRs/ICP/keywords remain task-scope context.

### Builds

- `build.sh --release` builds Linux amd64/arm64, macOS amd64/arm64, and Windows amd64 together, producing ZIPs with `skills/`, example configuration, and README.
- Release archives use Go linker stripping and ZIP compression. UPX is explicitly optional to avoid startup segmentation faults from self-extracting ELF binaries on some Linux environments.
- The release workflow smoke-tests Linux amd64 startup and publishes `SHA256SUMS` with the archives.

### Contributors

- [@neouks](https://github.com/neouks)

## [0.3.2] - 2026-08-20

### Added

- New tasks can link multiple existing tasks and inherit direct-source facts, findings, completed intents, asset scopes, and blackboard context live and read-only, while retaining independent queues, workspaces, and conversation histories.
- Added ordered task LLM chains with automatic next-profile switching on explicit provider quota exhaustion, persisting current profile, exhaustion state, and structured audit activity.
- Running/paused tasks can edit and reorder LLM chains or switch manually. Automatic/manual switches and chain exhaustion produce task-detail notices.
- Running workers support pause, resume, and cancellation. Pause retains blackboard data; cancellation transactionally removes the intent and its direct facts, findings, and execution records after writes stop.
- Task deletion can also remove associated assets, traffic, findings, and workspace files, with deletion barriers, concurrency protection, and auditable deletion counts.
- Added optional task concurrency limits. New tasks queue FIFO at capacity and start automatically when a slot becomes available.
- Added task/company asset pagination, task-category statistics, and paginated intent mock contracts.
- Added `build.sh` for single-binary builds with static frontend export, resource embedding, cross-platform targets, and build-version injection.

### Changed

- Explicit per-task LLM chains coexist with the global LLM Pool. Explicit chains retain strict quota-failover semantics; absent chains use existing agent-binding and global-configuration rules.
- Main-agent input supports growing multiline text, `Enter` to send, `Shift+Enter` for newlines, and protection against sends during Chinese IME composition.
- Planners, workers, and main agents share task-level LLM runtime, using the smallest candidate context window as the safe compaction threshold.
- Task details show Running, Idle, and Paused from actual LLM-call state. Source-task facts, findings, intents, asset references, and graph nodes consistently show provenance and remain read-only.
- Agent previews, task reports, and related details share one Markdown renderer.
- LLM configuration uses cards and drawers, with independently scrollable model lists inside drawers.
- Task pause no longer blocks main-agent conversations. Their orchestration sessions are independent, and new messages remain possible while paused; pausing terminates only the current round.
- Increased the default worker run wall-clock budget from 600 to 1200 seconds.

### Fixed

- Removed optimistic main-agent console echoes in favor of server-only rendering, fixing message disorder and cross-conversation content during pauses or send failures.
- Opening a main-agent conversation scrolls to the bottom and fully shows the last reply, maintaining position after lazy expansion instead of pushing it off-screen.
- Fixed main-agent runs continuing after task pause and orchestration-agent pauses failing to stop the main agent consistently.
- Fixed task badges and action buttons becoming inconsistent with completion or actual planner/worker/main-agent execution.
- Fixed late blackboard writes and leftover files caused by worker cancellation, task deletion, and concurrent writes.
- Fixed broken Markdown previews, overflow in long deletion confirmations, mobile widths, and misaligned task-detail buttons.
- Added named termination causes to every agent cancellation path, exposing the canceling actor, terminal state, rounds, duration, token usage, and unanswered tool calls in activity details.
- Fixed shutdown races where a parent context could overwrite the named `shutdown` cause, and invalid text from byte-truncating Chinese activity summaries.
- Fixed cancellation events with partial streamed output losing their actual cause, while retaining content generated before cancellation.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@neouks](https://github.com/neouks)

[Unreleased]: https://github.com/Autumn-27/ARTEX/compare/v0.3.10...HEAD
[0.3.10]: https://github.com/Autumn-27/ARTEX/compare/v0.3.9...v0.3.10
[0.3.9]: https://github.com/Autumn-27/ARTEX/compare/v0.3.8...v0.3.9
[0.3.8]: https://github.com/Autumn-27/ARTEX/compare/v0.3.7...v0.3.8
[0.3.7]: https://github.com/Autumn-27/ARTEX/compare/v0.3.6...v0.3.7
[0.3.6]: https://github.com/Autumn-27/ARTEX/compare/v0.3.5...v0.3.6
[0.3.5]: https://github.com/Autumn-27/ARTEX/compare/v0.3.4...v0.3.5
[0.3.4]: https://github.com/Autumn-27/ARTEX/compare/v0.3.3...v0.3.4
[0.3.3]: https://github.com/Autumn-27/ARTEX/compare/v0.3.2...v0.3.3
[0.3.2]: https://github.com/Autumn-27/ARTEX/compare/v0.3.1...v0.3.2
[0.3.14]: https://github.com/Autumn-27/ARTEX/compare/v0.3.13...v0.3.14
[0.3.13]: https://github.com/Autumn-27/ARTEX/compare/v0.3.12...v0.3.13
[0.3.12]: https://github.com/Autumn-27/ARTEX/compare/v0.3.11...v0.3.12
[0.3.11]: https://github.com/Autumn-27/ARTEX/compare/v0.3.10...v0.3.11

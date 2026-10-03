# Changelog

All notable changes to this project are recorded in this file. The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Language

#### Added

- **One-time startup migrations upgrade seeded Chinese defaults to English for existing databases.** Agent prompts, tool descriptions/schemas, intercept rules, and the reporter/retester agent names and trigger messages are upgraded, but only for rows still equal to a historical default; customized rows are left untouched and logged once at startup. Operators can also adopt the new English defaults manually with each tool's / prompt's "reset to default" action.

#### Changed

- **The entire project was converted from Simplified Chinese to English** — backend, prompts, UI, notifications, and docs.
- **The intercept model-decision reason sentinel changed from the legacy Chinese-character marker to the ASCII `[model]`.** Both are accepted on read, and existing rows are rewritten in place.
- **Chat @-mention wire tokens now use English kind words** (`finding`/`asset`/`company`/`api`/`ip`/`app`/`domain`/`subdomain`/`service`); the former Chinese words remain accepted for conversations stored earlier.
- **Notification vuln-class include/exclude keyword filters now carry English vulnerability classes.** New findings carry English vulnerability classes, so operators with stored Chinese keyword filters should update them; the `.Title`/`.SeverityLabel`/`.StatusLabel` webhook template values are now English too.
- **Existing installs receive the English build only from a release the project owner publishes, and must refresh the `skills/` directory manually** — the in-app updater replaces only the binary.

### Intercept

#### Added

- **New built-in intercept rule "delete-style API paths."** The built-in HTTP destructive rule previously recognized only the DELETE **method** (`curl -X DELETE`, `requests.delete(`, `method:'DELETE'`), and the path-based rule's word list held only `/clear /wipe /flush /purge /truncate /drop /destroy /factory-reset /reset-all` — but most applications' delete endpoints fire on GET/POST, so a call like `curl 'http://t/api/user/delete?id=1'` matched no built-in rule and really deleted the target's data. A `deny` rule has been added covering `/delete /del /remove /unlink /erase /destroy` (allowing suffix forms like `/deleteAll`, `/delete_user`, and `/delete-user`); the verb must be followed by a separator, so `/delivery`, `/details`, `/delta`, and `/delegate` are not caught by mistake. The rule uses its own seed marker bit, so **existing instances get it after upgrading** too; like the other built-in rules, it can be disabled or deleted under "System → Command interception."

### Finding notifications

#### Added

- **Findings can be pushed to IM.** A new "System → Notifications" page can push scan findings to six channel types: **DingTalk, Feishu, WeCom, a generic Webhook, Telegram, and email**. Any number of bot instances can be configured per type (for example, one DingTalk bot for an "incident response" group and another for a "daily digest" group), each with its own enable/disable, rate limit, and filter rules.
- **Notification timing comes in two modes: real-time and digest.** In real-time mode each matching finding is sent on its own the moment it hits; in digest mode a batch of findings is merged into one message on a global cycle (30 minutes by default), opening with "M new findings in the last N minutes" and a severity breakdown so you can tell at a glance whether anything needs immediate attention. To get "high severity in real time, everything else digested," create two channels and configure them separately — the policy is not hard-coded.
- **Filter rules support four dimensions:** minimum severity, a task / asset-scope restriction, vulnerability-class keyword include and exclude (exclude wins), and whether to receive status changes (off by default, because for most people a "notification" means a new finding, not a running log of status changes). Each message carries a "View details" button linking to the finding; its target address is set by the global "callback URL" setting, and if left blank no button is included.
- **Delivery history and manual resend.** The notifications page lists each delivery's status, attempt count, failure reason, and channel, filterable by channel and status; a failed item can be resent with one click (resending resets the retry count to zero, since a manual resend means the failure cause has already been addressed).
- **Channel credentials are echoed masked.** The Webhook URL, signing secret, Bot Token, SMTP password, and so on are each declared by their own channel (`SecretKeys()`), and the API echoes back only a masked value with a trailing-digits hint; submitting it back unchanged means "leave as is," while clearing the field deletes it.

#### Fixed

> This section records the conclusions of a security audit run after the feature's first version was complete. Each item was reproduced first, then fixed, then covered with a regression test.

- **Fixed a mask bypass via "change the target address, keep the credentials" (critical).** The masking mechanism exists to keep credentials from being echoed back to the browser, but the target address and the credentials are two independent sets of fields, and config merging preserves the stored value for any key that isn't mentioned. So **changing only the address and saying nothing about the credentials** made the server send the real stored credentials to an arbitrary address — the generic Webhook's `Authorization` header, Telegram's Bot Token (which goes into the request path), and the email channel's SMTP password (handed to the peer after STARTTLS) all leaked, completely silently and without relying on a redirect. This path was confirmed workable and reproduced on all four channels one by one. The rule now is: **whenever the target address changes, the caller must take an explicit stance on every credential field** (supply a new value, or explicitly blank it to say it is no longer needed) — echoing the masked value back unchanged means "keep the old credentials," which is exactly the attack shape, and is rejected along with the rest. "Automatically discarding the credentials" was deliberately not done, because for optional credential fields (such as `headers`) that would turn into "auth silently disappears but the API returns success," which is harder to diagnose than an error.
- **Fixed zero escaping of untrusted content in the markdown-based channels.** DingTalk, WeCom, and Feishu previously did no escaping at all (Telegram and email both did). A finding's title and summary come from model output (the model reads the target's responses), and an asset's `url` is a full URL obtained by scanning that contains a target-controllable query string. A finding titled `Login SQL injection\n[Urgent: click here to verify account](http://attacker.tld)` would render as a **clickable external link** in a security engineer's DingTalk/Feishu; `![](http://attacker.tld/beacon)` would be fetched by the client at render time — effectively announcing "this finding has been seen" and leaking the reader's IP. All of this is now collapsed to a single line and markdown metacharacters are escaped. A related design mistake was corrected at the same time: escaping must happen at each channel's own render exit, not inside the title function shared across the four contexts (markdown escaping leaking into Telegram's HTML would leave visible backslashes).
- **Fixed the SSRF surface of the delivery address.** Previously only the scheme and host were validated, so `169.254.169.254` (cloud metadata, from which instance credentials can be read), loopback addresses, and internal addresses could all be delivered to; and on a failed delivery the first 200 bytes of the response body went into `last_error` and were echoed by the delivery-history API, forming a semi-blind read primitive (able to read the first 200 bytes of any internal HTTP endpoint's response). Defense is now applied at the **dial stage** (rather than only validating when the config is saved) — that is the point where it actually takes effect — and it also covers DNS rebinding and same-host redirects, and rejects cross-host redirects (these providers' credentials are right there in the URL, so following a redirect means handing them to the redirect target). Loopback and link-local addresses require `ARTEX_NOTIFY_ALLOW_LOCAL=1` to be set explicitly before they are allowed (a local SMTP relay is a legitimate configuration and can't be blanket-blocked); **RFC1918 private networks are deliberately allowed**, because a self-hosted internal Mattermost / SMTP relay is very common, and defending to that degree would break normal deployments along with it.
- **Fixed notification credentials leaking through error messages.** On a failed delivery the `*url.Error` returned by `http.Client.Do` prints the **full URL** into the error text, and DingTalk's `access_token`, WeCom's `key`, Feishu's hook id, and Telegram's `/bot<token>/` are all **right there in the URL**. That string therefore flowed into four exits: stored in cleartext in `notification_deliveries.last_error`, echoed verbatim by the delivery-history API (bypassing the channel config's masking), the server log, and the error message returned to the frontend by the test-send API. All of this is now redacted — the error message keeps only `scheme://host` and the underlying cause (enough to pin down DNS / connectivity / certificate problems), and the path and query are discarded. The error text of address validation itself (a failed `url.Parse`) also carried the full address and gets the same treatment; **the previous round fixed only the former and missed the latter, and the regression tests at the time all went through the scheme branch and never covered the parse-failure path at all, which was a false guarantee** — a test that genuinely covers that branch has now been added.
- **Fixed silent loss caused by marking the whole batch as delivered after a digest message was truncated.** Every channel has a length cap (WeCom's 4096 bytes is the tightest), and when a batch doesn't fit the message is truncated, yet on a successful send the whole batch of deliveries was marked delivered — the items that got cut were **neither in the message nor in the failure list**, the delivery history still showed success, and findings vanished just like that. Packing is now done per **whole message**: only the items that fit in this message are marked delivered, the rest go back to the queue to continue in the next message, and the message header states truthfully, "This message shows the first N items; the remaining M will continue in the next message." Deferred items **do not consume a retry attempt** (the optimistic +1 taken on claim is subtracted back), otherwise a backlog of 500 would judge the tail items failed by the third segment — when they never erred at all.
- **Fixed a typo in the minimum-severity threshold silently disabling the filter.** When `min_severity` is misspelled as something like `hgih`, an unknown severity is 0 in the ordinal table and the test degenerates to `rank >= 0`, which is always true — the user thinks they restricted it to "high only" but actually floods every finding into the group, and it is completely indistinguishable from a correct config in the UI. The value is now validated on the write path, with the error message listing the allowed values (the read path stays lenient: a bad value already in the database won't make the whole channel unreadable).
- **Fixed `rate_per_min = 0` (no rate limit) being unreachable.** The docs, the UI hint, and the token bucket all interpret 0 as "no rate limit," but the database-write layer alone had `if RatePerMin <= 0 { use default }`, quietly turning an explicit 0 into 20 (DingTalk/WeCom/Telegram) or 100 (Feishu) — the operator thinks they lifted the limit but is actually throttled with no indication. Only the request body can express the difference between "unspecified" and "explicit 0," so the default is now filled at the API layer when the field is absent.
- **Fixed digest channels bypassing the token bucket entirely.** The `allow` count that `takeTokens` deducted went unused, so `rate_per_min` had no effect at all in digest mode. The number of items claimed is now bounded by both "this cycle's quota" and the in-memory upper bound.
- **Fixed failure disposition being judged by the batch's maximum attempt count, letting old deliveries drag new ones down with them.** The attempt counts within a batch are not the same, and one old delivery that had already retried twice would drag brand-new deliveries in the same batch into failed — a new finding would be lost permanently without a single retry used, exactly the opposite of the intent of "don't let old rows drag new rows under." The decision is now made per item: permanent failures are judged dead immediately, items whose own retry count is exhausted are judged dead, and the rest are rescheduled according to their own backoff tier.
- **Fixed deliveries with an unparseable snapshot in a digest batch being silently marked successful.** Such items are skipped at the render stage (so they don't take down the whole batch), but the subsequent whole-batch mark-delivered counted them as successful too. They are now explicitly judged failed with a reason, which can be found in the delivery history.
- **Fixed a single channel's per-cycle delivery count possibly exceeding the lease duration.** The lease is 3 minutes, and if the number of items a cycle can deliver serially is large enough that the worst-case time exceeds the lease, then in a multi-instance deployment another instance re-claims the lease-expired rows, sends them again, and double-increments the attempt count. The per-cycle cap is now derived from "lease / per-send timeout," and an assertion pins down the relationship between these three constants (while writing that assertion we immediately found that the original value of 6 used up the lease exactly with zero margin, and have adjusted it to 5).
- **Fixed Telegram truncation possibly cutting an HTML entity in half.** Truncation avoided only half-cut tags, not a severed entity fragment like `&amp`, which could make the parser reject the **entire** message — and over-long digest messages are common to begin with, so the cost is too high. Truncation now avoids both unclosed tags and entity fragments.
- **Fixed the email channel judging transient SMTP failures as permanent.** SMTP 4xx (such as a greylisting `450`) is a temporary rejection and the proper practice is to retry later, but previously everything was judged a permanent failure — a mail server with greylisting enabled would make **every** notification fall into failed after its first attempt, which is exactly the scenario automatic retry should serve. The leading digit of the reply code is now used to distinguish: 4xx is retryable, 5xx is a permanent failure, and when no code is available it is treated as retryable.
- **Fixed the mask literal actually being written into the database when a nested structure is submitted.** An object field like `webhook.headers` can only be masked or submitted as a whole; stuffing the mask sentinel inside the object can neither express "leave unchanged" nor avoid being stored as a real value, causing auth to silently fail afterward with no error. Such a submission is now explicitly rejected.

#### Design notes

- **The finding-write transaction does only one blind INSERT.** `notification_events` is written by `RecordFindingTx` within the **same transaction**, so a commit atomically guarantees both "the finding is persisted" and "the notification task exists." This INSERT deliberately does not read the channel tables or run the user's filter rules — otherwise one misconfigured filter condition could pollute or even abort the transaction and keep a high-severity finding from being stored. To isolate failure to this one statement (in PostgreSQL, an error in any statement within a transaction voids the whole transaction, so even `COMMIT` fails), it is wrapped in a `SAVEPOINT`, and on failure it only logs without affecting the finding write.
- **Delivery uses lease-based claiming rather than a long transaction.** After claiming with `FOR UPDATE SKIP LOCKED`, the row is set to `sending` and `next_attempt_at` is pushed into the future as a lease; the transaction is committed before the network delivery is done, so no database lock is held during delivery. A `sending` row left behind by a process crash is re-claimed by the next cycle after the lease expires — self-healing, and without causing infinite retries.
- **Rate limiting does not consume the retry budget.** The engine first computes how many items this cycle can still send from the channel's token bucket, then claims that many. The reverse order (claim first, discard later) would make a rate-limited delivery waste an attempt, and after three budgeted attempts are burned up by pure waiting it would fall into failed. Exceeding the limit does not drop messages; it only defers delivery to the next tick.

### Test infrastructure

#### Fixed

- **Fixed permanent asset residue caused by the cleanup order in the company-ICP-attribution test.** That test's cleanup is registered in `t.Cleanup`, but the connection is closed with `defer d.Close()` — `defer` runs first when the function returns and `t.Cleanup` runs after it, so all the cleanup statements landed on an **already-closed connection**, the error was discarded by `_, _ =`, and the test's assets and company remained in the database permanently. It uses `MAX(companies.id)+1` as a fake TaskID to tag assets, and once that number collides with another test's task id, the test that asserts "exactly N assets" fails inexplicably and is extremely hard to locate. The connection close now also goes through `t.Cleanup` and is registered first (last-in-first-out guarantees the cleanup runs first), and a cleanup failure is made visible rather than swallowed. The same pattern (`defer d.Close()` + a database write in `t.Cleanup`) appears in a dozen-odd other places in the `db` package; only the one empirically triggered was fixed this time.

### Traffic

#### Added

- **The traffic list gains "Clear all."** It deletes all traffic records at once, ignoring the current filter, and additionally cleans up historical host directories the index no longer tracks, leaving nothing behind. After clearing, it runs a full compaction (`optimize` + `VACUUM` + `wal_checkpoint(TRUNCATE)`) to return the index's disk space to the system, and reports how much was actually freed when done. Traffic evidence already bound to a finding is stored in a separate evidence database and is unaffected. A `VACUUM` on an empty database costs almost nothing, so this is also **the way for an existing instance to convert its index database to incremental-reclaim mode** — after clearing once, day-to-day per-host deletes reclaim space on their own.

#### Fixed

- **Fixed disk space not being freed after deleting traffic.** Deleting only made the data invisible; the space stayed in the index file the whole time. SQLite's row delete merely hangs pages on the freelist, and the index database did not enable `auto_vacuum` at creation, so the file never shrinks; meanwhile `ex_fts` is a `contentless_delete` full-text index, where `DELETE` only writes a tombstone and does not reclaim the original postings, so without merging they accumulate forever — deleting traffic actually made the index larger. Because bodies under 256KB are all inlined in this database, and the trigram index is roughly 2x the body size, an instance with heavy capture would occupy several times the disk for long-since-deleted traffic (measured: capture 6MB of bodies → 16MB index, still 16MB after deleting everything). A newly created index database now enables `auto_vacuum=incremental` directly, and after each delete commits it runs "full-text index incremental merge + `incremental_vacuum` + `wal_checkpoint(TRUNCATE)`" in the background in chunks, gradually returning space to the OS (the same scenario drops back to 104KB after deleting). Reclamation proceeds in chunks and releases the write lock between chunks, so it does not block traffic recording; it yields immediately on process exit, and the remaining work resumes on the next delete.

> Upgrade note: `auto_vacuum` can only be set at database creation, so **an existing instance's index database is still in the old mode**, where `incremental_vacuum` is a no-op — a one-line notice is printed at startup. After upgrading, such a database has already stopped the continued growth of tombstones (the full-text index merge runs as usual), and the space already occupied can be reclaimed once with the traffic list's "Clear all" — that step converts the database to incremental-reclaim mode, after which day-to-day deletes take effect on their own.

### LLM

#### Fixed

- **Fixed custom session headers not taking effect on the "one-shot LLM call" path.** The session header's value is taken from the session id on the request context, and that id is written to the context by agentcore only when a transcript store is mounted. Goal decomposition (round 0) and cold-node compaction (the §4 body call) are both one-shot calls with no store mounted, so there is no session id on the context and the gateway can't read the header — for an endpoint like opencode zen (which returns `400 MissingSessionID` outright when `x-opencode-session` is absent), this showed up as "round-0 goal decomposition fails with 400 while subsequent planner rounds are completely fine," and a compaction failure left only a one-line log, extremely hard to correlate. These two paths now explicitly attach a session id that is stable per exploration (`exp<N>-goals` / `exp<N>-compactor`): the header is carried normally, and llmrec can correctly attribute these two paths' token usage to the corresponding exploration (which it previously could not record at all).

### Findings

#### Fixed

- **Fixed the "By asset" view having no scrollbar when the asset list overflows.** The asset tree on the left was already wrapped in a scroll container, but the outer card gave only a `max-height` and no definite height, so the scroll viewport, relying on `height:100%`, could not resolve a height (in CSS, when only `max-height` is set and `height` is still `auto`, a percentage height has no effect), and when there were many assets the list either burst the card or was cut off and couldn't scroll. The height cap is now placed directly on the asset tree's native scroll container (`overflow-y-auto` + `max-h`, adapting to the window height), so with few assets the card shrinks to its content and with many assets it is capped and shows a scrollbar.

## [0.3.14] - 2026-09-24

### Task list

#### Added

- **The task list gains a "Findings" column.** It shows the count of Crit / High / Med / Low by severity tier, with nonzero tiers colored by severity, so you can see each task's finding volume and distribution at a glance.

#### Changed

- **The "Description / Goal" columns were narrowed.** Over-long content is ellipsized and shown in full on hover, reducing how much horizontal list space long text takes up.

### Exploration graph and planning overview

#### Changed

- **Streamlined the planning overview (graph_overview) and set a count cap on each list.** Recent facts, finished intents, pending intents, cold-region digests, and confirmed-finding details were all changed to "the latest window + a count fallback," with anything not shown queryable on demand; this significantly reduces the LLM context size per round and avoids context bloat on long tasks (the cold region of a related task's overview is rate-limited along with it).

#### Fixed

- **Eliminated the reverse duplicate edge between a digest and a finding in the exploration graph after cold nodes are folded.**

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.13] - 2026-09-19

### Asset intercept

#### Added

- **Added global asset intercept rules (a blocklist).** You can enter exact- and fuzzy-match domains / IPs / URLs as well as CIDR ranges, with full create/read/update/delete and enable/disable for rules; the management page is at "System → Asset intercept." By default it ships with fuzzy intercepts for government (`.gov` / `.gov.cn`) and education (`.edu` / `.edu.cn`) sites.
- **Asset intercept is wired into the execution chain.** Before an agent dispatches an intent (`add_intent`) or inserts assets (`insert_assets`), the target asset is run through an intercept check first — an intercepted intent is not dispatched, an intercepted asset is not inserted, and the asset info and the intercept reason are returned to the agent.
- **Added task-level intercept / allow (allowlist) rules.** Independent of the global rules and effective only for this task, evaluated in the order "intercept first, then allow" — a hit on intercept forbids it; if nothing hits intercept but this task has configured allow rules and none of them match, then "testing is not allowed"; if no allow rules are configured, the allowlist is not enabled. They can be entered when creating the task, and can be created/edited/deleted and enabled/disabled in the task detail "Overview."
- **Task templates support preset categories and task-level intercept/allow rules.** A template can save a task category and a set of task-level rules, which are carried into the new-task form when the template is applied.

### Operation review

#### Fixed

- **Tightened the model judge's output protocol to reduce false allows caused by truncation.** The judge's comment cap was lowered from about 1250 characters to about 300 characters and "output JSON only, with no preamble or code block" was enforced, so that the verdict isn't truncated by `MaxTokens` into something unparseable and then allowed through by the model-failure policy (fail-open).

### Task archiving

#### Fixed

- **Archiving now skips a symlink instead of failing the whole package.** The archive format supports only regular files and directories end to end, and previously any symlink in the working directory would cause the entire task archive to fail; it now skips the symlink and logs it, archiving the rest of the files normally (not following the link, not escaping the directory tree).

### Account and compliance

#### Added

- **Added a "Terms of use and disclaimer" dialog before login.** You must check to agree before you can log in.

### License and dependencies

#### Changed

- **The project adopts the AGPL-3.0 open-source license**, and the README's license and disclaimer notes were rounded out.
- **Upgraded norma to v0.4.1.**

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.12] - 2026-09-17

### Exploration activity feed

#### Added

- **The task detail page gains an "Exploration activity feed"** (#144): it presents the task's exploration nodes (origin/goal/intent/fact/finding/hint/compaction) as a timeline stream, with filtering by type, keyword search, ascending/descending order, pagination, and auto-refresh; while on "the first page, newest first" it is the live slot and refreshes every round, and when you leave that slot it only accumulates an unread count without disturbing your current reading, with "N new updates · Back to latest" to return in one click. It is grouped by day, so even when paging through a long task you can recognize "which day this happened."
- **Each row in the feed shows the node id** (#145), making it easier to cross-reference the exploration graph and locate a specific node.
- **Expanding a feed node's detail shows upstream/downstream and anchored assets** (#147): expanding an entry reveals that node's upstream (where it came from) / downstream (what it produced) relationships, and hovering a related item pops up that node's card (type/status/source/time/summary/payload snippet); it also lists the assets the node anchors (a type tag + identifiable text). The relevant data is delivered together with the feed page, so expanding sends no extra request.
- **Feed search supports filtering by node id** (#150): beyond "content / source," the search box adds node-id matching, so entering a plain number or the "#41" form shown in the UI pinpoints the corresponding node.

### Intent management

#### Added

- **Intent deletion supports two modes: "soft delete / hard delete"** (#149): unclaimed, running, and paused intents can all be deleted, and a reason must be entered; the delete mode is chosen in the confirmation dialog.
  - **Soft delete (default):** the intent is set to "deleted" and the reason is recorded in a separate field, keeping the intent node and all of its outputs and lineage.
  - **Hard delete:** physically removes the intent and the exclusive descendant nodes "supported only by it" (cascading along the output / intent chain to the leaves), to avoid leaving orphaned data; the deleted intent's token accounting is archived and kept by its original date; shared nodes (still referenced by other intents), goals, and the task root fact are all kept, and the hard-delete dialog shows the estimated number of cascade-affected nodes.

  Both modes notify the planner "this intent was deleted by the user + reason" and replan accordingly.

### Operation review

#### Added

- **Approval records can be filtered by status and decision source** (#139): approval records can be filtered by approval status and decision source to quickly locate the target record.

#### Fixed

- **Pending approval requests load independently of the history pagination** (#133): pending requests are no longer affected by the history list's pagination, and remain fully visible while you page through the history.

### Agent

#### Changed

- **The Worker role description was changed to generic network-security-platform wording** (#138).

#### Fixed

- **Fixed cold-node compaction never triggering.** The Compactor was wired into the per-task planner; cold-node compaction (cold-digest) previously never ran because it wasn't wired in, and is now restored.

### Chat

#### Added

- **Chat supports @-referencing records of multiple types, with scroll pagination** (#135): you can @-reference records of many types in chat, and the reference candidates support scroll-based paginated loading.

#### Fixed

- **Long message bubbles are constrained within the conversation panel** (#137): an over-long message bubble no longer overflows the conversation area.
- **Fixed uploading a file reporting "missing upload file" when a new conversation hasn't been created yet.** The Composer's file picker now snapshots the `FileList` into an array before clearing the input and then invokes the callback; previously, the draft state had to create the conversation asynchronously first, and by the time execution resumed the `FileList` live-bound to the input had already been cleared, so the upload was missing the file field and the backend returned 400.

### Traffic

#### Fixed

- **The traffic-recording proxy now listens on 127.0.0.1 only by default** (#129, #130): to avoid the open proxy being exposed on other network interfaces under the default configuration.

### Web

#### Fixed

- **Fixed the static-export task list page losing the global header.**
- **Added the missing associated-traffic mock on the demo finding detail page, fixing a blank screen.**

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@RuoJi6](https://github.com/RuoJi6)
- [@dingpotian](https://github.com/dingpotian)

## [0.3.11] - 2026-09-15

### Operation review

#### Added

- **Approval records support source navigation** (#125): clicking an approval record's "source" jumps back to the exact tool execution that triggered that approval — opening the original full conversation, auto-paginating to the target position, expanding the command and result in place and centering and highlighting it in the scroll area, with the surrounding messages kept readable; auto-correction stops when the user scrolls manually. Across ordinary conversations, task Workers, the planner, and the main agent's segmented sessions, it locates the exact point via a persisted `tool_use_id` and a task mapping (a new within-scope call-ID index), and when something is missing, duplicated, or ambiguously associated it gives an explicit notice instead of jumping to a different execution. When the conversation has been deleted or the task archived / its record is missing, it gives a clear notice.
- **Approval records support pagination** (#115).

#### Changed

- **Streamlined the model-review input** (#124, #125): the model review's input (the LLM judge used after no rule matches) was narrowed to a versioned JSON of "the current full tool call + an explicitly selected short background + the local working directory." The background takes only real user messages (the current user message of the chat / task main agent), the Worker no longer carries an intent summary and also clears the background inherited from a parent agent; the planner and auto-triggered sessions do not fabricate a user message. It no longer carries the task description, goals, operation constraints, the global exploration situation, historical calls, or the Worker's full intent — these are still used by their original logic for agent execution and standalone conversation auditing, just not fed into operation review. Every verdict is required to output JSON `decision`/`comment`, and the explanation must include "the action, the consequence on success, and the matched rule"; instructions in the arguments or the background cannot change the review policy. The snapshot and fingerprint of the input actually sent to the model are saved, old snapshots are kept and labeled with their version, and old inputs are not fabricated from current data.

#### Fixed

- **A model verdict wrapped in a code block is no longer silently allowed through** (#126): if the verdict JSON returned by the model was wrapped in a ``` code block, a parse failure was previously silently allowed through by the model-failure policy; it now strips the code-fence first and then parses, and only falls back to the configured failure policy if parsing still fails.

### Agent

#### Added

- **Added experimental noa context compaction** (norma upgraded to v0.4.0): an experimental feature that can be enabled in system settings, off by default. Once enabled, noa (model-driven context compaction) takes over context compaction for the four agent types — main agent, planner, Worker, and chat — replacing the built-in compaction; the compacted originals are persistently archived, collected under `<workDir>/noa/<sessionID>/` (not scattered across the individual task directories), in a globally unique subdirectory per session ID. If it fails to engage it automatically falls back to the built-in compaction without interrupting the actual task; the toggle is read once per run, and switching it affects only runs started afterward.

#### Fixed

- **Exploration-graph tools now reject instead of crashing on nil when there is no task context.** Calling an exploration-graph tool outside a task context now returns a clear error instead of crashing on a nil-store dereference.

### MCP

#### Added

- **Support for legacy SSE MCP services** (#117): compatible with an MCP server that provides only the old-style SSE transport.

### Traffic

#### Fixed

- **Traffic search matches a record's host in a port-aware way** (#114): `traffic_search`'s host matching now includes the port, preventing same-host records on different ports from cross-contaminating each other.
- **A failure in the `traffic_search` description migration no longer interrupts the subsequent reporter migration.** A single migration's failure is isolated and does not affect the execution of later migrations.

### Web

#### Changed

- **Added a divider between the task retest finding options** (#122): a separator was added between the retest finding options for better visual clarity.

#### Fixed

- **Fixed a full-page crash on the demo task detail page.** In mock mode the task detail's "Conversations" tab fetches `GET /api/tasks/<id>/side-questions` (side-question history), but the mock handler had no such route, and the read fallback — "a path ending in s is a collection" — returned `[]`, making `data.items` `undefined`, so the side-question hook's `merge()` iterating over it threw `TypeError: t is not iterable`; this exception occurred inside the `setItems` updater, was deferred by React and rethrown during the render phase, the caller's `catch` couldn't catch it, and the whole page was taken over by the error boundary showing "This page couldn't load." The mock handler now explicitly returns an empty side-question history, and `sideAPI.history` also normalizes its return value (a non-array `items` is always coerced to `[]`) as defense in depth.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@RuoJi6](https://github.com/RuoJi6)

## [0.3.10] - 2026-09-13

### Network

#### Added

- **Web search gains a DeepSeek official source.** It directly reuses the currently active LLM config. It differs in nature from the other three sources — DeepSeek has no directly callable search API, and search exists only inside its Anthropic-compatible API (the `web_search_20250305` server-side tool), executed by DeepSeek's server. It therefore **supports only the DeepSeek official endpoint + the anthropic protocol** (an OpenAI-protocol endpoint rejects the server-side tool outright), each search **consumes one extra model call**, the request **does not go through the search egress proxy**, and it **is not recorded in the traffic trail**, and the returned results **have only a title and a link** (no summary; fetch the body with WebFetch when needed). The settings page explains these limits but **does not validate or block**, leaving it to the user to confirm whether they are met, which can be verified with a real run using the "Test search" button.

### Agent

#### Added

- **The worker gains cross-work look-back.** Added `search_all_worker_traces` (without needing an intent_id first, it searches globally by keyword across the execution of all works in this task for matching steps) and `get_worker_trace` (after locking onto one work, list its step stream, search within it by keyword, and fetch the full content by step_id), used to reuse observations another work saw but didn't write into a fact, avoiding duplicated effort.
- **`node_detail` is delegated to the worker.** Together with the look-back tools above, once the worker has an intent_id / node id it can directly query that node's full detail.
- **A planning round triggered by `add_hint` is now explicitly announced.** Previously adding a hint only folded it into the situation overview and left the planner to discover it; now one `add_hint` records a trigger for the planner — "a human added N strategic hints: …" (a batch counts as one, rather than spamming one line each) — and the planner is explicitly told "this round was triggered by a new hint" and sees the hint content directly.

#### Changed

- **The global situation prompt was changed to a "loosen up" tone:** encouraging divergent exploration and timely reporting of cross-intent leads rather than converging too early.
- **Streamlined the wording of the worker's boundaries:** a first setback does not mean the direction is exhausted, with a focus on "work through the bypass techniques within this intent before drawing a conclusion."
- `insert_assets` drops the per-asset `related` parameter: its only purpose was to decide whether to add the asset to this task's scope, its value wasn't persisted (it was voided the moment the same asset was registered again, and the UI didn't show who had been judged unrelated), so in practice it only made the model do one more judgment that wasn't retained.
- **The test scope (`task_scope`) is no longer affected by the asset-coverage toggle.** `insert_assets`'s automatic scope entry (`source='auto'`) runs whether coverage is on or off, and `add_task_scope` is always provided to the planner / task main agent / goal-decomposition agent. The scope is the task's authorization boundary and the filter basis for asset queries, and the coverage toggle only decides whether to use it as the denominator for the metric — it should not decide whether to accumulate the scope itself. Previously turning coverage off disabled both the `auto` and `agent` write paths at once, leaving `task_scope` with only the rows added manually in the UI. `list_untested_assets` is still hidden when coverage is off (it is a pure coverage view by nature).

#### Fixed

- **Fixed assets cross-bleeding between tasks** (#59): `list_assets` previously hard-coded the task id to 0 and queried the entire shared asset store raw, and the model had no way to write a scope filter, so it treated another task's assets (IPs especially) as this task's targets and skewed the testing direction. It now returns only assets that fall within the test scope (`task_scope`) of [this task and its directly related tasks], matching by **attribution** rather than by literal value: with a root domain in scope you can find all of its subdomains/services/APIs, and with a network range you can find the hosts and services within that range; fetching an out-of-scope asset directly by id likewise returns nothing. Outside a task context (Auto/pentest), with no scope to rely on, it still falls back to the whole store. It also fixes the attribution blind spot where an IP-direct host (such as `http://1.2.3.4/api`) couldn't be matched by a network-range scope. The UI's "Test assets" view (filtered by task producer) is unaffected.
- **Fixed the worker's cross-work look-back tools being removed by mistake.** After `search_all_worker_traces` / `get_worker_trace` (and `node_detail`) were added to the worker's default tool set, an old migration that "narrows the worker's tool surface" unbound them again at startup, so the worker couldn't actually get these tools. They have been removed from that unbind list, and for databases that already ran the old migration they are bound back to the worker in a one-time fix.
- **Fixed an occasional database-write failure of the `/btw` side question.** Before the side_question checkpoint is written, the NUL (`\u0000`) escape that JSONB doesn't support is stripped, avoiding a write error for content containing that character.

### Triggers

#### Fixed

- **Merged trigger sessions deduplicate task descriptions/goals by task.** When multiple tasks trigger a merged session, the same task's description/goal is no longer concatenated into the message repeatedly, avoiding long goals stacking up over and over and blowing up the context.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.9] - 2026-09-11

### Agent

#### Added

- The task main agent supports **multiple sessions**: you can create, switch between, and independently reset context, and each session is interactive.
- Persistent `/btw` **side questions**: ask a follow-up without interrupting the main line; the Q&A is persisted and survives a restart.
- `spawn_task` gains `source_task_ids`, so a new task can **read-only inherit** the source task's assets and findings.

#### Changed

- Turned off the worker's cross-engagement memory and narrowed its default tool surface: reading context and cross-work review are the planner's job, and the worker only handles the execution and write-back of a single intent.
- Streamlined the worker's default prompt; removed the `terminated` and `worker_name` fields from `get_worker_output`; streamlined the `insert_assets` / `list_assets` descriptions.

#### Fixed

- Fixed a work being cut short prematurely by an **idle turn**: the model sometimes spends a whole turn outputting only thinking, giving neither body text nor a tool call, and the harness sees this as a natural end (`end_turn` with no `tool_use`) and wraps up directly with `completed` + an empty summary — an intent that isn't done yet is cut off halfway, showing up as "the task ended normally but with no text summary at all." None of the five layers of LLM retry can reach it: it is not an error, the entry point for the same-provider safe-window retry is a "stream failure," the circuit breaker judges `err == nil` a success outright, and intent rerun only recognizes `model_error`; the SDK's empty-response retry can't reach it either, because that layer judges emptiness by "whether any event was yielded," and a thinking delta is itself an event. The work's Stop hook now recognizes this kind of turn and injects a continuation instruction, letting the model continue to the next step carrying the thinking it already produced — "resend as-is" was deliberately not chosen, because this kind of idling is usually determined by the prompt and the shape of the context, a stable behavior rather than a random hiccup, and resending would only make the model think the same thing again.
- The count reuses the **empty-response retry count** from the LLM page's "Retry and backoff" (both answer "the model finished but produced no substantive content," just with different criteria and means), defaulting to 2, and setting `-1` turns it off and reverts to the old "idle means wrap up" behavior. It is a **total** cap for one intent rather than a consecutive count: the harness itself already hard-limits consecutive idling to a single nudge, and the quota refreshes only when a real tool turn has occurred, so this number guards against a loop like "tool → idle → nudge → tool → idle" burning up the intent's budget. On trigger the log records `[work <worker> · #<intent>] idle turn … injecting continuation instruction (n/N)`. See `docs/llm-retry-design.md` §1.1 for the design.
- Fixed the context budget and input layout of `/btw` under a long conversation; in a non-secure context (non-HTTPS access) the side-question request ID is generated with a degraded fallback.

### Traffic

#### Added

- A finding can be **linked to multiple pieces of traffic evidence**, which can be ordered, annotated, and distinguished by role (request/response/supporting).
- The reporter agent **automatically links** the relevant traffic evidence before writing the report.
- Added an agent **traffic-binding toggle** and filled in the handoff of evidence between agents.

### Tasks

#### Added

- Support for **session-level finding retest**: the retest runs in a separate agent session, with the running status shown in the list and detail.

### Intercept

#### Added

- Approval records gain **detail and an execution audit**: you can view the tool request context, the model/rule initial decision, the execution output, and the argument fingerprint.

### Assets

#### Added

- The task's test assets support **DSL search**.

### LLM

#### Fixed

- Fixed the test connection not sending the custom session header, causing opencode zen to return 400.

### Network

#### Added

- MCP's HTTP transport supports **skipping TLS certificate verification**, making it easy to connect to a service with a self-signed certificate.

### UI

#### Added

- The conversation list is grouped by agent, with expand/collapse and independent pinning; conversations can be filtered by agent.
- The attack-chain graph renders compacted (digest) nodes and collapses their members.

#### Fixed

- Fixed a blank screen caused by login credentials being out of sync.
- The intercept notice wording was changed to clearly indicate "platform control," to avoid being misread as a defense on the target's side.

### Deployment and updates

#### Added

- Support for **one-click updates from the page**: the system config page gains a "Version and updates" card, and the top bar lights up a hint when a new version is available. The flow is: download the release package → verify `SHA256SUMS` → smoke test → stage → exit and let the guardian script relaunch to complete the swap, and the page refreshes automatically.
- Added guardian startup scripts `start.sh` / `start.bat` as the official startup entry point (both the release package and the Docker image now include them, `install.sh` is unchanged), which decide whether to relaunch based on the exit code and forward SIGTERM to artex. The verification and swap logic are all in Go, keeping the scripts dead simple.
- Automatic fallback on failure: if verification or the smoke test fails, it is discarded and the current version keeps running; if the new version fails to start 3 times in a row, it rolls back to the previous version. The settings page also has a manual rollback (note that the database schema is not rolled back).
- Updates recognize only the GitHub domain and force HTTPS, and the release source is not configurable; development builds disable one-click updates. GitHub query results are cached for 30 minutes to keep the top-bar hint from exhausting the API quota.

#### Known limitations

- Under Docker it swaps only the program, not the image: the toolchain is not upgraded along with it, and rebuilding the container reverts to the version bundled in the image, so use `docker compose pull artex` when needed.
- It does not sync the `skills/` in the release package, so a built-in skill newly added in a new version does not take effect automatically.
- An update means a restart, which interrupts running tasks.

### Dependencies

#### Changed

- Upgraded norma to v0.3.7.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@RuoJi6](https://github.com/RuoJi6)

## [0.3.8] - 2026-09-09

### LLM

#### Added

- The LLM page gains a "Retry and backoff" tab: the **count** and **interval** of all five retry layers are configurable. A model call's failure passes, from innermost to outermost, through connect retry (SDK — connection reset / timeout / 429 / 5xx before the stream starts), empty-response retry (SDK — a normal end with no content at all, openai format only), same-provider safe-window retry (a stream-break replay before any output has been delivered to the caller), poll circuit breaker (cool down and skip after consecutive failures reach a threshold), and intent rerun (the whole intent reruns after the worker ends with model_error), with the outer layer taking over only once the inner is exhausted. Each layer has two knobs with a uniform meaning: blank = use the original default count and exponential backoff; a count = use that count; an interval = replace the exponential backoff with a fixed interval; -1 = turn this layer's retry off. The first three layers follow the endpoint and can override the global default field by field in each model config (one that only pins the interval still inherits the global count); the circuit breaker and intent rerun are process-level in meaning, with only one global copy. Changes take effect live after saving, with no restart needed. Leaving everything blank is the current behavior, and an upgraded old database is byte-for-byte unchanged (new columns default to 0, and an absent settings key means all defaults). The connection test deliberately omits these parameters — it has a hard 30s timeout, and stacking the user's configured retries on top would only test a usable endpoint into a timeout failure. See `docs/llm-retry-design.md` for the design.
- Each LLM config supports a custom **session header** (`session_header_key`): when non-empty, every LLM request carries that HTTP header, with the value being the currently running session id (`conv-<id>` for a chat session, `exp<x>-worker-i<intent>` for a worker, and so on), for gateways that do prompt caching / sticky routing by a session-id header. It is implemented by reading the session id from the request context and injecting it, with no change to norma needed, so even the same shared provider can emit different header values per session. The old database carries a migration, and the frontend's LLM config dialog gains an input field.

#### Fixed

- Fixed saving an LLM config erroring with `23502` outright when the session-header field is empty: the column is `NOT NULL DEFAULT ''`, and previously a misapplied `NULLIF($n,'')` wrote an unfilled session header as NULL and tripped the not-null constraint; the parameter is now passed directly with empty-string semantics.

### Agent

#### Added

- Wall-clock timeout now **wraps up in place**: relying on norma v0.3.6, when `MaxDuration` is reached it interrupts the running tool and continues on the live ctx for the wrap-up rounds (writing back what has been identified + a summary, with a final state of timeout), instead of relying on the worker's/planner's respective `maxDur+90s` external hard ctx to kill a stuck run outright into `aborted_tools`. Chat goes through the same harness and gets the same behavior automatically, and the mainagent has no `MaxDuration` and is unaffected.
- Stall fallback: when the planner is woken by a heartbeat / no-change wake and the whole graph has no open or running intents left, the opening is changed to a stall alert, clearly stating that no worker is running and there is no queued direction, and that this round must produce one or more mutually distinct new intents (it may not produce 0 intents).

#### Changed

- Reworked the worker prompt: the intent / startup instruction / anchored-asset raw JSON are moved into the system prompt, re-assembled every round, never squeezed out by compaction, and a continuation no longer depends on the transcript's first message being retained; the startup user message is slimmed down to just the global situation overview (degradable, stale-tolerant). The cost is that the system prompt mixes in per-intent data and loses cross-intent cache reuse — a deliberate trade-off for "never dropping an intent."
- Streamlined the planner / worker default body text and fixed several real-world issues: the planner distinguishes `recent_done` by state (for blocked/exhausted, check the trace first before deciding, treating it as neither a dead end nor a mindless rerun), negative conclusions are changed to "observations / in doubt, not final" and evidence is reviewed before they are believed, when the goal isn't met and there are no open/running intents it must produce (a hard bottom line), and depth takes priority over coverage; the worker writes a negative conclusion only as "an observation + a tentative reading," the verdict belongs to the planner, and cross-intent leads are written into a fact's summary and handed to the planner rather than chased by the worker itself. Reseeding appends the new defaults as a new version and switches to it, while the user's customizations / old versions are kept in history and can be rolled back.
- Narrowed the work agent's default tool set and asset-write-back prompt: the worker is only responsible for the execution and write-back of a single intent, and reading context / cross-work review are the planner's job, so `list_facts` / `node_detail` / `list_companies` and the cross-work searches (`search_all_worker_traces` / `list_worker_traces` / `get_worker_trace`) are removed from its default tools, leaving only `list_findings` (dedup-check before reporting a finding) + `add_finding` / `record_fact` + `insert_assets` / `list_assets`; the prompt drops the stale field descriptions that conflict with the `insert_assets` schema (`type=tech`/`on_url`/`props`). The old database carries a one-time migration that strips the corresponding bindings, while the planner's/main's bindings of the same names are left alone.

### Exploration graph

#### Added

- **Exploration-graph cold-node compaction (cold-digest):** folds old, long-inactive intents / facts into a digest node shown in `graph_overview`, while the original nodes are kept permanently and fully restorable by id (lossless storage, compaction only of presentation, reversible folding). Hot/cold is judged by reverse reachability + any live branch being hot, with R=6 rounds of debounce and connected-component grouping; background compaction (minor folds uncovered cold blocks, major re-sources and re-compacts to merge fragments) carries an activity re-check and a cooldown mutex, never getting on the hot path and never overwriting a revived node. The overview side provides `cold_digests` and a by-asset `cold_index`, with `expand_digest` / `expand_index` handling restoration. A related / inherited task's overview likewise reuses its own folded view, and `expand_digest` supports cross-task read-only restoration. `expand_digest` / `expand_index` are given only to the planner and main agent, not the worker. All carry old-database migrations.
- `graph_overview` outputs the full `finding_list`: findings are a task's highest-value product and a single task usually doesn't have many, so the overview now carries all of them (unlike facts, which get only the recent window), and the planner/worker can see every confirmed finding at a glance each round without calling `list_findings` again. Each is trimmed to `{id, summary, evidence?, from_intent?, assets?}`, where the affected assets are given directly as readable content (url / domain / ip:port) rather than a bare id.

#### Changed

- `graph_overview` no longer flattens `hosts`: the coverage block drops the host list (a large-scope task would carry up to 500 host strings each round, of limited value for planning decisions), keeping only `host_count`, with specific hosts queried on demand via `list_assets`. A top-level `done_intents_total` (the total number of finished intents) is added, parallel to the `recent_done_intents` that is truncated to ≤15, so the planner knows during deduplication whether anything was truncated.

### Tools

#### Changed

- `add_company_scope`'s default binding is changed from worker to planner: defining a company's asset scope is the job of planning / main / Auto, and the worker only executes exploration. A new database seeds the default binding as mainagent/planner/auto, and the old database carries a one-time migration.
- The planner is bound to `list_assets` by default: it now has both `list_assets` (DSL whole-store search) and `list_untested_assets` (untested within scope), with the old database backfilled once without overwriting a user's unbinding.

### Tasks

#### Added

- The task list gains a "Running Workers" column: it counts the task's intent nodes with `state='running'`, by exactly the same definition as the "Running Workers" in the task detail page's overview, and shows 0 when no Worker is running.

### Network

#### Added

- Added a **global egress proxy** configuration: all target traffic can egress through one unified global proxy (http/https/socks5, with `user:pass` support). When traffic capture is on it acts as the upstream of the MITM recording proxy (traffic is recorded as usual and then egresses through the proxy, with both the intercept and passthrough paths going through the upstream and not leaking the source IP); when capture is off it is injected directly into the agent's bash environment and WebFetch (`proxyEnv` adds `ALL_PROXY` for socks5 support). The configuration is stored in the settings KV table (no migration needed), and the frontend's system config page gains a "Global proxy" card, independent of the web-search proxy and the LLM proxy.

### UI

#### Fixed

- Corrected the semantic errors in the intent status labels: `exhausted` "Exhausted" → "Budget exhausted" (it actually means the step / time budget was cut off midway and only partial results were written back, not that the direction has been fully explored), `blocked` "Blocked" → "Errored" (it actually means the model / API / network failure retries were used up and the intent essentially wasn't truly explored, not a target / WAF block), and added the previously missing `stopped` "Stopped" (a work manually stopped by the user).

### Dependencies

#### Changed

- Upgraded norma to v0.3.4: MCP tool output gains truncation and spill-to-disk (see `651b961`), and the later v0.3.6 supports wall-clock wrap-up in place.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.7] - 2026-08-31

### LLM

#### Added

- The model config gains an "output cap," with a choice of which request field name the cap uses: the cap limits how many tokens a single reply can generate at most, is sent with every request, and 0 (the default) = don't send the field and let the server's default decide; it is a separate thing from the "context window" — the latter is the model's total capacity, used only locally to compute the compaction threshold, and does not appear in the request. The field-name switch is only meaningful for the `openai` (Chat Completions) format: empty (the default) sends `max_tokens`, which the vast majority of compatible gateways recognize; OpenAI's official reasoning models (the o series / GPT-5) conversely recognize only `max_completion_tokens` and return `unsupported_parameter` outright when they receive `max_tokens`, so such endpoints need to be switched to the new field manually. Each of the two other formats hard-fixes its field name (Anthropic's is `max_tokens`, the Responses API's is `max_output_tokens`), so for a non-openai format the switch is grayed out and cleared on save. A previously never-connected path was also wired up: none of the five — planner / worker / chat / main agent / goal decomposition — ever set an output cap, the openai family didn't send it at all and Anthropic fell back to the SDK's 8192, and merely adding the config option without connecting this line meant even a filled-in number wouldn't take effect; now the value, like "streaming output," is resolved per round from the config and takes effect on the next round after a failover switch. An upgraded old database behaves exactly the same (the new columns default to 0 and an empty field name).
- When "Test" is clicked in the backend, each HTTP attempt's status code and the gateway's raw response body are written to the server log (the body trimmed to 4K), to help diagnose 401s, quota messages, empty frames, a returned HTML page, and the like, rather than seeing only the UI's collapsed ok / err. This log does not depend on the "LLM recording" toggle.

#### Changed

- The task's LLM config chain can now be modified in any state, no longer restricted to "running / paused / chain exhausted": after a task ends (done / failed / timeout) the main agent conversation still uses this chain, and when a model on the chain has a problem it previously could neither be changed nor used to keep interacting. The terminal-state interception in both the backend HTTP and the DB transaction was removed, and the frontend dialog opens editing and saving for terminal-state tasks too. Saving a terminal-state task no longer reopens quota-blocked intents (those intents would be moved to open, with no worker to execute them and no longer meeting the condition for "rerun intent"); to keep running, still use rerun intent / add a goal, which pull the task back into the running state.

### Agent

#### Changed

- "Message a running Worker to steer its direction" now reuses the existing pause / resume + transcript resume mechanism, behaving the same as the main agent conversation: it previously used a home-grown intervention persistence protocol that touched the scheduling barrier, the resume flow, and a dozen-odd activity-query filters. The message is now injected via the next round's input and the intent runs directly in a dedicated goroutine, unconstrained by the 3-slot worker pool and running as soon as it is sent; the frontend keeps the message box and restores the "continue directly" button, with sending now carried over SSE. No DB schema change. Trade-off: messages are kept in memory only with no crash recovery, and sending a message may momentarily put the task one worker over its concurrency limit (a low-frequency scenario, acceptable).

### Tasks

#### Fixed

- Fixed an out-of-memory (OOM) crash when archiving a large task: archiving now streams the snapshot to disk instead of reading the whole task into memory at once; it also fills three recovery gaps on the cold-archive path — a modern install's traffic lives only in SQLite, and previously a crash between the PostgreSQL commit and the SQLite commit would strand an archived task's traffic in hot storage with no way to recover it, so now a staging log is written whether or not there is a history directory, and a missing `journal.json` is treated as discardable.
- Fixed the system stuttering when there are many tasks: loading conversations and the UI had noticeable lag. The queries for the task list, task context, and exploration records were all optimized, and the frontend's dashboard, conversation page, and task detail page correspondingly reduced duplicate requests.

### Assets

#### Changed

- Removed the "at most 256 rules" limit on a company's asset scope: a company entering its scope IP / domain by IP / domain easily hit this cap, and once hit had to be split into multiple companies, when the scope itself is no slower just because it has more entries. The cap was removed in all three places — backend, frontend, and demo mock — while a single rule is still limited to 1024 characters, and the 2 MiB request-body cap is kept as a backstop (roughly forty to fifty thousand rules).

### Skills

#### Fixed

- Fixed uploading a skill archive reporting "Upload failed: zip: unsupported compression": the Go standard library only ships the Store / Deflate decompressors built in, so bzip2 and Zstandard archives written by compression tools at a non-default level couldn't be opened at all. These two decompressors are now included (pure Go, introducing no new external dependency); for methods that genuinely can't be decompressed — Deflate64 / LZMA / XZ / PPMd and the like — and for encrypted archives, a clear message is now surfaced before decompression, naming exactly which file used which compression method and how to repackage it, instead of throwing the raw low-level error at the user.
- Fixed skill filename validation judging non-ASCII filenames illegal: path validation was formerly an `[A-Za-z0-9-_./]` ASCII whitelist, so a single file in the archive with a non-ASCII name (such as `références/notes.md`) would fail the whole upload with "the archive contains an illegal path." It was changed to a Unicode blocklist: filenames in any language and spaces are allowed, while control characters, invalid UTF-8, zero-width and bidirectional control characters (RLO filename spoofing), `\ % # ? * : " < > |`, `..` / absolute paths / empty path segments are still rejected, with the directory-traversal protection unchanged. Skill names are loosened likewise — ASCII is still limited to lowercase letters, digits, and hyphens (the agentskills.io spec), and non-ASCII letters (CJK and others) can be used directly as a skill name, but spaces, dots, and path separators are not accepted.
- Fixed garbled filenames or the whole archive being rejected after decompressing a Chinese archive produced by Windows compression tools: such zips don't set the UTF-8 flag bit and write filenames in GBK, so they are now decoded with a GBK fallback before path validation. A quoted frontmatter value like `name: "My Skill"` is also parsed correctly into the skill name.

### UI

#### Added

- The Findings page gains a flat "All findings" view, set as the default, switchable with the original "Grouped by task" view via a header tab: the grouped view requires expanding each task one by one to see its findings, which is a detour when you just want to scan a cross-task list at a glance. The flat view is one big cross-task table (10/20/50/100 per page) that behaves exactly like the grouped view — check to export, expand a row inline to see evidence and the detailed report, edit the name/category/severity inline, change the disposition status, deep-dive, and delete. The two views share the stat cards and the filter bar, switching doesn't lose the filters, and the current view and filters are both recorded locally; polling hits only the current view, inline changes are written to both caches in sync, and switching over won't show stale data.
- The Findings page gains a "By asset" view: an asset tree on the left (company → root domain / IP → subdomain → service → API, with the company layer appearing only when an asset really has an attribution), and on the right the findings under the entire subtree of the selected node, sharing the same table and the same filters as the other two views. The tree includes only assets that have findings, with the ancestor chain filled in on demand (when a finding hangs only off the deepest API, the layers above it are still reconstructed), and a node's count is aggregated over the subtree and deduplicated by finding; a finding whose asset has been deleted or that never had an associated asset goes into the "No associated asset" bucket. Unlike the other two views, the asset view does not poll — it queries only when you enter the view, change a filter, add/edit/delete a finding on the page, or click the refresh button on the tree, since the left tree is a navigation structure and there's no need to recompute it every 5 seconds. Each level of the tree shows only the increment relative to the level above (a subdomain drops the root-domain suffix, a service shows `https :443`, an API shows only the path), with the full value in the hover tooltip and the breadcrumb; when there are too many nodes it drops the API/service levels of a whole layer with a notice, while the counts still roll up to the level above.

#### Fixed

- Fixed the conversation transcript being squeezed and cut off in the conversation detail on mobile: on a narrow screen the conversation list is collapsed, yielding the width to the transcript itself.

### Dependencies

#### Changed

- Upgraded norma v0.3.2 → v0.3.3: added `Config.MaxTokensField`, letting the OpenAI Chat Completions output cap be sent as `max_completion_tokens` instead (reasoning models recognize only this key). The two keys are mutually exclusive, only one is sent, and the default is still `max_tokens`.
- Upgraded `golang.org/x/mod` v0.37.0 → v0.40.0, fixing two dependency security warnings.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@neouks](https://github.com/neouks)
- [@begininvoke](https://github.com/begininvoke)

## [0.3.6] - 2026-08-27

### LLM

#### Fixed

- Fixed the "non-streaming" toggle reverting to "streaming" after being saved and reopened (#69): the config-list API's DTO was missing the `streaming` field and the response never returned it, so the frontend, reading `undefined`, always fell back to the default of streaming; the actual write and DB storage were fine, it just couldn't be read back. The DTO restores the field (without `omitempty`, so `false` must also appear in the response).
- The connection test now verifies the model actually replied and tests in the config's real send/receive mode (#65): previously it only judged whether the HTTP succeeded, so a request that went through but got zero reply from the model (thinking burning up the budget / the body swallowed by a safety policy / the compatibility layer dropping `content`) still reported "connection successful," out of step with the "no reply" behavior in a conversation; and the test always went through streaming, so a non-streaming config was actually testing a different channel. Now an empty reply is judged a failure outright, on success the model's reply is shown in the message, and the current streaming toggle is carried into the test, keeping "the test passes" in step with "a conversation actually works."

### Agent

#### Changed

- `list_facts` was changed to pagination + keyword filtering, to keep a single call from blowing up the context when there are many facts (#74): it returns the latest 20 by default, supports `limit` (capped at 100) / a `before` cursor / a `q` summary keyword, and returns `{facts, total, has_more, next_before}`; an over-long single summary is truncated by length (the full text still goes through `node_detail`). The worker / planner prompts were updated to the paginated semantics. The old database flushes the new parameter schema into the tool catalog table via a one-time migration (`SeedTool` is insert-only on first insert, otherwise the tool management page shows "no parameters").

### Tools

#### Added

- The "Tool executions" page gains a tool call-count statistic (#72): the toolbar's "Stats" button opens a dialog showing each tool's call count, share, and failure count (sorted by count descending); it reuses the list's task / keyword filters, counts the entire result set rather than the current page, and fetches only when the dialog opens.

### Dependencies

#### Changed

- Upgraded norma v0.3.1 → v0.3.2: fixed reasoning/refusal being silently dropped in OpenAI-family responses (Responses adds `reasoning_text`), deduplication of the reasoning field's three names, bounded retry on an empty response (zero content blocks), and a gateway 400 caused by the compaction boundary severing a tool pairing (also fixing the existing orphans already baked into the transcript).

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.5] - 2026-08-25

### LLM

#### Added

- Each LLM config supports a streaming/non-streaming toggle (streaming by default): on uses streaming SSE; off uses true non-streaming (`stream:false`, returning the complete JSON in one shot), which can work around some gateways' poor SSE implementations (empty frames, dropped thinking-field frames), at the cost of losing in-flight real-time progress and real-time token counts. The worker / planner / mainagent / chat / goals all resolve the value dynamically from the currently active config; the three Provider-wrapping layers — `llmpool` / `llmrec` / the task runtime — are all compatible with non-streaming; `llm_profiles` gains a `streaming` column, `ALTER`-ed into the old database (default `true`, so old configs are unaffected).
- Support for LLM configs in the OpenAI Responses API format: each config gains a third format, `openai-responses` (hitting `POST /v1/responses`), alongside Chat Completions / Anthropic; `BaseURL` is normalized and the default model is `gpt-5`; the `format` constraint on `llm_profiles` adds `openai-responses` with an idempotent migration into the old database; the frontend format dropdown adds "OpenAI (Responses API)." Dependency upgrade: norma v0.3.1 (including the `reasoning_content` passthrough fix).
- Record the raw HTTP of LLM requests/responses: capture the real wire body at the HTTP transport layer, preserving the tool schema, `tool_use` blocks, and raw SSE frames that the normalized view doesn't show; each of norma's internal retry attempts is preserved individually; `llm_records` gains `raw_request` / `raw_response` columns, `ALTER`-ed into the old database; the recording page's detail panel adds a "raw" view toggle and request/response copy buttons (with an `execCommand` fallback compatible with non-secure contexts).

### Conversations

#### Changed

- The conversation list's multi-select was changed to a "multi-select" mode toggle: by default the list no longer keeps a per-row checkbox present (a cleaner look), and the header becomes an "N total + Multi-select" button; clicking "Multi-select" enters selection mode (checkboxes, select-all, bulk delete), "Done" exits and clears the selection, and a bulk delete that fully succeeds exits automatically (if some fail it stays in selection mode for easy retry); renaming / pinning / deleting a single item still goes through the per-row ⋯ menu.

### UI

#### Changed

- Enhancements to task management, conversation actions, and traffic viewing (#57): the task list's column-sort preference can be persistently remembered, inline renaming is now triggered directly by an icon (no longer via the menu), and the drawer (sheet) interaction and traffic-viewing details were refined.

#### Fixed

- Worker asset tags now show only the domain and IP, and the empty-tag case is handled correctly.

### Agent

#### Added

- The planner's goal judgment gains a "quantitative acceptance check": when a goal contains a quantifiable acceptance condition (asset test coverage reaching X%, capturing N flags, obtaining a certain privilege), it must check `graph_overview`'s measured values (`coverage.pct`, finding counts, etc.) before `prove_goal`, and if the measured value falls short `prove_goal` is forbidden and an intent is dispatched to close the gap instead, and it may not mark the goal met early on the grounds that "the main part is done / mostly achieved." This fixes the problem where a goal requiring 100% coverage was judged complete at a measured 40%.

#### Changed

- Rewrote the basis in the planner prompt for judging "0 intents this round": the original text described 0 intents as "the most common, most important principle," which made the planner stop too early while the goal was unmet and there were still untested surfaces in scope. It now should produce 0 intents only in two specific cases — ① every direction it can think of is already covered by an open/running/recent_done intent; ② the next step depends on the output of a currently running work and that output hasn't appeared yet (it should wait for it to finish and plan at the next wake after the graph updates). And a counter-constraint was added: when there genuinely is a new direction that isn't covered and doesn't depend on a running work, or the goal is unmet and there are still untested surfaces, don't stop just because "0 intents is common."
- Strengthened the "evidence bar for negative conclusions" in the worker prompt: for a negative conclusion that might make the planner abandon an entire direction — such as "not injectable / port closed / no login entry" — it must exhaust the reasonable techniques within that intent (varying the encoding / parameters / path / method) before concluding; if the techniques aren't fully worked through or the evidence is weak it is always marked `confidence=inferred`, to avoid welding a whole route shut with a hasty `observed` negative (an erroneous negative early in a task especially skews the direction and is hard to self-heal from).
- The above planner / worker default prompt changes are appended as a new version and switched to the current one via a one-time migration (`reseedPlannerPrompt` / `reseedWorkerPrompt`, each guarded by a settings flag); old versions are kept in the version history, and a user who has customized the prompt can restore it from the version records.

### Operations

#### Added

- Added `reset-password.sh` to reset the admin (the username is fixed as `ARTEX`) password: it supports both local / docker deployments, and the connection info can be specified explicitly or read automatically from `--dsn`/`$ARTEX_PG_DSN`/`config.json`; it uses `pgcrypto` inside the database to generate a bcrypt hash compatible with the backend login and writes it back to `settings.auth.password_hash`, with no service restart needed after the reset. The password is passed in via an environment variable, does not enter the process argv, and is escaped to prevent injection.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@neouks](https://github.com/neouks)

## [0.3.4] - 2026-08-24

### UI

#### Added

- After checking multiple tasks in the task list you can bulk-change their category, and the target category supports "Uncategorized" to move tasks out of their current category; the whole batch is written in one transaction, and the only failures would be tasks deleted after being checked.

#### Changed

- The "Task category" on the new-task form was changed from a dropdown to a searchable input box: typing filters the existing categories, and a name not in the database is created and selected instantly by pressing Enter (or clicking "Create" in the dropdown), with the selected item shown as a removable tag; it is still limited to a single category.

### Conversations

#### Fixed

- Fixed sending a message being blocked by "LLM not configured, can't chat" when the conversation had an LLM config selected but there was no globally active config. The pre-send check originally looked only at the globally active config, while actual execution prefers the conversation's selected config, so the two resolution logics were inconsistent; they are now unified into one (conversation/agent binding first, global as fallback).
- The "can't chat" message is now broken down by state: with no config at all it says "Add a config," with a config but none active it says "Activate one or assign one to this conversation," rather than always showing "not configured," making it easier to pin down.

### Agent

#### Added

- For a task whose goals are all met, an intent dispatched by the main agent via `add_intent` can be claimed and executed directly by a Worker and stops once done: at this point the Planner no longer runs (a task with no open goal doesn't enter planning), to keep it from re-judging the goals as met and cancelling the just-dispatched intent; once the frontier is drained the task returns to done. Before dispatching the intent, the main agent asks the user, based on the intent's content, whether to register it as a formal goal, and if registered the task resumes normal autonomous planning.

#### Changed

- When `get_worker_trace` / `get_task_worker_trace`'s `step_ids` exceeds the per-call cap (5), it no longer errors outright, instead returning the full content of the first 5 steps and reporting via `returned_step_ids` / `omitted_step_ids` / `notice` which were fetched this time and which weren't, with a hint "no need to fetch more if this is enough." Duplicate and invalid ids passed in are deduplicated and dropped before counting.

#### Fixed

- The task list was changed to descending task id (newest-created first), replacing the previous sort by creation time. Multiple tasks created at the same instant have identical timestamps and sort unstably, which combined with the 10-second polling and the random iteration of an in-memory map made them frequently swap positions in the list.

### Assets

#### Fixed

- Fixed company-attribution recomputation being knocked out by a single row of dirty data: when `assets.ip` holds a hostname (written by an agent or the asset API), `a.ip::inet` throws `22P02`, causing adding/editing a company asset scope and deleting a company to all fail and roll back. It now uses the safe conversion `try_inet()`, skipping an invalid value rather than aborting the whole statement.
- An unparseable `ip` is no longer skipped silently: after a company scope is saved it clearly reports how many assets' `ip` is not a valid IP (with the specific id and value), noting that these assets won't be attributed by IP/CIDR rules; the same information is also written to the server log, covering the paths with no frontend response (deleting a company, scopesentry sync, agent writes).
- The task test scope's IP/CIDR matching was likewise switched to `try_inet()`, replacing the previous regex guard that only checked the character set (a hostname of all-hex letters like `abc.def` previously still slipped through and triggered the same error).

#### Changed

- The `ip` field of `ip`, `service`, and `endpoint` assets no longer accepts a hostname: `insert_assets` and the asset API return a per-item error with an `index` and give the correction directly (use `type=subdomain` and fill `domain`, or resolve the A/AAAA record first), so the agent can fix it itself and re-insert. Other assets in the same batch are unaffected and are stored as usual.

### Contributors

- [@neouks](https://github.com/neouks)

## [0.3.3] - 2026-08-23

### Worker

#### Added

- A running Worker supports per-item pause, resume, and cancel; pausing preserves the intent, facts, and findings, and cancelling transactionally cleans up the current intent and its direct products after execution exits.
- Added a `paused` intent state, an execution barrier, and named termination reasons, to prevent late blackboard writes after a pause, cancel, or task deletion.
- Added an optional task concurrency limit; creating, resuming, finding deep-dive, and queue backfill all go through a unified persistent FIFO admission path.

#### Changed

- A Worker's single-run wall-clock default duration was changed from 600 seconds to 1200 seconds; after a timed-out task is revived, the clock restarts on the next real execution.
- The real execution state of the Worker, Planner, and main agent now uniformly drives the task status badge, fixing the Worker still showing "idle" while running.
- The Worker session keeps per-item controls and a current-session token summary, and the model name was moved to the right of the current session title.

#### Removed

- Removed the Worker multi-select, select-all, and bulk pause/resume UI, along with the Worker bulk-control API and the mock contract.
- Removed the token badge and tooltip in the Worker list rows, while the underlying full token ledger and task-aggregation API remain.

### LLM

#### Added

- A task supports an ordered LLM config chain; when it clearly detects a provider's quota is insufficient it automatically switches to the next config, and persists the current config, the exhausted state, and an error summary.
- Running and paused tasks support editing the full config chain, reordering it, and manually switching the current config, taking effect from the next LLM call.
- Automatic switches, manual switches, and full-chain exhaustion are written as structured system activity and surfaced as deduplicable alerts through the task activity stream.
- Added a task role-model resolution API that uniformly resolves the config and model the Main Agent, Planner, and Worker will use on their next call.
- Added an optional global LLM Pool, supporting a configurable call order, a specified-model failure fallback, health status, cooldown recovery, and manual reset.
- Added an always-on LLM usage ledger that aggregates input, output, and cache tokens by task, session, model, and config.

#### Changed

- Goal decomposition, the Planner, the Worker, and the main agent share the task-level LLM runtime (resolution priority below).
- Failover responds only to a clear quota, balance, or billing error; ordinary rate-limit, auth, network, server, and context errors do not trigger a wrong switch.
- The LLM config page was changed to a list of config cards with right-drawer editing, and provides the Pool's poll order, priority, exclusions, health status, and recovery actions.
- The config-chain-exhausted message supports wrapping on narrow screens; the current model was changed from an icon in the conversation list to a text label beside the current session title with a full tooltip.
- The order in which each role within a task resolves its LLM was changed to "agent binding → task config chain → global": a role with an explicitly bound model always runs on that model, an unbound role falls to the task chain, and when the task chain is empty it falls to the global.
- An empty outbound proxy means a direct connection and no longer falls back to the `HTTP_PROXY`/`HTTPS_PROXY` environment variables (an explicit `ARTEX_LLM_PROXY` is unaffected); the proxy input supports `socks5://user:pass@host:port` with credentials.

#### Fixed

- A transient streaming failure before commit (before any output has been produced) is safely backoff-retried on the same provider, significantly reducing the "interrupted after running only one or two tools, no summary, final state `model_error`" case; quota exhaustion, over-long context, and deterministic 4xx errors are not retried and are still handed to failover and compaction recovery respectively.

#### Removed

- Removed the LLM icon marker in the conversation list, to avoid confusion with the Worker status icon.

### UI

#### Added

- Tasks support creating, renaming, deleting, and filtering by global categories; a new task can select a category directly.
- Added global task-template CRUD and a right-side management drawer; a new task can load a preset description and goal, or save the current content as a template.
- A new task supports linking multiple source tasks and multiple company asset scopes; the company scope auto-recognizes domains, URLs, IPs, CIDRs, ICP filings, and company keywords from a single multi-line text box.
- A task's test assets support real-time adding and deleting, recording the source — manual, company, inherited, or agent-discovered; the current test assets and a source summary are shown beside the Worker session title.
- The Findings page groups by task and provides independent pagination within each group, and supports filling in a description for a finding and creating a high-priority deep-dive intent.
- Conversations support renaming, pinning, unpinning, and deleting; the task list supports bulk pause and resume on the current page.
- Traffic detail and tool-execution detail were changed to a right-side drawer; the HTTP message fills in `Host` and highlights the request line, status code, headers, JSON, and markup-language body.
- The task list's action column gains "Detail" and "Pause/Resume" buttons; the conversation send key can be configured in system settings (`Enter` / `Cmd+Enter`, etc.), and the web-search section gains a proxy input box.
- The badge to the right of the session title now shows the LLM config name, with the model ID moved to the hover tooltip; a task can have an optional name, and the task list gains a name column and search (falling back to the description when empty).
- The Skills page gains use-count, last-used-time, and missing-dependency statistics, making it easier to pin down a skill that isn't taking effect or isn't installed.

#### Changed

- The task title was changed to a focusable detail link; task assets, company assets, finding task-groups, and the findings within a group use real server-side pagination, stable sorting, and an exact total count.
- Adding a company now uses a right-side drawer, and scope entry is unified as multi-line text with real-time recognition, validation, and a type preview.
- The main agent input box supports auto-growth, `Enter` to send, and `Shift+Enter` for a newline, and avoids sending by mistake during a CJK IME's composition state.
- Agent previews, task reports, and related details now uniformly use a shared Markdown renderer; delete confirmations, long errors, and mobile drawer widths were fixed uniformly.
- The task-template picker displays and searches by template name and submits by template ID; the system's original font size was restored, and the app version is shown uniformly as `0.3.3`.
- The task total and current-session token counts highlight only the input, cache-read, and output values; the send button uses a cleaner up-arrow icon.
- The test scope in the task overview shows the company name rather than the ID.

#### Fixed

- Ordinary text containing a dot is no longer misjudged as an ICP filing number.
- Fixed a name collision in the variable catalog with a global runtime variable (such as `{{.Now}}`) causing the agent editor's variable list to render a duplicate key.

#### Removed

- Removed the standalone "Enter" button on the task card; clicking the task title now uniformly opens the detail.
- Removed the dashboard's "New task" button, the Logo URL input on the add-company form, and the finding-count badge at the top of the Findings page and in the task groups.
- Reverted the style that enlarged the global font by 10%, and removed the Worker checkbox, the model icon, and the row-level token statistics in the conversation list.

### Agent

#### Added

- A new task can directly link multiple existing tasks, read-only inheriting in real time the direct source tasks' goals, facts, findings, completed intents, asset scope, and blackboard context.
- The blackboard read tools support querying a source task's nodes, facts, findings, and execution traces on demand; inherited nodes carry a source marker and all write tools refuse to modify them.
- The company-scope tool supports domains, URLs, IPs, CIDRs, ICP filings, and company keywords; keywords serve only as an agent scope hint and do not participate in automatic asset attribution.
- The finding deep-dive action creates, in the original task, a high-priority manual Worker intent with an asset anchor and a `derived_from` edge.
- The overview gains a "Goal management" card, supporting manually viewing, adding, editing, and deleting goals; adding or editing notifies the Planner and revives the task, while deleting hard-deletes the goal node (cascading the edges and anchors) but does not revive it.
- The main agent gains the `steer_work` tool by default, which can inject a course-correction instruction into a running Worker in real time without interrupting it or losing progress (still verifying the intent belongs to this task).

#### Changed

- A source task's intents do not enter the new task's frontier, and the new task continues to use its own independent exploration, execution queue, working directory, and conversation history.
- Deleting a running intent no longer destroys data: it is instead stopped to `stopped`, a deletion reason is required, the reason is attached to the intent as a fact and written into the payload, and the planner perceives it via a `cancelled` trigger (keeping the intent's content and reason).
- The main agent conversation is now driven by the server-side activity stream, and the input can be recovered on a send failure; opening a conversation auto-sticks to the bottom and keeps the last reply visible after the detail lazy-loads.
- Pausing a task terminates the current Main Agent, Planner, and Worker calls, but does not prevent the user from starting a new main agent orchestration session during the pause.
- Cancellation, shutdown, and stream interruption now uniformly preserve the real termination reason, the content already generated, the number of rounds run, the elapsed time, the tokens, and any unreturned tool calls.

#### Removed

- Removed the main agent's client-side optimistic message echo, to avoid duplicate messages and cross-session bleed under a pause, a failure, or concurrent activity.

### Constraints

#### Added

- The goal-decomposition stage first extracts operation constraints (allow/deny) before decomposing goals, and the main agent can supplement them at runtime; constraints are injected into the Planner and Worker system prompts at the highest priority, and after debiasing, diversity and surface-expanding exploration explicitly obey the constraints.
- The overview gains a "Constraint management" card and create/edit/delete APIs; the constraint-injection scope can be toggled separately for the Planner / Worker (both on by default, read every round, taking effect immediately). The new `task_constraints` table uses `CREATE TABLE IF NOT EXISTS` and is auto-created in the old database.

### Intercept

#### Added

- Beyond regex/string rules, command interception gains a model fallback approval: when no rule matches, the model makes an `ALLOW`/`ASK`/`DENY` semantic decision, with the on-failure fallback action and the approval-timeout action configurable; the intercept page splits into two tabs, "Intercept rules / Model config," and a model decision result is annotated with a `[model]` prefix and a reason.

#### Fixed

- Hardened the judge's output parsing, to avoid a correct `DENY` decision being mistaken for an allow.

### Traffic

#### Changed

- Traffic recording now writes the whole exchange to SQLite (a large body overflows into a hash-deduplicated blob bucket), builds a trigram full-text index over text bodies, and supports arbitrary-substring and CJK text search; deletion degenerates to a single SQL transaction, dropping from hours to milliseconds and no longer stalling recording in the meantime. Added streaming download of large bodies and a `traffic_search` full-text parameter; old file-tree data needs no migration and can still be read, searched, and deleted.

### Tasks and assets

#### Added

- Task creation gains an "asset coverage feature" toggle (on by default): when off, coverage is not computed or shown, the situation graph shows only assets, `task_scope` is not accumulated automatically, and the related tools are removed from the Planner / main agent; company association is unaffected by this toggle.
- `insert_assets` gains a `related` marker per asset (default `true`): effective only when coverage is on, `false` only enters the shared asset store and doesn't count toward this task's coverage (such as an incidentally discovered side site or an unrelated asset).
- Company scope and task test assets uniformly support text recognition of domains, URLs, IPs, CIDRs, ICP filings, and keywords; a domain/IP can create or reuse a global asset, while a CIDR/ICP filing/keyword is kept as task-scope context.

### Build

- `build.sh --release` supports building Linux amd64/arm64, macOS amd64/arm64, and Windows amd64 in one go, and produces a zip release package with `skills/`, a config example, and a README.
- Uses the Go linker to strip debug info and produces a zip release package; UPX was made explicitly optional, to avoid its self-extracting ELF segfaulting at startup in some Linux environments.
- The Release Workflow runs a startup smoke test on the Linux amd64 binary and ships `SHA256SUMS` with the release package.

### Contributors

- [@neouks](https://github.com/neouks)

## [0.3.2] - 2026-08-20

### Added

- A new task supports linking multiple existing tasks, inheriting the direct source tasks' facts, findings, completed intents, asset scope, and blackboard context in real time and read-only; the new task still uses its own execution queue, working directory, and conversation history.
- A task supports an ordered LLM config chain. When it clearly detects a provider's quota is insufficient it automatically switches to the next config, and persists the current config, the exhausted state, and structured audit activity.
- Running and paused tasks support editing the LLM config chain, reordering it, and manually switching the current config; automatic switches, manual switches, and full-chain exhaustion are surfaced on the task detail page.
- A running Worker supports pause, resume, and cancel. Pausing preserves the blackboard data, and cancelling transactionally cleans up the intent and the facts, findings, and execution records it directly produced after the Worker stops writing.
- Deleting a task can optionally also clean up the associated assets, traffic, findings, and the task's on-disk files, and adds a deletion barrier, concurrency protection, and auditable deletion statistics.
- Added an optional task concurrency limit; new tasks that hit the limit queue in FIFO order and start automatically once a run slot frees up.
- Added task-asset pagination, company-asset pagination, task-category statistics, and paginated intent mock contracts.
- Added the `build.sh` single-binary build script, supporting static frontend export, asset embedding, cross-platform targets, and build-version injection.

### Changed

- A task-level explicit LLM config chain coexists with the existing global LLM Pool; the explicit chain continues to use strict quota-failover semantics, and without an explicit chain it follows the agent-binding and global-config rules.
- The main agent input box supports auto-growing multi-line input, `Enter` to send and `Shift+Enter` for a newline, and avoids sending by mistake during a CJK IME's composition state.
- The Planner, Worker, and main agent share the task-level LLM runtime; the task chain uses the smallest context window among the candidate models as the safe compaction threshold.
- The task detail page shows running, idle, and paused states based on the real LLM-call status; a source task's facts, findings, intents, asset references, and graph nodes are uniformly marked with their source and kept read-only.
- Agent previews, task reports, and related detail views now uniformly use a shared Markdown rendering component.
- LLM model configuration uses a card-and-drawer interaction, and the model list scrolls independently within the drawer.
- Pausing a task no longer blocks the main agent conversation: the main agent orchestration session is independent of the task pause, and new messages can still be sent during the pause (the pause only terminates the round currently in progress).
- A Worker's single-run wall-clock default duration was changed from 600 seconds to 1200 seconds.

### Fixed

- Removed the main agent console's optimistic echo in favor of pure server-side data rendering, fixing garbled messages and content bleeding into other conversations under scenarios like a pause or a send failure.
- Opening a main agent conversation scrolls to the bottom by default and shows the last reply in full: the last reply's full content auto-sticks to the bottom after lazy-loading, no longer being pushed off the screen.
- Fixed the main agent conversation still running after a task was paused, and the main agent not being stopped in sync when the orchestration agent paused the task.
- Fixed the task status badge and the action buttons being out of sync when a task completes or when the Planner, Worker, or main agent is actually running.
- Fixed possible late blackboard writes or residual files arising between Worker cancellation, task deletion, and concurrent writes.
- Fixed broken Markdown previews, long delete-confirmation text overflowing, mobile width, and some misaligned task-detail buttons.
- Added named termination reasons to all agent cancellation paths, and the activity detail can show the canceller, the final state, the number of rounds run, the elapsed time, the token usage, and any unreturned tool calls.
- Fixed a race where the parent context could preempt and overwrite the named `shutdown` reason when the backend shuts down, and a CJK activity summary being truncated by bytes into garbled text.
- Fixed a cancellation event with partial streaming output losing the real termination reason, while preserving the content already generated before cancellation.

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

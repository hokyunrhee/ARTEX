# English migration design

Design-only assessment of ARTEX at `160fe13`. No repository edits or database tests were made by this assessment. The requested attachment was read in full. The current environment supports Docker and already has the required toolchain; environment assumptions in the attachment are historical.

## Frozen evidence available before translation

The root agent captured a clean, original-binary startup on `artex_english_original`, separate from all test databases. These external files are the authoritative **runtime** defaults after all original startup migrations:

- `legacy-agents.json`: 8 rows, including the editable reporter and retester.
- `legacy-agent_prompts.json`: 8 rows; preserve exact template bytes when calculating hashes.
- `legacy-agent_prompt_vars.json`: prompt variable hints and examples.
- `legacy-tools.json`: all 51 tool descriptions, full JSON schemas, bindings and enabled flags. This includes server-private orchestration/platform constructors as well as agent and traffic tools.
- `legacy-intercept_rules.json`: all 20 final rule tuples.
- `legacy-asset_intercept_rules.json`: 4 built-in blocklist notes.
- `legacy-settings.json`: original migration flags. Treat the **names** as metadata; no configuration values from an existing user database belong in generated fixtures.

`prompt-hashes.json` was independently extracted using `extract-prompt-hashes.go`, a standard-library Go AST evaluator operating on committed Git blobs, not the working tree. It resolves raw/interpreted string literals, constant references and concatenation exactly as Go does. There are 78 examined revisions and no unresolved constants. Distinct recovered bodies: goals 2, planner 11, mainagent 5, worker 12, auto 1, pentest 2, reporter 3, retester 1; additionally the custom-agent assistant fallback has 1 body. The JSON records every observed revision for provenance and the base digest separately. Prompt bodies do not need to remain in the migrated source.

Before translation starts, also freeze these exact short compatibility values from `git show 160fe13:<path>`:

1. `reporterToolCallMessageV1` and the current `reporterToolCallMessage` (the latter becomes V2).
2. `server/finding_workflow.go`'s already-frozen traffic-search description (V2), and `traffic.TrafficSearchDescription` (V3).
3. The legacy judge output/context blocks; unlike editable agent prompts, these blocks must remain text for exact replacement and dual parsing.
4. Intercept seeds' historical display names/messages and pattern/target identities. Keep final runtime tuples plus source defaults when a historical rule changed in Git.
5. Exact system-generated persistence phrases used in SQL/Go cleanup. Do not infer these by broad Chinese-text matching.
6. Default reporter/retester name+description pairs, and the current default conversation title.

Every current `legacy-agent_prompts.json` body was verified against `base_sha256`: all eight match (goals, planner, mainagent, worker, auto, pentest, reporter, retester). Canonicalize JSON schemas through JSON parsing/serialization or PostgreSQL `jsonb`; do not compare whitespace-dependent JSON serialization.

Do not freeze user credentials. All baseline fixtures come from a newly created database seeded only by the original binary. Keep fingerprints in dedicated files and record each deliberate CJK literal line in the eventual allowlist. All final code comments around these literals should be English.

## Startup placement and ordering

Current startup is:

1. `db.Open`: apply schema under migration lock `7337741001`, then `seedBuiltins`; names/descriptions and prompt-variable metadata are refreshed here. Rule seed flags are evaluated here.
2. `server.New`: `wireTools` seeds agent+traffic tools; `seedPrompts` inserts absent built-in bodies.
3. `seedOrchestrationTools`: seeds server tools; runs historical schema refresh, historical prompt reseeds, reporter creation, reporter trigger upgrade and evidence/workflow migrations.
4. `seedFindingRetester`: creates retester in its own transaction and advisory lock `7337741010`.
5. Background scheduler/notifier/discovery and provider initialization begin.

Place the server-level English migration **after retester seeding and before any background goroutines or provider initialization**, so all 8 agents and all 51 tools exist before inspection. Put rule/note migration at the end of database seeding, after historical rule seeds.

Important preservation hazard: four old `reseed*Prompt` functions reset every non-current body, even a user customization, whenever an old flag is absent. `refreshBuiltinToolSchemas` similarly replaces descriptions/schemas unconditionally when its old flag is absent. Merely adding a new safe English pass after them is insufficient. Change these old entry points to use the same legacy-compare guard, or retire their unconditional mutations in favor of the safe migration while preserving any necessary structural defaults. Tests must explicitly cover an old database with **none** of these historical flags and customized rows.

Do not bump `tool_schema_refresh_v7_list_facts_paging` to deliver translations. Do not call `UpsertToolForce` during automatic migration: that API resets bindings and enabled state by design.

## Migration contracts

### Editable prompt bodies

Maintain `agent/legacy_prompts.go` containing base SHA-256 values plus recovered historical sets. Expose a small `IsLegacyPromptDefault(key, body)` helper; keep comparison exact (no TrimSpace/Unicode normalization). Unknown bodies are operator-owned even if mostly Chinese or nearly identical to a default.

Use one new `*_prompt_english_v1` flag for each of goals, planner, mainagent, worker, auto, pentest, reporter and retester. For each current row:

- If absent, let the normal creation/seed policy decide; a deleted reporter/retester must not be resurrected by English migration.
- If equal to the current English default, mark the migration complete without appending a duplicate prompt version.
- If its digest matches a frozen default for the same key, append the English body as a new version and move `current_prompt_id`, preserving all old history.
- Otherwise preserve it exactly and log a single English message identifying the agent key, never its prompt body. Mark the one-time inspection complete so restarts do not repeat the log.
- On a real database error, leave the flag unset and retry on the next startup. Do not use the old unconditional `defer SetSetting` pattern.

The exact generic `DefaultAssistantPrompt` is also persisted when operators create custom agents (`pgCreateAgent`, server/server_mgmt.go). Its base hash has been captured. A narrowly scoped `assistant_default_prompt_english_v1` pass can safely update custom agents whose current body is exactly this shipped starter body, while preserving all other custom content. If this pass is omitted, document that existing custom-agent starter prompts remain Chinese; that would be a gap against the broad existing-install goal.

Use transactional compare-and-append with a row lock on the agent, or an equivalent compare-and-swap on the expected `current_prompt_id`. A check outside the transaction followed by `ResetPromptToDefault` can overwrite a concurrent operator edit. The clean implementation is a DB helper using the same append/history semantics as `ResetPromptToDefault`, with the expected current ID/body checked under lock and flag committed atomically. Consider sharing this lock discipline with `SavePrompt` to avoid concurrent version-number conflicts, without changing its external behavior.

Prompt reset UI exists for the six `BuiltinPromptSeeds` keys. Reporter/retester currently return HTTP 400 from `pgResetPrompt` because they are editable custom agents outside that map. Do not promise a reset button for them in documentation without implementing and verifying support. Manual prompt editing remains available through `PUT /api/agents/{key}/prompt`, and history through `GET /api/agents/{key}/prompts`.

### Reporter/retester metadata and trigger

Use `reporter_agent_english_v1` and `retester_agent_english_v1`. The metadata update must match **both** the original name and description for the corresponding key. Preserve enabled/role/profile/budgets/triggers/bindings and all other fields; do not recreate deleted agents. Rows with either customized name or customized description stay untouched and are logged once.

Freeze the current reporter message as `reporterToolCallMessageV2` without translating V1. Under `reporter_trigger_english_v1`, update only reporter triggers with `on_tool_call=true` and a message exactly equal to V1 or V2. Target only the message column with a value predicate; avoid loading and resaving the whole trigger because that could overwrite concurrent edits to other fields. Preserve its requirement to read `get_finding_traffic`, use `finding_node_id` in `update_finding_report` and always pass the returned `evidence_version`.

### Tool defaults

Generate frozen metadata from all 51 runtime tools. Retain original key, description and JSON schema. Agent bindings, enabled state, custom execution settings and deferred state are never updated by the English pass.

Use `tool_defaults_english_v1`, with separate guarded updates per column:

```sql
UPDATE tools SET description=$english,updated_at=now()
WHERE key=$key AND system AND description=$legacy;
UPDATE tools SET schema=$english::jsonb,updated_at=now()
WHERE key=$key AND system AND schema=$legacy::jsonb;
```

This intentionally permits updating an untouched description while preserving a customized schema, and vice versa. The full-schema comparison protects custom parameter defaults, unknown properties and other non-description metadata. A custom schema remains intact even if some unchanged parameter descriptions stay Chinese. Log preserved customized columns once; the operator can explicitly reset them if desired. Do not merge translated defaults into a custom schema automatically.

Add `finding_workflow_tools_v4_english` specifically for `traffic_search`, allowing both the frozen V2 description and V3 current description. The existing frozen V2 literal in `finding_workflow.go` must remain verbatim. Its old v3 migration can route V2 to the new English text safely because it already compares exact text, but the new flag is needed for databases whose previous migration is already complete.

Tool extraction covers built-in SDK tool metadata exposed by `BuiltinToolSeeds`, not just files with visible Chinese. Some constructor schemas may already be English; matching those is harmless if the target is identical, but avoid unnecessary `updated_at` changes where practical.

### Built-in metadata, rules and notes

The six built-in agent name/description fields and prompt-variable hints/examples already update on conflict. Translate their code defaults; retain identifiers and rendering tokens. The user specifically accepts the existing auto-propagation policy for these rows. The API disallows editing built-in metadata, while reporter/retester remain editable and need guarded updates as above.

`intercept_rules` has no built-in discriminator and no unique constraint. Under `intercept_default_rules_english_v1`, compare key tuple `(pattern, match_target, match_type, name, message)` to each frozen seeded default, then set **only** name/message. Preserve priority, enabled, action, timeout fields, and every nonmatching row. Byte equality is the requested definition of an untouched default. Multiple identical seeded rows can be translated together; do not introduce deduplication.

Under `asset_intercept_default_notes_english_v1`, update notes only where `builtin=true`, kind/pattern equal the frozen identity and note equals the original note. Never recreate a deleted blocklist row.

Keep historical seed flags in place and translate their fresh-install source labels. V3's NOT EXISTS-by-name must accept both the old and English names if it runs before the English migration on a partially upgraded database, otherwise changing the seed label could duplicate a still-Chinese row when its v3 flag is absent.

### Historical display-only values

Translate producers immediately and add narrowly guarded idempotent cleanup for exact known system strings where cheap. Candidate columns: `task_asset_links.source_summary`, `task_scope.reason`, `finding_retests.error`, `notification_deliveries.last_error`; also known restore warnings and generated status strings. Prefix rewrites must be tied to the relevant system source/kind and preserve the arbitrary user suffix byte-for-byte (for example, a company name after the fixed creation-time prefix). Do not translate every Chinese title/body/activity row: those can be operator data.

Keep schema's shipped model-source backfill unchanged. Append the required reason prefix rewrite after it; readers permanently support both `[model]` and the frozen legacy prefix, including archived/audit JSON. This belongs to the §4A owner but must run before runtime verification.

### Notifications

Do not rewrite operator filter JSON or webhook template variable names. Chinese `vulnclass_include`/`exclude` values can stop matching English classes; warn in CHANGELOG and README. A small, explicitly documented alias expansion is optional, but should not broaden unrelated substring matching silently. `.Title`, `.SeverityLabel` and `.StatusLabel` values become English while the template identifiers remain unchanged.

## Required upgrade tests

Use fresh isolated PostgreSQL databases, serial packages (`-p 1`), and disable the opt-in live model test. Do not run alongside the root baseline. Use named fixtures with only the specific old defaults needed for each test; frozen historical digests can be tested without retaining whole prompt bodies in committed tests by passing body hashes through pure helpers and constructing DB upgrade fixtures from the external original snapshot during integration verification.

1. Fresh install: all eight agent bodies/default metadata and 51 tool defaults are English; flags finish; a second startup creates no new versions/duplicates.
2. Upgrade each of the eight base bodies: append exactly one English version, retain old row bytes and history, and select the new row. Historical digests are recognized by pure tests and representative actual legacy bodies by the runtime upgrade fixture.
3. One customized prompt, one customized description, one customized tool schema/default, one renamed reporter, one edited intercept rule and one edited asset note: every changed value survives unchanged. Also test only one of description/schema customized.
4. Preserve disabled tools/rules, tool bindings, rule action/priority/timeouts, custom agents and custom tools; missing reporter/retester remain missing when their original seed flags are complete.
5. Repeat upgrade with all old prompt-reseed and tool-refresh flags absent, proving old entry points cannot wipe customization before the English pass.
6. Reporter V1/V2 triggers both upgrade; altered message and unrelated trigger fields survive. Repeated migration is a no-op.
7. Traffic-search V2/V3 descriptions both upgrade; customized description survives; existing `TestFindingTrafficToolUpgradePreservesCustomization` and `TestFindingWorkflowMigrationPreservesUserConfiguration` continue to pass.
8. Legacy schema JSON with different key ordering still upgrades; customized nested parameter defaults stay byte-equivalent as JSONB.
9. Failure before commit leaves flags unset; a second run succeeds. Concurrent startup does not append duplicate prompt versions or overwrite a concurrent edited row.
10. Runtime upgrade fixture: old binary seeds, API customizes one prompt and one tool, inserts an old intercept reason/conversation token, then new binary boots. Verify all migration outcomes, permanent dual reads, old judge contract replacement, and English generated output without any model request.

## Glossary contract for translators

Use US English and sentence case. Keep API keys, enums, routes, environment variables, template placeholders and tool identifiers unchanged. A repository glossary should use English concepts/code identifiers as the source column so the required no-CJK gate does not need an unrequested glossary exception; the canonical rendering still captures each source term's meaning.

| Concept or identifier | Canonical rendering |
| --- | --- |
| intent | intent |
| fact | fact |
| finding record | finding |
| security flaw as a concept | vulnerability |
| asset | asset |
| company | company |
| company/task authorization boundary | asset scope |
| exploration chain/graph | exploration graph |
| asset coverage visualization | asset coverage map |
| btw/sidequestion | side question |
| intercept approval flow | intercept approval |
| stored approvals | approval records |
| human participation | human-in-the-loop |
| hint | strategic hint |
| goal | goal |
| goals agent | goal decomposition |
| planner | planner |
| mainagent | main agent |
| worker | worker |
| reporter | reporter |
| finding retest | finding retest |
| settlement/wrap-up prompt | wrap-up prompt |
| circuit-breaker behavior | circuit breaker |
| ICP registration | ICP filing |
| traffic capture/recording | traffic recording |
| evidence | evidence |
| push/notification | notification |
| digest delivery | digest mode |
| trigger | trigger |
| reusable task definition | task template |
| constraintBlock heading | Operation constraints |
| artifactSpec heading | Intermediate artifact output rules |

Severity labels: Critical, High, Medium, Low. Finding status labels: Pending, In progress, Confirmed, Resolved, Fixed, False positive, Ignored, Duplicate, Risk accepted. Delivery labels: Pending, Sending, Delivered, Skipped (and Failed where the existing enum requires it). These values must be identical in Go notifications and the frontend. Labels must not contain markdown metacharacters `*_()[]`; retain the existing severity emoji.

Length conversion ledger: enforceable rune limits remain the same number of **characters**, including 100-rune fact/intent summaries and AbortCause Short <=40 runes. Descriptive prompt guidance measured in Chinese characters can scale about 2.5x for English (judge rationale 120 becomes about 300 characters), always beneath ParseVerdict's 2400-byte cap. Record each individual scaled directive while translating; do not adjust byte/array/JSON limits implicitly. UI input-length changes require explicit review and matching backend-limit verification.

## Baseline graph-overview failure: root cause and minimal correction

`TestGraphOverviewExpandsAssociatedCompanyScope` asserts `coverage.scope`, `company_scope` and `company_keywords` at agent/blackboard_inheritance_test.go:85-104. Upstream commit **06a43f37ffa1ce664089994eca43d242f4fc242d** intentionally removed the complete `coverage.scope` output and its company enrichment from graph overview on September 24, 2026; its commit message and diff explicitly state this. The test was not updated. Current graph output intentionally carries aggregate coverage metrics and `host_count`; it does not promise inline scope details. Task creation still writes the company task_scope row and links company assets correctly.

Do not reintroduce removed production payload just to satisfy this stale test. A small test-only fix is justified as its own commit: rename the test to `TestGraphOverviewPreservesAssociatedCompanyAssets`; assert the persisted single company reference via `assets.ListTaskScope(task.ID)` and all five configured inputs via `companies.GetScope(companyID)`/Raw values; assert the graph overview omits `scope`; retain all existing coverage/host count, asset provenance, tool query, intent anchoring and coverage-disabled checks. This replaces an obsolete contract assertion with its current explicit contract without skipping/deleting the test or reducing the asset-association coverage. No code or tests were changed by this assessment.

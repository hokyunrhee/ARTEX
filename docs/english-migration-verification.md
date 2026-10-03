# English migration verification

This report records completed checks against upstream `160fe13c243be361eeac5408c40a96e824fa842c`. Translation slices, compatibility migrations, independent reviews, two fresh full Go suites, and the original-binary upgrade scenario have completed. The normal embedded Linux release, all 98 touched frontend files, and both fresh/upgrade runtime paths also pass. Final commits are validated independently before the requested push.

The checkout is `/workspace/ARTEX`. Raw logs and scratch fixtures live outside the repository under `/workspace/.artex-cloud/english`; paths below are relative to that evidence directory. No production credentials, local authentication files, database dumps, generated binaries, or screenshots are included in this document.

## Scope and counts

The baseline inventory scanned 580 source/text files and found 347 files containing 12,750 lines with CJK or full-width characters. The following are **baseline affected-file and hit-line counts by area**, not net removed-line counts. Some original lines are deliberately retained, and exact historical literals were added in dedicated compatibility files. Reflowed English lines and newly added tests/migration files are not counted as additional translated baseline lines. The final allowlist is the authority for retained exceptions.

| Baseline area | Affected files | CJK/full-width hit lines |
| --- | ---: | ---: |
| `.github` | 1 | 10 |
| `agent` | 37 | 1,038 |
| `cmd` | 1 | 5 |
| `config` | 1 | 4 |
| `db` | 54 | 1,262 |
| `docs` | 1 | 43 |
| `enrich` | 1 | 3 |
| `evidence` | 1 | 5 |
| `guard` | 1 | 7 |
| `intercept` | 7 | 137 |
| `llmpool` | 2 | 11 |
| `llmrec` | 1 | 6 |
| `mcphttp` | 1 | 3 |
| `notify` | 26 | 1,347 |
| `report` | 2 | 55 |
| `root` | 15 | 907 |
| `selfupdate` | 5 | 303 |
| `server` | 76 | 2,035 |
| `sidequestion` | 9 | 169 |
| `skills` | 7 | 588 |
| `traffic` | 5 | 195 |
| `web` | 1 | 10 |
| `web/src/app` | 11 | 281 |
| `web/src/app/function` | 22 | 1,480 |
| `web/src/app/system` | 16 | 1,063 |
| `web/src/components` | 24 | 732 |
| `web/src/config` | 1 | 2 |
| `web/src/hooks` | 1 | 2 |
| `web/src/lib` | 15 | 1,024 |
| `web/src/navigation` | 1 | 19 |
| `web/src/proxy.ts` | 1 | 4 |
| **Total** | **347** | **12,750** |

The core prompt/backend slice translated 1,318 original hit lines across its 49 Go files, retaining 11 intentional matching/test-vector lines (`agent-counts.json`, `agent-ledger.md`). The documentation/skills/scripts slice changed 28 existing files and added two gate files, covering 1,692 baseline hit lines; its only literal CJK residue is two target-authentication matchers. The frontend lint manifest contains 98 touched paths; the standard Biome invocation checked 96, with two previously excluded files checked explicitly under the same rules. These slice figures overlap the area inventory and must not be added to it.

The entire CHANGELOG history was translated: all 14 version/date headings, 279 original top-level bullets, and contributor links were retained. The evidence document is now [vulnerability-traffic-evidence.md](vulnerability-traffic-evidence.md). The [glossary](glossary.md), [migration design](english-migration-design.md), and [baseline plan](english-migration-plan.md) document terminology and compatibility policy. `/docs/` remains ignored; intended documentation files require explicit force-add and are force-added in the documentation commit.

## Baseline and Go verification

Commands run from the repository root, with the pinned toolchain environment loaded:

```sh
source /workspace/.artex-cloud/env.sh
go build -p 1 ./...
go vet -p 1 ./...
gofmt -l .
# Each database-backed run uses a separately created disposable database.
go test -p 1 -count=1 -json ./...
# The no-database comparison explicitly removes the DSN.
env -u ARTEX_PG_DSN go test -p 1 -count=1 -json ./...
```

Counts include test/subtest outcomes, not just package totals.

| Run | Passed | Failed | Skipped | Exit |
| --- | ---: | ---: | ---: | ---: |
| Original, first fresh database | 892 | 2 | 1 | 1 |
| Original, second fresh database | 892 | 2 | 1 | 1 |
| Original, no database | 581 | 22 | 233 | 1 |
| After isolated baseline test fixes | 894 | 0 | 1 | 0 |
| Final backend, first fresh database | 994 | 0 | 1 | 0 |
| Final backend, second fresh database | 994 | 0 | 1 | 0 |
| Final backend, no database | 652 | 22 | 247 | 1 |

Both final fresh runs have 15 passing packages and two packages without tests. The sole test skip is the opt-in external-model `TestLiveContextReview`; no migration test was skipped to obtain these results. The no-database run has exactly the original 22 failing server test names; additional database-dependent tests account for the larger skip count. Raw evidence: `baseline/go-summary.json`, `baseline/go-fixed.json`, `final-tests1.json`, `final-tests2.json`, `final-test-exits.json`, `final-tests-nodb.json`, and `final-nodb-exit.txt`.

The separate baseline-fix commit corrected an obsolete graph-overview assertion and isolated a leaking chat/provider fixture. Upstream had deliberately removed inline `coverage.scope`; the test now verifies stored scope, assets, and provenance without restoring removed product behavior. The chat fixture closes its manager after profile cleanup and cancels background work. These fixes eliminate both reproducible fresh-database failures without deleting or skipping tests.

Build and vet passed after the integrated contract, core, backend, and notification slices; their `*-integrated-build.log` and `*-integrated-vet.log` files contain no diagnostics. Formatting now lists only these three untouched baseline files (`final-gofmt.txt`):

```text
db/task_scope_test.go
server/assembly_test.go
server/tools_wire_test.go
```

## Compatibility and migration checks

| Request item | Implemented behavior and verification |
| --- | --- |
| 4A.1 Model decision prefix | Producers emit `[model]`; Go/SQL/UI readers accept current and legacy prefixes in rows, audit snapshots, and restored archives. An appended idempotent SQL rewrite changes historical row prefixes while preserving the shipped backfill. Mock data and regression tests use the same policy; runtime approval classification returned `model`. |
| 4A.2 Mention wire format | New tokens use `finding`, `asset`, `company`, `api`, `ip`, `app`, `domain`, `subdomain`, and `service`. Both parsers permanently accept old tokens. Snapshot headers/truncation labels are English. IDs, escaped labels, cursor behavior, and adjacent references remain covered by Node/Go tests; the original conversation survived upgrade. |
| 4A.3 Judge output | Current English segment markers and legacy verdicts both parse. The effective prompt replaces both known legacy contract blocks exactly once while retaining operator policy. Stored custom prompt text remains unchanged; replacement occurs when composing the effective prompt. `TestParseVerdictLegacyCompatibility` and `TestEffectiveJudgePromptReplacesLegacyBlocks` pass. |
| 4A.4 Stored activity parser | New summaries use `Tool X requests approval (#N)`. The transcript parser extracts the tool from either the English or legacy pattern, retaining the `(#N)` approval identity and stored activity rendering. |
| 4A.5 Default conversation title | New generated titles use `New conversation`; default-title recognition accepts the historical title and empty value. Custom titles survive. Backend, UI fallbacks, and mock generation agree; `TestDefaultConversationTitleCompatibility` passes. |
| 4A.6 Intra-file label equality | Transcript rendering and command-payload comparison share `COMMAND_LABEL = "Command"`; control flow no longer duplicates translated display text. |
| 4A.7 Log level classifier | `levelOf` recognizes actual English error/warning vocabulary, including failed/dropped/rejected/refused/unreachable and retry/retrying, while retaining existing English/emoji markers. `TestLevelOfEnglishVocabulary` covers casing, error precedence, warnings, and ordinary info messages. |
| 4A.8 Trigger separators | Merged messages use stable `-- Trigger N --` decorations and English task counts. Tests count the ASCII separator and verify a task header/goal appears only once for merged or individual fires. |
| 4A.9 Prompt section anchors | `Operation constraints` and `Intermediate artifact output rules` agree between headings, seeded bodies, references, and prompt assertions. Changed bodies participate in guarded reseeding; placeholder names remain unchanged. |
| 4A.10 API error control flow | `ApiError` carries stable HTTP status. Skill overwrite confirmation uses 409 and restored-archive pruning uses 404, with narrow English-text fallbacks only for legacy plain errors. Producer messages, mock errors, and Node contract tests were updated together. |
| 4A.11 Skill anchors | The translated API reconnaissance and ScopeSentry headings have matching GitHub-style internal anchor links. All 52 local links/fragments across 19 documents pass; malformed ScopeSentry frontmatter remains intact. |
| 4B.1 Prompt defaults | Exact SHA-256 matches include the original eight agent bodies and recoverable historical defaults. Upgrades append versions and retain history; unknown/custom bodies survive and are logged once. Generic starter prompts on custom agents also use exact historical digest matching. Target-readiness checks prevent premature completion flags; old unconditional reseed entry points now use preservation guards. |
| 4B.2 Reporter/retester | Metadata changes require both legacy name and description to match; missing/deleted agents are not recreated by translation. Prompt bodies use the same digest policy. Reporter V1/V2 trigger fingerprints stay frozen and only exact matches upgrade; changed trigger text and unrelated trigger fields survive. Retest verdict codes remain `reproduced`, `fixed`, and `inconclusive`. |
| 4B.3 Tools/default schemas | All 51 constructor defaults are covered by frozen metadata. Description/schema columns compare independently; customized nested schema defaults, custom tools, enabled state, bindings, and deferred settings survive. Old refresh paths are guarded even without old flags. The traffic-search pass accepts exactly V2/V3 historical descriptions and preserves custom descriptions. |
| 4B.4 Auto-propagating rows | Built-in agent names/descriptions and prompt-variable hints/examples use their existing conflict-update policy; identifiers and template-variable keys remain unchanged. Runtime upgrade queries confirmed English built-in metadata. |
| 4B.5 Rule seeds and notes | Rule matching uses the full frozen pattern/target/type/name/message identity and changes only name/message. Edited rules, enabled state, action, priority, and timeouts survive. Built-in asset-note updates require exact kind/pattern/note matches. Tests cover disabled/customized rows and absent old flags without duplicate rules. |
| 4B.6 Persisted display values | New SQL/Go producers emit English task associations, retest/deletion diagnostics, restore warnings, notification statuses, and delivery errors. Idempotent cleanup targets exact known system values for company-source summaries, manual company scope reasons, and disabled-channel delivery errors; company-name suffixes remain byte-for-byte. Arbitrary historical user content is not bulk-translated. |
| 4B.7 Notification filters/templates | Filter matching remains case-tolerant and operator keyword JSON is not rewritten. README/CHANGELOG call out reviewing localized `vulnclass_include/exclude` values. Template fields retain their names, while `.Title`, `.SeverityLabel`, and `.StatusLabel` values become English. |
| 4C Coupled assertions | Production messages and their assertions/fixtures were translated together. Cancellation codes remain stable; short cancellation labels stay within 40 runes. Finding evidence-status codes, guard messages, skill ZIP format names, FTS fixture/search pairs, notification labels, and negative packing assertions retain coverage in both full passing suites. |
| 4D Frontend contracts | Keys, enums, routes, graph NUL delimiters, pagination, and status semantics remain unchanged. Explicit Chinese locales became US English. Independent review corrected JSX spacing and plural forms; screenshots cover collapsed sidebar, command palette, and every task tab. |
| 4E Report exports | Task/finding Markdown and CSV render English. The CSV UTF-8 BOM and Unicode filename whitelist remain intact; runtime render checks passed. |

Migration scenario tests in the two passing full suites additionally cover unchanged versus edited prompts, repeat-start idempotence, preserving old history, a concurrent operator prompt edit, rollback/retry after an injected transaction error, four concurrent startup attempts, independently untranslated description/schema targets, custom descriptions/schemas/both/custom tools, renamed reporter/retester metadata, missing-agent non-resurrection, edited/disabled rules and notes, and absent historical reseed flags. Evidence resides in `db/english_test.go`, `server/english_test.go`, and the complete JSON event logs. The actual old-binary upgrade below supplements these targeted cases; fresh embedded-release seed verification also passed (eight prompts/agents, 51 tools, and 20 rules).

Actual migration settings include `<agent>_prompt_english_v1`, `tool_<key>_description_english_v1`, `tool_<key>_schema_english_v1`, `<agent>_agent_english_v1`, `reporter_trigger_english_v1`, `finding_workflow_tools_v4_english`, `intercept_default_rules_english_v1`, and `asset_intercept_default_notes_english_v1`. Customized values are preserved and logged once per completed inspection; failed transactions leave the corresponding migration unfinished for retry. Explicit reset controls remain available where already supported; reporter/retester prompt bodies can be edited manually.

### Original-binary upgrade evidence

The actual original binary seeded `artex_english_original`. Its API then customized the worker prompt and the `report_finding` description, bindings, and enabled state. Fixtures added a historical model decision, legacy audit prefixes and mention tokens, and an operator judge policy; old reseed flags were removed to test preservation through earlier startup paths. The English binary then opened the same database.

```sh
python3 /workspace/.artex-cloud/english/verify-upgrade.py
go run /workspace/.artex-cloud/english/runtime-render.go
python3 /workspace/.artex-cloud/english/runtime-notify.py
```

`upgrade-verification.json` records:

```json
{
  "tool_count": 51,
  "rule_count": 20,
  "english_default_prompts": 7,
  "custom_prompt_preserved": true,
  "custom_tool_fields_preserved": true,
  "legacy_model_rewrite": true,
  "legacy_audit_and_mentions_retained": true,
  "operator_judge_policy_preserved": true
}
```

The seven unchanged default prompt bodies became English. The customized worker body and its two versions remained unchanged; the seven upgraded agents each retained their old version and gained one English version, giving 16 total prompt-history rows. All 51 tool descriptions/schemas, default agent metadata, 20 rule labels/messages, and built-in asset notes were checked for English; the custom finding-tool description/bindings/disabled state survived. Startup logs contain one preservation notice for each customized field exercised.

Generated-output/runtime snippets (`runtime-render.log`, `runtime-notify.log`):

```text
PASS: stored operator policy preserved; both legacy judge blocks replaced exactly once
PASS: task/finding Markdown and CSV render English; CSV BOM retained
PASS: notification test endpoint delivered English JSON to a local receiver
PASS: approval API classifies migrated reason as model; conversation API preserves legacy mentions
```

These checks exercised the backend API and rendering functions. Restarting the final embedded binary against the upgraded database again preserved all custom values and 16 history rows, without repeating preservation notices (`upgrade-second-verification.json`).

## Frontend, browser, skills, and scripts

Final frontend evidence:

```sh
cd web
npx tsc --noEmit -p tsconfig.json
node --test src/lib/*.test.mjs
npx biome check --reporter=json
python3 - <<'PY_CHECK'
import json, subprocess
with open('/workspace/.artex-cloud/english/frontend-validation/touched-files.json') as source:
    touched = json.load(source)
subprocess.run(['npx', 'biome', 'check', *touched, '--reporter=json'], check=True)
PY_CHECK
NEXT_PUBLIC_MOCK=1 npm run build:static
# Preserve that mock export separately, then rebuild without NEXT_PUBLIC_MOCK.
npm run build:static
```

The integrated main checkout passed TypeScript, Node tests, and its normal static build. Node reports `tests 10`, `pass 10`, `fail 0`, `skipped 0`. The standard Biome invocation reports zero errors/warnings/information in its 96 checked touched files; two additional touched files previously excluded by configuration also pass an explicit check under the same rules. Repository totals are 9 errors, 3 warnings, and 8 informational diagnostics, all in untouched files. This improves the original 134/138/727 totals. The normal static build completed all 31 pages. The latest main-checkout results were confirmed by the integrating agent. All 98 touched paths have zero diagnostics. The explicit UI check uses an isolated copy with the same rules and includes those two files; the committed Biome configuration is unchanged.

Browser evidence uses system Chromium through Python Playwright, because the request's preinstalled CLI paths do not exist here. `frontend-tools/browser-review.py` exercised the saved mock export at 1440 by 900 and wrote 23 screenshots, including all 10 task-detail tabs, under `frontend-validation/screenshots`. Its report records zero page runtime errors, zero visible CJK, and zero page-width overflow. Routes cover dashboard, tasks, findings, assets, chat, agents, intercept settings, approval records, notifications/channel form/history, collapsed sidebar, and command palette. These are current review artifacts, separate from the deliberately unchanged upstream screenshots in the repository; the integrating agent confirmed the final 23-screen review after the overlap corrections.

```sh
for script in *.sh scripts/check-no-cjk.sh; do bash -n "$script"; done
bash build.sh --help
bash reset-password.sh --help
python3 /workspace/.artex-cloud/english/inventory/verify_docs.py
```

All six root shell scripts plus the gate passed syntax checks. All four skill Python files parsed and both JavaScript files passed `node --check`. `start.bat` retains all 54 CRLF lines and `chcp 65001`; translated executable parentheses are escaped correctly. The malformed ScopeSentry frontmatter remains unchanged. All 52 local Markdown paths/fragments across 19 documents passed link checks; all five additional migration-document link targets also resolve. Fourteen scratch-repository cases exercise exact exception hashes, duplicate occurrences, stale/malformed entries, moved lines, ignored docs, CJK filenames, and NUL-containing source. `inventory/docs-validation.txt` records these checks.

## Preserved cases and owner follow-ups

- Exact legacy compare strings, verdict alternatives, model prefixes, mention aliases, and frozen tool/schema/rule fingerprints remain solely for compatibility. Prompt bodies are frozen as digests. The source gate uses whole-line hashes and occurrence counts, never broad file exclusions. Three readiness-test lines use visible legacy Unicode fixtures rather than escapes; their focused tests passed after the source-only representation change.
- ICP/provider-response/authentication detection still matches real localized data. Unicode/GBK filename handling, CJK font fallbacks, the report filename whitelist, non-ASCII test vectors, full-width input separators, and mention escaping remain supported.
- Single-rune truncation/mask sentinels and matching consumers remain aligned; replacing them blindly would alter byte/rune budgets or secret-preservation behavior. Exploration-graph source retains its two literal NUL separators.
- Customized prompts, tools, schemas, rules, labels, templates, and historical operator content may remain in their original language. This is intentional preservation, not a failed automatic translation. Review customized values or use applicable explicit resets when English is desired.
- Notification template variables retain their identifiers, but label values are English. Review stored `vulnclass_include` / `vulnclass_exclude` keywords because new English finding classes may not match old localized keywords.
- Traffic full-text search still requires at least three runes. Short English terms such as `id`, `ip`, and `js` retain the existing metadata-only fallback and need a separate product decision.
- Go module/import paths, Docker image names, upstream clone/release/attribution URLs, demo URL, and existing screenshots remain unchanged. Publish the owner's English release/image, recapture screenshots, and repoint distribution links when ready.
- Compose still pulls the upstream Chinese-UI image. The in-app updater still targets `Autumn-27/artex` and replaces only the binary; obtain English builds from the owner's releases and refresh `skills/` manually.
- `docker-compose.bench.yml` is translated but cannot run from this checkout because `Dockerfile.bench` and `bench/.env` are absent. Husky remains disabled. No root `config.json` was created.

## Final release gates

- **Passed at integration:** `scripts/check-no-cjk.sh` printed `0` with 446 exact exception entries across 57 files. Supplemental review classified all 799 punctuation hits as unchanged or deliberate; no hidden nonlegacy CJK escapes or Chinese locale references remain. Intended ignored documentation files are force-added.
- **Passed:** all 98 touched files are Biome-clean (96 under the normal configuration, two under identical explicit rules). TypeScript, normal static export, 10 Node tests, and all 23 final screenshots pass.
- **Passed:** `ARTEX_SKIP_FRONTEND=1 ARTEX_SKIP_NPM_CI=1 ./build.sh` produced `dist/artex-linux-amd64/artex` from the final normal export. All 30 exported HTML files have zero CJK matches. CLI help and the startup banner are English. Fresh `/api/health` returned `ok: true`, `/api/auth/status` returned `initialized: false`, and `/setup/` served the English UI. SQL assertions confirmed eight English prompts/agents, 51 tools, 20 rules, and built-in notes (`embedded-runtime-verification.json`). A real browser rendered the setup form without errors; no administrator was initialized. The upgraded database also passed a second-start idempotence check and a local notification test with the final executable.
- **Delivery:** frontend directory slices each pass cumulative TypeScript, Biome, and a normal static build; exact snapshot hashes and logs are retained in `frontend-commit-snapshots/commit-groups.json`. The final delivery command is `git push -u origin claude/project-korean-to-english-f1u9sy`. Remote confirmation is reported in the final task response; no pull request is requested.

## Unchanged no-database failures

The final no-database failure set exactly matches the original baseline:

- `server/TestFindingTrafficToolUpgradePreservesCustomization`
- `server/TestFindingTrafficAPIAndExport`
- `server/TestFindingTrafficArchiveV3RoundTripAndRetry`
- `server/TestFindingTrafficFailedReportDoesNotTrigger`
- `server/TestFindingTrafficUTF8SegmentsAndInheritedWrites`
- `server/TestFindingWorkflowAutoHintToPlannerAndSetting`
- `server/TestFindingWorkflowMigrationPreservesUserConfiguration`
- `server/TestFindingWorkflowReporterBindsBeforeWritingReport`
- `server/TestNotifyEndToEndRealtimeDelivery`
- `server/TestNotifyChannelAPIMasksSecretsAndPreservesOnUpdate`
- `server/TestNotifyChannelAPICreateValidation`
- `server/TestNotifyFilterBlocksBelowThreshold`
- `server/TestNotifyDigestBatchesMultipleFindingsIntoOneMessage`
- `server/TestNotifyDisabledChannelDoesNotSend`
- `server/TestNotifyStatusChangeDelivery`
- `server/TestNotifyStatusChangeSuppressedByDefault`
- `server/TestNotifyTestMessageEndpoint`
- `server/TestNotifyDeliveriesHistoryAndRetry`
- `server/TestNotifyMetaAndSettingsRoundTrip`
- `server/TestNotifyDeepLinkUsesPublicBaseURL`
- `server/TestNotifyNoDeepLinkWithoutBaseURL`
- `server/TestNotifyDigestSegmentsAndDefersRemainder`

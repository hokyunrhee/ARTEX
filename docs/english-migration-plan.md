# English migration baseline and plan

Recorded before changing source at `160fe13c243be361eeac5408c40a96e824fa842c`.

The actual checkout is `/workspace/ARTEX`. PostgreSQL 16.15 runs in the existing Docker container on loopback port 55432. Go 1.26.3 and Node 24.19 are installed. No root `config.json` is needed. Husky remains disabled. The original binary and frozen database defaults are retained outside the checkout for upgrade testing.

## Baseline

`go build ./...` and `go vet ./...` passed. `gofmt -l .` listed six pre-existing files: `db/asset_intercept.go`, `db/assets_test.go`, `db/config.go`, `db/task_scope_test.go`, `server/assembly_test.go`, and `server/tools_wire_test.go`.

All Go suites used `go test -p 1 -count=1 -json ./...`; the following counts include subtests. Both DB runs used separate newly created databases, `artex_english_base1` and `artex_english_base2`. The no-DB run unset `ARTEX_PG_DSN`.

| Run | Passed | Failed | Skipped | Exit |
| --- | ---: | ---: | ---: | ---: |
| nodb | 581 | 22 | 233 | 1 |
| db1 | 892 | 2 | 1 | 1 |
| db2 | 892 | 2 | 1 | 1 |

Both fresh runs failed only `agent/TestGraphOverviewExpandsAssociatedCompanyScope` and `server/TestCoreTaskLifecyclePG`. The graph test expects `coverage.scope`, intentionally removed upstream in `06a43f37`; fix the stale assertion without changing product behavior. The core lifecycle test receives persisted LLM/concurrency state from earlier tests and leaked background workers; diagnose and isolate its fixture. The no-DB run has the pre-existing 22 server failures because those tests assume usable PostgreSQL.

Frontend: frozen `npm ci` installed 721 packages; TypeScript passed; both Node test files passed (4 + 3 tests); the static build generated 31 pages. Biome baseline is 134 errors, 138 warnings, and 727 informational diagnostics. Every touched file must finish with no diagnostics. The exploration graph contains exactly two literal NUL bytes. `start.bat` has 54 CRLF lines.

Inventory: 347 files, 12,750 CJK/fullwidth source lines; 1,504 lines also contain supplementary typographic punctuation outside the requested regex. Full command logs, JSON test events, classified inventory, original metadata, and browser evidence are retained under `/workspace/.artex-cloud/english`. System Chromium 151 works through Python Playwright 1.62.0; the browser paths in the supplied request do not exist in this machine.

## Implementation order

1. Separate, minimal test-only baseline fixes.
2. Frozen legacy metadata; dual-read wire contracts; idempotent migrations that upgrade only unchanged defaults and preserve custom rows.
3. Backend and prompt translation in package slices, keeping every template token, format verb, API field, and stable identifier.
4. Notification producers and assertions; frontend translations and coupled consumers; skills; scripts and documentation.
5. Independent semantic, coupling, residue, and browser review. Exact line exceptions only for documented Unicode support and legacy compatibility.
6. Build/vet/format checks, two fresh full test suites, the no-DB comparison, frontend gates, embedded runtime and original-to-English upgrade tests; verify each logical commit and push the requested branch without opening a PR.

The existing conditional reseed flags need special care: translation migrations must never consume their completion flags before their target defaults have actually been translated. Previously shipped unconditional refresh paths must not overwrite operator customizations on older installs before the conservative English migration can inspect them.

## Baseline test-only corrections

The obsolete graph-scope assertion now checks stored company scope and the deliberate absence of inline scope in graph overview, retaining all asset/provenance assertions. The chat fixture now closes its manager after profile cleanup and cancels background work through the test context. This fixes the fake active provider leaking into the lifecycle test. After these two test-only corrections, `go build ./...`, `go vet ./...`, and the complete suite on a new database passed: 894 test/subtest results passed, one opt-in live-model test skipped, zero failures.

## Completion status

All translation and compatibility slices are implemented and independently reviewed. Both final fresh-database suites passed with 994 test/subtest passes, zero failures, and one opt-in live-model skip. The no-database run has 652 passes, the same 22 baseline failures, and 247 skips. Build/vet, original-binary upgrade, report/notification, and fresh embedded-runtime checks passed. All 98 touched frontend files are Biome-clean, with unchanged-file totals of 9 errors/3 warnings/8 informational diagnostics. TypeScript, 10 Node tests, normal/mock static builds, and 23 screenshot reviews pass. The integrated source gate prints 0 with 446 exact exceptions across 57 files. Directory commits receive separate cumulative static builds before the requested push. See [English migration verification](english-migration-verification.md) for commands, before/after counts, every section 4 contract, preserved cases, and runtime evidence.

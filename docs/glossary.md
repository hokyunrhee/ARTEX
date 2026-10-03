# English terminology and migration glossary

Use US English and sentence case for interface text. Existing identifiers, API fields, settings keys, routes, enum values, Go template variables, and format verbs remain unchanged. Source terms below are written as Unicode escape notation so this document does not introduce CJK residue.

## Domain terms

| Legacy source (Unicode escapes) | English rendering | Usage |
| --- | --- | --- |
| `\u610f\u56fe` | intent | Exploration node; preserve identifier intent. |
| `\u4e8b\u5b9e` | fact | Verified or inferred observation; preserve identifier fact. |
| `\u53d1\u73b0` | finding | Finding record, not a generic discovery when used as a domain noun. |
| `\u6f0f\u6d1e` | vulnerability | Use finding when the code refers to the stored finding record. |
| `\u8d44\u4ea7` | asset | Preserve asset type keys. |
| `\u4f01\u4e1a` | company | Preserve company_id and related API fields. |
| `\u8d44\u4ea7\u8303\u56f4` | asset scope | Scope rules and allowed target coverage. |
| `\u63a2\u7d22\u94fe\u8def / \u63a2\u7d22\u56fe` | exploration graph | Use one name for both legacy display labels. |
| `\u8d44\u4ea7\u8986\u76d6\u56fe` | asset coverage map | Task scope and tested-asset visualization. |
| `\u65c1\u8def\u63d0\u95ee` | side question | Preserve sidequestion package and route identifiers. |
| `\u62e6\u622a\u5ba1\u6279` | intercept approval | Tool approval workflow. |
| `\u5ba1\u6279\u8bb0\u5f55` | approval records | Approval list and detail UI. |
| `\u4eba\u5728\u73af\u8def` | human-in-the-loop | Human participation in the task or conversation. |
| `\u6218\u7565\u63d0\u793a` | strategic hint | Hint issued to guide exploration. |
| `\u76ee\u6807` | goal | Preserve goal node kind and agent key goals. |
| `\u76ee\u6807\u62c6\u89e3` | goal decomposition | Agent display name: Goal decomposition. |
| `\u89c4\u5212\u5668 / \u89c4\u5212` | planner | Agent display name: Planner. |
| `\u4e3b agent` | main agent | Agent display name: Main agent; preserve mainagent key. |
| `\u6267\u884c / \u5de5\u4f5c\u8005` | worker | Agent display name: Worker. |
| `\u62a5\u544a\u64b0\u5199` | reporter | Agent display name: Reporter. |
| `\u6f0f\u6d1e\u590d\u6d4b` | finding retest | Retester agent display name: Finding retest. |
| `\u6536\u5c3e\u63d0\u793a\u8bcd` | wrap-up prompt | Preserve cancellation and budget semantics. |
| `\u7194\u65ad` | circuit breaker | Stop condition rather than generic failure. |
| `\u5907\u6848 / ICP \u5907\u6848` | ICP filing | Real filing identifiers and their parser alternatives stay unchanged. |
| `\u6d41\u91cf\u5f55\u5236` | traffic recording | Traffic request/response evidence capture. |
| `\u8bc1\u636e` | evidence | Preserve evidence_version and evidence field names. |
| `\u63a8\u9001 / \u901a\u77e5` | notification | Use delivery for an individual transmission attempt. |
| `\u6c47\u603b\u6a21\u5f0f` | digest mode | Grouped notification rendering. |
| `\u89e6\u53d1\u5668` | trigger | Preserve trigger keys and conditions. |
| `\u4efb\u52a1\u6a21\u677f` | task template | Saved task configuration. |
| `\u64cd\u4f5c\u7ea6\u675f` | operation constraints | Prompt section anchor: Operation constraints. |
| `\u4e2d\u95f4\u4ea7\u7269\u8f93\u51fa\u89c4\u7ea6` | intermediate artifact output rules | Prompt section anchor: Intermediate artifact output rules. |
| `\u4efb\u52a1` | task | Use task consistently for the platform work item. |
| `\u4f1a\u8bdd` | conversation | Use session for a single agent execution session when that is the code meaning. |
| `\u5de5\u4f5c\u7a7a\u95f4` | workspace | File and artifact workspace. |
| `\u6001\u52bf` | overview | Use graph overview for graph state and progress. |
| `\u6267\u884c\u8fc7\u7a0b` | execution trace | Worker actions and tool results. |
| `\u5f52\u6863` | archive | Persisted exported task archive. |
| `\u590d\u73b0` | reproduction | Use reproduced for the parsed retest verdict token. |
| `\u53d6\u8bc1` | evidence collection | Do not weaken evidence verification requirements. |
| `\u7ea6\u675f` | constraints | Preserve each individual rule and its priority. |
| `\u6e17\u900f\u6d4b\u8bd5` | penetration testing | Standalone agent display name: Penetration testing. |
| `\u65b0\u5bf9\u8bdd` | New conversation | Write English; accept both the current and legacy title when auto-renaming. |
| `\u9644\u4ef6\u6d88\u606f` | Attachment message | Display fallback only. |
| `\u547d\u4ee4` | Command | Use a shared constant wherever transcript logic compares this label. |
| `\u5b9e\u9645\u64cd\u4f5c` | Actual operation | English intercept verdict segment marker; parser also accepts legacy markers. |
| `\u6210\u529f\u540e\u7684\u540e\u679c` | Consequences if successful | English intercept verdict segment marker; preserve delimiter contract. |
| `\u547d\u4e2d\u89c4\u5219` | Matched rule | English intercept verdict segment marker; preserve delimiter contract. |

## Severity labels

Use these labels identically in the backend, interface, notifications, fixtures, and documentation.

| Existing key | Legacy source (Unicode escapes) | English label |
| --- | --- | --- |
| `critical` | `\u4e25\u91cd` | Critical |
| `high` | `\u9ad8\u5371` | High |
| `medium` | `\u4e2d\u5371` | Medium |
| `low` | `\u4f4e\u5371` | Low |

## Finding status labels

Use these labels identically in the backend, interface, notifications, fixtures, and documentation.

| Existing key | Legacy source (Unicode escapes) | English label |
| --- | --- | --- |
| `pending` | `\u5f85\u5904\u7406` | Pending |
| `in_progress` | `\u5904\u7406\u4e2d` | In progress |
| `confirmed` | `\u5df2\u786e\u8ba4` | Confirmed |
| `resolved` | `\u5df2\u5904\u7406` | Resolved |
| `fixed` | `\u5df2\u4fee\u590d` | Fixed |
| `false_positive` | `\u8bef\u62a5` | False positive |
| `ignored` | `\u5ffd\u7565` | Ignored |
| `duplicate` | `\u91cd\u590d` | Duplicate |
| `risk_accepted` | `\u98ce\u9669\u63a5\u53d7` | Risk accepted |

## Delivery status labels

Use these labels identically in the backend, interface, notifications, fixtures, and documentation.

| Existing key | Legacy source (Unicode escapes) | English label |
| --- | --- | --- |
| `pending` | `\u5f85\u53d1\u9001` | Pending |
| `sending` | `\u53d1\u9001\u4e2d` | Sending |
| `sent` | `\u5df2\u9001\u8fbe` | Delivered |
| `failed` | `\u5931\u8d25` | Failed |
| `skipped` | `\u5df2\u8df3\u8fc7` | Skipped |

Severity labels in notification subjects retain their existing emoji prefix. Plain labels contain no Markdown metacharacters. Delivery `sent` displays as Delivered; its enum remains `sent`.

## Other status labels

| Domain | Existing key | English label |
| --- | --- | --- |
| Intent | `open` | Available |
| Intent | `running` | Running |
| Intent | `paused` | Paused |
| Intent | `done` | Completed |
| Intent | `blocked` | Execution error |
| Intent | `exhausted` | Budget exhausted |
| Intent | `stopped` | Stopped |
| Intent | `deleted` | Deleted |
| Task | `created` | Created |
| Task | `queued` | Queued |
| Task | `running` | Running |
| Task | `paused` | Paused |
| Task | `done` | Completed |
| Task | `failed` | Failed |
| Task | `timeout` | Timed out |
| Engine | `exploring` | Exploring |
| Engine | `paused` | Paused |
| Engine | `stalled` | Stalled |
| Engine | `idle` | Idle |
| Goal | `open` | In progress |
| Goal | `met` | Achieved |
| Goal | `abandoned` | Abandoned |
| Approval | `allow` | Allow |
| Approval | `block` | Block |
| Node | `observed` | Observed |
| Node | `confirmed` | Confirmed |
| Node | `tombstoned` | Discarded |

## Character budgets

| Source location | Previous wording or limit | English wording or limit | Reason |
| --- | --- | --- | --- |
| `agent/tools.go` trace summaries | 100 CJK characters | 100 characters | `firstLine(..., 100)` enforces a rune count; do not increase it. |
| `intercept/prompt.go` judge comment | At most 120 CJK characters | About 300 characters | Prompt-only density adjustment, about 2.5 times; keep the parser limit at 2400 bytes. |
| Side-question input and retest notes | 4000 characters | 4000 characters | Validation enforces character counts, not a language-dependent target. |
| Task, template, category, conversation, and agent-key validation | Existing rune limits | Existing validated limits | Preserve code-enforced limits unless producer and consumer bounds are reviewed together. |

The frontend category-name limit remains 80 characters, matching backend category validation. Template names remain 120 characters (`MaxTaskTemplateNameRunes`), and task names remain 200 characters (`maxTaskNameRunes`). These validated bounds are language-independent. Mention display labels increase from 100 to 250 characters; stored record IDs, parsing, and resolution remain unchanged. All new mention tokens use the existing ASCII aliases.

The worker prompt and traffic-search descriptions retain the enforced three-character full-text minimum. No other core prompt character budgets changed. Cancellation Short labels remain at most 40 runes; the existing tests verify this. The 1200-token side-question summary budget, 30000-character SDK output cap, and 8192-byte traffic blob cap remain unchanged.

## Compatibility terminology

- New model-decision reasons use `[model]`; legacy reasons remain readable in database rows, audit JSON, archives, and frontend views.
- New mention wire words are `finding`, `asset`, `company`, `api`, `ip`, `app`, `domain`, `subdomain`, and `service`. Keep legacy token alternatives readable.
- `reproduced`, `fixed`, and `inconclusive` retest verdict tokens are unchanged.
- Operation constraints and Intermediate artifact output rules are matching prompt-section anchors. Translate every reference and assertion together.
- Translate output-language directives to English while preserving their strength and every other prompt rule.
- Preserve `{{.Variable}}`, Go formatting verbs, JSON schema keys, command names, tool identifiers, and exact enum values.
- Use ASCII punctuation in translated prose. Preserve approved parser punctuation, Unicode test vectors, and historical fingerprints through the explicit allowlist.
- English notification labels change webhook template values such as `.Title`, `.SeverityLabel`, and `.StatusLabel`; template field names remain unchanged.
- Operator-authored content remains unchanged. Frozen-default migration comparisons distinguish shipped defaults from custom content.

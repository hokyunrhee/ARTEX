# ARTEX translation glossary

This file fixes the one English rendering for every domain term used across the
backend (Go), LLM prompts, frontend (TypeScript/React), notifications, SQL,
documentation and tests. When the same concept appears in more than one place it
**must** use the same English word here, so the product reads as one system and
so code that matches on a string keeps matching. When a term is missing, prefer
the rendering already present in an English identifier or comment in the same
file, then add it here.

Conventions: US English. UI strings and headings use sentence case unless the
original is a proper noun or product name. Status/severity **labels** are
capitalized as shown (they are shown to users); the underlying **code values**
(`pending`, `critical`, …) never change.

## Core domain

| Chinese | English |
|---|---|
| 意图 | intent |
| 事实 | fact |
| 发现 | finding |
| 漏洞 | vulnerability (the issue); **finding** when the code means the record |
| 资产 | asset |
| 企业 | company |
| 资产范围 / 范围 | asset scope / scope |
| 探索链路 / 探索图 | exploration graph |
| 探索节点 | exploration node |
| 资产覆盖图 / 覆盖度 | asset coverage map / coverage |
| 旁路提问 / 旁路 | side question |
| 拦截审批 | intercept approval |
| 审批记录 | approval records |
| 人在环路 | human-in-the-loop |
| 战略提示 / 提示 | strategic hint / hint |
| 目标 | goal |
| 目标拆解 | goal decomposition |
| 规划器 / 规划 | planner / planning |
| 主 agent | main agent |
| 执行 / 工作者 / worker | worker |
| 报告撰写 | reporter |
| 漏洞复测 | finding retest |
| 收尾提示词 | wrap-up prompt |
| 熔断 | circuit breaker |
| 备案 / ICP 备案 | ICP filing (the Chinese marker 备案 stays in detection code) |
| 流量录制 / 流量 | traffic recording / traffic |
| 证据 | evidence |
| 推送 / 通知 | notification |
| 汇总模式 | digest mode |
| 触发器 | trigger |
| 任务模板 | task template |
| 操作约束 | operation constraints |
| 中间产物输出规约 | intermediate artifact output rules |
| 攻击面 | attack surface |
| 路线 | route (of attack) |
| 封锁 | blocked (a route ruled out) |
| 侦察 | reconnaissance |
| 深入利用 | exploitation |
| 对抗式自检 / 证伪 | adversarial self-check / refutation |
| 知识图谱 | knowledge graph |
| 自定义工具 | custom tool |
| 技能 | skill |
| 会话 | conversation (chat) / session (agent run), per surrounding usage |
| 内置 | built-in |

## Agents (`agents` table, `builtinAgents`, reporter/retester)

| key | Chinese name | English name | description |
|---|---|---|---|
| goals | 目标拆解 | Goal decomposition | Break a pentest task's goal into independent, verifiable sub-goals. |
| planner | 规划 | Planner | Read the situation, judge goals, and add exploration intents only when there is a genuinely uncovered direction (one planning loop per task). |
| mainagent | 主 | Main | Human interface: observe progress and turn the operator's intent into a hint or a high-priority intent. |
| worker | 执行 | Worker | Claim one intent, execute it, write the facts/findings back to the knowledge graph, then stop. |
| auto | Auto | Auto | Platform operator assistant: manage tasks (create/view/pause/hint) and assets with tools, and create/modify skills, custom tools and MCP servers. |
| pentest | 渗透测试 | Pentest | Solo pentest agent: one agent runs the whole chain from recon to exploitation to verification to wrap-up, planning, executing and adversarially verifying by itself. |
| reporter | 报告撰写 | Reporter | Detailed vulnerability report writing: triggered automatically when a finding is recorded; gathers evidence and the execution trace, then writes and saves a Markdown report. |
| retester | 漏洞复测 | Finding retest | Started manually from a finding's detail page; reads the original evidence and saves an independent retest verdict. |

## Severity labels (`notify/notify.go` `SeverityLabel`, `web/src/lib/status.ts`)

Keep the leading emoji in the Go `SeverityLabel` exactly.

| code | Chinese | English |
|---|---|---|
| critical | 🔴 严重 | 🔴 Critical |
| high | 🟠 高危 | 🟠 High |
| medium | 🟡 中危 | 🟡 Medium |
| low | 🔵 低危 | 🔵 Low |

Web `status.ts` severity labels (no emoji): Critical / High / Medium / Low.

## Finding statuses (`notify/notify.go` `StatusLabel`, `web/src/lib/status.ts`)

Labels pass through markdown escaping in notify, so they must contain no markdown
metacharacters (`*_()[]`). "False positive", "Risk accepted" (space, not underscore).

| code | Chinese | English |
|---|---|---|
| pending | 待处理 | Pending |
| in_progress | 处理中 | In progress |
| confirmed | 已确认 | Confirmed |
| resolved | 已处理 | Resolved |
| fixed | 已修复 | Fixed |
| false_positive | 误报 | False positive |
| ignored | 忽略 | Ignored |
| duplicate | 重复 | Duplicate |
| risk_accepted | 风险接受 | Risk accepted |

## Delivery statuses (`web/src/lib/status.ts` delivery)

| code | Chinese | English |
|---|---|---|
| pending | 待发送 | Pending |
| sending | 发送中 | Sending |
| sent | 已送达 | Delivered |
| failed | 失败 | Failed |
| skipped | 已跳过 | Skipped |

## Intent statuses (`web/src/lib/status.ts` intent)

| code | Chinese | English |
|---|---|---|
| open | 待领 | Unclaimed |
| running | 执行中 | Running |
| paused | 已暂停 | Paused |
| done | 已完成 | Done |
| blocked | 执行出错 | Errored |
| exhausted | 预算耗尽 | Budget exhausted |
| stopped | 已停止 | Stopped |
| deleted | 已删除 | Deleted |

## Task statuses (`web/src/lib/status.ts` task)

| code | Chinese | English |
|---|---|---|
| created | 已创建 | Created |
| queued | 排队中 | Queued |
| running | 运行中 | Running |
| paused | 已暂停 | Paused |
| done | 已完成 | Done |
| failed | 失败 | Failed |
| timeout | 已超时 | Timed out |

## Other status maps (`web/src/lib/status.ts`)

- engine: exploring 探索中 → Exploring; paused 已暂停 → Paused; stalled 停滞 → Stalled; idle 空闲 → Idle.
- goal: open 进行中 → In progress; met 已达成 → Met; abandoned 已放弃 → Abandoned.
- audit: allow 放行 → Allow; block 拦截 → Block.
- node: observed 观测 → Observed; confirmed 确认 → Confirmed; tombstoned 废弃 → Tombstoned.

## Judge verdict (`intercept/`)

- 放行 → Allow; 拦截 → Block; 转人工审批 → Send for human approval (`judgeActionLabel`, display only).
- Decision-source sentinel `[模型]` → `[model]` (ASCII); both accepted on read forever.
- Verdict contract segment markers (English successors, both sets parsed):
  `实际操作：` → `Action:`; `；成功后的后果：` → `; Consequence on success:`;
  `；命中规则：` → `; Matched rule:`.

## Chat @-mention wire words (`server/chat_mentions.go`, `web/src/lib/chat-mentions.ts`)

The English wire word is each kind's existing `alias` value; the nine Chinese
words stay accepted as legacy alternatives on read. Display label → alias:

| Chinese label | alias / English wire word |
|---|---|
| 漏洞 | finding |
| 资产 | asset |
| 企业 | company |
| 接口 | api |
| IP | ip |
| 应用 | app |
| 域名 | domain |
| 子域名 | subdomain |
| 服务 | service |

## Length-rule conversions (prompts)

Record every "N 字/汉字" → English conversion here:

- `agent/tools.go` summary ≤ 100 字 ↔ `firstLine(…, 100)` rune count → "100 characters" (code-enforced, keep exact number).
- `intercept/prompt.go` comment ≤ 120 个汉字 → "about 300 characters" (display guidance, scaled ~2.5×; stays well under the 2400-byte `ParseVerdict` cap).

## Notes

- Never translate existing English identifiers or code values: `planner`, `worker`,
  `mainagent`, `goals`, `auto`, `pentest`, `reporter`, `retester`, `intent`, `hint`,
  `resolved`, and every status/severity/kind enum value.
- Retest verdict tokens `reproduced` / `fixed` / `inconclusive` are parsed and stay.

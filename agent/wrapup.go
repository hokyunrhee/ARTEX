package agent

import (
	"strings"

	"github.com/Autumn-27/norma/harness"
)

// Wrap-up (settlement) prompts are injected by the SDK's settlement phase when an agent is terminated
// because of turn-budget exhaustion (MaxTurns) or timeout (run_seconds/MaxDuration),
// asking it to persist identified but unwritten results before producing a one-sentence summary, avoiding unfinished work.
//
// Each agent's wrap-up prompt can be overridden in the admin UI (agents.wrapup_prompt); empty uses the built-in default here.
// Only the prompt body is editable; disabled tools and the wrap-up phase's own turn budget are fixed code policies.

// WrapupOverride, if set, returns the stored wrap-up prompt for an agent key and
// whether a non-empty one exists. Wired by the server to the agents table (like
// PromptOverride for system prompts). nil / empty → the built-in default is used.
var WrapupOverride func(agentKey string) (string, bool)

// WrapupMaxTurnsOverride, if set, returns the admin-configured turn budget for the
// wrap-up phase of an agent and whether a positive one exists. Wired to the agents
// table. nil / ≤0 → the built-in per-agent default (wrapupTurnDefaults) is used.
var WrapupMaxTurnsOverride func(agentKey string) (int, bool)

// Built-in wrap-up defaults keyed by agent key. The worker reuses the historically hardcoded settleWrapUpPrompt
// from worker.go; planner/mainagent each have their own. Unmatched custom agents use the generic fallback.
var wrapupDefaults = map[string]string{
	"worker":    settleWrapUpPrompt,
	"planner":   plannerWrapUpDefault,
	"mainagent": mainAgentWrapUpDefault,
}

// wrapupTurnDefaults contains each agent's default turn budget for the wrap-up phase itself (overridable with a positive admin value).
// Each gets 10 turns, enough to persist results. Unmatched agents use genericWrapupTurns.
var wrapupTurnDefaults = map[string]int{
	"worker":    10,
	"planner":   10,
	"mainagent": 10,
}

const genericWrapupTurns = 10

const plannerWrapUpDefault = "This planning round is about to exhaust its turn budget. Only **this round** is ending: the system will wake you again as the situation changes, so the task is not terminating and you do not need to conclude all planning now. Persist the conclusions you already reached so this round is not wasted, but **do not invent intents merely to wrap up** (zero intents remains a completely normal result for this round). (1) If you identified exploration directions **that should be dispatched now**, submit them in one add_intent batch; do not hold back ready directions. (2) For goals proven achieved by a finding/fact, call prove_goal to mark met; do not miss any. (3) If you identified a sequential exploitation chain requiring separate steps, record it with TodoWrite so dispatch can continue at the next wake-up. Then end this round directly; no summary text is needed."

const mainAgentWrapUpDefault = "Your turn budget is about to run out and this interaction is ending. Do not start new exploration or operations. Give the user **one separate sentence of plain text** summarizing current progress, key conclusions, and the recommended next step."

const genericWrapUpDefault = "Your budget is about to run out and this run will terminate. First write back completed results that have not been persisted, then provide **one separate sentence of plain text** summarizing what you did and the key conclusions (it will be displayed as this run's result)."

// WrapupDefault returns the built-in default wrap-up prompt for an agent key —
// used by the admin UI as the "restore default" value and empty-field placeholder.
func WrapupDefault(agentKey string) string {
	if d, ok := wrapupDefaults[agentKey]; ok {
		return d
	}
	return genericWrapUpDefault
}

// WrapupTurnsDefault returns the built-in wrap-up turn budget for an agent key —
// used by the admin UI as the "0 = default N" hint.
func WrapupTurnsDefault(agentKey string) int {
	if n, ok := wrapupTurnDefaults[agentKey]; ok {
		return n
	}
	return genericWrapupTurns
}

// resolveWrapup returns the effective wrap-up prompt: the DB override (if set and
// non-empty) over the built-in default.
func resolveWrapup(agentKey string) string {
	if WrapupOverride != nil {
		if t, ok := WrapupOverride(agentKey); ok && strings.TrimSpace(t) != "" {
			return t
		}
	}
	return WrapupDefault(agentKey)
}

// resolveWrapupTurns returns the effective wrap-up turn budget: a positive DB
// override over the built-in per-agent default.
func resolveWrapupTurns(agentKey string) int {
	if WrapupMaxTurnsOverride != nil {
		if v, ok := WrapupMaxTurnsOverride(agentKey); ok && v > 0 {
			return v
		}
	}
	return WrapupTurnsDefault(agentKey)
}

// wrapupSettlement builds the settlement config for an agent's run. Prompt and the
// turn budget are admin-editable per agent; disabled tools are code-owned policy so
// a user can't edit away the "stop probing" guardrail. Resolved fresh each run
// (reads DB live), so edits apply on the next run without a restart.
func wrapupSettlement(agentKey string, disabledTools []string) *harness.Settlement {
	return &harness.Settlement{
		Prompt:        resolveWrapup(agentKey),
		DisabledTools: disabledTools,
		MaxTurns:      resolveWrapupTurns(agentKey),
	}
}

// ---------- Task-level timeout wrap-up prompts (see the task timeout and wrap-up design) ----------
//
// These are separate from per-run prompts: per-run means this run's budget is exhausted; task timeout means
// the entire task has reached its deadline and is ending. Their meaning is often opposite, especially for the planner:
// per-run says keep planning, while task timeout says stop planning and make a final evaluation. Only worker/planner have these.

// WrapupTaskTimeoutOverride / WrapupTaskTimeoutTurnsOverride provide DB overrides for task-timeout prompts and turn budgets
// (wired to agents.task_timeout_wrapup_prompt / _max_turns, worker/planner only).
var (
	WrapupTaskTimeoutOverride      func(agentKey string) (string, bool)
	WrapupTaskTimeoutTurnsOverride func(agentKey string) (int, bool)
)

var taskTimeoutWrapupDefaults = map[string]string{
	"worker":  workerTaskTimeoutDefault,
	"planner": plannerTaskTimeoutDefault,
}

const workerTaskTimeoutDefault = "**The entire task has reached its time limit and is about to end** (not just this run's budget; the whole exploration has reached its deadline). This is your last chance: (1) Persist **all** identified but unwritten results: new assets with insert_assets, exploration conclusions/facts with record_fact, confirmed vulnerabilities with report_finding. (2) Do not start any new commands or probes. (3) **Finish with one separate sentence of plain text** summarizing your key conclusions for this intent."

const plannerTaskTimeoutDefault = "**The entire task has reached its time limit and is about to end** (the whole task is terminating, not just this round). Make one final goal evaluation based on **all** current facts and findings: call prove_goal to mark met for goals proven by evidence; do not miss any. **Do not generate any new intents** (they would no longer be executed). End after the evaluation; no summary text is needed."

// TaskTimeoutWrapupDefault returns an agent's built-in task-timeout wrap-up prompt for admin placeholders/default restoration.
func TaskTimeoutWrapupDefault(agentKey string) string {
	return taskTimeoutWrapupDefaults[agentKey] // Empty for unconfigured agents (mainagent/chat)
}

// resolveTaskTimeoutWrapup gives a nonempty DB override precedence over the built-in default. Empty means the agent has no
// task-timeout prompt (not worker/planner), so the caller should fall back to the per-run prompt.
func resolveTaskTimeoutWrapup(agentKey string) string {
	if WrapupTaskTimeoutOverride != nil {
		if t, ok := WrapupTaskTimeoutOverride(agentKey); ok && strings.TrimSpace(t) != "" {
			return t
		}
	}
	return TaskTimeoutWrapupDefault(agentKey)
}

func resolveTaskTimeoutTurns(agentKey string) int {
	if WrapupTaskTimeoutTurnsOverride != nil {
		if v, ok := WrapupTaskTimeoutTurnsOverride(agentKey); ok && v > 0 {
			return v
		}
	}
	return resolveWrapupTurns(agentKey) // Default to the per-run turn budget
}

// wrapupSettlementForTask builds settlement for a worker/planner run that is aware
// of the task deadline. See §5 of the design doc:
// - clamped=true: the task deadline clamps this run; Timeout means the task deadline -> task-timeout prompt;
// MaxTurns means turns ran out within the clamped window while task time remains -> per-run prompt.
// - clamped=false: the task deadline is still distant; both reasons use the per-run prompt, as in wrapupSettlement.
//
// PromptByReason lets the harness select at wrap-up time using the actual reason, avoiding build-time mismatches.
func wrapupSettlementForTask(agentKey string, disabledTools []string, clamped bool) *harness.Settlement {
	perRun := resolveWrapup(agentKey)
	st := &harness.Settlement{
		Prompt:        perRun, // Fallback, also used for both reasons when not clamped
		DisabledTools: disabledTools,
		MaxTurns:      resolveWrapupTurns(agentKey),
	}
	if clamped {
		if tt := resolveTaskTimeoutWrapup(agentKey); tt != "" {
			st.PromptByReason = map[harness.TerminalReason]string{
				harness.ReasonTimeout:  tt,     // Task deadline reached
				harness.ReasonMaxTurns: perRun, // Turns exhausted while task time remains
			}
			st.MaxTurns = resolveTaskTimeoutTurns(agentKey)
		}
	}
	return st
}

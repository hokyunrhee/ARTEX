package agent

import (
	"strings"

	"github.com/Autumn-27/norma/harness"
)

// Wrap-up / settlement prompt: when an agent is terminated because [its step budget
// is exhausted (MaxTurns)] or [it timed out (run_seconds/MaxDuration)], the SDK's
// settlement phase injects this prompt so the agent first persists what it has
// identified but not yet written back, then emits a one-line summary, avoiding a
// dangling finish.
//
// Each agent's wrap-up prompt can be overridden from the admin UI as needed (stored
// in agents.wrapup_prompt); leaving it empty uses the built-in default here. Only
// [the prompt body] is editable; which tools are disabled and how many turns the
// wrap-up itself gets are code-fixed policy.

// WrapupOverride, if set, returns the stored wrap-up prompt for an agent key and
// whether a non-empty one exists. Wired by the server to the agents table (like
// PromptOverride for system prompts). nil / empty → the built-in default is used.
var WrapupOverride func(agentKey string) (string, bool)

// WrapupMaxTurnsOverride, if set, returns the admin-configured turn budget for the
// wrap-up phase of an agent and whether a positive one exists. Wired to the agents
// table. nil / ≤0 → the built-in per-agent default (wrapupTurnDefaults) is used.
var WrapupMaxTurnsOverride func(agentKey string) (int, bool)

// Built-in default wrap-up prompts, indexed by agent key. worker reuses the
// historically hardcoded settleWrapUpPrompt (defined in worker.go); planner/mainagent
// each have their own; a miss (custom agent) uses the generic fallback.
var wrapupDefaults = map[string]string{
	"worker":    settleWrapUpPrompt,
	"planner":   plannerWrapUpDefault,
	"mainagent": mainAgentWrapUpDefault,
}

// wrapupTurnDefaults: the built-in default turn budget for each agent's [own] wrap-up
// phase (overridable from the admin UI with a value >0). Each gets 10 turns so the
// wrap-up phase has enough steps to persist. A miss uses genericWrapupTurns.
var wrapupTurnDefaults = map[string]int{
	"worker":    10,
	"planner":   10,
	"mainagent": 10,
}

const genericWrapupTurns = 10

const plannerWrapUpDefault = "Your planning turns for this round are about to run out -- note that only [this round] is ending; the system will wake you again as the situation changes to keep planning, so this is not task termination and you do not need to wrap up the whole plan here. Land the conclusions you have already thought through this round so it isn't wasted, but also [do not force intents just to wrap up] (0 intents this round is still a perfectly normal result): (1) if you have already identified an exploration direction that [should be dispatched right now], submit it in one batched add_intent call (don't sit on ideas you've already formed); (2) for a goal already proven met by some finding/fact, call prove_goal to mark it met (don't miss any); (3) if you identified a serial exploitation chain that needs to be broken into steps, record it with TodoWrite so the next wake can keep dispatching. When done, end the round directly -- no summary text needed."

const mainAgentWrapUpDefault = "Your turns are about to run out and this interaction is about to end. Do not start any new exploration/operation. Please **summarize, in a single plain-text sentence on its own,** the current progress, the key conclusions, and the suggested next step for the user."

const genericWrapUpDefault = "You are about to be terminated because the budget is exhausted. First write back the completed results that have not been persisted, then **summarize, in a single plain-text sentence on its own,** what you did and the key conclusions you reached (this sentence is shown as the result of this run)."

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

// ---------- Task-level timeout wrap-up prompts (see docs/task-level-timeout-and-wrapup-design.md) ----------
//
// These are a [separate set] from the per-run wrap-up prompts: per-run means "the
// budget for this one run is used up"; task timeout means "the whole task has reached
// its deadline and is about to end". The semantics are often opposite (especially for
// planner: per-run says "don't stop, keep planning", task timeout says "the deadline
// is here, stop planning and make the final call"). Configured only for worker/planner.

// WrapupTaskTimeoutOverride / …TurnsOverride: DB overrides for the task-timeout
// wrap-up prompt and its turn count (wired to agents.task_timeout_wrapup_prompt /
// _max_turns, worker/planner only).
var (
	WrapupTaskTimeoutOverride      func(agentKey string) (string, bool)
	WrapupTaskTimeoutTurnsOverride func(agentKey string) (int, bool)
)

var taskTimeoutWrapupDefaults = map[string]string{
	"worker":  workerTaskTimeoutDefault,
	"planner": plannerTaskTimeoutDefault,
}

const workerTaskTimeoutDefault = "**The whole task has hit its timeout cap and is about to end** (not your budget for this run -- the entire exploration has reached its deadline). This is the last chance: (1) persist [all] of what you identified but haven't written back yet -- new assets via insert_assets, exploration conclusions/facts via record_fact, confirmed findings via report_finding; (2) do not start any new command/probe; (3) **finally, in a single plain-text sentence on its own,** summarize the key conclusions on this intent."

const plannerTaskTimeoutDefault = "**The whole task has hit its timeout cap and is about to end** (not just this round -- the entire task is terminating). Based on [all] current facts and findings, make one final goal assessment: for any goal proven met by evidence, call prove_goal to mark it met (don't miss any). **Do not generate any new intents** (dispatching an intent now would no longer be executed). Once the assessment is done, wrap up -- no summary text needed."

// TaskTimeoutWrapupDefault returns an agent's built-in default task-timeout wrap-up
// prompt (for the admin UI placeholder / restore-default).
func TaskTimeoutWrapupDefault(agentKey string) string {
	return taskTimeoutWrapupDefaults[agentKey] // unconfigured (mainagent/chat) returns an empty string
}

// resolveTaskTimeoutWrapup: DB override (non-empty) > built-in default. An empty
// string means this agent has no task-timeout prompt (not worker/planner), in which
// case the caller should fall back to the per-run prompt.
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
	return resolveWrapupTurns(agentKey) // default reuses the per-run turn count
}

// wrapupSettlementForTask builds settlement for a worker/planner run that is aware
// of the task deadline. See §5 of the design doc:
//   - clamped=true  → this run is squeezed by the task deadline: a Timeout wrap-up =
//     task deadline reached → task-timeout prompt; a MaxTurns wrap-up = steps ran out
//     first within the squeeze window with minutes left on the task → fall back to the
//     per-run prompt.
//   - clamped=false → the task is still far off: both reasons use the per-run prompt
//     (i.e. it degrades to wrapupSettlement).
//
// The PromptByReason handed to the harness picks by the [actual] reason at wrap-up
// time, so there is no build-time mismatch.
func wrapupSettlementForTask(agentKey string, disabledTools []string, clamped bool) *harness.Settlement {
	perRun := resolveWrapup(agentKey)
	st := &harness.Settlement{
		Prompt:        perRun, // fallback (also the value for both reasons when not clamped)
		DisabledTools: disabledTools,
		MaxTurns:      resolveWrapupTurns(agentKey),
	}
	if clamped {
		if tt := resolveTaskTimeoutWrapup(agentKey); tt != "" {
			st.PromptByReason = map[harness.TerminalReason]string{
				harness.ReasonTimeout:  tt,     // task deadline reached
				harness.ReasonMaxTurns: perRun, // steps ran out first, task still has time
			}
			st.MaxTurns = resolveTaskTimeoutTurns(agentKey)
		}
	}
	return st
}

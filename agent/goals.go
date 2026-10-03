package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/norma/agentcore"
	"github.com/Autumn-27/norma/llm"
	acperm "github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
	"github.com/Autumn-27/norma/transcript"
)

// goalsDefaultTmpl is the built-in EDITABLE body (section [A]) of the goals-decomposer
// prompt, seeded into agent_prompts. No template vars are used today.
const goalsDefaultTmpl = `You decompose penetration testing goals. Your responsibility is to identify **the final outcomes to achieve** from the user's input, not to plan attack steps.

**Step 1 (before decomposing goals): extract operation constraints**
Identify the operator's explicit rules about **which operations are allowed or prohibited** in the task goal/description, and register them individually with set_constraints (you may skip extraction if neither the description nor the goal contains operation constraints):
- type=deny: prohibited operations (for example, "do not scan ports," "do not write or delete in production," "no brute force," or "do not touch a particular subdomain").
- type=allow: explicitly permitted or restricted operating scope (for example, "passive reconnaissance only" or "only this domain").
- Constraints are neither goals nor attack steps; they define the boundaries of permitted actions.
- **Constraints must be self-contained and name the specific target explicitly**: replace references such as "the current target/port/IP/domain/site" with the **concrete values** in the task goal/description. Constraints are injected separately into execution prompts, where those references cannot be resolved without context.
  Example: if the target is https://abc.example.net, write "Only test abc.example.net," not "Only test the current target"; write "Only test target port 443; do not scan other ports," not "Only test the current port." If the original only says "the current target" but the target address is clear, include that address.
- **Register only constraints explicitly stated or emphasized in the goal/description; never invent them**. If the type is uncertain, use deny (the more conservative choice).
- If the goal/description contains no operation constraints, **do not** call set_constraints.
After registering any constraints, proceed with goal decomposition below.

**Goal = a final, deliverable/verifiable outcome**

**The following are not goals (do not list them as subgoals)**:
- Information gathering, reconnaissance, endpoint scanning
- Vulnerability analysis and verification processes
- Attack steps or exploitation methods
- Result verification steps

**Decomposition principles**:
- If the user describes only one final goal, output one
- If there are multiple **independent** final deliverables, list them separately
- Set vulnclass when a clear vulnerability class applies; leave it empty for information-gathering or business-logic goals
- Never invent goals the user did not mention

Submit the results by calling set_goals.`

// goalsScopeTail is the code-owned tail appended after the editable goals body
// WHEN an asset store + task context are available. It teaches the decomposer to
// also lift the explicit asset scope out of the goal/description and register it
// via add_task_scope. Kept in code (not the DB-editable body) so it always applies
// on released DBs and can't be edited away — same pattern as the trafficTool tail.
const goalsScopeTail = `

**Additional responsibility: register the test asset scope**
In addition to decomposing goals, identify the **explicitly stated test asset scope** in the task goal/description and register it with add_task_scope (the task's authorization boundary and the denominator for asset testing coverage). **Minimum-scope principle: register only the specific target the user explicitly named; never broaden it on your own.**
- A URL or an address with a hostname (such as https://xxx.example.com/path or app.example.com): use its **full hostname**, kind=subdomain, value=the full hostname.
  Example: target https://a1b2c3.lab.example.net/path -> kind=subdomain, value=a1b2c3.lab.example.net (**not** example.net).
  **Never** reduce a hostname with subdomains to its root domain. Registering all of example.com for xxx.example.com expands beyond the user's target and violates the minimum-scope principle.
- Use kind=root_domain, value=example.com **only** when the user provides a **bare root domain without any subdomains** (such as example.com), or explicitly says "the entire site / all subdomains / the whole domain."
- A plain IP or network range -> kind=ip / cidr, value=the IP or CIDR.
- **Do not** register company scope (company). The task has just been created and the company usually does not yet exist in the asset system, so registration would fail. Leave company-level scope to the later planning phase.
Other rules:
- Register only scope **explicitly stated in the goal/description**; never invent or infer unmentioned domains/IPs.
- Briefly state which sentence supports the scope in reason for auditing.
- If the goal/description contains no explicit asset scope, **do not** call add_task_scope.
First register any scope with add_task_scope, then submit the goals with set_goals.`

// GoalSpec is one decomposed objective.
type GoalSpec struct {
	Text      string `json:"text"`
	VulnClass string `json:"vulnclass,omitempty"`
}

// DecomposeGoals asks the LLM to break a pentest task goal into discrete,
// independently-verifiable objectives (each becomes a goal node). Returns nil if
// no provider is configured or the call yields nothing — the caller then falls
// back to a rule-based split so goal nodes always exist.
//
// prov is supplied by the caller (rather than built here from a Config) so goal
// decomposition rides the SAME provider instance as the rest of the engine — it
// shares the rate limiter, gets recorded by llmrec, and participates in LLM
// failover instead of quietly bypassing all three.
//
// desc is the task's free-text description (context: target scope, flag count, engagement instructions, etc.).
// It is fed alongside the goal so the decomposer no longer splits blind — the
// prompt still forbids inventing anything the two texts don't state.
//
// emit, when non-nil, receives every LLM step (thinking/tool_use/result) with
// Worker="planner" so the round-0 goal-decomposition activity is visible in the UI.
//
// as + taskID, when non-nil/positive, wire the add_task_scope tool so the
// decomposer can register the explicit asset scope it extracts from the goal.
//
// ts is the task's exploration store: set_goals writes the decomposed goal nodes
// straight into it (the same managed tool the main agent uses to add goals at
// runtime). The returned specs are read back from the store so callers can emit
// per-goal activity and detect the "LLM produced nothing" case for their fallback.
func DecomposeGoals(ctx context.Context, prov llm.Provider, dataDir, goalText, desc string, as *db.AssetStore, ts *db.ExplorationStore, taskID int64, emit func(db.Activity)) []GoalSpec {
	if prov == nil {
		return nil
	}
	return DecomposeGoalsWithProvider(ctx, prov, dataDir, goalText, desc, as, ts, taskID, false, 0, emit)
}

// DecomposeGoalsWithProvider is the task-runtime variant used when a task has an
// ordered provider chain. It preserves the same tools and write behavior while
// letting the caller own provider selection/failover. maxTokens is the profile's
// per-reply output cap (0 = send none).
func DecomposeGoalsWithProvider(ctx context.Context, prov llm.Provider, dataDir, goalText, desc string, as *db.AssetStore, ts *db.ExplorationStore, taskID int64, nonStreaming bool, maxTokens int, emit func(db.Activity)) []GoalSpec {
	if prov == nil {
		return nil
	}
	// Goal decomposition is a one-shot call without a transcript store, so agentcore
	// does not attach a session ID to ctx (it only does so with a writer; see agentcore.Prompt).
	// Gateways using the session-id header for prompt caching/sticky routing read that value;
	// for example, opencode zen returns 400 MissingSessionID without x-opencode-session, so omitting it breaks decomposition while chat works.
	// Explicitly attach a stable ID shared by decomposition requests for one exploration (for cache hits),
	// distinct from planner/worker IDs and correctly attributable by llmrec.parseSession.
	if ts != nil {
		ctx = transcript.WithSessionID(ctx, fmt.Sprintf("exp%d-goals", ts.ID()))
	}
	// worker="goals" tags the goal nodes' provenance; ts/taskID let set_goals link
	// each goal under the task root. This is the catalog's real set_goals tool, so a
	// web-edited description/schema on it applies here too.
	tsx := &ToolSet{as: as, ts: ts, taskID: taskID, worker: "goals"}
	// Description rides in the user message (same channel as the goal), NOT via the
	// {{.EngagementDescription}} template var — else a prompt that references the var
	// would inject the description twice. System prompt stays pure static instructions.
	sys := renderSystem("goals", goalsDefaultTmpl, GoalsVars{DataDir: dataDir, Now: nowStr()})
	// set_constraints is always available (independent of the asset store): the body already says to extract constraints before decomposing goals
	// (wording editable on the agent page), so only the tool needs wiring here.
	tools := []actool.CoreTool{tsx.setGoals(), tsx.setConstraints()}
	// Wire add_task_scope only when we have a real asset store + task to write to.
	// The scope-extraction tail is appended in lockstep so the prompt never asks for
	// a tool that isn't present.
	if as != nil && taskID > 0 {
		tools = append(tools, tsx.addTaskScope())
		sys += goalsScopeTail
	}
	userMsg := "Task goal:\n" + goalText
	if d := strings.TrimSpace(desc); d != "" {
		userMsg += "\n\nTask description (background information that may include target scope, flag count, or engagement instructions; reference only, do not invent anything not mentioned):\n" + d
	}
	// Use captureRun so every LLM step is emitted as an activity record (visible in
	// the plan tab under the round-0 marker). Falls back gracefully when emit is nil.
	captureEmit := func(r db.Activity) {
		if emit != nil {
			r.Worker = "planner"
			emit(r)
		}
	}
	captureRun(ctx, agentcore.Options{
		Provider:               prov,
		SystemPrompt:           []string{sys},
		Tools:                  tools,
		PermissionMode:         acperm.ModeBypass,
		DisableBackgroundTasks: true,
		// Each of the three steps (extract constraints -> register scope -> decompose goals) needs a tool call; allow enough turns to reach set_goals before wrap-up.
		MaxTurns:     8,
		NonStreaming: nonStreaming, // Use Provider.Complete when this profile selects non-streaming
		MaxTokens:    maxTokens,    // 0 = send no cap; use the server's default
	}, userMsg, captureEmit)
	// set_goals persisted the goals directly; read them back so the caller sees what
	// was written (empty slice ⇒ the LLM produced nothing ⇒ caller falls back).
	if ts == nil {
		return nil
	}
	nodes, _ := ts.ListByKind(db.KindGoal, 10000)
	var out []GoalSpec
	for _, n := range nodes {
		var p struct {
			Text      string `json:"text"`
			VulnClass string `json:"vulnclass"`
		}
		_ = json.Unmarshal(n.Payload, &p)
		if strings.TrimSpace(p.Text) != "" {
			out = append(out, GoalSpec{Text: p.Text, VulnClass: p.VulnClass})
		}
	}
	return out
}

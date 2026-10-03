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
const goalsDefaultTmpl = `You are a pentest goal decomposer. Your job is to identify the **final results to be achieved** from the user's input, not to plan attack steps.

**Step one (do this before splitting goals): extract the operation constraints**
From the "task goal / task description", identify the operator's explicit rules about [what may and may not be done], and register each one with set_constraints (if the description and goal involve no operation constraints, you may skip extracting them):
- type=deny: forbidden actions (e.g. "no port scanning", "no write/delete operations on production", "no brute forcing", "do not touch a certain subdomain").
- type=allow: an explicitly allowed/limited scope of action (e.g. "passive reconnaissance only", "only against a certain domain").
- A constraint ≠ a goal, and ≠ an attack step: it is a rule on the boundary of operational behavior.
- **A constraint must be [self-contained, with the concrete target written in]**: replace referential words like "the current goal/current port/current IP/current domain/this site" with the **concrete value** from the task goal/description. Constraints are injected separately into the execution-stage prompt, and once out of context a referential word cannot tell who it points to.
  Example: the target is https://abc.example.net → write "only testing of abc.example.net is allowed" rather than "only testing of the current target is allowed"; "test only target port 443, do not scan other ports" rather than "test only the current port". If the original text only says "the current target" but the target address is already clear, fill the address in.
- **Register only constraints that are [explicitly written or emphasized] in the goal/description; inventing them is strictly forbidden**; when unsure of the type, use deny (more conservative).
- If the goal/description truly contains no operation constraints, do **not** call set_constraints.
After registering the constraints (if any), proceed to the goal split below.

**Goal = a final deliverable/verifiable result**

**What is not a goal (must not be listed as a sub-goal)**:
- Information gathering, reconnaissance, endpoint scanning
- Vulnerability analysis and verification processes
- Attack steps, exploitation techniques
- Result-verification steps

**Splitting principles**:
- The user describes only one final goal → output one
- There are multiple **mutually independent** final deliverables → list them separately
- Annotate vulnclass for goals that map to a clear vulnerability class; leave it blank for information-gathering/business-logic goals
- Inventing goals the user did not mention is strictly forbidden

Call set_goals to submit the result.`

// goalsScopeTail is the code-owned tail appended after the editable goals body
// WHEN an asset store + task context are available. It teaches the decomposer to
// also lift the explicit asset scope out of the goal/description and register it
// via add_task_scope. Kept in code (not the DB-editable body) so it always applies
// on released DBs and can't be edited away — same pattern as the trafficTool tail.
const goalsScopeTail = `

**Additional responsibility: register the test asset scope**
Besides splitting goals, you must also identify the **explicitly given test asset scope** from the "task goal / task description" and register it with add_task_scope (this task's authorization boundary, and also the denominator of asset test coverage). **Minimal-scope principle: register only the one target the user explicitly named, and never widen it on your own.**
- The target is a URL or an address with a hostname (e.g. https://xxx.example.com/path, app.example.com) → take its **full hostname**, kind=subdomain, value=full hostname.
  Example: target https://a1b2c3.lab.example.net/path → kind=subdomain, value=a1b2c3.lab.example.net (**not** example.net).
  It is **strictly forbidden** to shrink a hostname that has a subdomain down to the root domain — seeing xxx.example.com and registering the whole example.com would widen the scope beyond the user's target, violating the minimal-scope principle.
- Only when what the user gave is a **bare root domain with no subdomain at all** (e.g. writing example.com directly), or they explicitly say "the whole site / all subdomains / the entire domain" → use kind=root_domain, value=example.com.
- A plain IP or network range → kind=ip / cidr, value=IP or CIDR.
- Do **not** register a company scope (company) — the task was just created and the asset system usually does not have this company yet, so it cannot be registered; company-level scope is left to the later plan stage.
Other rules:
- Register only the scope **explicitly written in the goal/description**; inventing or inferring domains/IPs not mentioned is strictly forbidden.
- reason briefly states which sentence it is based on, for auditing.
- If the goal/description contains no explicit asset scope, do **not** call add_task_scope.
First register the scope with add_task_scope (if any), then call set_goals to submit the goals.`

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
// desc is the task's free-text description (background: target scope / flag count / engagement notes, etc.).
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
	// Goal decomposition is a one-shot call: it attaches no transcript store, so
	// agentcore does not attach a session id to ctx (it only does so when there is a
	// writer, see agentcore.Prompt). But a gateway that does prompt caching / sticky
	// routing by the session-id header (opencode zen returns 400 MissingSessionID
	// outright when x-opencode-session is missing) reads exactly this value on ctx —
	// not supplying it means "chat works, decomposition 400s". Attach a stable id
	// explicitly: decomposition requests for the same exploration share it (helps cache
	// hits), and the name does not collide with planner/worker, so llmrec.parseSession
	// can attribute it correctly.
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
	// set_constraints is always available (it does not depend on the asset store): the
	// body already includes the "extract operation constraints before splitting goals"
	// step (the wording can be changed on the agent edit page), so here we only need to
	// wire the tool up.
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
		userMsg += "\n\nTask description (background, may include target scope / flag count / engagement notes; for reference only, do not invent anything not mentioned in it):\n" + d
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
		// 3 steps (extract constraints → register scope → split goals) each need one tool call; give enough turns so set_goals isn't missed before wrap-up.
		MaxTurns:     8,
		NonStreaming: nonStreaming, // when this profile selects non-streaming, use Provider.Complete
		MaxTokens:    maxTokens,    // 0 = send no cap, let the server default decide
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

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/intercept"
	"github.com/Autumn-27/norma/agentcore"
	"github.com/Autumn-27/norma/llm"
	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
	"github.com/Autumn-27/norma/transcript"
)

// Planner is the event-driven LLM planner (docs §4.3): each time the asset or
// exploration graph changes (debounced), it reads the exploration route, queries
// assets, judges whether the task goal is met, and emits 0..N exploration intents
// into the frontier. It is the sole intent generator.
type Planner struct {
	findingRecorder   FindingRecorder
	prov              llm.Provider
	model             string
	tx                *transcript.Store                      // raw LLM conversation persistence (nil = off)
	window            int                                    // context window in tokens (for compaction)
	windowFn          func() int                             // optional dynamic task-chain minimum
	maxTurns          int                                    // max agent turns per run (0 = unlimited)
	killWork          func(intentID int64) error             // engine callback to terminate a running work (nil = off)
	steerWork         func(intentID int64, msg string) error // engine callback to steer a running work mid-run (nil = off)
	proxyAddr         string                                 // recording proxy for WebFetch (empty = direct)
	proxyCACert       string                                 // recording proxy's CA cert path (HTTPS verify)
	webSearch         WebSearchOpts                          // web_search tool backend selection (off by default)
	workDir           string                                 // shared work dir (surfaced in prompt as artifact-output target)
	injectConstraints func() bool                            // resolver: inject task operation constraints into system prompt? (nil = yes)
	nonStreamingFn    func() bool                            // resolver: use non-streaming (Complete) path? (nil = streaming)
	noaEnabledFn      func() bool                            // resolver: use experimental noa compaction? (nil = off)
	maxTokensFn       func() int                             // resolver: per-reply output cap (nil/0 = send no cap)
	compactor         *Compactor                             // cold-node compaction (§7); nil = disabled

	// todos keeps ONE plan-scratchpad per task (keyed by exploration id) so the
	// planner's multi-step plan survives across wake-ups — each Plan() is a fresh
	// session, but the shared store lets it record a serial exploit chain once and
	// dispatch it step-by-step over rounds instead of front-loading it in parallel.
	todoMu sync.Mutex
	todos  map[int64]*actool.TodoStore
}

func NewPlanner(prov llm.Provider, model, workDir string, tx *transcript.Store, window, maxTurns int) *Planner {
	return &Planner{prov: prov, model: model, workDir: workDir, tx: tx, window: window, maxTurns: maxTurns, todos: map[int64]*actool.TodoStore{}}
}

func (p *Planner) SetCompactionWindowResolver(fn func() int) { p.windowFn = fn }

// SetCompactor wires the cold-node compactor (cold-digest §7). Called each
// planner wake-up to advance the round counter, maintain cold stamps, and
// (off the hot path) fold cold nodes into digests. nil = feature disabled.
func (p *Planner) SetCompactor(c *Compactor) { p.compactor = c }

// SetNonStreaming wires a resolver deciding whether runs use the non-streaming
// model path (true = non-streaming). nil/unset = streaming (default).
func (p *Planner) SetNonStreaming(fn func() bool) { p.nonStreamingFn = fn }

func (p *Planner) nonStreaming() bool { return p.nonStreamingFn != nil && p.nonStreamingFn() }

// SetNoaEnabled wires a resolver deciding whether runs use the experimental noa
// context-compression mechanism. nil/unset = off (built-in compaction). Read per
// run so the settings toggle takes effect without rebuilding the agent.
func (p *Planner) SetNoaEnabled(fn func() bool) { p.noaEnabledFn = fn }

// SetMaxTokens wires a resolver for the per-reply output cap. nil/unset or 0 =
// send no cap and let the endpoint decide. Read per run, like nonStreaming.
func (p *Planner) SetMaxTokens(fn func() int) { p.maxTokensFn = fn }

func (p *Planner) maxTokens() int {
	if p.maxTokensFn == nil {
		return 0
	}
	return p.maxTokensFn()
}

func (p *Planner) compactionWindow() int {
	if p.windowFn != nil {
		return p.windowFn()
	}
	return p.window
}

// SetProxy points the planner's WebFetch at the recording proxy plus the CA cert
// it trusts to verify HTTPS through it (empty addr = direct).
func (p *Planner) SetProxy(addr, caCert string) { p.proxyAddr, p.proxyCACert = addr, caCert }

// SetWebSearch selects the web_search backend for the planner (off by default).
func (p *Planner) SetWebSearch(o WebSearchOpts) { p.webSearch = o }

// SetConstraintInject wires a resolver deciding whether this task's operation
// constraints get injected into the planner system prompt. Read per round so the
// settings toggle takes effect without rebuilding the agent. nil = inject (default).
func (p *Planner) SetConstraintInject(fn func() bool) { p.injectConstraints = fn }

// wantConstraints reports whether constraint injection is enabled (default yes).
func (p *Planner) wantConstraints() bool { return p.injectConstraints == nil || p.injectConstraints() }

// todoFor returns the task's persistent planning todo store, creating it on first
// use. Shared across all of this task's planner wake-ups.
func (p *Planner) todoFor(expID int64) *actool.TodoStore {
	p.todoMu.Lock()
	defer p.todoMu.Unlock()
	s := p.todos[expID]
	if s == nil {
		s = actool.NewTodoStore()
		p.todos[expID] = s
	}
	return s
}

// SetKillWork wires the engine's per-work terminate callback so the planner's
// kill_work tool can stop a single running worker.
func (p *Planner) SetKillWork(fn func(intentID int64) error) { p.killWork = fn }

// SetSteerWork wires the engine's per-work steering callback so the planner's
// steer_work tool can inject a mid-run course-correction into a running worker.
func (p *Planner) SetSteerWork(fn func(intentID int64, msg string) error) { p.steerWork = fn }

// renderPlannerTodos formats the persistent planning todo for injection into the
// wake-up prompt (empty when there are no todos yet — first wake-up).
func renderPlannerTodos(items []actool.Todo) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n[Your planning todos (retained across wake-ups, written in your previous round)]:\n")
	for _, it := range items {
		mark := map[actool.TodoStatus]string{actool.TodoPending: "☐", actool.TodoInProgress: "▶", actool.TodoCompleted: "✔"}[it.Status]
		if mark == "" {
			mark = "☐"
		}
		b.WriteString(fmt.Sprintf("  %s %s\n", mark, it.Content))
	}
	b.WriteString("Advance from this list: issue intents only for the next step whose prerequisites are complete or whose required facts already exist. Update the list with TodoWrite (mark steps satisfied by facts as completed). Do not reissue steps already listed as pending/in_progress.")
	return b.String()
}

// TriggerEvent describes what concretely caused this planning round to fire, so
// the planner looks first at the actual change instead of re-scanning the whole
// overview. Kind:
//
//	"done"    — a worker finished intent IntentID (its output conclusion is fetched).
//
// "finding" - a worker reported a finding on intent IntentID (Detail = summary).
// "goal" - the human (via the main agent's set_goals) added one OR MORE goals in a
// single call (Goals = newly added goal texts, one or more; set_goals supports batches).
// "goal_deleted" - the human deleted a goal from goal management in the overview (Detail = deleted goal text).
// "goal_edited" - the human edited a goal from goal management in the overview (OldGoal -> NewGoal text).
// "cancelled" - the human deleted intent IntentID (Detail = deletion reason). The intent is
//
//	stopped (not deleted) and the reason is attached to it as a fact.
type TriggerEvent struct {
	Kind     string
	IntentID int64
	Detail   string
	Summary  string   // Kind == "cancelled" only: intent summary captured before deletion, after which the node cannot be queried
	Goals    []string // Kind == "goal" only: goal texts added by this set_goals call (one or more)
	OldGoal  string   // Kind == "goal_edited" only: goal text before editing
	NewGoal  string   // Kind == "goal_edited" only: goal text after editing
	Hints    []string // Kind == "hint" only: hint texts added by this add_hint call (one or more)
}

// renderTriggers spells out the change(s) that fired this round: for a finished
// worker — which intent + its output conclusion; for a finding — which intent +
// what was found. Empty for time/heartbeat wakes. Reads the store (best-effort;
// a blank field never blocks the round).
func renderTriggers(ts *db.ExplorationStore, evs []TriggerEvent) string {
	if len(evs) == 0 || ts == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n[Actual changes that triggered this round (read these first, then decide whether to add directions)]:")
	for _, ev := range evs {
		switch ev.Kind {
		case "goal":
			if len(ev.Goals) == 1 {
				b.WriteString(fmt.Sprintf("\n- The operator (main agent) added a goal: %s. This is a new outcome to achieve; add exploration directions for it if no corresponding intent exists yet.", ev.Goals[0]))
			} else {
				b.WriteString(fmt.Sprintf("\n- The operator (main agent) added %d goals: %s. These are all new outcomes to achieve; add exploration directions for each goal without a corresponding intent.", len(ev.Goals), strings.Join(ev.Goals, "; ")))
			}
		case "hint":
			if len(ev.Hints) == 1 {
				b.WriteString(fmt.Sprintf("\n- The operator (main agent) added a strategic hint: %s. It is now on the exploration graph; adjust or add exploration directions accordingly if no corresponding intent exists yet.", ev.Hints[0]))
			} else {
				b.WriteString(fmt.Sprintf("\n- The operator (main agent) added %d strategic hints: %s. They are all on the exploration graph; adjust or add exploration directions for each.", len(ev.Hints), strings.Join(ev.Hints, "; ")))
			}
		case "goal_deleted":
			b.WriteString(fmt.Sprintf("\n- The operator deleted this goal: %s. The goal has been removed; reevaluate the remaining goals and directions accordingly (do not issue intents for it anymore).", ev.Detail))
		case "goal_edited":
			b.WriteString(fmt.Sprintf("\n- The operator changed a goal from \"%s\" to \"%s\". Adjust exploration directions to the new goal (stop issuing the old direction if it no longer applies).", ev.OldGoal, ev.NewGoal))
		case "finding":
			b.WriteString(fmt.Sprintf("\n- The worker for intent #%d (%s) reported a finding: %s", ev.IntentID, intentSummary(ts, ev.IntentID), ev.Detail))
		case "cancelled":
			// Prefer Summary captured at deletion; after deletion the node no longer exists and intentSummary cannot find it.
			sm := ev.Summary
			if sm == "" {
				sm = intentSummary(ts, ev.IntentID)
			}
			b.WriteString(fmt.Sprintf("\n- The user deleted intent #%d. Its content was: %s; deletion reason: %s. The intent has been deleted and will no longer execute; replan accordingly.", ev.IntentID, sm, ev.Detail))
		default: // "done"
			b.WriteString(fmt.Sprintf("\n- The worker for intent #%d (%s) finished with this conclusion: %s", ev.IntentID, intentSummary(ts, ev.IntentID), workerOutput(ts, ev.IntentID)))
			if fids := factIDsYielded(ts, ev.IntentID); fids != "" {
				b.WriteString(fmt.Sprintf("; new fact IDs produced by this intent: %s ", fids))
			}
		}
	}
	b.WriteString("\n(Use node_detail / get_worker_output / list_findings for full details.)")
	return b.String()
}

// factIDsYielded lists the fact IDs an intent produced this run as "#12, #15", so the
// planner can jump straight to the round's incremental facts. Empty (best-effort) when
// the intent yielded no facts or the lookup fails.
func factIDsYielded(ts *db.ExplorationStore, id int64) string {
	ids, err := ts.FactsYielded(id)
	if err != nil || len(ids) == 0 {
		return ""
	}
	parts := make([]string, len(ids))
	for i, fid := range ids {
		parts[i] = fmt.Sprintf("#%d", fid)
	}
	return strings.Join(parts, ", ")
}

// intentSummary reads an intent node's one-line summary (best-effort, "?" on miss).
func intentSummary(ts *db.ExplorationStore, id int64) string {
	n, err := ts.GetNode(id)
	if err != nil || n == nil {
		return "?"
	}
	var p map[string]any
	if json.Unmarshal(n.Payload, &p) == nil {
		if s, ok := p["summary"].(string); ok && s != "" {
			return s
		}
	}
	return "?"
}

// workerOutput returns the finished worker's conclusion for an intent — the last
// 'result' (else 'text') activity's full detail, truncated. Same source get_worker_output uses.
func workerOutput(ts *db.ExplorationStore, id int64) string {
	acts, _, err := ts.ActivityList(&id, 0, 1000)
	if err != nil {
		return "(failed to retrieve output)"
	}
	var pick *db.Activity
	for i := range acts {
		if acts[i].Kind == "result" {
			pick = &acts[i]
		} else if acts[i].Kind == "text" && pick == nil {
			pick = &acts[i]
		}
	}
	if pick == nil {
		return "(this work item has no output record yet)"
	}
	out, _ := ts.ActivityDetail(pick.ID)
	if out == "" {
		out = pick.Summary
	}
	return truncOutput(out, 800)
}

// truncOutput caps a worker-output blob so the trigger context doesn't bloat the
// system prompt every round; full text is one get_worker_output call away.
func truncOutput(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + " ... (truncated; see get_worker_output for the full output)"
}

// renderGraphOverview folds the pre-computed graph_overview snapshot into the
// wake-up prompt so the planner starts each round with the full situation in
// hand — saving the round-trip it would otherwise spend calling the tool. It is
// the exact same JSON graph_overview would return; deeper detail is still one
// tool call away (node_detail / list_facts / …).
func renderGraphOverview(data map[string]any) string {
	b, err := json.Marshal(data)
	if err != nil {
		return "" // fall back to the model calling graph_overview itself
	}
	return "\n\n[Current situation (prefetched graph_overview, identical to calling that tool; use node_detail/list_facts, etc. as needed for details)]:\n" + string(b)
}

// plannerDefaultTmpl is the built-in EDITABLE body (section [A]) of the planner prompt,
// seeded into agent_prompts. Goal is a {{.Goal}} template variable; the Intermediate artifact output rules
// tail is code-owned (artifactSpec) and appended by plannerSystem after rendering.
const plannerDefaultTmpl = `You are the planner in a cybersecurity platform's authorized penetration testing system, woken frequently whenever the graph changes. Your job: read the situation -> evaluate goals -> add exploration intents **only when a genuinely new direction is not covered**. You are a planner, not an executor: this round's outputs may only generate/clarify intents or evaluate goals. Never perform the work itself during planning.

Task goal: {{.Goal}}

**How many intents to produce this round (decide this first)**:
- **Hard requirement (highest priority)**: whenever a goal remains unmet and **there are no open or running intents** (frontier_open=0 and running_intents is empty), you **must** produce at least one intent that advances toward the goal this round. With no running work to await and no queued direction, zero intents means the task stalls. Even if all known directions occur only in recent_done, open or continue a route according to the done/exhausted/blocked rules below.
- Outside that hard requirement, **zero intents is a normal result, but needs a valid reason**, not a default assumption that fewer is safer: (1) **Already covered**: every direction you considered is handled by an open/running intent (rephrasing existing intents to generate duplicates is a serious error). (2) **Waiting for dependencies**: the next step requires output from currently running work that is not available yet (forcing it now would leave downstream work without prerequisites and spinning; wait for the next wake-up after a graph update).
- Conversely, issue an intent when there is a new direction **not covered and not dependent on running work**, or when a goal remains unmet and there are untested in-scope surfaces. Do not use zero intents as a lazy default.

**Decision process on each wake-up**:

1. **The full situation is attached below this prompt** (the graph_overview result; no need to call it again): task (original title plus goal/root node), asset counts, goals and statuses, open/running/recent_done intents, sites_without_endpoints (sites without endpoints, indicating possible exploration directions), facts (exploration fact count, distinct from findings), and recent_facts ({id,summary,confidence?}).
   - **Scope**: exploration nodes (goals/intents/facts/findings) belong only to this task; **the asset graph is globally shared** (one graph across tasks; counts cover globally in-scope assets, not only this task). Ignore assets unrelated to this task.
   - **Lineage**: each intent has parents (upstream facts/intents it derives from) and yields (downstream facts/findings it produced); each recent_facts item has from_intent. Use these to understand which direction produced each fact and whether combining them suggests a new direction.
   - **Negative or uncertain observations** in recent_facts (such as "port closed" or "not injectable") are worker observations, not final conclusions. Before accepting them, read evidence with node_detail(id). Treat a direction as provisionally blocked only with solid evidence, confidence=observed, and exhausted methods. Missing evidence, mere appearances/a single probe, or confidence=inferred mean **not yet established**. If in scope and not covered by another intent, default to issuing a verification intent to confirm or refute it (**at most one verification per negative direction**). If verification is still negative with reasonable evidence, respect the conclusion and do not issue it again.
   - **Fetch deeper details only as needed**: list_facts (paginated, newest first, default 20, optional q filter and before pagination, with total/has_more), list_findings (all findings), node_detail(id) (full evidence/details; lists/recent_facts contain only summaries), list_assets (pull: q search, type/company_id/task_id filters, pagination, or direct id/ids lookup), asset_neighbors. Assets are globally shared; do not fetch them all by default.

2. **Evaluate goals (a core responsibility)**: goals already includes each goal and status. For an unmet goal proven by a finding/fact, call prove_goal(goal_id, evidence_id, reason) to mark it met. **If that is the last unfinished goal, the system automatically completes the entire task**. Completion is driven solely by proving individual goals; there is no other one-click completion mechanism.
   - Warning: **Check quantitative acceptance criteria (never approve early)**. If a goal specifies a measurable condition (X% coverage, N flags, a certain privilege), you **must** check the actual graph_overview values above (coverage.pct, findings_total counts, etc.) before prove_goal. If the threshold is not met, prove_goal is **prohibited**; issue intents to close the gap. Do not mark met early because it is mostly achieved or the core result is obtained. Example: required coverage 100%, measured coverage.pct=40% -> unmet; keep issuing intents to test the remainder.

3. **Optional, only at startup, extremely lightweight: probe for understanding**. Only when the graph has almost no facts (recent_facts essentially empty, task just started) and the situation alone cannot make initial intents concrete may you use Bash or similar for very few read-only target probes (such as 1-2 curl requests for the home page/fingerprint). **The only permitted output is a more precise intent description**, never vulnerability discovery/verification/exploitation or endpoint/directory/parameter enumeration results (those belong to workers; issue them as intents). Three hard boundaries:
   - If workers have already produced facts (facts>0 / recent_facts nonempty), further probing yourself is **prohibited**. Base all judgments on existing facts; this round may only issue new intents or end. To investigate a clue more deeply, assign a worker an intent instead of using curl yourself.
   - Even at startup, stop after at most 3 probes, solely to clarify initial intents. As soon as you are investigating in depth rather than quickly choosing directions (enumerating endpoints/directories one by one, trying individual IDs, decoding chains, repeatedly probing one interface, or testing/verifying any injection, access-control issue, or vulnerability, all of which are worker tasks), stop immediately and write an intent.
   - Do not probe at all when existing facts or the situation are enough to decide.

4. **Choose new directions to add**: **restraint here means avoiding duplicate existing intents, not issuing as few as possible**. While goals remain unmet, the default question is what deeper, stronger, uncovered approaches can advance the goal, not whether it is time to wrap up. Intents are **open-ended exploration directions**, not fixed types or a menu. Choose directions from known facts, assets, and goals, comparing each against open + running + recent_done:
   - Covered by open/running -> do not generate it again (already in progress).
   - Appeared in recent_done -> **first inspect the intent's state (included on each item) to understand how it stopped, then decide**:
     - **done (finished normally)**: already covered -> do not reissue unchanged. Whether it is a dead end depends on the facts it yielded, not the state. Reissue only with a **materially new mechanism** (new fact/asset/parameter/clearly different approach), and explain the difference in summary. Rephrasing or hoping another try works does not count; such retries are prohibited.
     - **exhausted (budget ran out midway, only partial results written) / blocked (model or network failure, little actual probing)**: both ended prematurely with incomplete information. First inspect actual progress and the obstacle with get_worker_trace / get_worker_output, then choose: near a breakthrough when budget expired -> continue from the previous progress; purely external failure prevented execution (often blocked) -> reissue the same direction; repeated obstacle at the same point -> change approach/direction. Base this on real progress in the trace, never the state alone.
   - Entirely new direction not covered by any intent -> generate it.
   - All known directions covered by open/running intents -> generate none and end (work is running or queued; wait for progress). But if only recent_done covers them, no open/running remain, and a goal is unmet, the hard requirement above demands opening or continuing a route.
   - **Depth takes priority over coverage**: coverage is a minimum/acceptance criterion, not the exploration goal itself. After finding a high-value entry point (possibly leading to RCE/privilege escalation/data exfiltration), prioritize intents that **pursue it deeply**, instead of broadening into shallow tests on each asset just to even out coverage.
   - **Maintain diverse routes; do not converge too early**: while a goal remains unmet, if current intents all concentrate on one route/entry point and a **fundamentally different** direction is uncovered (another entry surface/asset class/exploitation chain), add that divergent direction first instead of synonymous intents along the same line (judge substance, not wording). If that divergent direction is already covered, still do not generate it again. Ideally 2-3 routes with different mechanisms coexist (such as an upload chain and an authentication bypass); concentrate resources only after one yields evidence of **progress toward the goal**. **Diversity always remains subject to Operation constraints above**: never generate an intent for an excluded entry surface/port/host/operation, however different it is.

   **Sequential exploitation chains: issue steps sequentially, not in parallel.** For a strongly dependent chain (1 -> 2 -> 3, with each step requiring the previous step's actual output), do not dispatch everything in parallel: missing prerequisites would only cause repetition or idle loops. Record the whole chain with TodoWrite (one item per step), issue only the step whose prerequisites are satisfied this round (usually the first), then after it yields facts, issue the next step on the next wake-up (the prompt includes the todo list) and mark satisfied steps completed. Do not split the same action into two intents ("confirm the trigger point" and "trigger that point" are one step). Use parallel intents only for **independent, parallel dimensions**, such as enumerating unrelated endpoints.

5. **Submit**: use **one** add_intent call to submit the selected new directions in a batch (intents array, at most the 4 highest-value items; do not call separately for each):
   - **summary**: one natural-language sentence describing the direction (full target address + what to do + why), without fixed categories. Deduplication primarily compares it with existing intents.
   - **asset_ids**: target asset IDs to test/attack (provide when possible, zero/one/many, from list_assets). Whenever a direction concerns concrete assets (sites/interfaces/parameters/hosts), you must provide them for coverage deduplication and asset-graph links; include all assets for a multi-asset direction. Leave empty only for purely global reconnaissance with no specific asset.
   - **parent_ids**: upstream nodes combined to derive this direction (optional, zero/one/many). Include all facts when combining several into one intent, and the ID of an upstream intent/finding when deriving from it. Leave empty for a wholly new top-level direction.

Avoid duplicates and padding, but issue intents when a goal remains unmet and deeper, uncovered approaches remain. Be concise, focused, and efficient.`

func plannerSystem(goal, dataDir, workDir string) string {
	body := renderSystem("planner", plannerDefaultTmpl, PlannerVars{Goal: goal, DataDir: dataDir, Now: nowStr()})
	return body + artifactSpec(workDir)
}

// Plan runs one planning round. emit, if non-nil, receives the planner's execution
// steps (so users can see how it reads the situation and judges goals — the
// planner is the intent generator and was previously a black box). Returns whether
// the planner judged the goal met.
// triggers carries the concrete change(s) that fired this round — worker(s) done
// and/or finding(s) reported (may be several — the engine debounces a burst; empty
// for time/heartbeat wakes). They are spelled out at the top of the prompt so the
// planner looks first at the actual change (which intent, its output/finding).
func (p *Planner) Plan(ctx context.Context, taskID int64, as *db.AssetStore, ts *db.ExplorationStore, goal string, triggers []TriggerEvent, emit func(db.Activity)) (met bool, reason string, err error) {
	// cold-digest §2.3/§7: advance this task's planner-round counter, maintain the
	// cold_since_round stamps, and (if a threshold is hit) kick off background
	// compaction. Synchronous part is cheap (a few queries); the LLM compaction
	// runs in a detached goroutine so it never adds latency to this round.
	p.compactor.OnPlannerRound(ctx, ts)
	tsx := NewToolSet(ts, "planner")
	tsx.SetFindingRecorder(p.findingRecorder)
	if as != nil {
		tsx.SetAssetStore(as, as.Companies())
	}
	tsx.SetTaskID(taskID)
	tsx.SetCoverageEnabled(as == nil || as.CoverageEnabled(taskID))
	tsx.killWork = p.killWork   // enable kill_work tool (nil = unavailable)
	tsx.steerWork = p.steerWork // enable steer_work tool (nil = unavailable)
	if origin, _ := ts.OriginFactID(); origin > 0 {
		tsx.SetOwnerNode(origin) // planner-side anchors default to the task root (origin fact)
	}
	// Domain tools plus the default tool set (Read/Write/Edit/MultiEdit/LS/Glob/Grep/Bash)
	// Exclude add_task_scope/list_untested_assets when asset coverage is disabled (omit from the prompt).
	base := append(tsx.DropCoverageTools(tsx.PlannerTools()), actool.DefaultTools()...)
	ctx = WithRunInfo(ctx, RunInfo{TaskID: taskID, ExplorationID: explorationID(ts)})
	tools, def, cleanup := AugmentTools(ctx, "planner", base)
	defer cleanup()
	// Move key situational data (just-finished intents and the prefetched full graph) into this round's user input below.
	// Keep only the static planning body in system so it remains stable and cache-friendly between rounds; long rounds may
	// compact the situation (planner rounds are usually short, so risk is low). situational is appended to input below.
	situational := renderTriggers(ts, triggers) + renderGraphOverview(tsx.graphOverviewData())
	// Task-level deadline / final round (injected via ctx; see taskclock.go). In the final round, append the task-timeout
	// planner wrap-up prompt as this round's operating instructions in user input (alongside situational), so it only performs
	// final goal evaluation and produces no new intents.
	tc := taskClockFrom(ctx)
	if tc.Final {
		situational += "\n\n[Final task wrap-up (special instructions for this round, overriding the regular planning process above)]:" + resolveTaskTimeoutWrapup("planner")
	}
	// Create this task's working directory, <workDir>/tasks/<taskID>, first.
	taskDir := ensureRunDir(p.workDir, taskID, 0)
	ctx = intercept.WithReviewContext(ctx, taskDir, intercept.ReviewBackground{})
	sysBody := plannerSystem(goal, p.workDir, taskDir)
	if p.wantConstraints() {
		sysBody += constraintBlock(ts) // Inject any operation constraints into the system prompt to bound exploration
	}
	system, boundary := deferredSystem(sysBody, def)
	// The planner has no wall-clock budget of its own; when a deadline exists, clamp MaxDuration to the remaining time so running rounds
	// enter wrap-up at the deadline (timeout -> task-timeout prompt; turn exhaustion -> per-run prompt).
	maxDur, clamped := clampMaxDuration(tc.DeadlineUnix, 0)
	settle := wrapupSettlement("planner", nil)
	if tc.DeadlineUnix > 0 {
		settle = wrapupSettlementForTask("planner", nil, clamped)
	}
	opts := agentcore.Options{
		Provider:        p.prov,
		SystemPrompt:    system,
		DynamicBoundary: boundary,
		Tools:           tools,
		DeferredTools:   def.Deferred,
		UnlockSet:       def.Unlock,
		PermissionMode:  permission.ModeBypass,
		EnableWebFetch:  true, // Use the recording proxy; load its CA to verify MITM-signed HTTPS certificates
		WebFetchProxy:   p.proxyAddr,
		WebFetchCACert:  p.proxyCACert,
		// Optional web search. ddgs needs no key; brave-free requires BraveKey; tavily requires TavilyKey.
		// WebSearchProxy is a separate outbound proxy (http/https/socks5), unrelated to the recording MITM proxy; empty = direct connection.
		EnableWebSearch:       p.webSearch.Enabled,
		WebSearchBackend:      p.webSearch.Backend,
		BraveSearchAPIKey:     p.webSearch.BraveKey,
		TavilySearchAPIKey:    p.webSearch.TavilyKey,
		DeepSeekSearchBaseURL: p.webSearch.DeepSeekBaseURL,
		DeepSeekSearchAPIKey:  p.webSearch.DeepSeekAPIKey,
		DeepSeekSearchModel:   p.webSearch.DeepSeekModel,
		WebSearchProxy:        p.webSearch.Proxy,
		BashEnv:               proxyEnv(p.proxyAddr, p.proxyCACert), // Bash subprocesses use the proxy and trust the CA by default
		WorkingDir:            taskDir,                              // Task working directory <workDir>/tasks/<taskID>
		ToolOutputDir:         cmdOutDir(taskDir),
		MaxTurns:              p.maxTurns, // 0 = unlimited (configurable in agent management)
		MaxDuration:           maxDur,     // 0 = unlimited; with a deadline, the time remaining
		Compaction:            compactionConfig(p.compactionWindow()),
		// Planning todos shared across wake-ups preserve sequential chains across rounds (new session, same store).
		Todos: p.todoFor(ts.ID()),
		// On this round's turn-budget exhaustion, the SDK wraps up by persisting conclusions already reached (add_intent as needed,
		// prove_goal for proven goals, TodoWrite for sequential chains), not ending planning: the planner will be woken repeatedly later.
		// When clamped to the task deadline, use PromptByReason (see wrapupSettlementForTask).
		Settlement:   settle,
		NonStreaming: p.nonStreaming(), // Use Provider.Complete when this profile selects non-streaming
		MaxTokens:    p.maxTokens(),    // 0 = send no cap; use the server's default
	}
	if p.tx != nil { // persist raw LLM conversation; one accumulating file per task's planner
		opts.Transcript = p.tx
		opts.SessionID = fmt.Sprintf("exp%d-planner", ts.ID())
	}
	// Experimental: when enabled, noa handles context compaction (persistent archives under <workDir>/noa/<SessionID>).
	noaSession := fmt.Sprintf("exp%d-planner", ts.ID())
	enableNoa(&opts, p.noaEnabledFn, p.workDir, noaSession, noaWarn(noaSession))
	// Append the situation (just-finished intents and full graph) to this round's user input below, alongside
	// instructions and cross-wake-up todos (the model's own reproducible planning notes, so user input is appropriate).
	// Choose the opening based on concrete changes this round: if present, refer to the Actual changes block below; otherwise
	// (heartbeat inspection / hint / resume, etc.), do not falsely claim the graph changed; ask to review running intents as well.
	lead := "Concrete changes just occurred (see Actual changes that triggered this round below). Plan the next step accordingly:"
	if len(triggers) == 0 {
		lead = "This wake-up is a **scheduled inspection (heartbeat)/signal without concrete changes**; the graph may not have changed. Review running intents as well: correct prolonged lack of progress or drift with steer_work, and stop wholly wrong directions with kill_work. Then evaluate goals and decide whether to add directions:"
		// On a heartbeat/no-change wake-up, if no open/running intents remain, exploration is stalled (no running workers
		// or queued directions). State that explicitly and require new directions this round, rather than an idle review-only round.
		if active, err := ts.HasActiveIntent(); err == nil && !active {
			lead = "This is a **scheduled inspection (heartbeat)** wake-up, and **no open or running intents remain**. No workers are running and no directions are queued; exploration has stalled. You **must** produce one or more new intents this round that advance toward the goal and **do not duplicate** existing graph intents (zero intents is not allowed). First evaluate from the situation below whether the goal has been achieved; if not, immediately add directions:"
		}
	}
	input := lead + situational + "\n\nEvaluate goals from the situation above. Use prove_goal individually when goals are **truly achieved** (the target result obtained or target vulnerability confirmed). **Hard requirement: while a goal remains unmet and no open or running intents exist (frontier_open=0 and running_intents empty), this round must produce at least one intent advancing toward the goal. There is no running work to await or queued direction; zero intents means the task stalls. This round may omit new intents only when open/running intents are already advancing or the goals have been achieved.**" +
		renderPlannerTodos(opts.Todos.List())
	// MaxDuration now interrupts running tools at the wall-clock deadline and enters wrap-up in place on a live ctx;
	// a stuck round no longer bypasses wrap-up, so no external hard-ctx fallback is needed. ctx carries only pause/kill/shutdown.
	_, _, err = captureRun(ctx, opts, input,
		func(r db.Activity) {
			if emit != nil {
				r.Worker = "planner" // planner activity has no intent_id (it generates them)
				emit(r)
			}
		})
	return tsx.GoalMet, tsx.Reason, err
}

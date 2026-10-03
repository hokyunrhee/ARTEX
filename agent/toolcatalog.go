package agent

import (
	"context"
	"encoding/json"

	actool "github.com/Autumn-27/norma/tool"
)

// This file turns built-in tools from pure code into an enumerable catalog with DB overrides:
// - BuiltinToolSeeds() expands the three execution agents' built-in tool sets into seed records (key,
// description, parameter schema, and default agent bindings) for idempotent server-start seeding into tools.
// - The ToolResolve hook filters assembled tools by agent at runtime using tools rows,
// overrides descriptions/schemas, and injects default arguments. Keys/handlers remain in code; the DB changes only prose and defaults.
// Handlers (Call behavior) always come from code; the DB can only change model-visible descriptions and default arguments.

// ToolSeed is a seedable built-in tool snapshot: key is CoreTool.Name(), inseparably bound to the handler
// and read-only in the UI; Desc/Schema come from code definitions; Agents lists the code's default bindings.
type ToolSeed struct {
	Key    string         // CoreTool.Name(); immutable primary key
	Desc   string         // Top-level description, overridable in the UI
	Schema map[string]any // Parameter JSON Schema; structure read-only, description/default editable in the UI
	Agents []string       // Default agent keys (worker/planner/mainagent)
}

// builtinToolsByAgent constructs each execution agent's domain tools using a read-only skeleton ToolSet
// with nil stores. Constructors only put closures in Spec and never dereference stores during construction,
// so nil is safe. These tools only expose Name()/Description()/InputSchema() here; Call is never invoked.
//
// Deliberately excludes SDK general-purpose actool.DefaultTools() (Read/Write/Edit/MultiEdit/LS/Glob/
// Grep/Bash): every agent always has them, with no binding choices, and most documentation lives in Prompt(),
// while this table overrides only Description(), causing misleading partial overrides. No seed means no DB row;
// ToolResolve passes them through unchanged, preserving prior behavior. Only ARTEX domain tools enter the managed table.
func builtinToolsByAgent() map[string][]actool.CoreTool {
	ts := NewToolSet(nil, "")
	return map[string][]actool.CoreTool{
		"mainagent": ts.MainAgentTools(),
		"planner":   ts.PlannerTools(),
		"worker":    ts.WorkerTools(),
		// goals (goal decomposition) binds set_goals + set_constraints by default to persist decomposed goals
		// and extracted operation constraints. The same managed tools are shared with mainagent; the UI edits descriptions/schemas and agent bindings.
		"goals": {ts.setGoals(), ts.setConstraints()},
		// auto defaults to finding reporting and asset management; other domain tools can be enabled per agent in the UI.
		// Fresh databases receive these seeds; existing databases migrate through seedAutoDefaultBindings.
		"auto": {ts.addFinding(), ts.insertAssets(), ts.addCompanyScope(), ts.listAssets(), ts.listCompanies()},
		// pentest (independent penetration testing) defaults to asset lookup/insertion, finding reporting/lookup, and company lookup.
		// Fresh databases receive these seeds; existing databases migrate through seedPentestDefaultBindings.
		"pentest": {ts.listAssets(), ts.insertAssets(), ts.addFinding(), ts.listFindings(), ts.listCompanies()},
	}
}

// defaultUnbound tools still enter the system catalog, visible in the UI and manually bindable to agents,
// but have no default bindings. ToolResolve drops unbound tools for every agent; explicit opt-in is required.
// They remain in an agent's base tool set (such as goal_met in PlannerTools) so seeding can construct their
// description/schema and ToolResolve can retain them at runtime if the user manually binds them again.
//
// goal_met bypasses individual prove_goal calls and declares the entire task complete globally. It is powerful and risks false completion,
// duplicating automatic completion after prove_goal marks the last goal. No agent gets it by default; bind manually when needed.
var defaultUnbound = map[string]bool{"goal_met": true}

// BuiltinToolSeeds deduplicates and merges agents' built-in tool sets into seeds. Same-named tools, such as list_assets
// shared by multiple agents, become one record with the union of Agents; defaultUnbound tools always have empty bindings.
func BuiltinToolSeeds() []ToolSeed {
	byAgent := builtinToolsByAgent()
	order := []string{"mainagent", "goals", "planner", "worker", "auto", "pentest"}

	type acc struct {
		tool   actool.CoreTool
		agents []string
	}
	m := map[string]*acc{}
	var keys []string
	for _, ak := range order {
		for _, t := range byAgent[ak] {
			a, ok := m[t.Name()]
			if !ok {
				a = &acc{tool: t}
				m[t.Name()] = a
				keys = append(keys, t.Name())
			}
			a.agents = append(a.agents, ak)
		}
	}

	out := make([]ToolSeed, 0, len(keys))
	for _, k := range keys {
		a := m[k]
		agents := a.agents
		if defaultUnbound[k] {
			agents = []string{} // In the catalog and manually bindable, but not given to any agent by default ([] rather than null, like other tools)
		}
		out = append(out, ToolSeed{
			Key:    k,
			Desc:   a.tool.Description(),
			Schema: a.tool.InputSchema(),
			Agents: agents,
		})
	}
	return out
}

// ToolResolve, if set, post-processes an agent's fully-assembled tool list against
// the DB tools table: it drops tools not bound to this agent (or globally disabled)
// and wraps the rest so the model sees the DB-overridden description/schema and
// default arguments get injected. Tools with no matching DB row (MCP/skill/host tools like
// traffic) pass through untouched. nil = tools unchanged. Wired in server/assembly.go.
var ToolResolve func(ctx context.Context, agentKey string, tools []actool.CoreTool) []actool.CoreTool

// DecorateTool wraps t so Description()/InputSchema() report the DB overrides and
// Call() injects scalar parameter defaults (from schema's "default" props) whenever
// the model omitted them. Name/Prompt/permission/scheduler flags delegate to t, so
// the tool's identity and handler are unchanged. Empty desc/schema fall back to t's.
func DecorateTool(t actool.CoreTool, desc string, schema map[string]any) actool.CoreTool {
	if desc == "" {
		desc = t.Description()
	}
	if len(schema) == 0 {
		schema = t.InputSchema()
	}
	return &overriddenTool{CoreTool: t, desc: desc, schema: schema}
}

// overriddenTool is a CoreTool decorator: it embeds the original (so all behavioral
// methods — Prompt/IsReadOnly/IsConcurrencySafe/CheckPermissions/Name — delegate)
// and overrides only the model-facing description/schema plus default injection.
type overriddenTool struct {
	actool.CoreTool
	desc   string
	schema map[string]any
}

func (o *overriddenTool) Description() string         { return o.desc }
func (o *overriddenTool) InputSchema() map[string]any { return o.schema }

func (o *overriddenTool) Call(ctx context.Context, in json.RawMessage, tc *actool.ToolContext) (actool.Result, error) {
	return o.CoreTool.Call(ctx, injectDefaults(in, o.schema), tc)
}

// injectDefaults fills scalar parameter defaults declared in the (possibly edited)
// schema into the input JSON whenever the model omitted the field or left it empty/
// null. Structure (names/types/required) is untouched; only default values are merged in.
func injectDefaults(in json.RawMessage, schema map[string]any) json.RawMessage {
	defs := scalarDefaults(schema)
	if len(defs) == 0 {
		return in
	}
	m := map[string]json.RawMessage{}
	if len(in) > 0 {
		if err := json.Unmarshal(in, &m); err != nil {
			return in // non-object input: don't touch it
		}
	}
	changed := false
	for k, dv := range defs {
		if cur, ok := m[k]; !ok || isEmptyJSON(cur) {
			m[k] = dv
			changed = true
		}
	}
	if !changed {
		return in
	}
	b, err := json.Marshal(m)
	if err != nil {
		return in
	}
	return b
}

// scalarDefaults extracts properties[k]["default"] for scalar params (string/
// integer/number/boolean). Array/object defaults are skipped: merging them is
// ambiguous and not worth the surprise.
func scalarDefaults(schema map[string]any) map[string]json.RawMessage {
	props, _ := schema["properties"].(map[string]any)
	if len(props) == 0 {
		return nil
	}
	out := map[string]json.RawMessage{}
	for name, raw := range props {
		p, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		dv, ok := p["default"]
		if !ok || dv == nil {
			continue
		}
		switch p["type"] {
		case "string", "integer", "number", "boolean":
			if b, err := json.Marshal(dv); err == nil {
				out[name] = b
			}
		}
	}
	return out
}

func isEmptyJSON(raw json.RawMessage) bool {
	s := string(raw)
	return s == "null" || s == `""`
}

package agent

import (
	"encoding/json"
	"fmt"

	"github.com/Autumn-27/artex/db"
	actool "github.com/Autumn-27/norma/tool"
)

const findingIDGuidance = "\n\n**Finding ID conventions**: finding_id is the independent finding record ID; finding_node_id is the exploration node ID. The id fields in list_findings / list_task_findings / node_detail / get_task_node_detail remain exploration node IDs; read the independent ID from finding_id in the same response. get_finding_traffic / bind_finding_traffic use the independent finding_id. The legacy update_finding_report finding_id parameter still takes finding_node_id. Do not use the number in report_finding's first line for evidence tools, or guess other numbers after an ID error."

// The server supplies the persisted setting. A missing setting/host is off.
// Consulted at assembly and again on writes so an already-running session
// cannot keep binding after the user switches the feature off.
var FindingTrafficBindingEnabled func() bool

func findingTrafficBindingEnabled() bool {
	return FindingTrafficBindingEnabled != nil && FindingTrafficBindingEnabled()
}

// Applied after ToolResolve: user descriptions and prompts remain intact, while
// all actual reporters (including Planner and custom chat agents) see the same
// API contract. Disabled/unbound tools are never reintroduced here.
func findingWorkflowTools(agentKey string, tools []actool.CoreTool) ([]actool.CoreTool, string) {
	if !findingTrafficBindingEnabled() {
		out := make([]actool.CoreTool, 0, len(tools))
		for _, tool := range tools {
			if tool.Name() == "bind_finding_traffic" {
				continue
			}
			if agentKey == "reporter" && (tool.Name() == "traffic_search" || tool.Name() == "traffic_get" || tool.Name() == "traffic_blob") {
				continue
			}
			switch tool.Name() {
			case "report_finding", "add_hint", "add_task_hint":
				// Work on a copy: toggling back on must restore the original schema.
				raw, _ := json.Marshal(tool.InputSchema())
				var schema map[string]any
				if json.Unmarshal(raw, &schema) == nil {
					stripTrafficParameters(schema)
					tool = DecorateTool(tool, tool.Description(), schema)
				}
			}
			out = append(out, tool)
		}
		return out, ""
	}
	out := append([]actool.CoreTool(nil), tools...)
	has := map[string]bool{}
	for i, tool := range out {
		has[tool.Name()] = true
		note := ""
		switch tool.Name() {
		case "report_finding":
			note = "\nBy default, the reporter agent verifies and binds traffic before writing the report. In evidence, the reporting agent preserves verification commands, key output, existing real traffic IDs, and their purposes for the reporter to check against execution records; no extra packet lookup is required solely for binding. Explicit immediate binding remains supported: traffic_refs or evidence_hint_id may provide verified references; the latter reads structured references from the specified hint in this task. Any invalid reference causes the entire report submission to fail. TCP or no-packet cases do not require these optional parameters. Returned finding_id and finding_node_id refer to the independent record and exploration node, respectively."
		case "add_hint", "add_task_hint":
			note = "\nWhen handing off a confirmed vulnerability, preserve verified traffic IDs, roles, notes, and order in the corresponding hint's traffic_refs (top-level for a single hint, or in the matching hints element for a batch), and explain in text which specific vulnerability they prove. Do not hand off only text while discarding existing traffic references. Unverified candidates must not be passed as evidence."
		case "get_finding_traffic", "bind_finding_traffic", "list_findings", "list_task_findings", "node_detail", "get_task_node_detail", "update_finding_report":
			note = findingIDGuidance
		}
		if note != "" {
			out[i] = DecorateTool(tool, tool.Description()+note, tool.InputSchema())
		}
	}
	guidance := ""
	if has["report_finding"] || has["add_task_hint"] || has["add_hint"] {
		guidance = "\n\n**Traffic evidence handoff (optional)**: by default, the reporter agent automatically binds traffic after the finding is recorded and before writing the report. The reporting agent should preserve verification commands, key output, real traffic IDs already available, and their purposes in evidence, including intent_id within a task for traceability. No extra packet lookup is needed solely for binding. When Auto / Planner reports on behalf of a worker, retain the worker's existing references. add_hint / add_task_hint can hand off traffic_refs; explicit immediate binding remains supported through report_finding's traffic_refs / evidence_hint_id. Register TCP or no-packet findings normally; do not guess IDs or repeat probes solely to fill in missing packets."
		if has["add_task_hint"] && !has["add_hint"] {
			guidance += "\nIn a platform conversation without task context, do not call report_finding directly. Hand off through add_task_hint to the existing corresponding task for its agent to register, then verify the result with list_task_findings."
		}
		if has["prove_goal"] || has["goal_met"] {
			guidance += "\nBefore declaring a goal complete, finish reporting/handing off the evidence already available from this run. Do not end the task or cancel workers just because a text finding was registered while evidence handoff remains incomplete; no-packet cases require neither waiting nor forced traffic capture."
		}
	}
	if has["update_finding_report"] && has["bind_finding_traffic"] && has["get_finding_traffic"] {
		guidance += "\n\n**Automatic traffic binding before reporting (enabled)**: you are responsible for verifying and binding traffic for the finding that triggered this run before writing its report. First obtain explicit finding_id and finding_node_id values from report_finding's returned JSON or get_task_node_detail / list_task_findings. Read the finding details, the corresponding intent's execution trace, and existing evidence list, prioritizing real IDs handed off by the reporting agent. If this verification used HTTP and traffic tools are available, filter candidates with traffic_search, then verify each request/response with traffic_get to ensure it actually supports this vulnerability. Domain names and timestamps filter candidates but do not establish ownership. Bind confirmed evidence in reproduction order with bind_finding_traffic(finding_id, traffic_refs), choosing baseline / proof / verification / supporting and explaining each purpose. Operate only on this finding; do not create duplicate findings or probe the target again. After a successful binding, call get_finding_traffic again for the latest version, read the required bodies, then pass the version actually read as evidence_version to update_finding_report (whose finding_id parameter still takes finding_node_id). Do not append existing bindings again. For TCP, uncaptured traffic, unavailable tools, or no exact match, skip automatic binding and write the report normally from text/command evidence, explaining why; do not guess to fill in traffic. Do not claim success after a binding failure; preserve existing evidence and explain the missing binding in the report."
	}
	if guidance != "" || has["get_finding_traffic"] || has["update_finding_report"] {
		guidance += findingIDGuidance
	}
	return out, guidance
}

func stripTrafficParameters(schema map[string]any) {
	props, _ := schema["properties"].(map[string]any)
	delete(props, "traffic_refs")
	delete(props, "evidence_hint_id")
	if required, ok := schema["required"].([]any); ok {
		kept := required[:0]
		for _, key := range required {
			if key != "traffic_refs" && key != "evidence_hint_id" {
				kept = append(kept, key)
			}
		}
		schema["required"] = kept
	}
	if hints, ok := props["hints"].(map[string]any); ok {
		if items, ok := hints["items"].(map[string]any); ok {
			stripTrafficParameters(items)
		}
	}
}

// HintTrafficSchema is shared by the task-local and cross-task hint tools.
func HintTrafficSchema() map[string]any {
	return map[string]any{"type": "array", "description": "Optional: verified traffic references supporting the specific vulnerability in this hint, preserving order. After handoff, report_finding may carry these references through evidence_hint_id.", "items": obj(map[string]any{"traffic_id": str("Real traffic ID"), "role": str("baseline / proof / verification / supporting"), "note": str("Which conclusion this traffic supports")}, "traffic_id")}
}

func (t *ToolSet) findingRefsFromHint(hintID int64, explicit []db.TrafficRef) ([]db.TrafficRef, error) {
	if hintID <= 0 {
		return db.NormalizeTrafficRefs(explicit)
	}
	n, err := t.ts.GetNode(hintID) // local store only: inherited hints cannot supply evidence
	if err != nil {
		return nil, err
	}
	if n == nil || n.Kind != db.KindHint {
		return nil, fmt.Errorf("evidence_hint_id=%d must identify a hint node in this task (inherited hints cannot be used directly for binding)", hintID)
	}
	var payload struct {
		Refs []db.TrafficRef `json:"traffic_refs"`
	}
	if err := json.Unmarshal(n.Payload, &payload); err != nil {
		return nil, err
	}
	return db.NormalizeTrafficRefs(append(append([]db.TrafficRef{}, explicit...), payload.Refs...))
}

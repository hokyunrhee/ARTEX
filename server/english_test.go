package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/traffic"
)

func englishTestServer(t *testing.T) *Server {
	t.Helper()
	dsn, _, err := db.DSN()
	if err != nil {
		t.Skipf("PostgreSQL not configured: %v", err)
	}
	pg, err := db.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pg.Close() })
	return &Server{m: &Manager{pg: pg}}
}

func englishResetFlag(t *testing.T, pg *db.DB, key string) {
	t.Helper()
	value, exists, err := pg.GetSetting(key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if exists {
			pg.SetSetting(key, value)
		} else {
			pg.Exec(`DELETE FROM settings WHERE key=$1`, key)
		}
	})
	if _, err := pg.Exec(`DELETE FROM settings WHERE key=$1`, key); err != nil {
		t.Fatal(err)
	}
}

func TestEnglishFrozenToolsCoverEveryConstructor(t *testing.T) {
	s := &Server{}
	targets := map[string]db.EnglishToolDefault{}
	for _, target := range s.englishToolDefaults() {
		targets[target.Key] = target
	}
	legacy := db.LegacyEnglishTools()
	if len(legacy) != 51 {
		t.Fatalf("unexpected frozen tool count: %d", len(legacy))
	}
	for _, old := range legacy {
		target, ok := targets[old.Key]
		if !ok || target.Description == "" || !json.Valid(target.Schema) {
			t.Errorf("missing constructor metadata for %s", old.Key)
		}
	}
}

func TestEnglishReporterTriggerUpgradesKnownVersionsOnly(t *testing.T) {
	s := englishTestServer(t)
	pg := s.m.pg
	englishResetFlag(t, pg, "reporter_trigger_english_v1")
	var ids []int64
	bodies := []string{reporterToolCallMessageV1, reporterToolCallMessageV2, "Operator trigger", reporterToolCallMessageV2}
	for i, body := range bodies {
		var id int64
		if err := pg.QueryRow(`INSERT INTO agent_triggers(agent_key,enabled,on_tool_call,tool_call_message,tool_names,interval_sec) VALUES('reporter',false,$1,$2,'operator_tool',321) RETURNING id`, i != 3, body).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		t.Cleanup(func() { pg.Exec(`DELETE FROM agent_triggers WHERE id=$1`, id) })
	}
	if err := s.migrateEnglishReporterTrigger(); err != nil {
		t.Fatal(err)
	}
	for i, id := range ids {
		var body, names string
		var enabled bool
		var interval int
		if err := pg.QueryRow(`SELECT tool_call_message,tool_names,enabled,interval_sec FROM agent_triggers WHERE id=$1`, id).Scan(&body, &names, &enabled, &interval); err != nil {
			t.Fatal(err)
		}
		want := bodies[i]
		if i < 2 {
			want = reporterToolCallMessage
		}
		if body != want || names != "operator_tool" || enabled || interval != 321 {
			t.Fatalf("trigger %d lost custom settings", i)
		}
	}
	if !strings.Contains(reporterToolCallMessage, "MUST pass evidence_version") {
		t.Fatal("trigger lost required evidence version directive")
	}
}

func TestEnglishRefreshPreservesCustomToolWithoutHistoricalFlags(t *testing.T) {
	s := englishTestServer(t)
	pg := s.m.pg
	key := "get_task_worker_trace"
	existing, err := pg.GetTool(key)
	if err != nil {
		t.Fatal(err)
	}
	if existing == nil {
		if err := pg.SeedTool(key, "placeholder", json.RawMessage(`{}`), nil); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { pg.Exec(`DELETE FROM tools WHERE key=$1`, key) })
	} else {
		t.Cleanup(func() {
			agents, _ := json.Marshal(existing.Agents)
			pg.UpdateTool(key, existing.Description, existing.Schema, agents, existing.Enabled)
		})
	}
	for _, flag := range []string{"tool_schema_refresh_v7_list_facts_paging", "tool_" + key + "_description_english_v1", "tool_" + key + "_schema_english_v1"} {
		englishResetFlag(t, pg, flag)
	}
	custom := json.RawMessage(`{"type":"object","properties":{"task_id":{"type":"string","description":"Operator description","default":"operator-default"}}}`)
	if err := pg.UpdateTool(key, "Operator tool", custom, json.RawMessage(`["custom-agent"]`), false); err != nil {
		t.Fatal(err)
	}
	s.refreshBuiltinToolSchemas()
	got, err := pg.GetTool(key)
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != "Operator tool" || !strings.Contains(string(got.Schema), "operator-default") || got.Enabled || len(got.Agents) != 1 || got.Agents[0] != "custom-agent" {
		t.Fatalf("unflagged historical refresh changed operator data: %+v", got)
	}
}

func TestEnglishHistoricalPromptReseedsPreserveCustomVersions(t *testing.T) {
	s := englishTestServer(t)
	pg := s.m.pg
	for key, run := range map[string]func(){"goals": s.reseedGoalsPrompt, "planner": s.reseedPlannerPrompt, "mainagent": s.reseedMainAgentPrompt, "worker": s.reseedWorkerPrompt} {
		t.Run(key, func(t *testing.T) {
			a, err := pg.GetAgentByKey(key)
			if err != nil || a == nil {
				t.Fatal(err)
			}
			var oldID *int64
			if err := pg.QueryRow(`SELECT current_prompt_id FROM agents WHERE id=$1`, a.ID).Scan(&oldID); err != nil {
				t.Fatal(err)
			}
			var version int
			if err := pg.QueryRow(`SELECT COALESCE(MAX(version),0) FROM agent_prompts WHERE agent_id=$1`, a.ID).Scan(&version); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				pg.Exec(`UPDATE agents SET current_prompt_id=$2 WHERE id=$1`, a.ID, oldID)
				pg.Exec(`DELETE FROM agent_prompts WHERE agent_id=$1 AND version>$2`, a.ID, version)
			})
			for _, flag := range []string{key + "_prompt_english_v1", "goals_prompt_constraint_step_v1", "mainagent_prompt_goalless_intent_v1", "planner_prompt_compact_realistic_v2", "worker_prompt_compact_v4"} {
				englishResetFlag(t, pg, flag)
			}
			if _, err := pg.SavePrompt(a.ID, "Operator prompt for "+key, "customized", "operator"); err != nil {
				t.Fatal(err)
			}
			run()
			got, err := pg.CurrentPrompt(a.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got != "Operator prompt for "+key {
				t.Fatalf("historical reseed clobbered %s", key)
			}
			flag, _, err := pg.GetSetting(key + "_prompt_english_v1")
			if err != nil {
				t.Fatal(err)
			}
			if flag == "true" && !db.EnglishTargetReady(agent.BuiltinPromptSeeds()[key]) {
				t.Fatal("untranslated prompt consumed English flag")
			}
		})
	}
}

func TestEnglishTrafficDescriptionDefaultsAndCustomization(t *testing.T) {
	s := englishTestServer(t)
	pg := s.m.pg
	old, err := pg.GetTool("traffic_search")
	if err != nil {
		t.Fatal(err)
	}
	if old == nil {
		if err := pg.SeedTool("traffic_search", legacyTrafficSearchDescriptionV2, json.RawMessage(`{}`), nil); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { pg.Exec(`DELETE FROM tools WHERE key='traffic_search'`) })
	} else {
		t.Cleanup(func() {
			agents, _ := json.Marshal(old.Agents)
			pg.UpdateTool("traffic_search", old.Description, old.Schema, agents, old.Enabled)
		})
	}
	englishResetFlag(t, pg, "finding_workflow_tools_v4_english")
	for _, body := range []string{legacyTrafficSearchDescriptionV2, legacyTrafficSearchDescriptionV3, "Operator search description"} {
		pg.Exec(`DELETE FROM settings WHERE key='finding_workflow_tools_v4_english'`)
		if _, err := pg.Exec(`UPDATE tools SET description=$1,enabled=false WHERE key='traffic_search'`, body); err != nil {
			t.Fatal(err)
		}
		if err := s.migrateEnglishTrafficDescription(); err != nil {
			t.Fatal(err)
		}
		got, err := pg.GetTool("traffic_search")
		if err != nil {
			t.Fatal(err)
		}
		want := body
		if db.EnglishTargetReady(traffic.TrafficSearchDescription) && body != "Operator search description" {
			want = traffic.TrafficSearchDescription
		}
		if got.Description != want || got.Enabled {
			t.Fatal("traffic migration lost default/custom behavior")
		}
		flag, _, _ := pg.GetSetting("finding_workflow_tools_v4_english")
		if !db.EnglishTargetReady(traffic.TrafficSearchDescription) && flag == "true" {
			t.Fatal("untranslated traffic target consumed flag")
		}
	}
}

// Translating help text must not change required fields, types, enums, defaults,
// or parameter keys in any of the 51 tools frozen from the original server.
func TestEnglishToolSchemasKeepWireContract(t *testing.T) {
	s := &Server{}
	targets := map[string]db.EnglishToolDefault{}
	for _, target := range s.englishToolDefaults() {
		targets[target.Key] = target
	}
	var stripDescriptions func(any)
	stripDescriptions = func(value any) {
		switch node := value.(type) {
		case map[string]any:
			if _, ok := node["description"].(string); ok {
				delete(node, "description")
			}
			for _, child := range node {
				stripDescriptions(child)
			}
		case []any:
			for _, child := range node {
				stripDescriptions(child)
			}
		}
	}
	for _, old := range db.LegacyEnglishTools() {
		t.Run(old.Key, func(t *testing.T) {
			var before, after any
			if err := json.Unmarshal(old.Schema, &before); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(targets[old.Key].Schema, &after); err != nil {
				t.Fatal(err)
			}
			stripDescriptions(before)
			stripDescriptions(after)
			a, _ := json.Marshal(before)
			b, _ := json.Marshal(after)
			if string(a) != string(b) {
				t.Fatalf("wire schema changed: legacy=%s target=%s", a, b)
			}
		})
	}
}

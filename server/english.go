package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sort"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/traffic"
	actool "github.com/Autumn-27/norma/tool"
)

const reporterEnglishDescription = "Write detailed vulnerability reports: run automatically when a finding is recorded, retrieve evidence and execution traces, write a Markdown report, and save it to the finding."
const retesterEnglishDescription = "Start manually from finding details, read the original evidence, and save an independent retest verdict."

// englishToolDefaults reads constructors, never database overrides. Migration is
// per column/key so untranslated targets remain retryable during staged updates.
func (s *Server) englishToolDefaults() []db.EnglishToolDefault {
	byKey := map[string]db.EnglishToolDefault{}
	for _, seed := range agent.BuiltinToolSeeds() {
		raw, _ := json.Marshal(seed.Schema)
		byKey[seed.Key] = db.EnglishToolDefault{Key: seed.Key, Description: seed.Desc, Schema: raw}
	}
	tools := append([]actool.CoreTool{}, traffic.SeedToolMetas()...)
	tools = append(tools, s.orchestrationTools()...)
	tools = append(tools, s.platformTools()...)
	tools = append(tools, s.findingRetestTools()...)
	for _, tool := range tools {
		raw, _ := json.Marshal(tool.InputSchema())
		byKey[tool.Name()] = db.EnglishToolDefault{Key: tool.Name(), Description: tool.Description(), Schema: raw}
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]db.EnglishToolDefault, 0, len(keys))
	for _, key := range keys {
		result = append(result, byKey[key])
	}
	return result
}

// migrateEnglishDefaults runs after retester creation and before background
// scheduling. Unknown or edited rows are preserved, including on old databases
// whose historical unconditional reseed flags were never set.
func (s *Server) migrateEnglishDefaults() error {
	pg := s.m.pg
	prompts := agent.BuiltinPromptSeeds()
	prompts["reporter"] = agent.ReporterDefaultPrompt
	prompts["retester"] = agent.RetesterDefaultPrompt
	keys := make([]string, 0, len(prompts))
	for key := range prompts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := pg.MigrateEnglishPrompt(key, prompts[key], agent.LegacyPromptDigests(key)); err != nil {
			return err
		}
	}
	// The generic starter was also persisted for newly created custom agents.
	agents, err := pg.ListAgents()
	if err != nil {
		return err
	}
	for _, a := range agents {
		if _, known := prompts[a.Key]; !known && !a.Builtin {
			if err := pg.MigrateEnglishPrompt(a.Key, agent.DefaultAssistantPrompt, agent.LegacyPromptDigests("assistant_fallback")); err != nil {
				return err
			}
		}
	}
	if err := pg.MigrateEnglishAgentMetadata("reporter", "Reporter", reporterEnglishDescription); err != nil {
		return err
	}
	if err := pg.MigrateEnglishAgentMetadata("retester", "Finding retest", retesterEnglishDescription); err != nil {
		return err
	}
	// Run the two recognized traffic-description upgrades before the per-tool pass
	// classifies a V2 description as an operator customization.
	if err := s.migrateEnglishTrafficDescription(); err != nil {
		return err
	}
	for _, target := range s.englishToolDefaults() {
		if err := pg.MigrateEnglishTool(target); err != nil {
			return err
		}
	}
	return s.migrateEnglishReporterTrigger()
}

func (s *Server) migrateEnglishReporterTrigger() error {
	return s.m.pg.EnglishMigration("reporter_trigger_english_v1", db.EnglishTargetReady(reporterToolCallMessage), func(tx *sql.Tx) ([]string, error) {
		rows, err := tx.Query(`SELECT id,tool_call_message FROM agent_triggers WHERE agent_key='reporter' AND on_tool_call FOR UPDATE`)
		if err != nil {
			return nil, err
		}
		var ids []int64
		var preserved []string
		for rows.Next() {
			var id int64
			var body string
			if err := rows.Scan(&id, &body); err != nil {
				rows.Close()
				return nil, err
			}
			if body == reporterToolCallMessageV1 || body == reporterToolCallMessageV2 {
				ids = append(ids, id)
			} else if body != reporterToolCallMessage {
				preserved = append(preserved, fmt.Sprintf("reporter trigger #%d", id))
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			if _, err := tx.Exec(`UPDATE agent_triggers SET tool_call_message=$2,updated_at=now() WHERE id=$1`, id, reporterToolCallMessage); err != nil {
				return nil, err
			}
		}
		return preserved, nil
	})
}

func (s *Server) migrateEnglishTrafficDescription() error {
	return s.m.pg.EnglishMigration("finding_workflow_tools_v4_english", db.EnglishTargetReady(traffic.TrafficSearchDescription), func(tx *sql.Tx) ([]string, error) {
		var current string
		var system bool
		err := tx.QueryRow(`SELECT description,system FROM tools WHERE key='traffic_search' FOR UPDATE`).Scan(&current, &system)
		if err == sql.ErrNoRows {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if !system || current == traffic.TrafficSearchDescription {
			return nil, nil
		}
		if current != legacyTrafficSearchDescriptionV2 && current != legacyTrafficSearchDescriptionV3 {
			return []string{"traffic_search description"}, nil
		}
		_, err = tx.Exec(`UPDATE tools SET description=$1,updated_at=now() WHERE key='traffic_search'`, traffic.TrafficSearchDescription)
		return nil, err
	})
}

func (s *Server) reseedEnglishPrompt(key string) {
	if err := s.m.pg.MigrateEnglishPrompt(key, agent.BuiltinPromptSeeds()[key], agent.LegacyPromptDigests(key)); err != nil {
		log.Printf("[english] upgrade %s prompt failed: %v", key, err)
	}
}

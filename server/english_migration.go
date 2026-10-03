package server

import (
	"encoding/json"
	"log"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/traffic"
)

// This file holds the one-time migrations that upgrade an existing database's seeded
// Chinese defaults to the English ones introduced by the language conversion.
//
// Deliberate deviation from the earlier reseed* passes (server/orchestration.go), which
// append a new default version UNCONDITIONALLY: these compare the row's current value
// against the frozen 160fe13-and-earlier defaults (prompt bodies by SHA-256 digest in
// agent/legacy_prompts.go, short strings by literal) and upgrade ONLY rows still equal to
// one of them. A row an operator customized is never equal to a frozen default, so it is
// left untouched and logged once at startup. Do not "simplify" this back to an
// unconditional reset — that would clobber operator edits on upgrade.

// Legacy seeded names/descriptions used as migration compare keys (kept Chinese on
// purpose; allowlisted for the no-CJK gate).
const (
	legacyReporterAgentName = "报告撰写"
	legacyReporterAgentDesc = "漏洞详细报告撰写：发现漏洞时自动触发，查取证据与执行过程后写 Markdown 报告并回写。"
	legacyRetesterAgentName = "漏洞复测"
	legacyRetesterAgentDesc = "从漏洞详情手动启动，读取原证据并保存独立复测结论。"
)

// englishReporterAgentName/Desc and englishRetesterAgentName/Desc are the English
// successors (kept in sync with agent seeding in orchestration.go / finding_retests.go).
const (
	englishReporterAgentName = "Reporter"
	englishReporterAgentDesc = "Detailed vulnerability report writing: triggered automatically when a finding is recorded; gathers evidence and the execution trace, then writes and saves a Markdown report."
	englishRetesterAgentName = "Finding retest"
	englishRetesterAgentDesc = "Started manually from a finding's detail page; reads the original evidence and saves an independent retest verdict."
)

// reseedPromptsEnglishV1 upgrades each built-in agent's prompt body from a frozen
// Chinese default to the current English default, for existing databases only. A row
// whose current body hashes to a historical default is reset (old version kept in
// history); a customized row is left untouched and logged. Fresh installs already seed
// English, so their current body is not in the frozen set and nothing happens.
func (s *Server) reseedPromptsEnglishV1() {
	const flag = "prompt_bodies_english_v1"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	defer func() { _ = s.m.pg.SetSetting(flag, "true") }() // attempt once

	seeds := agent.BuiltinPromptSeeds()
	// agentKey -> (digest map key, English body). The digest map key matches
	// agent/legacy_prompts.go; reporter/retester bodies are not in BuiltinPromptSeeds.
	type promptTarget struct {
		agentKey  string
		digestKey string
		english   string
	}
	targets := []promptTarget{
		{"goals", "goals", seeds["goals"]},
		{"planner", "planner", seeds["planner"]},
		{"mainagent", "mainagent", seeds["mainagent"]},
		{"worker", "worker", seeds["worker"]},
		{"auto", "auto", seeds["auto"]},
		{"pentest", "pentest", seeds["pentest"]},
		{"reporter", "reporter", agent.ReporterDefaultPrompt},
		{db.FindingRetestAgentKey, "retester", agent.RetesterDefaultPrompt},
	}
	for _, t := range targets {
		if t.english == "" {
			continue
		}
		a, err := s.m.pg.GetAgentByKey(t.agentKey)
		if err != nil || a == nil {
			continue // fresh DB: agent not created yet → seedPrompts handles English
		}
		cur, err := s.m.pg.CurrentPrompt(a.ID)
		if err != nil || cur == "" || cur == t.english {
			continue
		}
		if !agent.IsLegacyPromptBody(t.digestKey, cur) {
			log.Printf("[prompts] %s prompt is customized — left unchanged by the English migration", t.agentKey)
			continue
		}
		if _, err := s.m.pg.ResetPromptToDefault(a.ID, t.english); err != nil {
			log.Printf("[prompts] %s English reseed failed: %v", t.agentKey, err)
			continue
		}
		log.Printf("[prompts] %s prompt reset to the English default (was an unmodified Chinese default)", t.agentKey)
	}
}

// migrateReporterRetesterAgentMetaEnglish renames the reporter/retester agent rows and
// descriptions from Chinese to English — but only rows still equal to the frozen Chinese
// seed, so a renamed/edited row is left as is.
func (s *Server) migrateReporterRetesterAgentMetaEnglish() {
	const flag = "reporter_retester_meta_english_v1"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	defer func() { _ = s.m.pg.SetSetting(flag, "true") }()
	exec := func(key, zhName, zhDesc, enName, enDesc string) {
		if _, err := s.m.pg.Exec(
			`UPDATE agents SET name=$2, description=$3 WHERE key=$1 AND name=$4 AND description=$5`,
			key, enName, enDesc, zhName, zhDesc); err != nil {
			log.Printf("[agents] English name/desc migration for %s failed: %v", key, err)
		}
	}
	exec("reporter", legacyReporterAgentName, legacyReporterAgentDesc, englishReporterAgentName, englishReporterAgentDesc)
	exec(db.FindingRetestAgentKey, legacyRetesterAgentName, legacyRetesterAgentDesc, englishRetesterAgentName, englishRetesterAgentDesc)
}

// upgradeReporterTriggerMessageEnglish rewrites the reporter's finding-trigger message to
// the current English default, for triggers still carrying one of the two frozen Chinese
// defaults (V1, or the pre-conversion current message V2). A user-edited message is left
// untouched. reporterToolCallMessageV1/V2 stay Chinese as compare fingerprints.
func (s *Server) upgradeReporterTriggerMessageEnglish() {
	const flag = "reporter_trigger_english_v1"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	defer func() { _ = s.m.pg.SetSetting(flag, "true") }()
	triggers, err := s.m.pg.ListTriggersFor("reporter")
	if err != nil {
		log.Printf("[reporter] list triggers for English migration failed: %v", err)
		return
	}
	for _, t := range triggers {
		if !t.OnToolCall {
			continue
		}
		if t.ToolCallMessage != reporterToolCallMessageV1 && t.ToolCallMessage != reporterToolCallMessageV2 {
			continue // user-edited or already English
		}
		t.ToolCallMessage = reporterToolCallMessage
		if err := s.m.pg.UpdateTrigger(t); err != nil {
			log.Printf("[reporter] English trigger-message migration failed: %v", err)
			return
		}
		log.Printf("[reporter] finding-trigger message reset to the English default")
	}
}

// migrateBuiltinToolTextEnglish upgrades every system tool row's description and schema
// from a frozen Chinese default to the current (English) code default, for existing
// databases only. A row holding an operator customization (its current desc/schema hashes
// to nothing in the frozen set) is preserved. Fresh installs seed English directly, so
// their current values are not in the frozen set and nothing changes. Agent bindings and
// the enabled flag are never touched. Called from seedFindingWorkflowTools so the direct
// test call exercises it.
func (s *Server) migrateBuiltinToolTextEnglish() {
	const flag = "builtin_tool_text_english_v1"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	rows, err := s.m.pg.ListTools()
	if err != nil {
		log.Printf("[tools] English text migration: list tools failed: %v", err)
		return
	}
	// Build the English code-default desc/schema for every built-in tool key.
	type toolDefault struct {
		desc   string
		schema json.RawMessage
	}
	defaults := map[string]toolDefault{}
	addDefault := func(key, desc string, schema any) {
		raw, _ := json.Marshal(schema)
		defaults[key] = toolDefault{desc, raw}
	}
	for _, sd := range agent.BuiltinToolSeeds() {
		addDefault(sd.Key, sd.Desc, sd.Schema)
	}
	for _, t := range traffic.SeedToolMetas() {
		addDefault(t.Name(), t.Description(), t.InputSchema())
	}
	for _, t := range s.orchestrationTools() {
		addDefault(t.Name(), t.Description(), t.InputSchema())
	}
	for _, t := range s.platformTools() {
		addDefault(t.Name(), t.Description(), t.InputSchema())
	}
	for _, t := range s.findingRetestTools() {
		addDefault(t.Name(), t.Description(), t.InputSchema())
	}
	for _, row := range rows {
		if !row.System {
			continue
		}
		def, ok := defaults[row.Key]
		if !ok {
			continue
		}
		if agent.IsLegacyToolDesc(row.Key, row.Description) && row.Description != def.desc {
			if _, err := s.m.pg.Exec(`UPDATE tools SET description=$2,updated_at=now() WHERE key=$1 AND system AND description=$3`,
				row.Key, def.desc, row.Description); err != nil {
				log.Printf("[tools] English desc migration for %s failed: %v", row.Key, err)
				return // leave flag unset → retry next start
			}
		}
		if agent.IsLegacyToolSchema(row.Key, row.Schema) && agent.ToolSchemaSHA256(row.Schema) != agent.ToolSchemaSHA256(def.schema) {
			if _, err := s.m.pg.Exec(`UPDATE tools SET schema=$2::jsonb,updated_at=now() WHERE key=$1 AND system AND schema=$3::jsonb`,
				row.Key, string(def.schema), string(row.Schema)); err != nil {
				log.Printf("[tools] English schema migration for %s failed: %v", row.Key, err)
				return
			}
		}
	}
	_ = s.m.pg.SetSetting(flag, "true")
}

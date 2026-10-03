package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func englishDB(t *testing.T) *DB {
	t.Helper()
	d, err := Open(testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}
func englishDigest(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}
func englishFlag(t *testing.T, d *DB, key string) bool {
	t.Helper()
	value, _, err := d.GetSetting(key)
	if err != nil {
		t.Fatal(err)
	}
	return value == "true"
}

func TestEnglishTargetReadiness(t *testing.T) {
	for _, value := range []string{"", "  ", "legacy \u6a21\u578b", "Fullwidth\uff1a", "Japanese \u3042"} {
		if EnglishTargetReady(value) {
			t.Errorf("accepted untranslated target %q", value)
		}
	}
	if !EnglishTargetReady("Report evidence_version; keep {{.Goal}} and 100 characters.") {
		t.Fatal("rejected English target")
	}
}

func TestEnglishPromptUpgradePreservesHistoryAndEdits(t *testing.T) {
	d := englishDB(t)
	for _, custom := range []bool{false, true} {
		t.Run(fmt.Sprint(custom), func(t *testing.T) {
			key := fmt.Sprintf("english_prompt_%d", time.Now().UnixNano())
			a, err := d.CreateAgent(key, key, "")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { d.DeleteAgent(key); d.Exec(`DELETE FROM settings WHERE key=$1`, key+"_prompt_english_v1") })
			body := "historical shipped default"
			if custom {
				body += " with an operator edit"
			}
			if _, err := d.SavePrompt(a.ID, body, "original", "operator"); err != nil {
				t.Fatal(err)
			}
			legacy := []string{englishDigest("historical shipped default")}
			if err := d.MigrateEnglishPrompt(key, "English replacement", legacy); err != nil {
				t.Fatal(err)
			}
			current, _ := d.CurrentPrompt(a.ID)
			versions, _ := d.ListPromptVersions(a.ID)
			if custom {
				if current != body || len(versions) != 1 {
					t.Fatalf("custom prompt changed: current=%q versions=%d", current, len(versions))
				}
			} else {
				if current != "English replacement" || len(versions) != 2 || versions[1].Template != body {
					t.Fatalf("upgrade lost history: current=%q versions=%+v", current, versions)
				}
			}
			if err := d.MigrateEnglishPrompt(key, "English replacement", legacy); err != nil {
				t.Fatal(err)
			}
			after, _ := d.ListPromptVersions(a.ID)
			if len(after) != len(versions) {
				t.Fatal("repeat startup appended a prompt")
			}
			if !englishFlag(t, d, key+"_prompt_english_v1") {
				t.Fatal("completed inspection did not set flag")
			}
		})
	}
}

func TestEnglishPromptDefersUntranslatedTarget(t *testing.T) {
	d := englishDB(t)
	key := fmt.Sprintf("english_defer_%d", time.Now().UnixNano())
	a, err := d.CreateAgent(key, key, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.DeleteAgent(key); d.Exec(`DELETE FROM settings WHERE key=$1`, key+"_prompt_english_v1") })
	legacy := "\u65e7\u9ed8\u8ba4" // A deliberate legacy-language fixture, represented without raw CJK.
	d.SavePrompt(a.ID, legacy, "original", "system")
	if err := d.MigrateEnglishPrompt(key, legacy, []string{englishDigest(legacy)}); err != nil {
		t.Fatal(err)
	}
	if englishFlag(t, d, key+"_prompt_english_v1") {
		t.Fatal("unchanged untranslated target consumed flag")
	}
	if err := d.MigrateEnglishPrompt(key, "English default", []string{englishDigest(legacy)}); err != nil {
		t.Fatal(err)
	}
	if current, _ := d.CurrentPrompt(a.ID); current != "English default" {
		t.Fatal("deferred prompt never upgraded")
	}
}

func TestEnglishMigrationRollbackRetriesAndSerializes(t *testing.T) {
	d := englishDB(t)
	flag := fmt.Sprintf("english-atomic-%d", time.Now().UnixNano())
	valueKey := flag + "-value"
	t.Cleanup(func() { d.Exec(`DELETE FROM settings WHERE key IN ($1,$2)`, flag, valueKey) })
	boom := errors.New("injected failure")
	err := d.EnglishMigration(flag, true, func(tx *sql.Tx) ([]string, error) {
		_, err := tx.Exec(`INSERT INTO settings(key,value) VALUES($1,'partial')`, valueKey)
		if err != nil {
			return nil, err
		}
		return nil, boom
	})
	if !errors.Is(err, boom) || englishFlag(t, d, flag) {
		t.Fatalf("failure not retryable: %v", err)
	}
	if _, exists, _ := d.GetSetting(valueKey); exists {
		t.Fatal("failed transaction persisted mutation")
	}
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- d.EnglishMigration(flag, true, func(tx *sql.Tx) ([]string, error) {
				_, err := tx.Exec(`INSERT INTO settings(key,value) VALUES($1,'done')`, valueKey)
				return nil, err
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if !englishFlag(t, d, flag) {
		t.Fatal("successful retry missing flag")
	}
}

func TestEnglishToolColumnsPreserveCustomization(t *testing.T) {
	d := englishDB(t)
	for _, column := range []string{"none", "description", "schema", "both", "custom-tool"} {
		t.Run(column, func(t *testing.T) {
			key := fmt.Sprintf("english-tool-%d", time.Now().UnixNano())
			old := EnglishToolDefault{Key: key, Description: "old built-in description", Schema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string","description":"old parameter"}}}`)}
			target := EnglishToolDefault{Key: key, Description: "English description", Schema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string","description":"English parameter"}}}`)}
			if err := d.SeedTool(key, old.Description, old.Schema, json.RawMessage(`["custom-agent"]`)); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				d.Exec(`DELETE FROM tools WHERE key=$1`, key)
				d.Exec(`DELETE FROM settings WHERE key LIKE $1`, "tool_"+key+"_%_english_v1")
			})
			if _, err := d.Exec(`UPDATE tools SET enabled=false,deferred=true WHERE key=$1`, key); err != nil {
				t.Fatal(err)
			}
			customDesc := column == "description" || column == "both"
			customSchema := column == "schema" || column == "both"
			if customDesc {
				d.Exec(`UPDATE tools SET description='OPERATOR' WHERE key=$1`, key)
			}
			if customSchema {
				d.Exec(`UPDATE tools SET schema=jsonb_set(schema,'{properties,q,default}','"operator default"') WHERE key=$1`, key)
			}
			if column == "custom-tool" {
				d.Exec(`UPDATE tools SET system=false WHERE key=$1`, key)
			}
			if err := d.migrateEnglishToolColumns(target, old); err != nil {
				t.Fatal(err)
			}
			got, err := d.GetTool(key)
			if err != nil {
				t.Fatal(err)
			}
			if got.Enabled || !got.Deferred || len(got.Agents) != 1 || got.Agents[0] != "custom-agent" {
				t.Fatalf("runtime configuration changed: %+v", got)
			}
			want := target.Description
			if customDesc {
				want = "OPERATOR"
			}
			if column == "custom-tool" {
				want = old.Description
			}
			if got.Description != want {
				t.Fatalf("description=%q want=%q", got.Description, want)
			}
			if customSchema && !strings.Contains(string(got.Schema), "operator default") {
				t.Fatal("custom parameter default erased")
			}
			if !customSchema && column != "custom-tool" && !strings.Contains(string(got.Schema), "English parameter") {
				t.Fatal("default schema did not upgrade")
			}
			if column == "custom-tool" && strings.Contains(string(got.Schema), "English parameter") {
				t.Fatal("custom tool schema changed")
			}
		})
	}
}

func TestEnglishToolDefersColumnsIndependently(t *testing.T) {
	d := englishDB(t)
	key := fmt.Sprintf("english-tool-defer-%d", time.Now().UnixNano())
	old := EnglishToolDefault{Key: key, Description: "legacy \u6a21\u578b", Schema: json.RawMessage(`{"description":"legacy"}`)}
	d.SeedTool(key, old.Description, old.Schema, nil)
	t.Cleanup(func() {
		d.Exec(`DELETE FROM tools WHERE key=$1`, key)
		d.Exec(`DELETE FROM settings WHERE key LIKE $1`, "tool_"+key+"_%_english_v1")
	})
	target := old
	target.Schema = json.RawMessage(`{"description":"English schema"}`)
	if err := d.migrateEnglishToolColumns(target, old); err != nil {
		t.Fatal(err)
	}
	if englishFlag(t, d, "tool_"+key+"_description_english_v1") {
		t.Fatal("legacy description consumed flag")
	}
	if !englishFlag(t, d, "tool_"+key+"_schema_english_v1") {
		t.Fatal("ready schema was not migrated")
	}
}

func TestEnglishPromptSeesConcurrentOperatorEdit(t *testing.T) {
	d := englishDB(t)
	key := fmt.Sprintf("english_concurrent_%d", time.Now().UnixNano())
	a, err := d.CreateAgent(key, key, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.DeleteAgent(key); d.Exec(`DELETE FROM settings WHERE key=$1`, key+"_prompt_english_v1") })
	if _, err := d.SavePrompt(a.ID, "old default", "original", "system"); err != nil {
		t.Fatal(err)
	}
	tx, err := d.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SELECT id FROM agents WHERE id=$1 FOR UPDATE`, a.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- d.MigrateEnglishPrompt(key, "English default", []string{englishDigest("old default")}) }()
	if _, err := savePromptTx(tx, a.ID, "concurrent operator edit", "edit", "operator"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	current, _ := d.CurrentPrompt(a.ID)
	if current != "concurrent operator edit" {
		t.Fatalf("concurrent edit overwritten: %q", current)
	}
}

func TestEnglishAgentMetadataRequiresWholeDefaultPair(t *testing.T) {
	d := englishDB(t)
	var old []struct{ Key, Name, Description string }
	if err := json.Unmarshal(legacyEnglishAgentsJSON, &old); err != nil {
		t.Fatal(err)
	}
	for _, legacy := range old {
		t.Run(legacy.Key, func(t *testing.T) {
			existing, err := d.GetAgentByKey(legacy.Key)
			if err != nil {
				t.Fatal(err)
			}
			if existing == nil {
				if _, err = d.CreateAgent(legacy.Key, legacy.Name, legacy.Description); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { d.DeleteAgent(legacy.Key) })
			} else {
				t.Cleanup(func() { d.UpdateAgentMeta(legacy.Key, existing.Name, existing.Description) })
			}
			flag := legacy.Key + "_agent_english_v1"
			saved, exists, err := d.GetSetting(flag)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if exists {
					d.SetSetting(flag, saved)
				} else {
					d.Exec(`DELETE FROM settings WHERE key=$1`, flag)
				}
			})
			for _, custom := range []string{"none", "name", "description"} {
				name, description := legacy.Name, legacy.Description
				if custom == "name" {
					name = "Operator name"
				}
				if custom == "description" {
					description = "Operator description"
				}
				if err := d.UpdateAgentMeta(legacy.Key, name, description); err != nil {
					t.Fatal(err)
				}
				d.SetSetting(flag, "false")
				if err := d.MigrateEnglishAgentMetadata(legacy.Key, "English name", "English description"); err != nil {
					t.Fatal(err)
				}
				got, err := d.GetAgentByKey(legacy.Key)
				if err != nil {
					t.Fatal(err)
				}
				if custom == "none" {
					name, description = "English name", "English description"
				}
				if got.Name != name || got.Description != description {
					t.Fatalf("%s customization changed: %+v", custom, got)
				}
			}
			if existing == nil {
				d.DeleteAgent(legacy.Key)
				d.SetSetting(flag, "false")
				if err := d.MigrateEnglishAgentMetadata(legacy.Key, "English name", "English description"); err != nil {
					t.Fatal(err)
				}
				if got, _ := d.GetAgentByKey(legacy.Key); got != nil {
					t.Fatal("deleted agent resurrected")
				}
			}
		})
	}
}

func TestEnglishRuleUpgradePreservesBehaviorAndCustomization(t *testing.T) {
	d := englishDB(t)
	var old, targets []englishRule
	json.Unmarshal(legacyEnglishRulesJSON, &old)
	json.Unmarshal(englishRulesJSON, &targets)
	for _, custom := range []bool{false, true} {
		name := old[0].Name
		if custom {
			name = "Operator rule"
		}
		var id int64
		if err := d.QueryRow(`INSERT INTO intercept_rules(name,enabled,action,priority,match_target,match_type,pattern,message) VALUES($1,false,'allow',912,$2,$3,$4,$5) RETURNING id`, name, old[0].MatchTarget, old[0].MatchType, old[0].Pattern, old[0].Message).Scan(&id); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { d.Exec(`DELETE FROM intercept_rules WHERE id=$1`, id) })
		if err := d.SetSetting("intercept_default_rules_english_v1", "false"); err != nil {
			t.Fatal(err)
		}
		if err := d.migrateEnglishRules(); err != nil {
			t.Fatal(err)
		}
		var gotName, message, action, pattern string
		var enabled bool
		var priority int
		if err := d.QueryRow(`SELECT name,message,action,pattern,enabled,priority FROM intercept_rules WHERE id=$1`, id).Scan(&gotName, &message, &action, &pattern, &enabled, &priority); err != nil {
			t.Fatal(err)
		}
		wantName, wantMessage := targets[0].Name, targets[0].Message
		if custom {
			wantName, wantMessage = name, old[0].Message
		}
		if gotName != wantName || message != wantMessage || enabled || action != "allow" || priority != 912 || pattern != old[0].Pattern {
			t.Fatalf("rule behavior/customization changed: %s %s %s %t %d", gotName, message, action, enabled, priority)
		}
	}
	// Historical seed flags being absent must not duplicate edited rules by name.
	var before, after int
	d.QueryRow(`SELECT count(*) FROM intercept_rules`).Scan(&before)
	for _, flag := range []string{"intercept_default_rules_v1", "intercept_default_rules_v2", "intercept_default_rules_v3"} {
		d.Exec(`DELETE FROM settings WHERE key=$1`, flag)
	}
	if err := d.seedDefaultInterceptRules(); err != nil {
		t.Fatal(err)
	}
	if err := d.seedDefaultInterceptRulesV2(); err != nil {
		t.Fatal(err)
	}
	if err := d.seedDefaultInterceptRulesV3(); err != nil {
		t.Fatal(err)
	}
	d.QueryRow(`SELECT count(*) FROM intercept_rules`).Scan(&after)
	if before != after {
		t.Fatalf("missing historical flags duplicated rules: %d -> %d", before, after)
	}
}

func TestEnglishAssetNotesPreserveDisabledAndEditedRules(t *testing.T) {
	d := englishDB(t)
	var old, targets []englishAssetRule
	json.Unmarshal(legacyEnglishAssetRulesJSON, &old)
	json.Unmarshal(englishAssetRulesJSON, &targets)
	for i := 0; i < 2; i++ {
		var id int64
		var note string
		var enabled bool
		if err := d.QueryRow(`SELECT id,note,enabled FROM asset_intercept_rules WHERE builtin AND kind=$1 AND pattern=$2`, old[i].Kind, old[i].Pattern).Scan(&id, &note, &enabled); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { d.Exec(`UPDATE asset_intercept_rules SET note=$2,enabled=$3 WHERE id=$1`, id, note, enabled) })
		value := old[i].Note
		if i == 1 {
			value = "Operator note"
		}
		if _, err := d.Exec(`UPDATE asset_intercept_rules SET note=$2,enabled=false WHERE id=$1`, id, value); err != nil {
			t.Fatal(err)
		}
	}
	d.SetSetting("asset_intercept_default_notes_english_v1", "false")
	if err := d.migrateEnglishRules(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		var note string
		var enabled bool
		if err := d.QueryRow(`SELECT note,enabled FROM asset_intercept_rules WHERE builtin AND kind=$1 AND pattern=$2`, old[i].Kind, old[i].Pattern).Scan(&note, &enabled); err != nil {
			t.Fatal(err)
		}
		want := targets[i].Note
		if i == 1 {
			want = "Operator note"
		}
		if note != want || enabled {
			t.Fatalf("asset note/settings changed: %s %t", note, enabled)
		}
	}
}

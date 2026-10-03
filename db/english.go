package db

import (
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

// These frozen 160fe13 runtime defaults are comparison fingerprints, never seeds.
// Their legacy text must remain unchanged so edited settings can be distinguished.
//
//go:embed legacy_english_tools.json
var legacyEnglishToolsJSON []byte

//go:embed legacy_english_agents.json
var legacyEnglishAgentsJSON []byte

//go:embed legacy_english_intercept_rules.json
var legacyEnglishRulesJSON []byte

//go:embed legacy_english_asset_intercept_rules.json
var legacyEnglishAssetRulesJSON []byte

type EnglishToolDefault struct {
	Key         string          `json:"key"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
}

func LegacyEnglishTools() []EnglishToolDefault {
	var values []EnglishToolDefault
	if err := json.Unmarshal(legacyEnglishToolsJSON, &values); err != nil {
		panic(err)
	}
	return values
}

// EnglishTargetReady prevents partially translated builds from consuming flags.
// Once a target becomes English, unchanged English defaults are valid no-ops.
func EnglishTargetReady(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	for _, r := range value {
		if r >= 0x3400 && r <= 0x4dbf || r >= 0x4e00 && r <= 0x9fff ||
			r >= 0x3000 && r <= 0x303f || r >= 0xff00 && r <= 0xffef ||
			r >= 0x3040 && r <= 0x30ff || r >= 0xac00 && r <= 0xd7af {
			return false
		}
	}
	return true
}

// EnglishMigration commits a migration and its flag together. Failure is retryable;
// custom rows are inspected once and logged only after a successful commit.
func (d *DB) EnglishMigration(flag string, ready bool, migrate func(*sql.Tx) ([]string, error)) error {
	if !ready {
		return nil
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`SELECT pg_advisory_xact_lock(7337741020)`); err != nil {
		return err
	}
	var done string
	err = tx.QueryRow(`SELECT value FROM settings WHERE key=$1`, flag).Scan(&done)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if done == "true" {
		return nil
	}
	preserved, err := migrate(tx)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO settings(key,value) VALUES($1,'true') ON CONFLICT(key) DO UPDATE SET value='true'`, flag); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	for _, item := range preserved {
		log.Printf("[english] preserved customized %s", item)
	}
	return nil
}

// MigrateEnglishPrompt deliberately differs from old unconditional reseeds:
// only known historical defaults receive a new version; all edits survive.
func (d *DB) MigrateEnglishPrompt(key, target string, legacy []string) error {
	return d.EnglishMigration(key+"_prompt_english_v1", EnglishTargetReady(target), func(tx *sql.Tx) ([]string, error) {
		var id int64
		var promptID sql.NullInt64
		err := tx.QueryRow(`SELECT id,current_prompt_id FROM agents WHERE key=$1 FOR UPDATE`, key).Scan(&id, &promptID)
		if err == sql.ErrNoRows {
			return nil, nil
		} // Never resurrect a deleted agent.
		if err != nil {
			return nil, err
		}
		if !promptID.Valid {
			return nil, nil
		}
		var current string
		if err := tx.QueryRow(`SELECT template_text FROM agent_prompts WHERE id=$1`, promptID.Int64).Scan(&current); err != nil {
			return nil, err
		}
		if current == target {
			return nil, nil
		}
		sum := sha256.Sum256([]byte(current))
		digest := hex.EncodeToString(sum[:])
		known := false
		for _, value := range legacy {
			if value == digest {
				known = true
				break
			}
		}
		if !known {
			return []string{"prompt for agent " + key}, nil
		}
		_, err = savePromptTx(tx, id, target, "Updated built-in default to English", "system")
		return nil, err
	})
}

// MigrateEnglishTool upgrades columns independently. A customized schema retains
// every nested description/default while an untouched description can still move.
func (d *DB) MigrateEnglishTool(target EnglishToolDefault) error {
	var legacy *EnglishToolDefault
	for _, item := range LegacyEnglishTools() {
		if item.Key == target.Key {
			copy := item
			legacy = &copy
			break
		}
	}
	if legacy == nil {
		return nil
	}
	return d.migrateEnglishToolColumns(target, *legacy)
}

func (d *DB) migrateEnglishToolColumns(target, legacy EnglishToolDefault) error {
	for _, column := range []string{"description", "schema"} {
		value, old := target.Description, legacy.Description
		cast := ""
		if column == "schema" {
			value, old, cast = string(target.Schema), string(legacy.Schema), "::jsonb"
			// Decode escaped Unicode before checking readiness; JSON spelling does
			// not change whether a description has actually been translated.
			var decoded any
			if err := json.Unmarshal(target.Schema, &decoded); err != nil {
				return err
			}
			canonical, err := json.Marshal(decoded)
			if err != nil {
				return err
			}
			value = string(canonical)
		}
		if err := d.EnglishMigration("tool_"+target.Key+"_"+column+"_english_v1", EnglishTargetReady(value), func(tx *sql.Tx) ([]string, error) {
			var current string
			var system, equal bool
			query := `SELECT ` + column + `::text,system,` + column + `=$2` + cast + ` FROM tools WHERE key=$1 FOR UPDATE`
			err := tx.QueryRow(query, target.Key, value).Scan(&current, &system, &equal)
			if err == sql.ErrNoRows {
				return nil, nil
			}
			if err != nil {
				return nil, err
			}
			if !system || equal {
				return nil, nil
			}
			result, err := tx.Exec(`UPDATE tools SET `+column+`=$2`+cast+`,updated_at=now() WHERE key=$1 AND system AND `+column+`=$3`+cast, target.Key, value, old)
			if err != nil {
				return nil, err
			}
			n, err := result.RowsAffected()
			if err != nil {
				return nil, err
			}
			if n == 0 {
				return []string{"tool " + target.Key + " " + column}, nil
			}
			return nil, nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// MigrateEnglishAgentMetadata never resets user-editable reporter/retester fields
// unless both display values still match the shipped pair.
func (d *DB) MigrateEnglishAgentMetadata(key, name, description string) error {
	var legacy []struct{ Key, Name, Description string }
	if err := json.Unmarshal(legacyEnglishAgentsJSON, &legacy); err != nil {
		return err
	}
	for _, old := range legacy {
		if old.Key != key {
			continue
		}
		return d.EnglishMigration(key+"_agent_english_v1", EnglishTargetReady(name) && EnglishTargetReady(description), func(tx *sql.Tx) ([]string, error) {
			var curName, curDesc string
			err := tx.QueryRow(`SELECT name,COALESCE(description,'') FROM agents WHERE key=$1 FOR UPDATE`, key).Scan(&curName, &curDesc)
			if err == sql.ErrNoRows {
				return nil, nil
			}
			if err != nil {
				return nil, err
			}
			if curName == name && curDesc == description {
				return nil, nil
			}
			if curName != old.Name || curDesc != old.Description {
				return []string{"metadata for agent " + key}, nil
			}
			_, err = tx.Exec(`UPDATE agents SET name=$2,description=$3 WHERE key=$1`, key, name, description)
			return nil, err
		})
	}
	return fmt.Errorf("no frozen agent metadata for %s", key)
}

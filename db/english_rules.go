package db

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed english_intercept_rules.json
var englishRulesJSON []byte

//go:embed english_asset_intercept_rules.json
var englishAssetRulesJSON []byte

type englishRule struct {
	Name        string `json:"name"`
	Message     string `json:"message"`
	Pattern     string `json:"pattern"`
	MatchTarget string `json:"match_target"`
	MatchType   string `json:"match_type"`
}
type englishAssetRule struct {
	Kind    string `json:"kind"`
	Pattern string `json:"pattern"`
	Note    string `json:"note"`
}

func (d *DB) migrateEnglishRules() error {
	var legacy, targets []englishRule
	if err := json.Unmarshal(legacyEnglishRulesJSON, &legacy); err != nil {
		return err
	}
	if err := json.Unmarshal(englishRulesJSON, &targets); err != nil {
		return err
	}
	ready := len(legacy) == len(targets)
	for _, target := range targets {
		ready = ready && EnglishTargetReady(target.Name) && EnglishTargetReady(target.Message)
	}
	if err := d.EnglishMigration("intercept_default_rules_english_v1", ready, func(tx *sql.Tx) ([]string, error) {
		var preserved []string
		for i, old := range legacy {
			target := targets[i]
			rows, err := tx.Query(`SELECT id,name,COALESCE(message,'') FROM intercept_rules WHERE pattern=$1 AND match_target=$2 AND match_type=$3 FOR UPDATE`, old.Pattern, old.MatchTarget, old.MatchType)
			if err != nil {
				return nil, err
			}
			var ids []int64
			for rows.Next() {
				var id int64
				var name, message string
				if err := rows.Scan(&id, &name, &message); err != nil {
					rows.Close()
					return nil, err
				}
				if name == old.Name && message == old.Message {
					ids = append(ids, id)
				} else if name != target.Name || message != target.Message {
					preserved = append(preserved, fmt.Sprintf("intercept rule #%d", id))
				}
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, err
			}
			for _, id := range ids {
				if _, err := tx.Exec(`UPDATE intercept_rules SET name=$2,message=$3 WHERE id=$1`, id, target.Name, target.Message); err != nil {
					return nil, err
				}
			}
		}
		return preserved, nil
	}); err != nil {
		return err
	}
	var oldAssets, newAssets []englishAssetRule
	if err := json.Unmarshal(legacyEnglishAssetRulesJSON, &oldAssets); err != nil {
		return err
	}
	if err := json.Unmarshal(englishAssetRulesJSON, &newAssets); err != nil {
		return err
	}
	ready = len(oldAssets) == len(newAssets)
	for _, target := range newAssets {
		ready = ready && EnglishTargetReady(target.Note)
	}
	if err := d.EnglishMigration("asset_intercept_default_notes_english_v1", ready, func(tx *sql.Tx) ([]string, error) {
		var preserved []string
		for i, old := range oldAssets {
			var customized int
			if err := tx.QueryRow(`SELECT count(*) FROM asset_intercept_rules WHERE builtin AND kind=$1 AND pattern=$2 AND note<>$3 AND note<>$4`, old.Kind, old.Pattern, old.Note, newAssets[i].Note).Scan(&customized); err != nil {
				return nil, err
			}
			if customized > 0 {
				preserved = append(preserved, "asset intercept note for "+old.Pattern)
			}
			if _, err := tx.Exec(`UPDATE asset_intercept_rules SET note=$1 WHERE builtin AND kind=$2 AND pattern=$3 AND note=$4`, newAssets[i].Note, old.Kind, old.Pattern, old.Note); err != nil {
				return nil, err
			}
		}
		return preserved, nil
	}); err != nil {
		return err
	}
	return d.migrateEnglishSystemLabels()
}

// Exact legacy system strings are fingerprints, not a license to rewrite user
// content. Prefix rewrites preserve company names byte-for-byte.
func (d *DB) migrateEnglishSystemLabels() error {
	// Display-only cleanup is repeatable: older code or restored archives can
	// introduce these strings after a flag has already been consumed.
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, query := range []string{
		`UPDATE task_asset_links SET source_summary='Company associated at task creation: '||substr(source_summary,length('任务创建时关联企业：')+1) WHERE source='company' AND source_summary LIKE '任务创建时关联企业：%'`,
		`UPDATE task_scope SET reason='Company associated at task creation' WHERE source='manual' AND kind='company' AND reason='任务创建时关联企业'`,
		`UPDATE notification_deliveries SET last_error='Channel disabled' WHERE last_error='渠道已停用'`,
	} {
		if _, err := tx.Exec(query); err != nil {
			return err
		}
	}
	return tx.Commit()
}

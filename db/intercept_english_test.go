package db

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestInterceptSourceEnglishAndLegacy(t *testing.T) {
	for _, reason := range []string{"[model] reason", "[模型] reason"} {
		if got := interceptSource(0, reason); got != "model" {
			t.Errorf("source=%s for %q", got, reason)
		}
		if got := interceptSource(12, reason); got != "rule" {
			t.Errorf("rule precedence lost: %s", got)
		}
	}
	if got := interceptSource(0, "ordinary reason"); got != "unknown" {
		t.Errorf("source=%s", got)
	}
}

func TestInterceptEnglishMigrationAndArchiveCompatibility(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	scope := "english-contract-" + t.Name()
	defer d.Exec(`DELETE FROM intercept_pending WHERE task_id=$1`, scope)
	for _, prefix := range []string{"[model]", "[模型]"} {
		t.Run(prefix, func(t *testing.T) {
			reason := prefix + " legacy snapshot"
			audit := &InterceptAudit{InitialReason: reason, DecisionReason: reason, ExecutionStatus: "succeeded"}
			id, err := d.CreateDecidedIntercept(0, 0, scope, "test", "Read", []byte(`{}`), "allowed", reason, audit)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = d.Exec(`UPDATE intercept_pending SET decision_source='' WHERE id=$1`, id); err != nil {
				t.Fatal(err)
			}
			before, err := d.GetInterceptDetail(id)
			if err != nil || before.DecisionSource != "model" {
				t.Fatalf("legacy source inference: %+v %v", before, err)
			}
			for range 2 {
				if _, err = d.Exec(schemaSQL); err != nil {
					t.Fatal(err)
				}
			}
			after, err := d.GetInterceptDetail(id)
			if err != nil || after.DecisionSource != "model" || !strings.HasPrefix(after.Reason, "[model]") || after.Audit.InitialReason != reason || after.Audit.DecisionReason != reason {
				t.Fatalf("migration corrupted source, reason, or audit: %+v %v", after, err)
			}
			// Restore an old source-less archive after startup migrations have already run.
			var raw []byte
			if err = d.QueryRow(`SELECT to_jsonb(ip) FROM intercept_pending ip WHERE id=$1`, id).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var row map[string]any
			if err = json.Unmarshal(raw, &row); err != nil {
				t.Fatal(err)
			}
			row["decision_source"] = ""
			row["reason"] = reason
			row["id"] = fmt.Sprint(id)
			rows, _ := json.Marshal([]any{row})
			tx, err := d.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if _, err = tx.Exec(`DELETE FROM intercept_pending WHERE id=$1`, id); err != nil {
				t.Fatal(err)
			}
			if err = restoreInterceptRows(tx, rows); err != nil {
				t.Fatal(err)
			}
			var source, savedReason, initial string
			if err = tx.QueryRow(`SELECT decision_source,reason,audit->>'initial_reason' FROM intercept_pending WHERE id=$1`, id).Scan(&source, &savedReason, &initial); err != nil {
				t.Fatal(err)
			}
			if source != "model" || savedReason != reason || initial != reason {
				t.Fatalf("archive compatibility changed: %q %q %q", source, savedReason, initial)
			}
		})
	}
}

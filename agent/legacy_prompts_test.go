package agent

import (
	"encoding/hex"
	"testing"
)

func TestLegacyPromptDigestCoverage(t *testing.T) {
	for key, minimum := range map[string]int{"goals": 2, "planner": 11, "mainagent": 5, "worker": 12, "auto": 1, "pentest": 2, "reporter": 3, "retester": 1, "assistant_fallback": 1} {
		values := LegacyPromptDigests(key)
		if len(values) < minimum {
			t.Errorf("%s: missing recovered historical defaults", key)
		}
		seen := map[string]bool{}
		for _, value := range values {
			decoded, err := hex.DecodeString(value)
			if err != nil || len(decoded) != 32 || seen[value] {
				t.Errorf("%s: invalid/duplicate digest %q", key, value)
			}
			seen[value] = true
		}
		if len(values) > 0 {
			values[0] = "edited"
			if LegacyPromptDigests(key)[0] == "edited" {
				t.Fatal("caller can mutate frozen digests")
			}
		}
		if IsLegacyPromptDefault(key, "operator customization") {
			t.Fatalf("%s: recognized custom prompt", key)
		}
	}
	if len(LegacyPromptDigests("unknown")) != 0 || IsLegacyPromptDefault("unknown", "") {
		t.Fatal("unknown agent has legacy defaults")
	}
}

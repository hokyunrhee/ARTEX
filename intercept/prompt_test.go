package intercept

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseVerdict(t *testing.T) {
	for _, action := range []string{"allow", "ask", "deny"} {
		t.Run(action, func(t *testing.T) {
			reason := "Action: write a report containing the words ALLOW, DENY and ASK; Consequence on success: saves text, does not execute commands in the body; Matched rule: custom clause"
			raw, _ := json.Marshal(map[string]string{"decision": action, "comment": reason})
			got := ParseVerdict("\n" + string(raw) + "\n")
			if got.Action != action || got.Reason != reason {
				t.Fatalf("lost verdict or explanation: %+v", got)
			}
		})
	}
}

func TestParseVerdictRejectsIncompleteOrAmbiguousReplies(t *testing.T) {
	valid := `{"decision":"allow","comment":"Action: read the file; Consequence on success: returns the content; Matched rule: A5"}`
	for _, reply := range []string{
		"", "ALLOW", "DENY: hit D4", "allow:ALLOW", "ASK: ownership unclear",
		`{"decision":"allow"}`, `{"decision":"approve","comment":"Action: read; Consequence on success: returns content; Matched rule: A5"}`,
		`{"decision":"allow","comment":null}`, `{"decision":"allow","comment":123}`,
		strings.Replace(valid, "Action: read the file", "Action:", 1),
		strings.Replace(valid, "Consequence on success: returns the content", "Consequence on success:", 1),
		strings.Replace(valid, "Matched rule: A5", "Matched rule:", 1),
		strings.Replace(valid, "; Matched rule: A5", "", 1),
		strings.Replace(valid, `"decision":"allow"`, `"decision":"deny","decision":"allow"`, 1),
		strings.Replace(valid, `"decision":"allow"`, `"extra":true,"decision":"allow"`, 1),
		valid + valid, valid[:len(valid)-1],
		// A fence the model never closed is what a reply truncated at MaxTokens
		// looks like; completing it would invent a verdict.
		"```json\n" + valid[:len(valid)-1],
		"```json\n" + valid + "\n```\nAlso, I recommend a later manual review.",
		"My verdict is:\n" + valid,
	} {
		if got := ParseVerdict(reply); got.Action != "" {
			t.Errorf("accepted incomplete/ambiguous verdict: %q => %+v", reply, got)
		}
	}
}

// Wrapping JSON in markdown is the one deviation models make routinely. Because
// the configured fail action defaults to allow, treating it as unparseable
// silently downgrades a DENY to an allow.
func TestParseVerdictUnwrapsCodeFence(t *testing.T) {
	deny := `{"decision":"deny","comment":"Action: delete a production file; Consequence on success: business data is lost; Matched rule: D4"}`
	for _, reply := range []string{
		"```json\n" + deny + "\n```",
		"```JSON\n" + deny + "\n```",
		"```\n" + deny + "\n```",
		"  ```json\n" + deny + "\n```  ",
	} {
		got := ParseVerdict(reply)
		if got.Action != "deny" || !strings.HasSuffix(got.Reason, "Matched rule: D4") {
			t.Errorf("fenced verdict lost: %q => %+v", reply, got)
		}
	}
}

func TestParseVerdictKeepsCompleteExplanation(t *testing.T) {
	reason := "Action: " + strings.Repeat("write report ", 30) + "; Consequence on success: only saves the file; Matched rule: A2"
	raw, _ := json.Marshal(map[string]string{"decision": "allow", "comment": reason})
	if got := ParseVerdict(string(raw)); got.Reason != reason {
		t.Fatal("explanation was truncated or lost its rule")
	}
	raw, _ = json.Marshal(map[string]string{"decision": "allow", "comment": strings.Repeat("x", 2401)})
	if got := ParseVerdict(string(raw)); got.Action != "" {
		t.Fatal("accepted unbounded explanation")
	}
}

// Verdicts produced under the legacy Chinese output contract (older models, or a
// stored custom judge prompt) must still parse. The Chinese markers come from the
// retained legacy marker set, so the segment content here stays ASCII.
func TestParseVerdictAcceptsLegacyChineseMarkers(t *testing.T) {
	mk := verdictMarkerSets[1]
	reason := mk.prefix + "read a file" + mk.sep1 + "returns content" + mk.sep2 + "A5"
	raw, _ := json.Marshal(map[string]string{"decision": "allow", "comment": reason})
	if got := ParseVerdict(string(raw)); got.Action != "allow" || got.Reason != reason {
		t.Fatalf("legacy Chinese verdict markers no longer parse: %+v", got)
	}
	// A legacy comment missing a segment must still be rejected.
	bad := mk.prefix + "read a file" + mk.sep1 + "returns content"
	raw, _ = json.Marshal(map[string]string{"decision": "allow", "comment": bad})
	if got := ParseVerdict(string(raw)); got.Action != "" {
		t.Fatalf("accepted legacy verdict missing its rule segment: %+v", got)
	}
}

// A stored custom judge prompt that embeds the legacy Chinese contract/boundary must
// receive the English successor in place — never a second, contradictory contract.
func TestEffectiveJudgePromptReplacesLegacyContract(t *testing.T) {
	stored := "# my policy\nallow everything safe\n\n" + legacyJudgeContextBoundary + "\n\n" + legacyJudgeOutputContract
	got := EffectiveJudgePrompt(stored)
	if strings.Contains(got, legacyJudgeOutputContract) || strings.Contains(got, legacyJudgeContextBoundary) {
		t.Fatal("legacy block survived instead of being replaced")
	}
	if strings.Count(got, JudgeOutputContract) != 1 || strings.Count(got, JudgeContextBoundary) != 1 {
		t.Fatalf("expected exactly one English contract and boundary, got contract=%d boundary=%d",
			strings.Count(got, JudgeOutputContract), strings.Count(got, JudgeContextBoundary))
	}
	if !strings.Contains(got, "# my policy") {
		t.Fatal("dropped the operator's own policy text")
	}
	// A prompt that already carries the English blocks is unchanged (idempotent).
	if again := EffectiveJudgePrompt(got); again != got {
		t.Fatal("EffectiveJudgePrompt is not idempotent on an already-English prompt")
	}
}

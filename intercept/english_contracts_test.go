package intercept

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseVerdictLegacyCompatibility(t *testing.T) {
	for _, action := range []string{"allow", "ask", "deny"} {
		reason := "实际操作：读取文件；成功后的后果：返回内容；命中规则：A5"
		raw, _ := json.Marshal(map[string]string{"decision": action, "comment": reason})
		if got := ParseVerdict(string(raw)); got.Action != action || got.Reason != reason {
			t.Fatalf("legacy verdict changed: %+v", got)
		}
		for _, bad := range []string{
			strings.Replace(reason, "读取文件", "", 1),
			strings.Replace(reason, "返回内容", "", 1),
			strings.Replace(reason, "A5", "", 1),
			strings.Replace(reason, "；命中规则：", "; Matched rule: ", 1),
			strings.Replace(reason, "实际操作：", "Actual operation: ", 1),
		} {
			raw, _ := json.Marshal(map[string]string{"decision": action, "comment": bad})
			if got := ParseVerdict(string(raw)); got.Action != "" {
				t.Fatalf("accepted incomplete or mixed contract: %+v", got)
			}
		}
	}
}

func TestEffectiveJudgePromptReplacesLegacyBlocks(t *testing.T) {
	policy := "Custom rule S1: preserve every file."
	old := policy + "\n\n" + legacyJudgeContextBoundary + "\n\n" + legacyJudgeOutputContract
	got := EffectiveJudgePrompt(old)
	if !strings.HasPrefix(got, policy) || strings.Contains(got, legacyJudgeContextBoundary) || strings.Contains(got, legacyJudgeOutputContract) {
		t.Fatal("custom policy changed or a legacy block survived")
	}
	if strings.Count(got, JudgeContextBoundary) != 1 || strings.Count(got, JudgeOutputContract) != 1 {
		t.Fatal("application contract duplicated")
	}
	if EffectiveJudgePrompt(got) != got {
		t.Fatal("prompt upgrade is not idempotent")
	}
}

func TestVerdictReasonByteLimit(t *testing.T) {
	prefix, suffix := "Actual operation: ", "; Consequences if successful: Content returned; Matched rule: A5"
	for _, size := range []int{2400, 2401} {
		reason := prefix + strings.Repeat("a", size-len(prefix)-len(suffix)) + suffix
		raw, _ := json.Marshal(map[string]string{"decision": "allow", "comment": reason})
		got := ParseVerdict(string(raw))
		if (got.Action != "") != (size == 2400) {
			t.Fatalf("byte cap mismatch at %d bytes: %+v", size, got)
		}
	}
}

package server

import "testing"

// levelOf classifies a log line by substring-matching an English vocabulary (plus the
// ✕/⚠ glyphs). Nothing else covers it, so a future vocabulary change would regress
// silently without this test.
func TestLogSinkLevelOf(t *testing.T) {
	cases := []struct {
		msg  string
		want string
	}{
		{"[notify] delivery failed: timeout", "error"},
		{"[activity] task 3 write dropped", "error"},
		{"request rejected by guard", "error"},
		{"SMTP dial refused by SSRF guard", "error"},
		{"target unreachable after retries", "error"},
		{"[engine] FATAL provider init", "error"},
		{"panic recovered in tool", "error"},
		{"tool boom ✕ handler", "error"},
		{"[mcp] server disabled by config", "warn"},
		{"[worker] intent blocked, retry in 3s", "warn"},
		{"retrying LLM request (1/2)", "warn"},
		{"skip empty batch", "warn"},
		{"work#3 stopped", "warn"},
		{"⚠ budget nearly exhausted", "warn"},
		{"[pg] database configured from env", "info"},
		{"listening on 127.0.0.1:8787", "info"},
	}
	for _, c := range cases {
		if got := levelOf(c.msg); got != c.want {
			t.Errorf("levelOf(%q) = %q, want %q", c.msg, got, c.want)
		}
	}
}

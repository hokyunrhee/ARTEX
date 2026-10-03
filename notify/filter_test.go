package notify

import "testing"

func TestParseFilterMalformedFallsBackToMatchAll(t *testing.T) {
	// Malformed JSON, empty input, wrong-typed fields — all must degrade to a
	// zero-value Filter, i.e. "no filtering". This invariant is where "better
	// over-push than under-push" lands: if this ever changes to error or
	// half-parse, one wrong character would silently drop all high-severity
	// notifications.
	cases := []struct {
		name string
		raw  string
	}{
		{"empty input", ""},
		{"invalid JSON", `{not json`},
		{"truncated JSON", `{"min_severity":`},
		{"type mismatch", `{"min_severity": 123, "task_ids": "abc"}`},
		{"top level is an array", `[1,2,3]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := ParseFilter([]byte(tc.raw))
			if f.MinSeverity != "" || len(f.TaskIDs) != 0 || len(f.AssetIDs) != 0 {
				t.Fatalf("a malformed config should degrade to a zero-value Filter, got %+v", f)
			}
			// A zero-value Filter must match any event.
			ev := Snapshot{Kind: EventFindingCreated, Severity: "low", VulnClass: "XSS"}
			if !Match(f, ev) {
				t.Fatal("a zero-value Filter should match all events")
			}
		})
	}
}

func TestMatchSeverityThreshold(t *testing.T) {
	ev := func(sev string) Snapshot {
		return Snapshot{Kind: EventFindingCreated, Severity: sev}
	}
	cases := []struct {
		min    string
		sev    string
		expect bool
	}{
		{"", "low", true},
		{"", "critical", true},
		{"high", "critical", true},
		{"high", "high", true},
		{"high", "medium", false},
		{"high", "low", false},
		{"critical", "high", false},
		{"critical", "critical", true},
		// An unknown severity has ordinal 0 and should be blocked by any non-empty
		// threshold (when in doubt, don't push).
		{"low", "", false},
		{"low", "unknown", false},
		{"", "", true},
	}
	for _, tc := range cases {
		got := Match(Filter{MinSeverity: tc.min}, ev(tc.sev))
		if got != tc.expect {
			t.Errorf("min=%q sev=%q: expected %v got %v", tc.min, tc.sev, tc.expect, got)
		}
	}
}

func TestMatchScopeRestrictions(t *testing.T) {
	ev := Snapshot{
		Kind:      EventFindingCreated,
		Severity:  "high",
		TaskID:    7,
		AssetIDs:  []int64{10, 20},
		VulnClass: "SQL注入",
	}
	cases := []struct {
		name   string
		filter Filter
		expect bool
	}{
		{"empty scope = no restriction", Filter{}, true},
		{"task matches", Filter{TaskIDs: []int64{7}}, true},
		{"task doesn't match", Filter{TaskIDs: []int64{8}}, false},
		{"task multi-select includes a match", Filter{TaskIDs: []int64{8, 7}}, true},
		{"asset intersects", Filter{AssetIDs: []int64{20, 99}}, true},
		{"asset doesn't intersect", Filter{AssetIDs: []int64{99}}, false},
		{"task and asset both match", Filter{TaskIDs: []int64{7}, AssetIDs: []int64{10}}, true},
		{"task matches but asset doesn't", Filter{TaskIDs: []int64{7}, AssetIDs: []int64{99}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Match(tc.filter, ev); got != tc.expect {
				t.Errorf("expected %v got %v", tc.expect, got)
			}
		})
	}
}

func TestMatchVulnClassKeywords(t *testing.T) {
	ev := func(class string) Snapshot {
		return Snapshot{Kind: EventFindingCreated, Severity: "high", VulnClass: class}
	}
	// The vulnclass values and keyword lists are kept in CJK as the substring-match
	// vectors: this test verifies case-insensitive substring matching over
	// multibyte content.
	cases := []struct {
		name   string
		filter Filter
		class  string
		expect bool
	}{
		{"empty include = accept all", Filter{}, "任意类型", true},
		{"include matches", Filter{VulnClassInclude: []string{"SQL"}}, "SQL注入", true},
		{"include doesn't match", Filter{VulnClassInclude: []string{"命令执行"}}, "SQL注入", false},
		{"include multi-word, any match", Filter{VulnClassInclude: []string{"命令执行", "SQL"}}, "SQL注入", true},
		{"case-insensitive", Filter{VulnClassInclude: []string{"sql"}}, "SQL注入", true},
		{"exclude matches = excluded", Filter{VulnClassExclude: []string{"信息泄露"}}, "信息泄露", false},
		{"exclude doesn't match = passes", Filter{VulnClassExclude: []string{"信息泄露"}}, "SQL注入", true},
		// Exclude takes priority over include: on a double hit it's out.
		{"exclude takes priority over include", Filter{
			VulnClassInclude: []string{"SQL"},
			VulnClassExclude: []string{"注入"},
		}, "SQL注入", false},
		// Pure-whitespace keywords must be ignored, or it degrades to "match every
		// string containing a space".
		{"whitespace keyword ignored", Filter{VulnClassInclude: []string{"", "  "}}, "SQL注入", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Match(tc.filter, ev(tc.class)); got != tc.expect {
				t.Errorf("expected %v got %v", tc.expect, got)
			}
		})
	}
}

func TestMatchStatusChangeRequiresOptIn(t *testing.T) {
	ev := Snapshot{Kind: EventFindingStatusChanged, Severity: "critical", FromStatus: "pending", ToStatus: "fixed"}
	// Off by default: for the vast majority, "push findings" means discovering new
	// findings, not a running status log.
	if Match(Filter{MinSeverity: "low"}, ev) {
		t.Fatal("a status-change event should be skipped when not opted in")
	}
	if !Match(Filter{OnStatusChange: true}, ev) {
		t.Fatal("after enabling on_status_change, a status-change event should match")
	}
	// Creation events are unaffected by on_status_change.
	created := Snapshot{Kind: EventFindingCreated, Severity: "critical"}
	if !Match(Filter{MinSeverity: "low"}, created) {
		t.Fatal("creation events should not depend on on_status_change")
	}
}

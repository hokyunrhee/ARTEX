package notify

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Filter is the JSONB contract for notification_channels.filter. Every field is
// optional; missing fields mean no filtering, also the fallback for malformed
// configuration in ParseFilter.
type Filter struct {
	// MinSeverity is low/medium/high/critical; an empty value sets no threshold.
	MinSeverity string `json:"min_severity"`
	// Empty TaskIDs/AssetIDs impose no restriction; nonempty lists must intersect the event.
	TaskIDs  []int64 `json:"task_ids"`
	AssetIDs []int64 `json:"asset_ids"`
	// Empty VulnClassInclude accepts all classes; otherwise at least one keyword must
	// match. Any VulnClassExclude match excludes the event, taking precedence over
	// include. Matching uses case-insensitive substrings, avoiding invalid regexes
	// that could silently disable a channel.
	VulnClassInclude []string `json:"vulnclass_include"`
	VulnClassExclude []string `json:"vulnclass_exclude"`
	// OnStatusChange enables finding-status events (meaningful only in realtime mode).
	OnStatusChange bool `json:"on_status_change"`
}

// ParseFilter parses channel filters and never returns an error.
//
// Malformed configuration deliberately falls back to a zero Filter (no filtering,
// all events match). For vulnerability notifications, an extra message is much
// better than silently losing a high-severity finding. Treating parse errors as
// no-delivery would leave a channel looking configured while sending nothing.
func ParseFilter(raw []byte) Filter {
	var f Filter
	if len(raw) == 0 {
		return f
	}
	// On parse failure, f stays zero-valued and does not filter events.
	_ = json.Unmarshal(raw, &f)
	return f
}

// ValidMinSeverity reports whether s is a valid threshold; empty means no threshold.
func ValidMinSeverity(s string) bool {
	if s == "" {
		return true
	}
	_, ok := severityRank[s]
	return ok
}

// Validate checks constrained filter values when saving channel configuration.
//
// Match handles unknown thresholds as rank >= 0, always true. A typo such as
// "hgih" would therefore silently send all severities. This avoids missing
// findings but could flood a channel whose operator expects severity filtering,
// without revealing the misconfiguration. Reject it at the write boundary.
//
// Validation applies only to writes. Reads retain ParseFilter's tolerant behavior
// so historical invalid values do not make the entire channel unreadable.
func (f Filter) Validate() error {
	if !ValidMinSeverity(f.MinSeverity) {
		return fmt.Errorf("Invalid minimum severity %q; choose low / medium / high / critical, or leave blank for no limit", f.MinSeverity)
	}
	return nil
}

// Match decides whether this channel should receive an event. It never returns an
// error; like ParseFilter, internal errors favor matching. Check event type, then
// severity, task/asset scope, and vulnerability-class keywords in that order.
func Match(f Filter, s Snapshot) bool {
	// Status-change events require explicit opt-in. By default, notifications report
	// new findings rather than every subsequent status transition.
	if s.Kind == EventFindingStatusChanged && !f.OnStatusChange {
		return false
	}
	if !AtLeast(s.Severity, f.MinSeverity) {
		return false
	}
	if len(f.TaskIDs) > 0 && !slices.Contains(f.TaskIDs, s.TaskID) {
		return false
	}
	if len(f.AssetIDs) > 0 && !intersectsInt(f.AssetIDs, s.AssetIDs) {
		return false
	}
	// Exclusion takes precedence even when an include keyword also matches.
	if len(f.VulnClassExclude) > 0 && containsAnyFold(s.VulnClass, f.VulnClassExclude) {
		return false
	}
	if len(f.VulnClassInclude) > 0 && !containsAnyFold(s.VulnClass, f.VulnClassInclude) {
		return false
	}
	return true
}

func intersectsInt(a, b []int64) bool {
	// Linear scans suffice for these manually selected lists of a few dozen entries;
	// constructing a map would cost more than it saves.
	for _, v := range b {
		if slices.Contains(a, v) {
			return true
		}
	}
	return false
}

// containsAnyFold reports whether s contains any keyword, ignoring case.
func containsAnyFold(s string, keywords []string) bool {
	lower := strings.ToLower(s)
	for _, kw := range keywords {
		kw = strings.ToLower(strings.TrimSpace(kw))
		if kw != "" && strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

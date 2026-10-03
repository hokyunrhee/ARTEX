package notify

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Filter is the contract for the notification_channels.filter JSONB column: a
// channel instance's filter conditions. Every field is optional, and the default
// is "no filtering" — which is exactly the fallback semantics for a malformed
// config, see ParseFilter.
type Filter struct {
	// MinSeverity is the minimum severity threshold (low/medium/high/critical);
	// empty = no threshold.
	MinSeverity string `json:"min_severity"`
	// TaskIDs / AssetIDs: an empty array means no restriction; a non-empty one
	// requires the event to intersect it.
	TaskIDs  []int64 `json:"task_ids"`
	AssetIDs []int64 `json:"asset_ids"`
	// VulnClassInclude empty means accept all; non-empty requires vulnclass to hit
	// any one of its keywords.
	// VulnClassExclude excludes on hitting any one keyword (exclude takes priority
	// over include).
	// Matching is a case-insensitive substring — safer than a regex: a user's
	// misconfigured regex won't make the channel silently fail.
	VulnClassInclude []string `json:"vulnclass_include"`
	VulnClassExclude []string `json:"vulnclass_exclude"`
	// OnStatusChange decides whether this channel receives finding-status-change
	// events (meaningful only in realtime mode).
	OnStatusChange bool `json:"on_status_change"`
}

// ParseFilter parses a channel's filter config.
//
// **Never returns an error.** This is a deliberate design choice: a malformed
// filter config always degrades to a zero-value Filter (= no filtering = matches
// everything), because for a findings notification system, **pushing one extra
// beats silently dropping one high-severity finding**. Making a parse failure
// mean "don't push" gives the user a channel that looks configured but pushes
// nothing — the worst failure mode.
func ParseFilter(raw []byte) Filter {
	var f Filter
	if len(raw) == 0 {
		return f
	}
	// On a parse failure f stays zero-valued, i.e. no filtering.
	_ = json.Unmarshal(raw, &f)
	return f
}

// ValidMinSeverity reports whether s is a valid severity threshold (an empty
// string means no threshold).
func ValidMinSeverity(s string) bool {
	if s == "" {
		return true
	}
	_, ok := severityRank[s]
	return ok
}

// Validate checks the **value-restricted** fields in a filter config, for use
// when saving a channel.
//
// Why it must be blocked at write time: Match's decision for an unknown threshold
// is `rank >= 0`, which is always true — that is, a single typo in min_severity
// ("hgih") makes the filter **silently fail** into "push everything". This aligns
// with this package's "better over-push than under-push" tradeoff (nothing is
// dropped), but the consequence is a user who thinks they're doing tiered pushing
// while actually flooding the group with every finding, with no sign that they
// misconfigured it. This kind of "silent degradation" is exactly what should be
// blocked at the entry point.
//
// Note that Validate is used only on the **write** path. The read path still uses
// ParseFilter's tolerant semantics, so bad values already in historical data
// don't make the whole channel unreadable.
func (f Filter) Validate() error {
	if !ValidMinSeverity(f.MinSeverity) {
		return fmt.Errorf("minimum severity %q is invalid; choose: low / medium / high / critical, or leave empty for no limit", f.MinSeverity)
	}
	return nil
}

// Match decides whether an event should be delivered to a channel carrying these
// filter conditions.
//
// **Never returns an error**, for the same reason as ParseFilter: any internal
// anomaly is treated as a "match". Decision order: event type → severity
// threshold → task/asset scope → finding-type keywords.
func Match(f Filter, s Snapshot) bool {
	// Status-change events are received only by channels that explicitly opt in.
	// Off by default, because the vast majority of users expect "push" to mean "a
	// new finding was discovered", not a running log of every status transition.
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
	// Exclude takes priority: hitting any exclude keyword is out, even if it also
	// hits the include list.
	if len(f.VulnClassExclude) > 0 && containsAnyFold(s.VulnClass, f.VulnClassExclude) {
		return false
	}
	if len(f.VulnClassInclude) > 0 && !containsAnyFold(s.VulnClass, f.VulnClassInclude) {
		return false
	}
	return true
}

func intersectsInt(a, b []int64) bool {
	// A linear scan of small sets is fine; both sides are on the order of "a few
	// dozen hand-checked items", so building a map costs more than it saves.
	for _, v := range b {
		if slices.Contains(a, v) {
			return true
		}
	}
	return false
}

// containsAnyFold reports whether s contains any of keywords (case-insensitive).
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

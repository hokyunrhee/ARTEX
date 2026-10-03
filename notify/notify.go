// Package notify implements IM and email adapters for finding notifications.
//
// This leaf package depends only on the standard library, without database or
// server knowledge. Configuration arrives as map[string]any from the JSONB
// notification_channels.config column, and content arrives as Message. Signing,
// UTF-8 truncation, and filtering can therefore be unit-tested without PostgreSQL;
// the server orchestrates delivery.
//
// Channel implementations must be stateless. The same instance may serve multiple
// configurations or bots concurrently. Every credential comes from cfg; never
// cache Webhook URLs or similar values in implementation fields.
package notify

// Channel type identifiers also define valid notification_channels.kind values.
// The server validates the allowlist, like findings.status, rather than using
// a DB CHECK that would complicate adding channels.
const (
	KindDingTalk = "dingtalk" // DingTalk custom bot.
	KindFeishu   = "feishu"   // Feishu (including Lark) custom bot.
	KindWeCom    = "wecom"    // WeCom group bot.
	KindWebhook  = "webhook"  // Generic Webhook with custom method, headers, and JSON template.
	KindTelegram = "telegram" // Telegram Bot API
	KindEmail    = "email"    // SMTP email.
)

// Event types stored in notification_events.kind.
const (
	EventFindingCreated       = "finding_created"
	EventFindingStatusChanged = "finding_status_changed"
)

// InitKind is the fallback when configuration kind is empty.
const InitKind = KindDingTalk

// severityRank maps severity to a comparable rank. Unknown values yield zero and
// are blocked by any min_severity threshold to avoid flooding on uncertain severity.
var severityRank = map[string]int{
	"low":      1,
	"medium":   2,
	"high":     3,
	"critical": 4,
}

// SeverityRank returns the rank, or zero for an unknown severity.
func SeverityRank(severity string) int { return severityRank[severity] }

// SeverityLabel returns an English severity label with its emoji for message
// titles and cards. Unknown values are returned unchanged rather than invented.
func SeverityLabel(severity string) string {
	switch severity {
	case "critical":
		return "🔴 Critical"
	case "high":
		return "🟠 High"
	case "medium":
		return "🟡 Medium"
	case "low":
		return "🔵 Low"
	default:
		return severity
	}
}

// StatusLabel renders a finding status in English for status-change messages.
func StatusLabel(status string) string {
	switch status {
	case "pending":
		return "Pending"
	case "in_progress":
		return "In progress"
	case "confirmed":
		return "Confirmed"
	case "resolved":
		return "Resolved"
	case "fixed":
		return "Fixed"
	case "false_positive":
		return "False positive"
	case "ignored":
		return "Ignored"
	case "duplicate":
		return "Duplicate"
	case "risk_accepted":
		return "Risk accepted"
	default:
		return status
	}
}

// AtLeast checks severity against min. Empty min permits all severities.
// Unknown severity has rank zero and fails any nonempty threshold; see severityRank.
func AtLeast(severity, min string) bool {
	if min == "" {
		return true
	}
	return SeverityRank(severity) >= SeverityRank(min)
}

package notify

// Snapshot is the contract for notification_events.snapshot (JSONB). The DB
// finding-write transaction produces it; server delivery and filtering consume it.
// It belongs to the notification domain: the DB serializes it without interpreting
// its fields.
//
// Store finding fields redundantly because names, severity, and status can change
// afterward. Notifications must reflect the conclusion at the time of the event;
// looking it up later could misleadingly show a severity changed to low. Snapshots
// also avoid joining findings, tasks, and assets during fan-out and rendering.
type Snapshot struct {
	// Event type: finding_created / finding_status_changed.
	Kind      string  `json:"kind"`
	FindingID int64   `json:"finding_id"`
	TaskID    int64   `json:"task_id"`
	VulnClass string  `json:"vulnclass"`
	Name      string  `json:"name"`
	Severity  string  `json:"severity"`
	Summary   string  `json:"summary"`
	AssetIDs  []int64 `json:"asset_ids"`
	// Nonempty only for kind=finding_status_changed.
	FromStatus string `json:"from_status,omitempty"`
	ToStatus   string `json:"to_status,omitempty"`
}

// Item is a finding to be rendered for notification delivery.
type Item struct {
	FindingID int64
	Name      string
	VulnClass string
	Severity  string
	Summary   string
	// Assets holds resolved display names, such as domains/IPs, supplied by the server.
	// This package does not access the database and cannot resolve those names itself.
	Assets []string
	// DetailURL links to finding details; omit it when public_base_url is not configured.
	DetailURL string
	// For status-change events only; render "Pending -> Fixed" when both are nonempty.
	FromStatus string
	ToStatus   string
}

// IsStatusChange reports whether this item is a status-change event.
func (i Item) IsStatusChange() bool { return i.FromStatus != "" || i.ToStatus != "" }

// Title prefers the operator-provided name, falls back to vulnclass, then uses a
// placeholder if both are empty. Never return an empty title.
func (i Item) Title() string {
	if i.Name != "" {
		return i.Name
	}
	if i.VulnClass != "" {
		return i.VulnClass
	}
	return "(Untitled finding)"
}

// Message is the complete content of one channel delivery.
type Message struct {
	// One item for realtime delivery, or the entire batch for a digest.
	// An empty slice is invalid; callers must provide at least one item.
	Items []Item
	// Batch=true selects digest rendering, including its title, time window, and count.
	Batch bool
	// WindowMinutes is the digest interval, used for "last N minutes" when Batch=true.
	// Pass the configured value explicitly instead of time.Since to keep rendering deterministic.
	WindowMinutes int
	// HomeURL is the dashboard address (global public_base_url); omit the link when empty.
	HomeURL string
}

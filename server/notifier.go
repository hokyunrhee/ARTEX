package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/notify"
)

// Global keys in the settings table; no new table is needed.
const (
	// settingNotifyEnabled is the master switch, enabled by default. It provides a
	// maintenance stop; configuring a channel is what actually enables notifications.
	settingNotifyEnabled = "notify_enabled"
	// settingNotifyPublicBaseURL is the externally accessible URL for finding detail
	// links, such as https://artex.example.com. Empty means no link button.
	// The project has no existing reusable public-URL setting.
	settingNotifyPublicBaseURL = "notify_public_base_url"
	// settingNotifyDigestMinutes is the digest interval in minutes.
	settingNotifyDigestMinutes = "notify_digest_interval_min"
)

const (
	// notifyTick is the delivery polling interval. Its three seconds bound engine
	// responsiveness and account for most delay between storing a finding and IM delivery.
	notifyTick = 3 * time.Second
	// notifyLease is the claim lease. It must substantially exceed the worst-case
	// delivery time (the notify HTTP client has a 15-second timeout), or two
	// dispatchers could send the same row concurrently.
	notifyLease = 3 * time.Minute
	// notifyFanOutPerTick limits events dispatched per round so enabling a channel
	// does not expand the entire historical backlog at once.
	notifyFanOutPerTick = 200
	// notifyDefaultDigestMinutes is the default digest interval.
	notifyDefaultDigestMinutes = 30
	// notifyUnlimitedBurstPerTick bounds sends per round for unlimited channels,
	// preventing thousands of findings from blocking one loop for an extended period.
	notifyUnlimitedBurstPerTick = 50
	// notifyMaxSendsPerChannelPerTick caps deliveries per channel per round.
	//
	// Derive this cap from the lease. Serial sends whose worst-case duration exceeds
	// the three-minute notifyLease leave later rows expired before completion. A
	// single process has a non-reentrant Run goroutine, but another process sharing
	// the database could reclaim and duplicate them, double-increment attempts, and
	// mark them failed while the original process is still sending.
	//
	// Three minutes / 30 seconds permits six sends with no margin, which is unsafe.
	// Use five: 150 seconds leaves 30 seconds spare. TestNotifyTickBudgetFitsWithinLease
	// enforces the relationship among this value, notifyLease, and notifySendTimeout.
	notifyMaxSendsPerChannelPerTick = 5
	// notifySendTimeout bounds one delivery. Its product with the per-round cap must
	// stay below notifyLease; see TestNotifyTickBudgetFitsWithinLease.
	notifySendTimeout = 30 * time.Second
)

// notifyBackoff is indexed by attempts already made. The three-attempt budget,
// including the first send, matches db.MaxNotifyAttempts; change them together.
var notifyBackoff = []time.Duration{
	time.Second,
	5 * time.Second,
	30 * time.Second,
}

// Notifier delivers finding notifications.
//
// It runs in its own goroutine alongside Scheduler (see server.New), with a
// separate tick because its three-second responsiveness differs from trigger
// scheduling. A blocked notifier must not prevent agents from being triggered.
type Notifier struct {
	s  *Server
	pg *db.DB

	// mu protects buckets. Few channels and little contention make one mutex sufficient.
	mu      sync.Mutex
	buckets map[int64]*notifyBucket
}

// notifyBucket is a per-channel token bucket.
//
// A counter reset every minute permits 20 sends just before a boundary and another
// 20 just after, appearing as 40 in one second to the platform. Constant-rate
// token replenishment avoids that boundary burst.
type notifyBucket struct {
	tokens   float64
	lastFill time.Time
}

func newNotifier(s *Server) *Notifier {
	return &Notifier{s: s, pg: s.m.pg, buckets: map[int64]*notifyBucket{}}
}

// Run loops until ctx ends. server.New starts it once.
func (n *Notifier) Run(ctx context.Context) {
	if n.pg == nil {
		return
	}
	t := time.NewTicker(notifyTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n.step(ctx)
		}
	}
}

// step fans out new events, then sends due deliveries.
//
// Failures are logged without stopping the loop; notification failures must never
// become process-wide failures. Each tick is independent and naturally retries.
func (n *Notifier) step(ctx context.Context) {
	if !n.enabled() {
		return
	}
	if _, _, err := n.pg.FanOutPendingEvents(ctx, notifyFanOutPerTick); err != nil {
		log.Printf("[notify] failed to dispatch events: %v", err)
		return
	}
	channels, err := n.pg.ListNotificationChannels(ctx)
	if err != nil {
		log.Printf("[notify] failed to read channels: %v", err)
		return
	}
	baseURL := n.publicBaseURL()
	for _, ch := range channels {
		if !ch.IsEnabled() {
			continue
		}
		// Tokens count messages, equivalent to HTTP requests, not findings. Realtime
		// sends one finding per message; an entire digest consumes only one token.
		//
		// Both modes check tokens before claiming within the allowance; reversing this
		// order would consume retries for deliveries blocked only by rate limiting.
		now := time.Now()
		if ch.Mode == db.NotifyModeDigest {
			tokens, claimLimit := digestTickPlan()
			if n.takeTokens(ch.ID, ch.RatePerMin, tokens, now) <= 0 {
				continue
			}
			n.stepDigest(ctx, ch, claimLimit, baseURL)
			continue
		}
		allow := n.takeTokens(ch.ID, ch.RatePerMin, notifyMaxSendsPerChannelPerTick, now)
		if allow <= 0 {
			continue
		}
		n.stepRealtime(ctx, ch, allow, baseURL)
	}
}

// digestTickPlan returns token consumption and the maximum digest batch size.
//
// These have different units, warranting a dedicated function:
//   - tokens counts messages. One batch is one HTTP request, so this is always 1;
//     rate_per_min still limits digest messages per minute.
//   - claimLimit counts findings and is bounded by memory, not request allowance.
//
// Previously the lease-derived per-round request budget was passed as batch size
// to make digest respect rate_per_min. At rate_per_min=20, a three-second tick
// replenished only one token, yielding one finding per digest and never reaching
// db.MaxDigestBatchSize. Readers received a stream of one-finding digest messages.
//
// End-to-end tests manually passed large stepDigest limits and missed the step
// calculation. TestDigestTickPlanDecouplesBatchSizeFromSendBudget directly verifies it.
func digestTickPlan() (tokens, claimLimit int) {
	return 1, db.MaxDigestBatchSize
}

// stepRealtime claims and sends one message per finding for one channel.
func (n *Notifier) stepRealtime(ctx context.Context, ch *db.NotificationChannel, allow int, baseURL string) {
	deliveries, err := n.pg.ClaimRealtimeDeliveries(ctx, ch.ID, allow, notifyLease)
	if err != nil {
		log.Printf("[notify] failed to claim realtime deliveries channel=%d: %v", ch.ID, err)
		return
	}
	if len(deliveries) == 0 {
		return
	}
	channel, cfg, ok := n.adapt(ch)
	if !ok {
		_ = n.pg.FailDeliveries(ctx, deliveryIDs(deliveries), fmt.Sprintf("Channel type %q is not registered", ch.Kind))
		return
	}
	for _, dl := range deliveries {
		msg, err := n.renderSingle(ctx, dl, baseURL)
		if err != nil {
			// Rendering failures are local data problems; retries cannot fix them.
			_ = n.pg.FailDeliveries(ctx, []int64{dl.ID}, err.Error())
			continue
		}
		n.send(ctx, channel, cfg, msg, []*db.NotificationDelivery{dl})
	}
}

// stepDigest aggregates due pending deliveries for one channel into one message.
func (n *Notifier) stepDigest(ctx context.Context, ch *db.NotificationChannel, allow int, baseURL string) {
	window := n.digestInterval()
	due, err := n.pg.DigestBatchDue(ctx, ch.ID, window)
	if err != nil {
		log.Printf("[notify] failed to check digest batch channel=%d: %v", ch.ID, err)
		return
	}
	if !due {
		return
	}
	deliveries, err := n.pg.ClaimDigestBatch(ctx, ch.ID, allow, notifyLease)
	if err != nil {
		log.Printf("[notify] failed to claim digest batch channel=%d: %v", ch.ID, err)
		return
	}
	if len(deliveries) == 0 {
		return
	}
	channel, cfg, ok := n.adapt(ch)
	if !ok {
		_ = n.pg.FailDeliveries(ctx, deliveryIDs(deliveries), fmt.Sprintf("Channel type %q is not registered", ch.Kind))
		return
	}
	msg, included, err := n.renderBatch(ctx, deliveries, baseURL, int(window.Minutes()))
	if err != nil {
		_ = n.pg.FailDeliveries(ctx, deliveryIDs(deliveries), err.Error())
		return
	}
	// Explicitly fail deliveries whose broken snapshots cannot enter the message.
	// Otherwise they remain outside included and all subsequent success/failure
	// updates, stuck in sending until their lease expires and they are reclaimed.
	if skipped := excludeDeliveries(deliveries, included); len(skipped) > 0 {
		reason := "The event snapshot cannot be parsed; this finding cannot be rendered as a message"
		if fErr := n.pg.FailDeliveries(ctx, deliveryIDs(skipped), reason); fErr != nil {
			log.Printf("[notify] failed to mark invalid-snapshot deliveries channel=%s ids=%v: %v", ch.Kind, deliveryIDs(skipped), fErr)
		}
		log.Printf("[notify] skipped %d deliveries with unparseable snapshots channel=%d", len(skipped), ch.ID)
	}
	// Pass only included deliveries to send: included[i] must match msg.Items[i].
	// send uses that mapping to mark the first K items reported by the channel.
	n.send(ctx, channel, cfg, msg, included)
}

// send delivers and transitions states according to the result.
//
// A batch shares one send result: delivered or retried together. Do not retry
// individual entries from a digest, which would corrupt its grouping semantics.
//
// The exception is segmentation at channel length limits. If only the first K
// items fit, defer the rest to the next batch. Marking them sent would make those
// findings disappear from both the message and the failure list.
func (n *Notifier) send(ctx context.Context, channel notify.Channel, cfg map[string]any, msg notify.Message, deliveries []*db.NotificationDelivery) {
	// Bound each send so one stalled channel cannot block the remaining channels this round.
	sendCtx, cancel := context.WithTimeout(ctx, notifySendTimeout)
	defer cancel()
	delivered, err := channel.Send(sendCtx, cfg, msg)
	if err == nil && delivered > 0 {
		if delivered > len(deliveries) {
			// A channel cannot deliver more items than supplied. If it reports that, log the
			// renderer bug and mark all supplied items delivered rather than corrupting records.
			log.Printf("[notify] channel reported %d delivered items for %d deliveries channel=%s; treating all as delivered",
				delivered, len(deliveries), channel.Kind())
			delivered = len(deliveries)
		}
		sent, rest := deliveries[:delivered], deliveries[delivered:]
		if err := n.pg.MarkDeliveriesSent(ctx, deliveryIDs(sent)); err != nil {
			log.Printf("[notify] failed to mark deliveries as sent channel=%s ids=%v: %v", channel.Kind(), deliveryIDs(sent), err)
		}
		if len(rest) > 0 {
			// The message reached its length limit; immediately requeue the remainder for
			// the next tick. Use DeferDeliveries rather than RescheduleDeliveries: this is
			// not a failure and must undo the optimistic claim-time retry increment.
			if err := n.pg.DeferDeliveries(ctx, deliveryIDs(rest),
				fmt.Sprintf("This message reached the channel length limit; only the first %d items were delivered, and the rest are deferred to the next batch", delivered)); err != nil {
				log.Printf("[notify] failed to queue the next segment channel=%s ids=%v: %v", channel.Kind(), deliveryIDs(rest), err)
			}
		}
		return
	}
	if err == nil {
		// No error and no delivered count is a failure with backoff, preventing endless
		// claims of a delivery that can never reach a terminal state.
		err = fmt.Errorf("Channel did not report any delivered items (delivered=%d)", delivered)
	}

	// Decide failures per delivery, not using the maximum attempts for the batch.
	//
	// Previously maxAttempts(deliveries) could fail an entire batch, letting an older
	// retried row drag fresh rows into failed before they received any retry. This
	// contradicted the intent to isolate new rows from exhausted older ones.
	permanent := notify.IsPermanent(err)
	var failIDs, exhaustedIDs []int64
	byDelay := map[time.Duration][]int64{}
	for _, dl := range deliveries {
		switch {
		case permanent:
			failIDs = append(failIDs, dl.ID)
		case dl.Attempts >= db.MaxNotifyAttempts:
			exhaustedIDs = append(exhaustedIDs, dl.ID)
		default:
			delay := notifyBackoff[min(dl.Attempts, len(notifyBackoff)-1)]
			byDelay[delay] = append(byDelay[delay], dl.ID)
		}
	}

	if len(failIDs) > 0 {
		if fErr := n.pg.FailDeliveries(ctx, failIDs, err.Error()); fErr != nil {
			log.Printf("[notify] failed to mark failed deliveries channel=%s ids=%v: %v", channel.Kind(), failIDs, fErr)
		}
	}
	if len(exhaustedIDs) > 0 {
		reason := fmt.Sprintf("Still failing after %d retries: %s", db.MaxNotifyAttempts, err)
		if fErr := n.pg.FailDeliveries(ctx, exhaustedIDs, reason); fErr != nil {
			log.Printf("[notify] failed to mark failed deliveries channel=%s ids=%v: %v", channel.Kind(), exhaustedIDs, fErr)
		}
	}
	// Group rescheduling by delay. Only three backoff levels exist, avoiding up to
	// 500 separate UPDATE round trips for a large batch.
	for delay, group := range byDelay {
		if rErr := n.pg.RescheduleDeliveries(ctx, group, delay, err.Error()); rErr != nil {
			log.Printf("[notify] failed to reschedule deliveries channel=%s ids=%v: %v", channel.Kind(), group, rErr)
		}
	}
	if len(failIDs)+len(exhaustedIDs) > 0 {
		log.Printf("[notify] delivery failed channel=%d kind=%s permanent=%d retries_exhausted=%d pending_retry=%d: %s",
			deliveries[0].ChannelID, channel.Kind(), len(failIDs), len(exhaustedIDs), len(byDelay), err)
	}
}

// excludeDeliveries returns members of all absent from keep, comparing pointer
// identity. Deliveries omitted from a message need explicit handling.
func excludeDeliveries(all, keep []*db.NotificationDelivery) []*db.NotificationDelivery {
	inKeep := make(map[*db.NotificationDelivery]bool, len(keep))
	for _, dl := range keep {
		inKeep[dl] = true
	}
	var out []*db.NotificationDelivery
	for _, dl := range all {
		if !inKeep[dl] {
			out = append(out, dl)
		}
	}
	return out
}

// adapt looks up the channel and parses configuration. ok=false means an
// unregistered type, which must fail rather than retry indefinitely.
func (n *Notifier) adapt(ch *db.NotificationChannel) (notify.Channel, map[string]any, bool) {
	channel, ok := notify.Get(ch.Kind)
	if !ok {
		return nil, nil, false
	}
	var cfg map[string]any
	if len(ch.Config) > 0 {
		// On parse failure use an empty map; the channel Validate method can identify
		// missing fields more helpfully than a JSON parsing error.
		_ = json.Unmarshal(ch.Config, &cfg)
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	return channel, cfg, true
}

// renderSingle renders a single-finding message.
func (n *Notifier) renderSingle(ctx context.Context, dl *db.NotificationDelivery, baseURL string) (notify.Message, error) {
	snap, err := parseSnapshot(dl)
	if err != nil {
		return notify.Message{}, err
	}
	item, err := n.itemFor(ctx, snap, baseURL)
	if err != nil {
		return notify.Message{}, err
	}
	return notify.Message{Items: []notify.Item{item}, HomeURL: baseURL}, nil
}

// renderBatch parses snapshots individually; one broken entry must not discard
// the entire digest.
//
// The returned included slice must map one-to-one to msg.Items. Callers mark the
// first K deliveries sent based on the channel report. Skipping a snapshot without
// also excluding its delivery would shift indexes, marking bad entries delivered
// and good ones undelivered. stepDigest explicitly fails the omitted entries.
func (n *Notifier) renderBatch(ctx context.Context, deliveries []*db.NotificationDelivery, baseURL string, windowMinutes int) (notify.Message, []*db.NotificationDelivery, error) {
	items := make([]notify.Item, 0, len(deliveries))
	included := make([]*db.NotificationDelivery, 0, len(deliveries))
	for _, dl := range deliveries {
		snap, err := parseSnapshot(dl)
		if err != nil {
			// A broken snapshot enters neither the message nor included. The caller must
			// explicitly fail it instead of letting it masquerade as delivered.
			log.Printf("[notify] skipped an unparseable snapshot in digest batch delivery=%d: %v", dl.ID, err)
			continue
		}
		item, err := n.itemFor(ctx, snap, baseURL)
		if err != nil {
			return notify.Message{}, nil, err
		}
		items = append(items, item)
		included = append(included, dl)
	}
	if len(items) == 0 {
		return notify.Message{}, nil, fmt.Errorf("All %d deliveries in the digest batch have unparseable snapshots", len(deliveries))
	}
	return notify.Message{
		Items:         items,
		Batch:         true,
		WindowMinutes: windowMinutes,
		HomeURL:       baseURL,
	}, included, nil
}

// itemFor converts an event snapshot to an item and resolves asset names and detail links.
func (n *Notifier) itemFor(ctx context.Context, snap notify.Snapshot, baseURL string) (notify.Item, error) {
	assets, err := n.pg.NotificationAssetNames(ctx, snap.AssetIDs)
	if err != nil {
		// Missing asset names must not prevent delivery; an omitted asset line is less
		// serious than a missing notification.
		log.Printf("[notify] failed to resolve asset names finding=%d: %v", snap.FindingID, err)
	}
	item := notify.Item{
		FindingID:  snap.FindingID,
		Name:       snap.Name,
		VulnClass:  snap.VulnClass,
		Severity:   snap.Severity,
		Summary:    snap.Summary,
		Assets:     assets,
		FromStatus: snap.FromStatus,
		ToStatus:   snap.ToStatus,
	}
	if baseURL != "" {
		// The detail route is web/src/app/(main)/function/findings/detail/page.tsx;
		// it reads the finding ID from the id query parameter.
		item.DetailURL = fmt.Sprintf("%s/function/findings/detail?id=%d", baseURL, snap.FindingID)
	}
	return item, nil
}

// takeTokens removes at most want tokens and returns the actual count.
//
// One token means one message or HTTP request. Realtime callers request the
// number of sends; a digest batch needs one. Capacity equals the per-minute limit
// and replenishes at a constant rate. ratePerMin<=0 means unlimited, but returns
// a finite generous allowance to bound per-round backlog processing.
//
// The want cap matters: draining the bucket would discard tokens beyond the
// caller's round limit, making saved burst capacity unreachable and charging even
// rounds with no pending deliveries.
func (n *Notifier) takeTokens(channelID int64, ratePerMin, want int, now time.Time) int {
	if want <= 0 {
		return 0
	}
	if ratePerMin <= 0 {
		return min(want, notifyUnlimitedBurstPerTick)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	b := n.buckets[channelID]
	if b == nil {
		b = &notifyBucket{tokens: float64(ratePerMin), lastFill: now}
		n.buckets[channelID] = b
	}
	// Replenish using actual elapsed time at ratePerMin/60 per second.
	if elapsed := now.Sub(b.lastFill).Seconds(); elapsed > 0 {
		b.tokens = minF(float64(ratePerMin), b.tokens+elapsed*float64(ratePerMin)/60)
		b.lastFill = now
	}
	// Add a tiny epsilon before converting to int: floating accumulation such as
	// 0.5 + 0.5 can yield 0.9999999999 and incorrectly remove an available token.
	// 1e-9 is far smaller than one token and does not permit a real deficit.
	take := min(int(b.tokens+1e-9), want)
	if take <= 0 {
		return 0
	}
	b.tokens -= float64(take)
	return take
}

// enabled reads the master switch.
func (n *Notifier) enabled() bool {
	return n.pg.GetBool(settingNotifyEnabled, true)
}

// publicBaseURL returns the external link base without a trailing slash.
func (n *Notifier) publicBaseURL() string {
	v, ok, err := n.pg.GetSetting(settingNotifyPublicBaseURL)
	if err != nil || !ok {
		return ""
	}
	return trimTrailingSlash(v)
}

// digestInterval returns the configured interval or its default for invalid/missing values.
func (n *Notifier) digestInterval() time.Duration {
	v, ok, err := n.pg.GetSetting(settingNotifyDigestMinutes)
	if err != nil || !ok {
		return time.Duration(notifyDefaultDigestMinutes) * time.Minute
	}
	m := 0
	if _, err := fmt.Sscanf(v, "%d", &m); err != nil || m <= 0 {
		return time.Duration(notifyDefaultDigestMinutes) * time.Minute
	}
	return time.Duration(m) * time.Minute
}

// parseSnapshot parses the event snapshot for a delivery.
func parseSnapshot(dl *db.NotificationDelivery) (notify.Snapshot, error) {
	var snap notify.Snapshot
	if len(dl.Snapshot) == 0 {
		return snap, fmt.Errorf("Event snapshot for delivery %d is empty", dl.ID)
	}
	if err := json.Unmarshal(dl.Snapshot, &snap); err != nil {
		return snap, fmt.Errorf("Failed to parse event snapshot for delivery %d: %w", dl.ID, err)
	}
	if snap.Kind == "" {
		// The event row is authoritative; an older version may have written the snapshot copy.
		snap.Kind = dl.EventKind
	}
	return snap, nil
}

func deliveryIDs(deliveries []*db.NotificationDelivery) []int64 {
	out := make([]int64, 0, len(deliveries))
	for _, dl := range deliveries {
		out = append(out, dl.ID)
	}
	return out
}

func trimTrailingSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

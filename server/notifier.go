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

// Global settings keys (stored in the settings key-value table, no table needed).
const (
	// settingNotifyEnabled is the master notification switch. On by default: it is a
	// one-click kill switch for maintenance windows, not the feature's enable condition —
	// the real enable condition is "whether any channel is configured".
	settingNotifyEnabled = "notify_enabled"
	// settingNotifyPublicBaseURL is the externally reachable address used to build the
	// finding-detail back-link (e.g. https://artex.example.com). Leave it empty and the
	// message carries no back-link button. The project has no reusable external-address
	// setting, so this is a new one.
	settingNotifyPublicBaseURL = "notify_public_base_url"
	// settingNotifyDigestMinutes is the digest-mode interval (minutes).
	settingNotifyDigestMinutes = "notify_digest_interval_min"
)

const (
	// notifyTick is the delivery engine's poll interval. 3 seconds is the ceiling on how
	// real-time the engine is, and the main latency source between "a finding is persisted"
	// and "the message reaches the IM".
	notifyTick = 3 * time.Second
	// notifyLease is the lease duration taken when a delivery is claimed. It must be clearly
	// larger than the worst-case cost of a single delivery (the notify package's HTTP client
	// times out at 15 seconds), or the same row could be delivered by two dispatchers at once.
	notifyLease = 3 * time.Minute
	// notifyFanOutPerTick caps how many events are fanned out per tick, so enabling a channel
	// for the first time does not expand the entire historical backlog into delivery tasks at once.
	notifyFanOutPerTick = 200
	// notifyDefaultDigestMinutes is the default digest interval.
	notifyDefaultDigestMinutes = 30
	// notifyUnlimitedBurstPerTick is the per-tick delivery cap for a channel with no rate limit.
	// It exists to prevent "a channel configured with no rate limit + a scan that finds thousands
	// of findings" from stalling a single loop iteration for a long time.
	notifyUnlimitedBurstPerTick = 50
	// notifyMaxSendsPerChannelPerTick is how many deliveries one channel sends at most per tick.
	//
	// This cap is derived backwards from the **lease duration**: claiming a row stamps it with a
	// lease (notifyLease = 3 minutes), and if a tick delivers serially enough rows that the
	// worst-case cost exceeds the lease, the lease expires on the later rows before they are sent.
	// Within one process this does not matter (Run is a single goroutine running serially, and a
	// tick does not re-enter), but when **two processes share one database**, the peer re-claims
	// the lease-expired rows and sends them again, double-increments attempts, and judges them
	// failed while the original process is still delivering.
	//
	// Value: 3-minute lease / 30-second per-send timeout = 6 would **use up the lease exactly**
	// with zero margin, which is unacceptable; 5 makes the worst case 150 seconds, leaving a
	// 30-second margin. This relationship is pinned by TestNotifyTickBudgetFitsWithinLease —
	// changing any one of notifyLease, notifySendTimeout or this value breaks that assertion.
	notifyMaxSendsPerChannelPerTick = 5
	// notifySendTimeout is the timeout for a single delivery. It also drives the previous
	// constant's value, and their product must not exceed notifyLease; see
	// TestNotifyTickBudgetFitsWithinLease.
	notifySendTimeout = 30 * time.Second
)

// notifyBackoff is the backoff sequence for failed retries, indexed by the number of
// attempts already made. The 3 chances (including the first) correspond to
// db.MaxNotifyAttempts; the two must be changed together.
var notifyBackoff = []time.Duration{
	time.Second,
	5 * time.Second,
	30 * time.Second,
}

// Notifier is the delivery engine for finding notifications.
//
// It runs alongside the Scheduler as an independent goroutine (see server.New). It
// deliberately does not reuse the Scheduler's tick: the real-time requirement of
// notifications (3 seconds) differs from the triggers' business cadence, and their
// failures are independent — a stuck notification must not affect agent triggering.
type Notifier struct {
	s  *Server
	pg *db.DB

	// mu guards buckets. Channels are few and contention is low, so a single mutex is
	// enough; it is not worth introducing a finer-grained structure for it.
	mu      sync.Mutex
	buckets map[int64]*notifyBucket
}

// notifyBucket is one channel's token bucket.
//
// A token bucket is used instead of a "count per minute then reset" sliding window because
// the latter's boundary effect is bad: sending 20 at the end of a window and another 20 the
// next instant is 40 within one second to the platform and gets rate-limited; a token bucket
// refills at a constant rate and naturally avoids such bursts.
type notifyBucket struct {
	tokens   float64
	lastFill time.Time
}

func newNotifier(s *Server) *Notifier {
	return &Notifier{s: s, pg: s.m.pg, buckets: map[int64]*notifyBucket{}}
}

// Run loops until ctx ends. Started once by server.New.
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

// step runs one iteration: first fan out new events, then deliver the due tasks.
//
// A failure at any step only logs and does not break the loop — a notification-system
// failure must never escalate into a process-level problem. Each tick is independent, and
// the next one retries naturally.
func (n *Notifier) step(ctx context.Context) {
	if !n.enabled() {
		return
	}
	if _, _, err := n.pg.FanOutPendingEvents(ctx, notifyFanOutPerTick); err != nil {
		log.Printf("[notify] failed to fan out events: %v", err)
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
		// The token bucket's unit of measure is the **number of messages** (equivalent to the
		// number of HTTP requests), not the number of findings. In realtime mode the two are
		// the same (one finding, one message); in digest mode a whole batch of findings is
		// combined into one message, so it consumes only one token.
		//
		// Both modes ask the token bucket first, then claim per the allowance — the order must
		// not be reversed, or a delivery blocked by rate limiting would already have spent a retry.
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

// digestTickPlan returns a digest channel's token consumption and batch-size upper bound
// for this tick.
//
// The two return values are of **two different dimensions**, which is exactly why this is
// a separate function:
//
//   - tokens is the number of messages. A batch of findings is combined into one message and
//     one HTTP request, so it is always 1. rate_per_min therefore still applies to digest
//     (at most this many digest messages per minute).
//   - claimLimit is how many findings this batch holds at most. It is bounded only by a memory
//     cap and is unrelated to the request budget.
//
// To make rate_per_min apply to digest, the per-tick request budget
// (notifyMaxSendsPerChannelPerTick, derived backwards from the lease) was once passed straight
// through as the batch size. The result: a channel with rate_per_min=20 only refills 1 token in
// a 3-second tick, so each digest message held only 1 finding — digest degraded into "realtime
// delivery with digest wording", and readers got a stream of "1 new finding in the last 30
// minutes", while db.MaxDigestBatchSize was never reachable.
//
// This symptom is hard to catch in end-to-end tests (existing cases all pass a large enough
// limit to stepDigest by hand, bypassing the allowance math in step), so the decision is
// consolidated here and pinned directly by TestDigestTickPlanDecouplesBatchSizeFromSendBudget.
func digestTickPlan() (tokens, claimLimit int) {
	return 1, db.MaxDigestBatchSize
}

// stepRealtime claims and delivers a channel's realtime tasks, one message per finding.
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
		_ = n.pg.FailDeliveries(ctx, deliveryIDs(deliveries), fmt.Sprintf("channel type %q is not registered", ch.Kind))
		return
	}
	for _, dl := range deliveries {
		msg, err := n.renderSingle(ctx, dl, baseURL)
		if err != nil {
			// A render failure is a local data problem; retrying will not improve it.
			_ = n.pg.FailDeliveries(ctx, []int64{dl.ID}, err.Error())
			continue
		}
		n.send(ctx, channel, cfg, msg, []*db.NotificationDelivery{dl})
	}
}

// stepDigest aggregates a channel's pending deliveries into one message and sends it when the
// batch is due.
func (n *Notifier) stepDigest(ctx context.Context, ch *db.NotificationChannel, allow int, baseURL string) {
	window := n.digestInterval()
	due, err := n.pg.DigestBatchDue(ctx, ch.ID, window)
	if err != nil {
		log.Printf("[notify] failed to judge the digest batch channel=%d: %v", ch.ID, err)
		return
	}
	if !due {
		return
	}
	deliveries, err := n.pg.ClaimDigestBatch(ctx, ch.ID, allow, notifyLease)
	if err != nil {
		log.Printf("[notify] failed to claim the digest batch channel=%d: %v", ch.ID, err)
		return
	}
	if len(deliveries) == 0 {
		return
	}
	channel, cfg, ok := n.adapt(ch)
	if !ok {
		_ = n.pg.FailDeliveries(ctx, deliveryIDs(deliveries), fmt.Sprintf("channel type %q is not registered", ch.Kind))
		return
	}
	msg, included, err := n.renderBatch(ctx, deliveries, baseURL, int(window.Minutes()))
	if err != nil {
		_ = n.pg.FailDeliveries(ctx, deliveryIDs(deliveries), err.Error())
		return
	}
	// Deliveries whose snapshot is broken and did not make it into the message must be failed
	// explicitly. Otherwise they stay outside included — in neither the message nor the failure
	// list — and on a successful send the later batch marking misses their status, leaving them
	// stuck at sending forever until the lease expires and they are re-claimed repeatedly.
	if skipped := excludeDeliveries(deliveries, included); len(skipped) > 0 {
		reason := "event snapshot could not be parsed; this finding cannot be rendered into a message"
		if fErr := n.pg.FailDeliveries(ctx, deliveryIDs(skipped), reason); fErr != nil {
			log.Printf("[notify] failed to mark bad-snapshot deliveries channel=%s ids=%v: %v", ch.Kind, deliveryIDs(skipped), fErr)
		}
		log.Printf("[notify] skipped %d deliveries with an unparsable snapshot channel=%d", len(skipped), ch.ID)
	}
	// Hand send only those that made it into the message: included[i] corresponds strictly to
	// msg.Items[i], and send relies on that correspondence to land "the channel reported it held
	// the first K" onto the correct delivery rows.
	n.send(ctx, channel, cfg, msg, included)
}

// send delivers and transitions state by the result.
//
// One batch of deliveries (possibly dozens in digest mode) shares a single send result:
// either delivered, or the whole batch retries. There is no per-item retry — a digest message
// is a single message, and resending part of it would corrupt the batch's semantics.
//
// The only exception is **segmentation caused by the channel's length limit**: the channel
// reports it actually held only the first K, so from the K+1'th on they must wait for the next
// batch rather than being marked successful along with the rest. Otherwise the truncated
// findings are in neither the message nor the failure list and vanish entirely.
func (n *Notifier) send(ctx context.Context, channel notify.Channel, cfg map[string]any, msg notify.Message, deliveries []*db.NotificationDelivery) {
	// Cap a single delivery so a stuck channel does not drag down the rest of this tick's channels.
	sendCtx, cancel := context.WithTimeout(ctx, notifySendTimeout)
	defer cancel()
	delivered, err := channel.Send(sendCtx, cfg, msg)
	if err == nil && delivered > 0 {
		if delivered > len(deliveries) {
			// The channel cannot report more delivered than the delivery count; if it really
			// happens, the render layer miscounted. Treating it as all-delivered and recording
			// the problem is better than writing the records inconsistently.
			log.Printf("[notify] channel reported delivered count %d exceeds delivery count %d channel=%s, treating as all delivered",
				delivered, len(deliveries), channel.Kind())
			delivered = len(deliveries)
		}
		sent, rest := deliveries[:delivered], deliveries[delivered:]
		if err := n.pg.MarkDeliveriesSent(ctx, deliveryIDs(sent)); err != nil {
			log.Printf("[notify] failed to mark delivered channel=%s ids=%v: %v", channel.Kind(), deliveryIDs(sent), err)
		}
		if len(rest) > 0 {
			// This message hit the channel's length limit: the rest go straight back to the
			// queue to be continued on the next tick. Use DeferDeliveries rather than
			// RescheduleDeliveries — this is not a failure and should not spend the retry budget
			// (claiming already optimistically +1'd it, and this is where it is given back).
			if err := n.pg.DeferDeliveries(ctx, deliveryIDs(rest),
				fmt.Sprintf("this message hit the channel length limit; only the first %d were delivered, the rest wait for the next batch", delivered)); err != nil {
				log.Printf("[notify] failed to enqueue the segmented continuation channel=%s ids=%v: %v", channel.Kind(), deliveryIDs(rest), err)
			}
		}
		return
	}
	if err == nil {
		// The channel neither reported an error nor said how many were delivered. Treat it as a
		// failure (go through backoff), so this delivery is not re-claimed repeatedly yet never
		// marked off.
		err = fmt.Errorf("channel did not report a delivered count (delivered=%d)", delivered)
	}

	// Failure handling is decided **per item**, not by judging the whole batch on its maximum
	// attempt count.
	//
	// It used to be `if maxAttempts(deliveries) >= MaxNotifyAttempts` judging the whole batch
	// dead, but the attempt counts within a batch differ: an old delivery already retried twice
	// (attempts=2) would drag brand-new deliveries in the same batch (attempts=1) into failed —
	// a new finding is lost permanently without a single retry used, exactly the opposite of the
	// intent to "not let old rows drag new rows under".
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
			log.Printf("[notify] error marking failed status channel=%s ids=%v: %v", channel.Kind(), failIDs, fErr)
		}
	}
	if len(exhaustedIDs) > 0 {
		reason := fmt.Sprintf("still failing after %d retries: %s", db.MaxNotifyAttempts, err)
		if fErr := n.pg.FailDeliveries(ctx, exhaustedIDs, reason); fErr != nil {
			log.Printf("[notify] error marking failed status channel=%s ids=%v: %v", channel.Kind(), exhaustedIDs, fErr)
		}
	}
	// Reschedule grouped by delay: there are only 3 backoff tiers, so the number of groups is
	// naturally small, and there is no need to send one UPDATE per item (that would make a
	// 500-item batch do 500 round trips).
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

// excludeDeliveries returns those in all that are not in keep (compared by pointer identity).
// Used to find the deliveries that "did not make it into the message" — they must be handled
// explicitly and cannot be left in a gray zone.
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

// adapt fetches the channel implementation and parses its config.
// ok=false means the type is not registered, and the delivery should be failed directly
// rather than retried forever.
func (n *Notifier) adapt(ch *db.NotificationChannel) (notify.Channel, map[string]any, bool) {
	channel, ok := notify.Get(ch.Kind)
	if !ok {
		return nil, nil, false
	}
	var cfg map[string]any
	if len(ch.Config) > 0 {
		// Give an empty map when config parsing fails: the channel's own Validate reports
		// "which field is missing", and that error guides the user to a fix better than a
		// JSON parse error.
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

// renderBatch renders a digest message. It parses snapshots one by one — if one is broken it
// skips only that one, rather than letting it sink the whole digest.
//
// The return value included corresponds **strictly one-to-one** with msg.Items (the i'th
// delivery ↔ the i'th item). This correspondence is a hard requirement: the caller decides to
// mark the first K deliveries delivered based on "the channel reported it held the first K". If
// a broken snapshot is skipped here without removing the skipped delivery from included, the
// indices misalign — a bad item that should fail gets marked delivered, and a good item is
// misjudged as not delivered. The broken ones are failed explicitly by the caller; see stepDigest.
func (n *Notifier) renderBatch(ctx context.Context, deliveries []*db.NotificationDelivery, baseURL string, windowMinutes int) (notify.Message, []*db.NotificationDelivery, error) {
	items := make([]notify.Item, 0, len(deliveries))
	included := make([]*db.NotificationDelivery, 0, len(deliveries))
	for _, dl := range deliveries {
		snap, err := parseSnapshot(dl)
		if err != nil {
			// A broken snapshot enters neither the message nor included — its handling is the
			// caller's responsibility (fail it explicitly, rather than slipping it in among the
			// "delivered").
			log.Printf("[notify] skipping an unparsable snapshot in the digest batch delivery=%d: %v", dl.ID, err)
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
		return notify.Message{}, nil, fmt.Errorf("all %d deliveries in the digest batch are unparsable", len(deliveries))
	}
	return notify.Message{
		Items:         items,
		Batch:         true,
		WindowMinutes: windowMinutes,
		HomeURL:       baseURL,
	}, included, nil
}

// itemFor renders an event snapshot into a notification item, also resolving asset names and
// the detail back-link.
func (n *Notifier) itemFor(ctx context.Context, snap notify.Snapshot, baseURL string) (notify.Item, error) {
	assets, err := n.pg.NotificationAssetNames(ctx, snap.AssetIDs)
	if err != nil {
		// Failing to resolve asset names should not block the notification: missing a name is
		// far lighter than missing the notification, and the message just loses one asset line.
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
		// The detail page route is in web/src/app/(main)/function/findings/detail/page.tsx,
		// which reads the finding id from the query parameter id.
		item.DetailURL = fmt.Sprintf("%s/function/findings/detail?id=%d", baseURL, snap.FindingID)
	}
	return item, nil
}

// takeTokens takes **at most want** tokens from the channel's token bucket and returns the
// number actually taken.
//
// One token = one message (one HTTP request). In realtime mode the caller passes however many
// it wants; in digest mode a whole batch of findings sends one message, so it passes 1.
//
// The bucket capacity is the channel's per-minute cap, refilled at a constant rate. ratePerMin<=0
// means no rate limit and returns a finite but large enough value, preventing a single loop
// iteration from being dragged down by an unbounded backlog.
//
// The want cap is required: without it the only option is to drain the whole bucket, while the
// caller has its own per-tick cap, so the extra tokens taken are both unusable and vanish into
// thin air before the next refill — the accumulated burst capacity is never reachable, and even
// "this tick has no pending delivery" would still be charged.
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
	// Refill by the real elapsed time, at a rate of ratePerMin/60 per second.
	if elapsed := now.Sub(b.lastFill).Seconds(); elapsed > 0 {
		b.tokens = minF(float64(ratePerMin), b.tokens+elapsed*float64(ratePerMin)/60)
		b.lastFill = now
	}
	// Add a tiny epsilon before truncating: the token count is accumulated as a float, so
	// refilling in two steps, 0.5 + 0.5 may yield 0.9999999999, and a direct int() truncates it
	// to 0 — a bucket that is mathematically full yet yields no token. 1e-9 is far smaller than
	// one token and will not overlook a real shortfall.
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

// publicBaseURL returns the external address used for back-links, with any trailing slash removed.
func (n *Notifier) publicBaseURL() string {
	v, ok, err := n.pg.GetSetting(settingNotifyPublicBaseURL)
	if err != nil || !ok {
		return ""
	}
	return trimTrailingSlash(v)
}

// digestInterval returns the digest interval, falling back to the default when invalid or unset.
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
		return snap, fmt.Errorf("the event snapshot for delivery %d is empty", dl.ID)
	}
	if err := json.Unmarshal(dl.Snapshot, &snap); err != nil {
		return snap, fmt.Errorf("failed to parse the event snapshot for delivery %d: %w", dl.ID, err)
	}
	if snap.Kind == "" {
		// The event kind follows the event row; the copy in the snapshot may have been written
		// by an older version.
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

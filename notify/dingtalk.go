package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"
)

// dingTalkChannel implements DingTalk custom bots.
//
// Platform constraints drive these choices:
//   - Each bot is limited to 20 messages/minute. Excess messages may be silently
//     discarded even with HTTP 200, so enforce limits locally in DefaultRatePerMin.
//   - Security modes are signing, custom keywords, or an IP allowlist. Signing is
//     the only mode independent of message content, so it is the supported mode
//     (a plain webhook with none of these modes enabled is also supported).
//   - Success and failure both use HTTP 200; inspect errcode in the body or failed
//     deliveries would be recorded as successful.
type dingTalkChannel struct{}

func (dingTalkChannel) Kind() string { return KindDingTalk }

func (dingTalkChannel) DefaultRatePerMin() int { return 20 }

// DingTalk Webhook URLs contain access_token and are credentials; mask the whole URL.
func (dingTalkChannel) SecretKeys() []string { return []string{"webhook", "secret"} }

// The destination is the DingTalk Webhook URL. Changing it requires explicitly
// supplying the signing key for the new destination.
func (dingTalkChannel) DestinationKeys() []string { return []string{"webhook"} }

func (dingTalkChannel) Validate(cfg map[string]any) error {
	hook := cfgString(cfg, "webhook")
	if hook == "" {
		return errors.New("Missing Webhook URL")
	}
	if err := validateHTTPURL(hook); err != nil {
		return fmt.Errorf("Invalid Webhook URL: %w", err)
	}
	return nil
}

// Send uses an ActionCard with a button for a single item with a detail link;
// otherwise it sends markdown.
func (c dingTalkChannel) Send(ctx context.Context, cfg map[string]any, m Message) (int, error) {
	hook := cfgString(cfg, "webhook")
	if err := c.Validate(cfg); err != nil {
		return 0, Permanent(err)
	}
	endpoint, err := dingTalkSignedURL(hook, cfgString(cfg, "secret"), time.Now())
	if err != nil {
		return 0, Permanent(err)
	}

	title := markdownTitle(m)
	// DingTalk documents no explicit markdown byte limit, but cap the body to guard
	// against unexpectedly large evidence fields.
	text, kept := markdownBody(m, 20000)

	var payload any
	if !m.Batch && len(m.Items) == 1 && m.Items[0].DetailURL != "" {
		payload = map[string]any{
			"msgtype": "actionCard",
			"actionCard": map[string]any{
				"title":          title,
				"text":           text,
				"btnOrientation": "0",
				"singleTitle":    "View details",
				"singleURL":      m.Items[0].DetailURL,
			},
		}
	} else {
		payload = map[string]any{
			"msgtype":  "markdown",
			"markdown": map[string]any{"title": title, "text": text},
		}
	}

	raw, err := doJSON(ctx, "POST", endpoint, nil, payload)
	if err != nil {
		return 0, err
	}
	// DingTalk reports business errors inside HTTP 200 responses.
	var res struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return 0, fmt.Errorf("Failed to parse DingTalk response: %w (%s)", err, snippet(raw))
	}
	if res.ErrCode != 0 {
		// 301000 means signature validation failed; 310000 means a keyword mismatch.
		// Both are configuration errors that retries cannot resolve.
		return 0, Permanent(fmt.Errorf("DingTalk returned error %d: %s", res.ErrCode, res.ErrMsg))
	}
	return kept, nil
}

// dingTalkSignedURL appends timestamp and sign according to the official algorithm.
//
// Sign timestamp + "\n" + secret using HMAC-SHA256 with secret as the key, then
// base64-encode and URL-encode the result. The timestamp is in milliseconds.
// An empty secret returns the URL unchanged for bots without signing enabled.
func dingTalkSignedURL(hook, secret string, now time.Time) (string, error) {
	if secret == "" {
		return hook, nil
	}
	ts := strconv.FormatInt(now.UnixMilli(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "\n" + secret))
	sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	u, err := url.Parse(hook)
	if err != nil {
		// Do not propagate err: url.Parse includes the full URL, including access_token.
		return "", fmt.Errorf("Failed to parse Webhook URL: %s", redactRequestTarget(hook))
	}
	q := u.Query()
	q.Set("timestamp", ts)
	q.Set("sign", sign)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// validateHTTPURL checks the URL, supported scheme, and literal IP destinations.
//
// Two details matter:
//  1. Errors must redact credentials. url.Parse returns *url.Error containing the
//     complete original URL, which can include a DingTalk access_token, WeCom key,
//     Telegram bot token, or Feishu hook id. Returning it directly once leaked
//     credentials through test API 400 responses, delivery last_error values,
//     server logs, and the delivery-history API.
//  2. Check literal IPs immediately; hostnames are checked by blockInternalDial at
//     connection time, which also covers DNS rebinding. Checking literals here
//     gives feedback when saving configuration rather than after first delivery.
//
// Restricting schemes is defensive: file:// and gopher:// could produce unexpected
// http.Client behavior; there is no reason to expose those schemes.
func validateHTTPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("Cannot parse URL (%s)", redactRequestTarget(raw))
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("Only http/https is supported; received %q", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("Missing hostname")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && isBlockedDialIP(ip) && !allowLocalTargets() {
		return fmt.Errorf("Delivery to local/link-local address %s is blocked (set %s=1 if delivery to a local service is required)", ip, AllowLocalTargetsEnv)
	}
	return nil
}

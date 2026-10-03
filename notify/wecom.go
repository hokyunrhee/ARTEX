package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// weComMarkdownLimit is the hard byte limit for WeCom markdown content. It is the
// strictest of the six channels and the main reason for TruncateBytes.
const weComMarkdownLimit = 4096

// weComChannel implements WeCom group bots.
//
// Platform details:
//   - Authentication uses only the URL key; no signing. The Webhook URL is the credential.
//   - Markdown content is limited to 4096 bytes. Oversized messages are rejected,
//     not truncated. Three-byte CJK characters allow only about a thousand
//     characters, so truncate on the client.
//   - The client must also enforce the 20-message/minute limit.
type weComChannel struct{}

func (weComChannel) Kind() string { return KindWeCom }

func (weComChannel) DefaultRatePerMin() int { return 20 }

// WeCom uses only the key inside its Webhook URL and supports no signing;
// mask the entire URL, with no other secret fields.
func (weComChannel) SecretKeys() []string { return []string{"webhook"} }

// The sole Webhook field is both destination and credential, so changing it
// cannot leave separate old credentials attached to a new destination.
func (weComChannel) DestinationKeys() []string { return []string{"webhook"} }

func (weComChannel) Validate(cfg map[string]any) error {
	hook := cfgString(cfg, "webhook")
	if hook == "" {
		return errors.New("Missing Webhook URL")
	}
	if err := validateHTTPURL(hook); err != nil {
		return fmt.Errorf("Invalid Webhook URL: %w", err)
	}
	return nil
}

func (c weComChannel) Send(ctx context.Context, cfg map[string]any, m Message) (int, error) {
	if err := c.Validate(cfg); err != nil {
		return 0, Permanent(err)
	}
	// A digest of 50 one-line entries plus prefixes can easily exceed 4096 bytes.
	// Truncate here rather than rely on rejection, preserving at least the first items.
	content, kept := markdownBody(m, weComMarkdownLimit)
	payload := map[string]any{
		"msgtype":  "markdown",
		"markdown": map[string]any{"content": content},
	}
	raw, err := doJSON(ctx, "POST", cfgString(cfg, "webhook"), nil, payload)
	if err != nil {
		return 0, err
	}
	var res struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return 0, fmt.Errorf("Failed to parse WeCom response: %w (%s)", err, snippet(raw))
	}
	if res.ErrCode != 0 {
		// 45009 is the rolling-window rate limit and allows retry after backoff. It
		// indicates an overly aggressive client rate_per_min. Retries are a fallback;
		// the actual remedy is lowering that channel's configured rate.
		if res.ErrCode == 45009 {
			return 0, fmt.Errorf("WeCom rate limit %d: %s", res.ErrCode, res.ErrMsg)
		}
		// 93000 means an invalid Webhook key: a permanent failure retries cannot fix.
		return 0, Permanent(fmt.Errorf("WeCom returned error %d: %s", res.ErrCode, res.ErrMsg))
	}
	return kept, nil
}

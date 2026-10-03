package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// weComMarkdownLimit is the hard cap on the WeCom group bot's markdown content
// (bytes, not characters). This is the tightest limit of all six channels and
// the main reason TruncateBytes exists.
const weComMarkdownLimit = 4096

// weComChannel implements the WeCom group bot.
//
// Platform traits:
//   - The only one authenticated by a key on the URL, with no signing support —
//     so the webhook URL itself is the entire credential.
//   - markdown content caps at 4096 **bytes**, and over-long content gets the
//     whole thing rejected (not truncated). At 3 bytes per CJK character, that
//     means the body has room for only a bit over a thousand characters and must
//     be truncated client-side.
//   - Rate-limited to 20 messages/minute, likewise backstopped by client-side
//     rate limiting.
type weComChannel struct{}

func (weComChannel) Kind() string { return KindWeCom }

func (weComChannel) DefaultRatePerMin() int { return 20 }

// WeCom has only one credential field, the webhook (the key on the URL), and it
// doesn't support signing — the whole URL is the entire credential, and no other
// field needs masking.
func (weComChannel) SecretKeys() []string { return []string{"webhook"} }

// WeCom has only the one webhook field, which is both the destination and the
// credential, so there's no such thing as "credentials left over after an address
// change".
func (weComChannel) DestinationKeys() []string { return []string{"webhook"} }

func (weComChannel) Validate(cfg map[string]any) error {
	hook := cfgString(cfg, "webhook")
	if hook == "" {
		return errors.New("missing Webhook URL")
	}
	if err := validateHTTPURL(hook); err != nil {
		return fmt.Errorf("invalid Webhook URL: %w", err)
	}
	return nil
}

func (c weComChannel) Send(ctx context.Context, cfg map[string]any, m Message) (int, error) {
	if err := c.Validate(cfg); err != nil {
		return 0, Permanent(err)
	}
	// A digest batch can be long (50 items × one line each + prefix) and easily
	// exceed 4096 bytes. Truncation is done here rather than relying on a platform
	// error: a rejection means the whole batch is lost, while truncation delivers
	// at least the first several items.
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
		return 0, fmt.Errorf("failed to parse WeCom response: %w (%s)", err, snippet(raw))
	}
	if res.ErrCode != 0 {
		// 45009 is "API call over the limit" — the platform's rate-limit window
		// rolls, so retrying after a backoff is effective, hence it's explicitly
		// classified as retryable. Reaching here means the client's rate_per_min is
		// configured too aggressively; a retry is only a backstop, and the real fix
		// is to lower that channel's rate limit.
		if res.ErrCode == 45009 {
			return 0, fmt.Errorf("WeCom rate limited %d: %s", res.ErrCode, res.ErrMsg)
		}
		// 93000 is an invalid webhook key — a permanent failure that a retry won't
		// heal.
		return 0, Permanent(fmt.Errorf("WeCom returned error %d: %s", res.ErrCode, res.ErrMsg))
	}
	return kept, nil
}

package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// telegramTextLimit is the sendMessage text limit in characters.
const telegramTextLimit = 4096

// telegramChannel implements the Telegram Bot API.
//
// Platform details:
//   - Authentication is in /bot<token>/sendMessage; no signing is required.
//   - Use HTML instead of MarkdownV2, whose 18 reserved characters must all be
//     escaped or the whole message fails. HTML text requires only & < > escaping.
//   - Business errors can also appear inside HTTP 200 responses; inspect ok.
type telegramChannel struct{}

func (telegramChannel) Kind() string { return KindTelegram }

// Telegram permits roughly 1 message/second in private chats and 20/minute in
// groups. Use the conservative rate.
func (telegramChannel) DefaultRatePerMin() int { return 20 }

// Bot Token is a credential; chat_id identifies the recipient and cannot send
// messages without the token, so it is not treated as secret.
func (telegramChannel) SecretKeys() []string { return []string{"bot_token"} }

// base_url selects the API endpoint receiving the token, such as a custom proxy.
// Changing it requires an explicit token choice.
func (telegramChannel) DestinationKeys() []string { return []string{"base_url"} }

func (telegramChannel) Validate(cfg map[string]any) error {
	if cfgString(cfg, "bot_token") == "" {
		return errors.New("Missing Bot Token")
	}
	if cfgString(cfg, "chat_id") == "" {
		return errors.New("Missing Chat ID")
	}
	if base := cfgString(cfg, "base_url"); base != "" {
		if err := validateHTTPURL(base); err != nil {
			return fmt.Errorf("Invalid API URL: %w", err)
		}
	}
	return nil
}

func (c telegramChannel) Send(ctx context.Context, cfg map[string]any, m Message) (int, error) {
	if err := c.Validate(cfg); err != nil {
		return 0, Permanent(err)
	}
	endpoint, err := telegramEndpoint(cfg)
	if err != nil {
		return 0, Permanent(err)
	}
	text, kept := telegramHTML(m)
	payload := map[string]any{
		"chat_id":                  cfgString(cfg, "chat_id"),
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": false,
	}
	raw, err := doJSON(ctx, "POST", endpoint, nil, payload)
	if err != nil {
		return 0, err
	}
	var res struct {
		OK          bool   `json:"ok"`
		ErrorCode   int    `json:"error_code"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return 0, fmt.Errorf("Failed to parse Telegram response: %w (%s)", err, snippet(raw))
	}
	if res.OK {
		return kept, nil
	}
	// 429 is a rate limit and can be retried with backoff. Other failures (400 invalid
	// parameters, 401 token, 403 blocked bot, 404 missing chat) require configuration changes.
	if res.ErrorCode == 429 {
		return 0, fmt.Errorf("Telegram rate limit: %s", res.Description)
	}
	return 0, Permanent(fmt.Errorf("Telegram returned error %d: %s", res.ErrorCode, res.Description))
}

// telegramEndpoint constructs sendMessage. Empty base_url uses the official API;
// an explicit value supports a self-hosted Bot API proxy for restricted networks.
func telegramEndpoint(cfg map[string]any) (string, error) {
	base := cfgString(cfg, "base_url")
	if base == "" {
		base = "https://api.telegram.org"
	}
	base = strings.TrimSuffix(base, "/")
	token := cfgString(cfg, "bot_token")
	raw := base + "/bot" + token + "/sendMessage"
	u, err := url.Parse(raw)
	if err != nil {
		// Do not propagate err or echo addr: the URL includes the Bot Token.
		return "", fmt.Errorf("Failed to construct API URL (API URL: %s)", redactRequestTarget(base))
	}
	return u.String(), nil
}

// telegramHTML returns HTML text and the number of included items; see Channel.Send.
func telegramHTML(m Message) (string, int) {
	var b strings.Builder
	b.WriteString("<b>" + telegramEscape(markdownTitle(m)) + "</b>\n")
	if m.Batch {
		// Telegram limits characters, so pack with runeSize.
		footer := ""
		if m.HomeURL != "" {
			footer = fmt.Sprintf("\n\n<a href=\"%s\">View all in ARTEX</a>", telegramEscapeAttr(m.HomeURL))
		}
		kept := packItemCount(m.Items, telegramTextLimit, telegramReservedRunes, footer, runeSize, func(it Item, idx int) string {
			return telegramBatchLine(it, idx+1)
		})
		items := m.Items[:kept]
		b.Reset()
		b.WriteString("<b>" + telegramEscape(telegramBatchTitle(m, items, len(m.Items))) + "</b>")
		for i, it := range items {
			b.WriteString("\n" + telegramEscape(telegramBatchLine(it, i+1)))
		}
		b.WriteString(footer)
		return TruncateHTML(b.String(), telegramTextLimit), kept
	}
	if len(m.Items) == 0 {
		return b.String(), 0
	}
	it := m.Items[0]
	if it.IsStatusChange() {
		b.WriteString(fmt.Sprintf("\n<b>Status change</b>: %s -> %s",
			telegramEscape(StatusLabel(it.FromStatus)), telegramEscape(StatusLabel(it.ToStatus))))
	}
	if it.VulnClass != "" && it.VulnClass != it.Title() {
		b.WriteString("\n<b>Type</b>: " + telegramEscape(it.VulnClass))
	}
	if a := assetLine(it.Assets, maxAssetsShown); a != "" {
		b.WriteString("\n<b>Assets</b>: " + telegramEscape(a))
	}
	if s := OneLine(it.Summary, maxSummaryRunes); s != "" {
		b.WriteString("\n<b>Summary</b>: " + telegramEscape(s))
	}
	if it.DetailURL != "" {
		b.WriteString(fmt.Sprintf("\n\n<a href=\"%s\">View details</a>", telegramEscapeAttr(it.DetailURL)))
	}
	return TruncateHTML(b.String(), telegramTextLimit), 1
}

// telegramReservedRunes reserves characters for the title and possible truncation notice.
const telegramReservedRunes = 160

// telegramBatchLine renders an unescaped digest entry; the caller escapes it.
func telegramBatchLine(it Item, idx int) string {
	if a := assetLine(it.Assets, maxAssetsShown); a != "" {
		return fmt.Sprintf("%d. %s · %s - %s", idx, SeverityLabel(it.Severity), it.Title(), a)
	}
	return fmt.Sprintf("%d. %s · %s", idx, SeverityLabel(it.Severity), it.Title())
}

// telegramBatchTitle uses the number of items actually included, not the total
// batch size, so the heading does not overstate the visible content.
func telegramBatchTitle(m Message, items []Item, total int) string {
	title := fmt.Sprintf("Findings digest - %d total", total)
	if extra := total - len(items); extra > 0 {
		title += fmt.Sprintf(" (showing the first %d; the remaining %d will follow in the next message)", len(items), extra)
	}
	if m.WindowMinutes > 0 {
		title = fmt.Sprintf("Last %d minutes - %s", m.WindowMinutes, title)
	}
	return title
}

// telegramEscape escapes HTML text using the three entities Telegram recognizes.
// Double-escape existing entities such as &amp; intentionally: display the original
// characters instead of allowing injected HTML.
func telegramEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// telegramEscapeAttr also escapes quotes, preventing a URL from closing href
// early and turning subsequent text into an injection point.
func telegramEscapeAttr(s string) string {
	s = telegramEscape(s)
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}

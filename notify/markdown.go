package notify

import (
	"fmt"
	"strings"
)

// This file is the message rendering shared by the "Markdown-family" channels
// (DingTalk, WeCom). Feishu uses card JSON, Telegram uses HTML, and email uses
// HTML, each rendered in its own adapter.

// maxAssetsShown is the maximum number of assets listed in a message. A finding
// may anchor dozens of assets; listing them all would swamp the message with no
// informational value — nobody reads the domains past the third in an IM.
const maxAssetsShown = 3

// maxSummaryRunes is how many characters the summary is compressed to. An IM
// message is a "prompt to go look at the details", not the report itself; the
// full content lives in the platform.
const maxSummaryRunes = 120

// markdownReservedBytes is reserved for the message header (digest line +
// severity distribution + a possible truncation note) and footer (platform
// link). When packing whole items, this amount is deducted from the budget so
// the header and footer are never truncated — once they are, the reader can't
// even tell "which batch this is, and how many items aren't shown".
const markdownReservedBytes = 320

// markdownEscape escapes markdown metacharacters.
//
// Why it's required: finding titles, summaries, types, and asset display names
// all come from **untrusted sources** — titles and summaries come from model
// output (the model reads the target's responses), and an asset's url is the
// full scanned URL (including a target-controlled query string). Without
// escaping, a finding whose title is
//
//	login SQL injection\n[Urgent: click here to verify your account](http://attacker.tld)
//
// would render as a **clickable external link** in a security engineer's
// DingTalk/Feishu; and `![](http://attacker.tld/beacon)` would be fetched by the
// client at render time, effectively reporting "this finding has been seen" and
// leaking the reader's IP. Even benign content, with injected bold or blockquote
// markup, can push the serious finding below the fold.
//
// The escape set covers the title/link/emphasis/list/blockquote/strikethrough
// characters that change structure or produce clickable elements. `\` must be
// processed first, or it would re-escape the backslashes added afterward.
func markdownEscape(s string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		"`", "\\`",
		"*", `\*`,
		"_", `\_`,
		"[", `\[`,
		"]", `\]`,
		"(", `\(`,
		")", `\)`,
		"!", `\!`,
		"#", `\#`,
		">", `\>`,
		"|", `\|`,
		"~", `\~`,
	)
	return replacer.Replace(s)
}

// markdownText collapses untrusted text to a single line and escapes it, for use
// in a markdown body. Single-lining is the other half of escaping: a newline on
// its own can forge a new list item or blockquote, and escape characters don't
// stop it.
func markdownText(s string, maxRunes int) string {
	return markdownEscape(OneLine(s, maxRunes))
}

// markdownTitle returns the message title (the IM platform's title bar / card
// title); its content is the **unescaped original text**.
//
// Escaping is deliberately not done here: this title is shared across four
// render contexts — markdown body, Telegram HTML, a Feishu card's plain_text,
// and the generic webhook's JSON plus the email subject. Each context has
// different escape rules (markdown escaping stuffed into HTML leaves visible
// backslashes, stuffed into JSON it pollutes the data), so escaping must be done
// by each output end, see writeItem / feishuItemLines / telegramEscape. Markdown
// escaping was once added in the shared function, and the result was visible
// backslashes like `\(1\)` in Telegram messages.
func markdownTitle(m Message) string {
	if m.Batch {
		return fmt.Sprintf("Findings digest · %d total", len(m.Items))
	}
	if len(m.Items) == 0 {
		return "Finding notification"
	}
	it := m.Items[0]
	return fmt.Sprintf("[%s] %s", SeverityLabel(it.Severity), OneLine(it.Title(), 0))
}

// markdownBody renders the message body and returns the body plus the **number
// of items actually written**.
//
// The returned kept is the number of items this delivery genuinely sent, and the
// caller marks only the first kept as delivered — items kept out by the channel
// length cap must wait for the next batch rather than being marked successful
// along with the rest. This is exactly the source of "silent loss": the message
// was truncated, but the delivery record shows everything delivered, and there
// is nowhere to see that the back half was never sent.
//
// maxBytes<=0 means no limit.
func markdownBody(m Message, maxBytes int) (string, int) {
	if !m.Batch {
		if len(m.Items) == 0 {
			return "", 0
		}
		var b strings.Builder
		writeItem(&b, m.Items[0], "", true)
		// A single message is sent even when over-long (backstopped by the final
		// truncation): partial information about one finding beats sending none.
		return TruncateBytes(b.String(), maxBytes), 1
	}

	footer := ""
	if m.HomeURL != "" {
		footer = fmt.Sprintf("\n[View all in the platform](%s)\n", m.HomeURL)
	}
	kept := packItemCount(m.Items, maxBytes, markdownReservedBytes, footer, byteSize, func(it Item, idx int) string {
		var b strings.Builder
		writeItem(&b, it, fmt.Sprintf("%d. ", idx+1), false)
		return b.String()
	})

	items := m.Items[:kept]
	var b strings.Builder
	b.WriteString(markdownBatchIntro(m, items, len(m.Items)))
	for i, it := range items {
		writeItem(&b, it, fmt.Sprintf("%d. ", i+1), false)
	}
	b.WriteString(footer)
	return TruncateBytes(b.String(), maxBytes), kept
}

// markdownBatchIntro renders the start of a digest message: the time window,
// item count, and severity distribution. With these, whoever receives the digest
// can judge whether the batch needs immediate attention without clicking into
// the platform.
//
// items are the ones **actually packed in**; total is how many the batch should
// have. When they differ, it must state outright "how many more are in the next
// message" — otherwise the reader assumes the number in the header is the whole
// batch, and the items that were never sent don't exist anywhere in the UI.
func markdownBatchIntro(m Message, items []Item, total int) string {
	var b strings.Builder
	if m.WindowMinutes > 0 {
		fmt.Fprintf(&b, "**%d new findings in the last %d minutes**", total, m.WindowMinutes)
	} else {
		fmt.Fprintf(&b, "**%d new findings**", total)
	}
	if extra := total - len(items); extra > 0 {
		fmt.Fprintf(&b, " (showing the first %d here; the remaining %d continue in the next message)", len(items), extra)
	}
	// Give the distribution by severity so the reader can see at a glance whether
	// there's anything critical. Only the items **actually included in this
	// message** are counted, so that "🔴 Critical 3" matches the items that can be
	// counted below.
	counts := map[string]int{}
	for _, it := range items {
		counts[it.Severity]++
	}
	var parts []string
	for _, sev := range []string{"critical", "high", "medium", "low"} {
		if n := counts[sev]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", SeverityLabel(sev), n))
		}
	}
	if len(parts) > 0 {
		b.WriteString("\n" + strings.Join(parts, " · "))
	}
	b.WriteString("\n\n")
	return b.String()
}

// writeItem renders a single finding item.
//
// prefix is used for the numbering in a digest list; when single=true it renders
// the full version (with summary and link back), while a digest list renders
// only a one-line summary — otherwise 50 digest items become a long document.
//
// All externally sourced content (title/type/assets/summary) goes through
// markdownText: single-lining + escaping. The link back is built from the
// admin-configured public_base_url, which is not untrusted content and must stay
// a clickable link, so it is output as-is.
func writeItem(b *strings.Builder, it Item, prefix string, single bool) {
	line := fmt.Sprintf("%s**%s · %s**", prefix, SeverityLabel(it.Severity), markdownText(it.Title(), 0))
	if !single {
		// Digest mode: shown on a single line, with assets and a compressed summary
		// following.
		var extras []string
		if a := assetLine(it.Assets, maxAssetsShown); a != "" {
			extras = append(extras, markdownText(a, 0))
		}
		if it.Summary != "" {
			extras = append(extras, markdownText(it.Summary, 60))
		}
		if len(extras) > 0 {
			line += " — " + strings.Join(extras, " · ")
		}
		b.WriteString(line + "\n")
		return
	}
	b.WriteString(line + "\n")
	if it.IsStatusChange() {
		fmt.Fprintf(b, "**Status change**: %s → %s\n",
			markdownText(StatusLabel(it.FromStatus), 0), markdownText(StatusLabel(it.ToStatus), 0))
	}
	if it.VulnClass != "" && it.VulnClass != it.Title() {
		fmt.Fprintf(b, "**Type**: %s\n", markdownText(it.VulnClass, 0))
	}
	if a := assetLine(it.Assets, maxAssetsShown); a != "" {
		fmt.Fprintf(b, "**Assets**: %s\n", markdownText(a, 0))
	}
	if it.Summary != "" {
		if s := markdownText(it.Summary, maxSummaryRunes); s != "" {
			fmt.Fprintf(b, "**Summary**: %s\n", s)
		}
	}
	if it.DetailURL != "" {
		fmt.Fprintf(b, "[View details](%s)\n", it.DetailURL)
	}
}

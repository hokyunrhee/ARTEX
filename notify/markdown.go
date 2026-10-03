package notify

import (
	"fmt"
	"strings"
)

// Shared message rendering for markdown channels (DingTalk and WeCom). Feishu uses
// card JSON, while Telegram and email use HTML in their respective adapters.

// maxAssetsShown limits displayed assets. A finding can anchor dozens of assets;
// listing all would overwhelm the message, with little value after the first few.
const maxAssetsShown = 3

// maxSummaryRunes bounds summaries. IM messages direct readers to details rather
// than replacing the full report stored in ARTEX.
const maxSummaryRunes = 120

// markdownReservedBytes leaves room for digest headings, severity counts, possible
// truncation notices, and the platform link. Deduct this when packing whole items
// so readers can always identify the batch and see how many items remain.
const markdownReservedBytes = 320

// markdownEscape escapes characters that affect markdown structure.
//
// Titles, summaries, types, and asset names are untrusted: models consume target
// responses, and discovered URLs may contain attacker-controlled queries. A title
// such as "Login SQL injection\n[Urgent: verify your account](http://attacker.tld)"
// could otherwise become a clickable link in DingTalk or Feishu. An injected
// ![](http://attacker.tld/beacon) could reveal that the message was read and expose
// the reader's IP. Even accidental formatting can push serious findings below a fold.
//
// Escape heading, link, emphasis, list, quote, and strikethrough characters that
// change structure or create links. Process backslashes first so later inserted
// escape backslashes are not escaped again.
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

// markdownText flattens untrusted text to one line and escapes it. Newlines alone
// can forge list items or blockquotes, which character escaping does not prevent.
func markdownText(s string, maxRunes int) string {
	return markdownEscape(OneLine(s, maxRunes))
}

// markdownTitle returns the unescaped title for channel headers/cards.
//
// Consumers include markdown, Telegram HTML, Feishu plain_text, Webhook JSON, and
// email subjects. Each must escape for its own output context; markdown escapes
// in HTML become visible backslashes, and in JSON corrupt the data. See writeItem,
// feishuItemLines, and telegramEscape. Escaping here once produced visible \(1\)
// sequences in Telegram messages.
func markdownTitle(m Message) string {
	if m.Batch {
		return fmt.Sprintf("Findings digest - %d total", len(m.Items))
	}
	if len(m.Items) == 0 {
		return "Finding notification"
	}
	it := m.Items[0]
	return fmt.Sprintf("[%s] %s", SeverityLabel(it.Severity), OneLine(it.Title(), 0))
}

// markdownBody returns rendered text and the number of items actually included.
//
// Only the first kept items may be marked delivered. Items excluded by a channel
// length limit remain for the next batch; marking them successful would silently
// lose findings while the history incorrectly claimed complete delivery.
//
// maxBytes<=0 disables the limit.
func markdownBody(m Message, maxBytes int) (string, int) {
	if !m.Batch {
		if len(m.Items) == 0 {
			return "", 0
		}
		var b strings.Builder
		writeItem(&b, m.Items[0], "", true)
		// Send a single oversized item with final truncation: partial information is
		// better than delivering nothing.
		return TruncateBytes(b.String(), maxBytes), 1
	}

	footer := ""
	if m.HomeURL != "" {
		footer = fmt.Sprintf("\n[View all in ARTEX](%s)\n", m.HomeURL)
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

// markdownBatchIntro renders the window, count, and severity distribution so
// readers can judge urgency without opening ARTEX.
//
// items are the entries actually included; total is the full batch count. If they
// differ, explicitly state how many will follow in the next message so readers
// do not mistake the visible count for the entire batch.
func markdownBatchIntro(m Message, items []Item, total int) string {
	var b strings.Builder
	if m.WindowMinutes > 0 {
		fmt.Fprintf(&b, "**In the last %d minutes: %d new findings**", m.WindowMinutes, total)
	} else {
		fmt.Fprintf(&b, "**New findings: %d**", total)
	}
	if extra := total - len(items); extra > 0 {
		fmt.Fprintf(&b, " (showing the first %d; the remaining %d will follow in the next message)", len(items), extra)
	}
	// Count severities only among included items, so a label such as Critical 3
	// matches the entries a reader can count in this message.
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

// writeItem renders one finding. prefix numbers digest entries; single=true
// includes the summary and detail link. Digest entries stay on one line so a
// 50-item digest does not become a lengthy document.
//
// External title/type/asset/summary text passes through markdownText for flattening
// and escaping. Detail links derive from administrator-configured public_base_url
// and must remain clickable, so write them unchanged.
func writeItem(b *strings.Builder, it Item, prefix string, single bool) {
	line := fmt.Sprintf("%s**%s · %s**", prefix, SeverityLabel(it.Severity), markdownText(it.Title(), 0))
	if !single {
		// Digest mode uses one line per finding with compressed assets and summary.
		var extras []string
		if a := assetLine(it.Assets, maxAssetsShown); a != "" {
			extras = append(extras, markdownText(a, 0))
		}
		if it.Summary != "" {
			extras = append(extras, markdownText(it.Summary, 60))
		}
		if len(extras) > 0 {
			line += " - " + strings.Join(extras, " · ")
		}
		b.WriteString(line + "\n")
		return
	}
	b.WriteString(line + "\n")
	if it.IsStatusChange() {
		fmt.Fprintf(b, "**Status change**: %s -> %s\n",
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

package notify

import (
	"strings"
	"unicode/utf8"
)

const ellipsis = "…"

// TruncateBytes limits s to max bytes without splitting UTF-8 characters.
//
// WeCom markdown has a hard 4096-byte limit, not a character limit. A CJK character
// uses three bytes; slicing arbitrarily can split it, producing invalid UTF-8
// that the platform rejects or renders as replacement boxes. Backtrack from the
// budget boundary to a rune start using utf8.RuneStart to detect continuation bytes.
//
// max<=0 disables the limit. Append an ellipsis unless the budget cannot hold it.
func TruncateBytes(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	budget := max - len(ellipsis)
	suffix := ellipsis
	if budget < 0 {
		// If max cannot hold an ellipsis, truncate without it rather than exceed the limit.
		budget = max
		suffix = ""
	}
	cut := budget
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + suffix
}

// OneLine collapses whitespace and then truncates by character count. IM titles
// and tables cannot safely contain newlines from summaries. max<=0 disables truncation.
func OneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	return TruncateRunes(s, max)
}

// TruncateRunes limits s to max characters, not bytes, with an ellipsis on truncation.
// max<=0 disables the limit.
//
// WeCom limits bytes, while Telegram limits characters. Using bytes for Telegram
// would needlessly reduce a 4096-character CJK message to about 1365 characters.
// Keep both functions and choose the one matching the channel's units.
func TruncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 1 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + ellipsis
}

// TruncateHTML limits an HTML fragment by character count without partial tags.
//
// Simple truncation might leave <a href="htt, causing rejection or swallowing the
// remaining body as an attribute. After truncating, backtrack before an unmatched <.
//
// Do not balance tags by adding closers: Telegram handles unclosed tags, while
// implementing balancing would need attribute quoting, comments, and self-closing
// tag parsing with disproportionate complexity.
func TruncateHTML(s string, max int) string {
	if max <= 0 || len([]rune(s)) <= max {
		return s
	}
	cut := TruncateRunes(s, max)
	// If the tail contains an unmatched <, discard the incomplete tag starting there.
	if lt := strings.LastIndex(cut, "<"); lt >= 0 && !strings.Contains(cut[lt:], ">") {
		cut = cut[:lt]
	}
	// Also remove incomplete HTML entities, such as &amp; truncated to &amp. An
	// entity-aware parser may reject the entire message otherwise; oversized digests
	// are common and should not cause complete notification loss.
	if amp := strings.LastIndex(cut, "&"); amp >= 0 && !strings.Contains(cut[amp:], ";") {
		cut = cut[:amp]
	}
	return cut
}

// packItemCount finds how many complete items fit in the budget.
//
// Rendering a full batch and truncating loses later entries while their delivery
// rows can still claim success. Packing whole items leaves excluded entries for
// the next batch; kept is exactly the number delivered in this message.
//
// maxSize<=0 disables the limit; reserve leaves room for headers and footers. size
// measures channel units (bytes for WeCom/DingTalk, characters for Telegram);
// incorrect units silently shorten CJK messages. render returns each entry's
// actual text because lengths vary and cannot be estimated reliably.
//
// Return at least one when items exist. An oversized single item must still be
// sent with the caller's final truncation rather than block the batch forever.
func packItemCount(items []Item, maxSize, reserve int, footer string, size func(string) int, render func(Item, int) string) int {
	if maxSize <= 0 {
		return len(items)
	}
	budget := maxSize - reserve - size(footer)
	if budget < 0 {
		budget = 0
	}
	used := 0
	for i, it := range items {
		used += size(render(it, i))
		if used > budget && i > 0 {
			return i
		}
	}
	return len(items)
}

// byteSize and runeSize name the two packItemCount measurement units so callers
// need not infer them from anonymous func(s string) int closures.
func byteSize(s string) int { return len(s) }
func runeSize(s string) int { return utf8.RuneCountInString(s) }

// assetLine displays assets on one line, omitting entries beyond limit and showing
// the total. A finding can anchor dozens of assets that would overwhelm the message.
func assetLine(assets []string, limit int) string {
	if len(assets) == 0 {
		return ""
	}
	if limit <= 0 || len(assets) <= limit {
		return strings.Join(assets, ", ")
	}
	return strings.Join(assets[:limit], ", ") + " (" + itoa(len(assets)) + " assets total)"
}

// itoa is a short strconv.Itoa equivalent for display concatenation, avoiding
// repeated strconv imports.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
